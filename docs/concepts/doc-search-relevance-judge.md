---
type: Concept
title: Doc Search Relevance Judge
description: TypeSafe/Jev relevance judge for doc_search results — filters out-of-scope knowledge docs before get_top body inlining.
tags:
  - typesafe
  - knowledge
  - doc_search
  - architecture
timestamp: 2026-10-04T12:33:27Z
---
# Doc Search Relevance Judge

## Overview

When a decision backend is connected, every `doc_search` call is filtered by a relevance judge that asks whether each returned knowledge document is in scope for the query. Out-of-scope docs are hidden before `get_top` body inlining, so the caller only sees and reads documents the judge considers relevant. The judge is **fail-open**: on any error it falls back to showing all results.

The judge shares its lenient core and confidence floor with the [discovery relevance judge](concepts/discovery-typesafe-judge.md) through `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go:57`).

## Activation

There is no separate config flag. `Agent.docSearchJudge()` (`internal/agent/doc_search_typesafe.go:127`) returns the `DocSearchJudge` callback only when `discoveryJudgeClient()` yields a keyed `Decider`. A nil config, a non-decision factory result, or a keyless client all mean "no judge" and every doc_search result is shown as before.

**Since 2026-10-04, doc_search is independent of discovery.** Previously both judges called the same `discoveryJudgeClient()` and shared one client, so setting one model moved both. Now doc_search resolves through `resolveDecider(slotDocSearch)` and reads its own `doc_search.judge_model` config key (default `typesafe/jev-latest`). Setting `discovery.judge_model` no longer silently changes the doc_search judge.

## Injection seam

`DocSearchJudge` is a function type (`internal/agent/doc_tools.go:27`):

```go
type DocSearchJudge func(query string, docs []*knowledge.Doc) ([]*knowledge.Doc, error)
```

`newDocToolsWithJudge(workDir, judge)` (`internal/agent/doc_tools.go:39`) wires the judge into `DocSearchTool`. The context subagent builds its doc tools this way (`internal/agent/subagent.go:417`):

```go
newDocToolsWithJudge(wd, t.mainAgent.docSearchJudge())
```

Tests and the plain builder use `newDocTools(workDir)` which passes a nil judge (no filtering).

## State shape

`buildDocSearchJudgeState(query, docs)` (`internal/agent/doc_search_typesafe.go:39`) assembles:

- `request` — the doc_search query string.
- `candidates` — one `{id, name, type?, tags?, summary?}` per doc:
  - `id` = doc.Path
  - `name` = doc.Title
  - `type` = doc.Type (omitted when empty)
  - `tags` = doc.Tags (omitted when empty)
  - `summary` = description + body, capped at 1000 chars (`docSearchJudgeSummaryCap`, line 13)

There is no transcript tail — doc_search operates on a keyword query, not a conversation turn.

## Confidence floor and rubric

The floor is `relevanceJudgeMinConfidenceDefault` = **0.5** (`internal/agent/relevance_typesafe.go:20`), resolved by `resolveRelevanceJudgeMinConfidence()` (`internal/agent/relevance_typesafe.go:27`). This is intentionally decoupled from the high-stakes permission floor (`autoJudgeMinConfidenceDefault` = 0.85).

`docSearchJudgeInstructions` (`internal/agent/doc_search_typesafe.go:71`) asks whether each candidate document is "in the same scope as the search request". Answer yes when the document is even slightly relevant — it touches the same subject, component, decision, workflow, or concept as the request. Answer no only when it is of a different scope — an unrelated subject that merely happens to share a word with the request. This is the same lenient rubric used by the discovery judge.

## Filter-before-get_top

In `DocSearchTool.Execute` (`internal/agent/doc_tools.go:110`), filtering runs **before** `get_top` body inlining:

1. `store.Search(...)` returns the full page of results.
2. If a judge is wired, `judge(query, results)` returns the in-scope subset.
3. Filtered count is noted in the header: `"%d out-of-scope result(s) omitted by the relevance judge"`.
4. `get_top` body inlining runs on the *kept* results, so inlined bodies are always the top in-scope docs.

When all results are filtered out, a specific message is returned: `"Found N matching document(s), but none are in scope for this query (relevance judge omitted M)."`.

## Fail-open matrix

| Condition | Behaviour |
|---|---|
| Judge is nil (no decision backend connected) | Show all results — no filtering |
| Transport or decode error from `DecideCtx` | Show all results (judge returns `(docs, nil)` internally) |
| Candidate answer missing or not `type:"noul"` | That doc is kept (fail-open per candidate) |
| Real below-threshold noul (Noul < 0.5) | Vetoes only that doc |

The judge can only ever **hide** results — it never makes doc_search return fewer results because of a failure.

## Side usage

`judgeRelevanceQuestions` records the decide-call tokens as side usage via `RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, deciderLabel(client))`. The label is provider-qualified (e.g. `typesafe/jev-latest`), so the usage ledger attributes spend to the backend that actually answered.

## Debug lines

Emitted by `judgeRelevanceQuestions` under kind `KNOWLEDGE` with tag `doc_search_typesafe`:

- `doc_search_typesafe kept=<n> vetoed=<n> min=<threshold> model=<model>` — summary line after the judge call. The `model=` field is the provider-qualified label from `deciderLabel(client)`.
- `doc_search_typesafe id=<id> noul=<score> verdict=keep|veto` — per-candidate verdict.

## Scope boundary

Filtering applies **only** to `doc_search`. The `doc_get` tool reads a single doc by path — it is an explicit caller choice and is never filtered by the judge.

## See also

- [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md) — the discovery judge that shares the same lenient core and confidence floor. Since 2026-10-04 it resolves its own `discovery.judge_model`, so the two judges no longer share one client.
