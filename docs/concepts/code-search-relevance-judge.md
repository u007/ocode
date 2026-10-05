---
type: Concept
title: Code-Search Relevance Judge
description: TypeSafe/Jev relevance judge for grep/rgrep/glob — per-file judging against a required intent, carried on the tool-execution context, fail-open with disclosed footers. As-built record (v1 landed 2026-09-28); `list` deferred.
tags:
  - typesafe
  - code-search
  - relevance-judge
  - tools
  - architecture
timestamp: 2026-10-05T00:45:48Z
---
# Code-Search Relevance Judge

**Status:** implemented — v1 (`grep`, `rgrep`, `glob`) landed 2026-09-28; every code anchor below was verified against the tree on that date. The linked design spec's status line predates implementation — this page is the as-built record.

## Overview

The three code-search tools — `grep`, `rgrep` and `glob` — return per-file result sets straight into the transcript, and a repo-wide keyword match routinely returns dozens of files that have nothing to do with what the caller was looking for. When the TypeSafe provider is connected, each matching **file** is now scored by a relevance judge (Jev, `typesafe/jev-latest`) against the caller's stated `intent` before anything is rendered: out-of-scope files are removed **together with all of their matching lines**. The judge runs between collection and formatting, so one judge call covers every `output_mode` value of the tool that made it.

The judge shares its lenient core with the [doc search relevance judge](concepts/doc-search-relevance-judge.md) — the 0.5 confidence floor, per-candidate fail-open, and one `noul` (yes-probability) question per candidate — through `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go:57`). The shared `noul` mechanics (decision API, floor rationale, lenient rubric) are documented once in [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md) and are deliberately not restated here.

Its contract is **fail-open**: a judge can only ever hide a result. An error, a timeout, or a missing answer keeps results — byte-identical to an unfiltered run.

## What is filtered

