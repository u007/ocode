---
type: Design
title: Code-Search Relevance Judge (v1)
description: 'Approved design spec (awaiting implementation): extend the doc_search TypeSafe/Jev relevance judge to the code-search tools grep/rgrep/glob — per-file judging carried on the execution context, required intent argument, 4s DecideCtx budget, 40-candidate cap, vetoes disclosed in output footers. `list` deferred per user decision 2026-09-28.'
tags:
  - design-spec
  - code-search
  - relevance-judge
  - typesafe
  - tools
  - agent
timestamp: 2026-09-28T06:47:18Z
---
# Code-Search Relevance Judge (v1)

**Status:** approved design, not yet implemented. The `list` deferral (§8) was confirmed by the user on 2026-09-28, so it is a decision rather than an open question; the follow-up is tracked as a TODO, not a design change.
**Extends:** `concepts/doc-search-relevance-judge.md` (the `doc_search` result judge), `concepts/discovery-typesafe-judge.md` (the shared `noul` mechanics).

## 1. Problem

The three in-scope code-search tools — `grep`, `rgrep` and `glob` — return raw result sets straight into the transcript. A repo-wide keyword match routinely returns dozens of files, most of which are not what the caller was actually looking for. The model then pays context for all of them, and its next turn has to re-read the list to work out which matter.

`doc_search` already solves this for the knowledge bundle: a TypeSafe/Jev relevance judge filters the result page before the expensive inlining step. This spec extends the same mechanism to code search.

The judge cannot infer intent on its own. For `doc_search` the `query` string is the sole intent anchor. For code search the pattern is not an intent — `grep "Test"` may be looking for test helpers, a `Test` struct, or a substring collision. So this design adds an explicit, required `intent` input to the tool schema.

## 2. Decisions

Four decisions were taken explicitly, and each constrains the rest of the design.

**D1 — Judging unit: per file.** One `noul` question per result *file*, not per matching line. Per-line would mean hundreds of questions and a state payload an order of magnitude larger, for a modest additional saving. Per-file removes most of the bulk: one out-of-scope file takes all of its matching lines with it.

**D2 — On veto: keep in-scope results, then report.** Vetoed results are removed from the rendered output, and a footer states how many were omitted and why they went. Not a silent drop: a silent drop makes "omitted as irrelevant" indistinguishable from "never existed", and the model cannot correct a wrong veto it cannot see. Not a demote: the point is to stop paying context for them.

**D3 — Tool scope for v1: `grep`, `rgrep`, `glob`.** See §8 for why `list` is deferred.

**D4 — Cost guard: always judge when TypeSafe is connected.** A result-size threshold and a config flag were both considered and declined. The consequence — an extra ~0.8 s and one remote call on every search — is accepted, and §6 explains the timeout budget that makes it safe.

## 3. Seam: the judge rides the execution context

`internal/tool` cannot import `internal/agent` (the agent imports the tool package), and the built-ins are constructed by the *host*, not by the agent — `internal/server/agent_session.go:525` and `internal/tui/model.go:2244` both call `tool.InitBuiltinTools*` and then hand the resulting slice to `agent.NewAgent`.

Two candidate seams were considered.

**Rejected: a field on the tool struct, wired from `NewAgent`.** The tool list is per-session, so this looks safe, and it was the first design. It is wrong. Sub-agents do not get their own tool objects — they are handed the parent's:

- `internal/agent/ask.go:166` — `tools = a.GetTools()`
- `internal/agent/subagent.go:1126` and `:1128` — `t.mainAgent.GetTools()`
- `internal/agent/advisor_tool.go:176` — `t.mainAgent.GetTools()`

Each of those then calls `NewAgent`. A `NewAgent` that wrote a judge onto the tool structs would have a child overwrite the parent's judge with one bound to the child — and the advisor child is transient, shut down immediately after a single call (`advisor_tool.go`). The parent's searches would then judge against a dead agent. It is also a data race, because the search tools are `Parallel() == true`.

