---
type: Concept
title: Doc Search Relevance Judge
description: TypeSafe/Jev relevance judge for doc_search results — filters out-of-scope knowledge docs before get_top body inlining.
tags:
  - typesafe
  - knowledge
  - doc_search
  - architecture
timestamp: 2026-09-18T16:27:30Z
---
---
title: Doc Search Relevance Judge
type: Concept
description: TypeSafe/Jev relevance judge for doc_search results — filters out-of-scope knowledge docs before get_top body inlining.
tags:
  - typesafe
  - knowledge
  - doc_search
  - architecture
---

# Doc Search Relevance Judge

## Overview

When the TypeSafe provider is connected, every `doc_search` call is filtered by a relevance judge that asks Jev (the `typesafe/jev-latest` model) whether each returned knowledge document is in scope for the query. Out-of-scope docs are hidden before `get_top` body inlining, so the caller only sees and reads documents the judge considers relevant. The judge is **fail-open**: on any error it falls back to showing all results.

The judge shares its lenient core and confidence floor with the [discovery relevance judge](concepts/discovery-typesafe-judge.md) through `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go:41`).

## Activation

There is no separate config flag. `Agent.docSearchJudge()` (`internal/agent/doc_search_typesafe.go:126`) returns the `DocSearchJudge` callback only when `discoveryJudgeClient()` yields a keyed `TypesafeClient`. A nil config, a non-TypeSafe factory result, or a keyless client all mean "no judge" and every doc_search result is shown as before.

`discoveryJudgeClient()` (`internal/agent/discovery_typesafe.go:40`) is the **shared connected check** for both the discovery and doc_search judges — same factory, model, caching, and resolution.

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

`buildDocSearchJudgeState(query, docs)` (`internal/agent/doc_search_typesafe.go:38`) assembles:

- `request` — the doc_search query string.
- `candidates` — one `{id, name, type?, tags?, summary?}` per doc:
  - `id` = doc.Path
  - `name` = doc.Title
  - `type` = doc.Type (omitted when empty)
  - `tags` = doc.Tags (omitted when empty)
  - `summary` = description + body, capped at 1000 chars (`docSearchJudgeSummaryCap`, line 12)

There is no transcript tail — doc_search operates on a keyword query, not a conversation turn.

## Confidence floor and rubric

The floor is `relevanceJudgeMinConfidenceDefault` = **0.5** (`internal/agent/relevance_typesafe.go:19`), resolved by `resolveRelevanceJudgeMinConfidence()` (`internal/agent/relevance_typesafe.go:26`). This is intentionally decoupled from the high-stakes permission floor (`autoJudgeMinConfidenceDefault` = 0.85).

`docSearchJudgeInstructions` (`internal/agent/doc_search_typesafe.go:70`) asks whether each candidate document is "in the same scope as the search request". Answer yes when the document is even slightly relevant — it touches the same subject, component, decision, workflow, or concept as the request. Answer no only when it is of a different scope — an unrelated subject that merely happens to share a word with the request. This is the same lenient rubric used by the discovery judge.

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
| Judge is nil (TypeSafe not connected) | Show all results — no filtering |
| Transport or decode error from `Decide` | Show all results (judge returns `(docs, nil)` internally) |
| Candidate answer missing or not `type:"noul"` | That doc is kept (fail-open per candidate) |
| Real below-threshold noul (Noul < 0.5) | Vetoes only that doc |

The judge can only ever **hide** results — it never makes doc_search return fewer results because of a failure.

## Side usage

`judgeRelevanceQuestions` records the decide-call tokens as side usage via `RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, "typesafe/"+client.Model)`.

## Debug lines

Emitted by `judgeRelevanceQuestions` under kind `KNOWLEDGE` with tag `doc_search_typesafe`:

- `doc_search_typesafe kept=<n> vetoed=<n> min=<threshold> model=<model>` — summary line after the judge call.
- `doc_search_typesafe id=<id> noul=<score> verdict=keep|veto` — per-candidate verdict.

## Scope boundary

Filtering applies **only** to `doc_search`. The `doc_get` tool reads a single doc by path — it is an explicit caller choice and is never filtered by the judge.

## See also

- [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md) — the discovery judge that shares the same lenient core, floor, and `discoveryJudgeClient` connected check.