- `grep`, `rgrep` and `glob` each build a structured `[]tool.SearchResult` — one entry per matching file with `Path`, `Summary` (a bounded sample of matched lines, line numbers preserved) and `Count` (`SearchResult`, `internal/tool/search_judge.go:11`) — run the judge on the whole set, then format only the kept files through their existing `output_mode` switch (`writeSearchResultBlock`, `internal/tool/search_judge_apply.go:107`).
- Judge call sites: `grep` at `internal/tool/search.go:648` (`runSearchJudge(ctx, "grep", …)`), `glob` at `internal/tool/search.go:313`, `rgrep` at `internal/tool/rgrep.go:265` (after `groupResults` at `internal/tool/rgrep.go:363` groups rg's JSON records per file).
- Judging is per **file**, not per matching line: one out-of-scope file takes every one of its matching lines with it.
- In `grep` the judge runs **before** `truncateOutput` (`internal/tool/search.go:682`), so the output cap applies to the kept set; the disclosure footer is appended after truncation so a cut can never hide that results were filtered.
- `glob` results carry only a `Path` (`Count` 0 — a plain listing); `grep`/`rgrep` carry counts and sampled match lines.

## When the judge is live

There is **no config key**. The judge is active exactly when `(*Agent).searchResultJudge` (`internal/agent/search_typesafe.go:149`) resolves a keyed `*TypesafeClient` through `(*Agent).discoveryJudgeClient` (`internal/agent/discovery_typesafe.go:41`) — the same shared connected check the discovery and doc_search judges use (same factory, `typesafe/jev-latest` model, caching). A nil config, a non-TypeSafe factory result, or a keyless client all mean "no judge".

With no judge, `runSearchJudge` returns every result untouched (`internal/tool/search_judge_apply.go:79`) and appends no footer: output is **byte-identical to before** this feature existed. That is the common case and it stays that way.

## The `intent` argument

`grep`, `rgrep` and `glob` gained a new schema argument `intent` — one sentence stating what the caller is looking for and why — listed in `required`: glob at `internal/tool/search.go:212`, grep at `internal/tool/search.go:465`, rgrep at `internal/tool/rgrep.go:136` (field `grepParams.Intent`, `internal/tool/search.go:422`).

- **`required` is a teaching device, not enforcement.** An empty `intent` skips the judge entirely and logs `intent-missing` (`internal/agent/search_typesafe.go:156`); it never returns an error demanding the argument, which would burn a turn on a formality.
- The requirement is also reinforced in the prompt directive that already names the search tools (`internal/agent/prompt.go:52`), pinned by `TestDocPromptDirectiveRequiresSearchIntent` (`internal/agent/search_wiring_test.go:272`).
- Model laziness is measurable: the count of `intent-missing` debug lines is the signal for whether `required` is honoured in practice.
- **Empty intent and replayed pre-change sessions degrade to unfiltered — correct, not broken.** A recorded tool call from before this feature has no `intent`, so a replay takes the skip path.

## The seam: the judge rides the execution context

The judge is carried on the tool-execution **context**, not on the tool struct: `tool.WithSearchResultJudge` (`internal/tool/search_judge.go:72`) attaches it, `tool.SearchJudgeFromContext` (`internal/tool/search_judge.go:82`) reads it back. It is attached **per call** inside `executeToolCallWithContext` (`internal/agent/agent.go:5396`), gated to the three tool names (`internal/agent/agent.go:5395`) and attached alongside the snapshot store, work dir and full-output flag (`internal/agent/agent.go:5134`). Any other tool's dispatch never touches the judge client factory.

The rationale is load-bearing: sub-agents and the transient advisor are handed the **parent's** tool objects (`internal/agent/ask.go:166`, `internal/agent/subagent.go:1126`, `internal/agent/advisor_tool.go:176` all call `GetTools()`), so a judge field on the tool struct would be overwritten by each child — and the advisor is shut down after one call, leaving the parent judging against a dead agent. It would also be a data race, since the search tools are `Parallel() == true`. With the context seam each agent attaches its own judge for its own call: no shared mutable state, no clobbering.

Two regression tests pin this (`internal/agent/search_wiring_test.go`):

- `TestSearchJudgeIsPerAgentOnSharedTools` (`internal/agent/search_wiring_test.go:94`) — parent, sub-agent, advisor and disconnected agent each judge with their own judge; a child coming and going leaves the parent's judging intact.
- `TestSearchToolsOnlyInvokedThroughAgentDispatch` (`internal/agent/search_wiring_test.go:143`) — no production code outside the agent dispatch chain calls `Execute`/`ExecuteCtx` on the three tools, so no caller can silently bypass filtering.

## Fail-open contract

**A judge can only ever hide a result — never create, never drop on failure.** Every failure mode keeps results, and the failure itself is disclosed rather than swallowed (the `SearchResultJudge` signature's `error` return exists precisely so "the judge errored and I kept everything" stays distinguishable from "the judge ran and everything was in scope", `internal/tool/search_judge.go:50`).

- The code-search judge runs on a short budget: `searchJudgeTimeout` = **4s** (`internal/agent/search_typesafe.go:27`), applied through the context-aware `DecideCtx` (`internal/agent/typesafe.go:88`). Fail-open still *waits*, so without this a provider that accepts the connection and then stalls would add the full default timeout to every search in the session.
- The permission, auto-continue and doc_search judges keep the 30s `typesafeRequestTimeout` (`internal/agent/typesafe.go:23`) — they are higher-stakes and lower-frequency: `Decide` (`internal/agent/typesafe.go:76`) remains a `DecideCtx` wrapper with that fallback, used by `internal/agent/permission_typesafe.go:252` and `internal/agent/autocontinue_typesafe.go:126`; the doc_search relevance path passes a background context and inherits the same 30s fallback.

| Condition | Behaviour |
|---|---|
| TypeSafe not connected (judge nil) | Unfiltered, no footer, byte-identical to before |
| `intent` empty | Judge not called, unfiltered, `intent-missing` logged |
| HTTP transport / decode error | All results kept, `[relevance judge unavailable — …]` footer |
| Judge call exceeds 4s | Context deadline, all results kept, same error footer |
| Missing or non-`noul` answer for a candidate | That candidate kept (per-candidate fail-open) |
| Real below-floor `noul` (< 0.5) | That candidate vetoed |
| Result set over the 40 cap | First 40 judged, remainder kept unjudged and disclosed |
| All results vetoed | All-vetoed message with the actionable tail |

## Candidate cap and pre-judge order

`SearchJudgeMaxCandidates` = **40** (`internal/tool/search_judge_apply.go:14`). Results past the cap are **kept unjudged, never dropped** — the cap is a cost control and must not decide relevance — and the footer discloses how many were not judged (`runSearchJudge`, `internal/tool/search_judge_apply.go:78`).

The pre-judge order is each tool's existing output order, so the 40 judged candidates are the first 40 in that order. For `glob` that order is **mtime descending** (`sort.Slice`, `internal/tool/search.go:291`), so on a large glob the 40 judged files are the 40 most recently touched. Because unjudged results are kept, a skewed sample costs coverage, not correctness — but on a large `glob` the cap, not relevance, decides how much judging happens, and the footer's unjudged count makes that visible.

## Rubric and state

State is built by `buildSearchJudgeState` (`internal/agent/search_typesafe.go:48`): the request (the `intent`), the tool name, a query map limited to the whitelist `searchJudgeQueryFields` = `pattern`/`path`/`include` (`internal/agent/search_typesafe.go:33`, non-secret scalars only — the decide call is a network round trip), and one candidate per file keyed by its path. Per-file summaries are capped at **400 chars** (`searchJudgeSummaryCap`, `internal/agent/search_typesafe.go:16`) — much smaller than the doc_search judge's 1000, because the question is a yes/no call — and preserve match line numbers so the judge sees *where* the file matched, not merely that it did.

Each candidate gets one `noul` question in a single decide call, delegated to the shared `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go:57`), which supplies the lenient floor `relevanceJudgeMinConfidenceDefault` = **0.5** (`internal/agent/relevance_typesafe.go:20`), per-candidate fail-open, side-usage accounting and debug lines. The floor is deliberately decoupled from the high-stakes permission floor (`permissions.auto.min_confidence` = 0.85) — a relevance veto only hides a retrieval result, it never grants a tool call.

The rubric (`searchJudgeInstructions`, `internal/agent/search_typesafe.go:85`) is lenient in the same product sense as the doc rubric — answer yes when the file is even slightly relevant; answer no only for a genuinely different area of the codebase — plus two guards the doc rubric does not need:

- **Do not veto a file merely for being a test, fixture, or generated artifact** — those are frequently the target of the search.
- **Do not veto on a substring match unrelated to the request** — the `\bTest\b`-style collision, or incidental boilerplate.

## Output footers (exact strings)

A footer is appended only when there is something to say. Judge ran with **zero vetoes**, or **no judge wired** → no footer, bare list — the common case stays byte-identical, and the *absence* of filtering is never announced (the debug lines below are the observability path).

| State | Footer |
|---|---|
| Judge ran, vetoed > 0 | `[relevance judge: N of M result(s) omitted as out of scope for this intent]` — N = vetoed, M = judged (`Footer()`, `internal/tool/search_judge_apply.go:58`, format at `:65`) |
| …and the cap was hit | appended: `; K beyond the judge cap were not judged` (`internal/tool/search_judge_apply.go:67`) |
| Judge errored or timed out | `[relevance judge unavailable — results are unfiltered: <err>]` (`internal/tool/search_judge_apply.go:60`) |

When the judge vetoes **everything**, the list is replaced by an actionable message rather than a bare count, so the model narrows instead of re-running in a loop (`searchJudgeAllVetoedMessage`, `internal/tool/search_judge_apply.go:123`):

```
Found N matching file(s), but none are in scope for this intent (relevance judge omitted all N).
Narrow "pattern"/"include", or re-run with an intent that matches what these files contain.
```

The tail differs per tool: `grep` and `rgrep` say `Narrow "pattern"/"include"` (`internal/tool/search.go:655`, `internal/tool/rgrep.go:272`); `glob` says `Narrow "pattern"/"path"` (`internal/tool/search.go:318`).

## Debug lines

Emitted under kind `TOOL`, tag `search_typesafe`:

- `search_typesafe tool=<grep|rgrep|glob> intent-missing (intent empty; judge skipped, N result(s) unfiltered)` — `internal/agent/search_typesafe.go:156`.
- `search_typesafe tool=<tool> judge=<model> failed (fail-open, all N result(s) kept): <err>` — `internal/agent/search_typesafe.go:163`.
- `search_typesafe id=<path> noul=<score> verdict=keep|veto` per candidate, and `search_typesafe kept=<n> vetoed=<n> min=<0.50> model=<model>` per call — from `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go:57`) with this path's debug kind and tag (`internal/agent/search_typesafe.go:121`).