**Adopted: the judge is carried on the execution context**, exactly as the session's project root is (`tool.WithWorkDir`, `internal/tool/workdir_ctx.go`). `executeToolCallWithContext` builds a `toolCtx` and already attaches three things to it — the snapshot store, the work dir (`internal/agent/agent.go:4911`, the `tool.WithWorkDir` call), and the full-output flag. The judge is attached alongside them. Each agent attaches its own judge for its own call, so there is no shared mutable state, no clobbering, and no race.

Coverage is complete for model-initiated searches. Every `grep`/`rgrep`/`glob`/`list` invocation in non-test code goes through the dispatch chain in `internal/agent/agent.go:4951-4963` (the tool-dispatch `if/else if` chain that ends in `t.Execute(args)`; `agent.go` has uncommitted changes in the working tree, so re-derive this range by symbol before relying on it); the other `Execute`/`ExecuteCtx` call sites in the package are the tool's own `Execute` → `ExecuteCtx` delegation, or agent-owned tools (`advisor`, `task`, `doc_*`). A regression test pins that invariant, because a future direct-`Execute` caller would silently lose filtering.

## 4. Tool-side contract

New file `internal/tool/search_judge.go`:

```go
// SearchResult is one file-level search hit the judge can score.
type SearchResult struct {
    Path    string // display path, as the tool would render it
    Summary string // bounded sample of what matched (first few lines, with line numbers)
    Count   int    // match count; 0 for a plain listing
}

// SearchJudgeRequest is one search call's judge input.
type SearchJudgeRequest struct {
    Tool    string            // "grep" | "rgrep" | "glob"
    Intent  string            // the caller's required intent argument
    Query   map[string]string // pattern / path / include — non-secret scalars only
    Results []SearchResult
}

// SearchResultJudge filters Results against Intent, returning the subset to
// render, how many were vetoed, and any error. On a non-nil error the caller
// must render every result unfiltered.
type SearchResultJudge func(SearchJudgeRequest) (kept []SearchResult, vetoed int, err error)

// WithSearchResultJudge returns a context carrying judge for search tools.
func WithSearchResultJudge(ctx context.Context, judge SearchResultJudge) context.Context

// SearchJudgeFromContext returns the judge stored by WithSearchResultJudge, or nil.
func SearchJudgeFromContext(ctx context.Context) SearchResultJudge
```

### Why the signature keeps `error`

An earlier draft dropped it, reasoning that every failure is fail-open so an error can only produce a worse outcome than nil. That is wrong about a different thing: it makes *"the judge errored and I returned everything"* indistinguishable from *"the judge ran and everything was in scope."* Both render as a complete list with no footer, and the model — and the logs — cannot tell a judged result from an unjudged one. The error is what lets the tool say so (§7).

### Candidate cap

`searchJudgeMaxCandidates = 40`. Results past the cap are **kept unjudged**, never dropped: the cap is a cost control and must not decide relevance. The pre-judge order is each tool's existing output order, and the footer discloses the cap whenever it is hit (§7).

This order is not neutral, and the spec records it rather than hiding it: `glob` sorts by modification time, so its 40 judged files are the 40 most recently touched, which is a skewed sample of the other 60. Because unjudged results are kept, a skewed sample costs coverage, not correctness — but it does mean the cap, not relevance, decides how much judging happens on a large `glob`. The footer names the unjudged count so this is never invisible.

## 5. Per-tool changes

All three in-scope tools currently build a formatted **string**, so the judge has nothing structured to score. Each tool gains a structured pass producing `[]SearchResult`, the judge runs on it, and the tool's existing formatter then runs on the kept set. One judge call site per tool covers all `output_mode` values.

**`grep`** (`internal/tool/search.go:403`, `:441`) is the easy one: it already builds `fileResult{path, count, lines}` internally and formats through a switch on `output_mode`. The internal type becomes `SearchResult` and the switch moves after the judge.

**`glob`** (`search.go:187`, `:222`) builds `matches []globMatch{path, mtime}` and caps at `globMaxResults = 100`, sorting by mtime. That order is preserved as the pre-judge order.

**`rgrep`** (`internal/tool/rgrep.go:89`, `:140`) is the risky one. It shells out to `rg --json` into a `cappedBuffer`, and `format()` (`rgrep.go:304`) re-decodes the raw JSON inside a switch on `output_mode` using `rgJSONRecord`. The refactor replaces that re-decode with a grouping pass. This is tractable because rg is invoked with `--sort path` (`rgrep.go`, argv construction), so records for one path are contiguous. The grouping pass must preserve exactly: `rgMaxLineLen` line truncation, path ordering, `files_with_matches` deduplication, and the `cappedBuffer` truncated-tail behaviour. A golden-output test pins the judge-absent output byte-for-byte against the current implementation.

**`list` is deferred** — see §8.

## 6. Agent-side judge

New file `internal/agent/search_typesafe.go`, mirroring `doc_search_typesafe.go`.

`buildSearchJudgeState(req)` produces:

```json
{
  "request": "<intent>",
  "tool": "grep",
  "query": { "pattern": "...", "path": "...", "include": "..." },
  "candidates": [ { "id": "path/to/file.go", "path": "...", "count": 12, "summary": "L214:...\nL219:..." } ]
}
```

`searchJudgeSummaryCap = 400` — code lines are long and the question is yes/no, so a much smaller window than `docSearchJudgeSummaryCap` (1000) is right. Match line numbers are preserved in the summary so the judge can see *where* the file matched, not just that it did. Candidate ids are paths, unique within a result set, and each question refers to its candidate by backticked path — the same convention as the doc search judge.

Each candidate gets one `noul` question, and the request goes through the **existing** `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go:41`) so the lenient `relevanceJudgeMinConfidenceDefault` floor (0.5), per-candidate fail-open, side-usage accounting and debug lines are inherited rather than reimplemented. Rubric is lenient in the same product sense as the doc rubric — even slight relevance is kept; only a genuinely different area of the codebase is vetoed — with two guards the doc rubric does not need:

- do not veto a file merely for being a test, fixture, or generated artifact; those are frequently the target
- do not veto on a substring match that is not what the request is about (the `\bTest\b` collision)

Activation is unchanged from `doc_search`: no config flag; `(*Agent) searchResultJudge()` returns nil unless `discoveryJudgeClient()` (`internal/agent/discovery_typesafe.go:40`) yields a keyed `*TypesafeClient`. A nil judge means unfiltered.

### Timeout budget

`typesafeRequestTimeout` is 30 s (`internal/agent/typesafe.go:22`) and is applied as an `http.Client.Timeout` inside `Decide`. Under D4 every search pays a judge round trip, and fail-open still *waits* — a provider that accepts the connection and then stalls would add 30 s to every search in the session. That is the real cost of D4, and it is not acceptable.

The relevance judge therefore gets a short budget: `searchJudgeTimeout = 4s`, roughly 5× the measured Jev median of 872 ms (700–900 ms range; see `superpowers/specs/2026-09-22-laya-local-judge-evaluation.md`). Timing out early costs only an unfiltered result, so a tight budget is nearly free.

`Decide` currently takes no context and builds its client with the fixed timeout, so this requires a context-aware variant rather than a second constant:

```go
func (c *TypesafeClient) DecideCtx(ctx context.Context, state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error)
func (c *TypesafeClient) Decide(state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error) // existing callers, unchanged
```

`Decide` becomes a wrapper over `DecideCtx` with a `context.Background()`-derived timeout, so the three existing callers — `permission_typesafe.go:248`, `autocontinue_typesafe.go:126`, `relevance_typesafe.go:45` — are untouched and keep the 30 s budget appropriate to their stakes. A side benefit is that the relevance judge now also stops when the user interrupts, which the current fixed-timeout form cannot do.

## 7. Rendering and footers

After formatting the kept set, each tool appends a footer only when there is something to say. Three states must be distinguishable:

| State | Footer |
|---|---|
| Judge ran, vetoed > 0 | `[relevance judge: 12 of 40 result(s) omitted as out of scope for this intent]` plus `; 47 beyond the judge cap were not judged` when the cap was hit |
| Judge ran, vetoed == 0 | none appended |
| Judge errored | `[relevance judge unavailable — results are unfiltered: <err>]` |
| No judge wired (TypeSafe not connected) | none appended; identical to today's output |

Vetoing everything is its own case, and it needs an actionable tail rather than just a count, or the model responds by widening the pattern and re-running in a loop:

```
Found 40 matching file(s), but none are in scope for this intent (relevance judge omitted all 40).
Narrow "pattern"/"include", or re-run with an intent that matches what these files contain.
```

Note that the "judge ran, vetoed 0" and "no judge wired" states both render a bare list. That is intentional — the common case stays byte-identical to today — but it means the *absence* of filtering is never announced. Debug logging is the observability path: the tool logs `intent-missing` for an empty intent, and the agent logs `kept=/vetoed=/min=/model=` per judged call.

One ordering consequence, stated because it is load-bearing: in `grep` the judge runs **before** `truncateOutput` (`search.go:609`, the `return truncateOutput(...)` line). Filtering first means the output cap applies to the kept set, so dropping out-of-scope files can change which lines survive the cut. That is the intended effect — the cap bounds what the model pays for — but it does mean the pre-judge order determines what is at risk of being cut, which is another reason not to have the cap decide relevance.

## 8. Scope decisions

**`list` is deferred out of v1.** It is `os.ReadDir` on a single directory (`search.go:641`, `ListTool.ExecuteCtx`, appending sibling names at `:680`) returning them as a `[]string`, with no cap and no `required` in its schema. Judging sibling filenames within one directory is the weakest relevance signal of the code-search tools and the highest false-positive risk — a veto hides a name the model needed — for the least context saved, since the bulk this feature targets comes from `glob`'s 100-result cap and `grep`'s per-line noise. Adding it later is a `SearchResult` producer and one call site; nothing in the design forecloses it.

**`read` is never judged.** Same carve-out as `doc_get`: reading a named path is an explicit caller choice, and the doc-search concept doc records that carve-out as deliberate. A judge cannot improve an explicit choice.

**`ast`, `lsp`, `ast_grep` are out of v1.** They return precise symbol hits, so the noise problem is materially smaller, and a wrong veto there hides a definition or a call site the model specifically asked for. The trade-off is recorded honestly: the model *can* route around a judged `grep` by reaching for `ast`, so "a second unjudged path is a bypass" (`concepts/discovery-mcp-tool-gating.md`) does not hold in its strong form here. That principle governs attach gates; this feature is a context-economy filter, and the judge can only ever hide a result, never grant one. If debug logs show the model preferring `ast` to evade filtering, adding it is the v2 move.

**Custom and MCP search tools are out.** Nothing in ocode can enforce a required argument on a third-party tool schema.

## 9. The `intent` argument

Added to the schema of `grep`, `rgrep`, and `glob`, and listed in `required`, with a description that states the judge depends on it. Also reinforced in the directive line at `internal/agent/prompt.go:51`, the one place that already names grep/glob to the model, so the requirement is stated outside the schema too.

**An empty `intent` skips the judge entirely.** `required` is a teaching device for the model, not an enforcement mechanism: ocode unmarshals tool args into a params struct and ignores missing fields, so a missing `intent` is benign here, and returning an error to demand it would burn a turn on a formality. The risk that a stricter provider rejects a tool call whose arguments omit a required field is not fully eliminated by this design; it is judged low because the widely used providers in this repo do not validate tool arguments against the declared schema before dispatch, and ocode itself does not. Replayed pre-change sessions, whose recorded tool calls have no `intent`, degrade to the unfiltered path — correct, not broken.

Model laziness is a real failure mode for a required-but-unenforced argument, so it is made measurable: a call with an empty intent emits a `intent-missing` debug line, and the count is the signal for whether `required` is being honoured in practice.