## Scope decisions

- **`read` is never judged.** Reading a named path is an explicit caller choice — the same carve-out the doc_search judge applies to `doc_get` (see [Doc Search Relevance Judge](concepts/doc-search-relevance-judge.md), "Scope boundary"). A judge cannot improve an explicit choice.
- **`ast`, `lsp`, `ast_grep` are out of v1.** They return precise symbol hits, so the noise problem is materially smaller, and a wrong veto there hides a definition or call site the model specifically asked for. Stated honestly: the model *can* route around a judged `grep` by reaching for `ast`, so the "a second unjudged path is a bypass" principle ([Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md)) does not hold in its strong form here — that principle governs attach gates, while this is a context-economy filter where the judge can only ever hide a result, never grant one. If debug logs show the model preferring `ast` to evade filtering, adding those tools is the v2 move.
- **Custom and MCP search tools are out.** Nothing in ocode can enforce a `required` argument on a third-party tool schema.
- **`list` is deferred.** It is `os.ReadDir` on a single directory (`ListTool`, `internal/tool/search.go:685`) with no cap: judging sibling filenames is the weakest relevance signal of the code-search tools and the highest false-positive risk (a veto hides a name the model needed) for the least context saved. Tracked in `TODO.md` under "Code-search relevance judge (2026-09-28)" (`TODO.md:2718`); rationale in spec §8. Adding it later is a `SearchResult` producer plus one call site.

## See also

- [Doc Search Relevance Judge](concepts/doc-search-relevance-judge.md) — the sibling judge for `doc_search`, sharing the floor and `judgeRelevanceQuestions`.
- [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md) — the shared `noul` mechanics, floor rationale and lenient rubric.
- [Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md) — the fail-open philosophy and the bypass principle referenced in the scope decisions.
- Design spec: `superpowers/specs/2026-09-28-code-search-relevance-judge-design.md` — the contract source (signatures, failure matrix, test list).