## 10. Failure matrix

| Condition | Behaviour |
|---|---|
| TypeSafe not connected | judge nil, unfiltered, byte-identical to today |
| `intent` empty | judge not called, unfiltered, `intent-missing` logged |
| HTTP transport / decode error | `err` returned, unfiltered, error footer rendered |
| Judge call exceeds 4 s | context deadline, unfiltered, error footer rendered |
| Missing or non-`noul` answer for a candidate | that candidate kept (per-candidate fail-open) |
| Real below-floor `noul` | that candidate vetoed |
| Result set over the 40 cap | first 40 judged, remainder kept unjudged and disclosed in the footer |
| All results vetoed | all-filtered message with the actionable tail |

The invariant: **a judge failure can never make a search return fewer results than it does today.** It can only ever hide results, and every hidden result is either named in a footer or, in the fail-open case, simply not hidden.

## 11. Tests

- Pure: `buildSearchJudgeState` shape, summary cap, per-capability query fields, and that the query map carries no secret material.
- Tool-side, the primary regression guard: a nil judge produces output byte-identical to the current implementation for every `output_mode`, for all three tools. This is what proves the refactor changed no formatting.
- Tool-side: the judge receives correctly grouped per-file results for each `output_mode`; vetoed files' lines are absent from every mode; cap boundaries at 39/40/41; the all-vetoed message; the error footer.
- `rgrep` golden output: judge-absent content/count/`files_with_matches` output matches the pre-refactor implementation exactly.
- Agent-side, with a fake `TypesafeClient`: veto-all, keep-all, missing `noul`, transport error, empty intent, and that the 4 s budget actually bounds a stalled call.
- Wiring: a sub-agent's searches are judged by the sub-agent, not the parent, and a parent search is unaffected by a transient advisor child — the regression test for the seam defect in §3.
- Wiring: an integration test pinning that no non-test code path calls the three in-scope search tools outside the agent dispatch chain.
- All mutation-verified per project convention.

## 12. Risks

- **False vetoes are invisible to correctness testing.** The 0.5 lenient floor was tuned on docs and skills, not on code; there is no recorded false-positive data for code relevance. The rubrics in §6 are the mitigation, and the footer is the mitigation for a wrong call. This is the risk most likely to need a follow-up tuning pass after real use.
- **D4 costs latency on every search.** Accepted per §2, bounded per §6.
- **Parallel searches in one message each fire their own judge.** Five `grep` calls in one message are five `Decide` round trips, though they run concurrently so wall-clock impact is roughly one judge latency. Accepted for v1; the candidate cap is the natural future lever.
- **The candidate cap silently reduces judging on large result sets.** Disclosed in the footer (§7).

## 13. Documentation

- This spec.
- `concepts/code-search-relevance-judge.md` — the operator-facing concept page, created at implementation time (concept pages are written once the behaviour exists, not from a design).
- `skills/ocode-tools/SKILL.md` — the new context seam, the `intent` argument, and the footer contract.
- `CHANGES.md` — an entry on implementation.

---

## Amendment (2026-09-28)

An advisor review round after approval revised four points in the design as originally drafted:

1. **Seam** — the judge rides the execution context (`WithSearchResultJudge` on the `toolCtx`) rather than living as a field on the tool struct wired from `NewAgent`; the tool-struct variant was rejected because sub-agents inherit the parent's tool objects and would clobber the parent's judge (§3).
2. **Timeout** — a context-aware `DecideCtx` with a `searchJudgeTimeout = 4s` budget replaces a fixed/second-constant approach, keeping the existing 30 s `Decide` callers untouched (§6).
3. **Error signature** — `SearchResultJudge` keeps its `error` return so an errored judge is distinguishable from a run that kept everything (§4).
4. **Cap honesty** — results past `searchJudgeMaxCandidates = 40` are kept unjudged and disclosed in the footer, never dropped, so the cap cannot decide relevance (§4, §7).
