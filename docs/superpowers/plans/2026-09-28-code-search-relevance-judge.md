---
type: Plan
title: Code-Search Relevance Judge Implementation Plan
description: Approved implementation plan for extending the TypeSafe/Jev relevance judge to grep/rgrep/glob behind a required intent argument (tasks 1–8).
tags:
  - plan
  - go
  - typesafe-judge
  - code-search
  - permissions
timestamp: 2026-09-28T06:56:59Z
---
# Code-Search Relevance Judge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing TypeSafe/Jev relevance judge from `doc_search` to the `grep`, `rgrep` and `glob` tools, so out-of-scope search results never reach the transcript, anchored on a new required `intent` argument.

**Architecture:** The judge rides the tool-execution context. `internal/agent` resolves a per-agent judge closure and attaches it to the `toolCtx` that `executeToolCallWithContext` already builds for the snapshot store, work dir and full-output flag; `internal/tool` reads it back and filters a structured per-file result set between collection and formatting. Because the judge travels on the context rather than living on the tool struct, a sub-agent or transient advisor agent automatically judges with its own agent and cannot clobber its parent's.

**Tech Stack:** Go (module `github.com/u007/ocode`), `internal/tool` + `internal/agent`, the existing `TypesafeClient` `noul` decision API, the project's Go test suite.

**Spec:** `superpowers/specs/2026-09-28-code-search-relevance-judge-design.md` — read it before starting. It is the contract source: it carries the exact type and function signatures, the judge state shape, the rubric, the footer strings and the full failure matrix. This plan never restates a signature; look it up in the spec. Where this plan and the spec disagree, the spec wins.

## Global Constraints

- **No new configuration.** Activation is provider-connection only, matching `doc_search`: the judge is live exactly when `discoveryJudgeClient()` yields a keyed `*TypesafeClient`. Do not add a config key, a `permissions.*` key, or an `.env.example` entry. If you find yourself wanting a toggle, that is a design change — stop and raise it.
- **Fail-open is the load-bearing invariant.** A judge failure must never make a search return fewer results than it does today. The judge can only ever hide a result, and every hidden result is either named in a footer or, on failure, not hidden at all.
- **Keep the shared lenient floor.** Use `relevanceJudgeMinConfidenceDefault` (0.5) through the existing `judgeRelevanceQuestions` helper. Do not introduce a new floor and do not reuse `permissions.auto.min_confidence` (0.85) — that is the high-stakes permission floor and overloading it here is a documented anti-pattern.
- **The candidate cap never decides relevance.** `searchJudgeMaxCandidates = 40`; results past the cap are kept unjudged, never dropped, and the footer discloses the unjudged count.
- **Timeout discipline.** `searchJudgeTimeout = 4s`, applied through a new `DecideCtx`. The existing `Decide` keeps `typesafeRequestTimeout` (30s) because its three other callers are high-stakes.
- **`intent` is taught, not enforced.** It appears in `required` for the three schemas, but an empty `intent` skips the judge and is logged as `intent-missing`. Never return an error demanding it — that burns a turn on a formality.
- **Do not touch** `read`, `ast`, `lsp`, `ast_grep`, or any custom or MCP tool. Their exclusion is a decision, not an oversight.
- **Error handling:** no empty catch blocks. Every caught error is either handled and logged, or carries an inline `// intentionally not logged: <reason>` comment. In particular the fail-open path must log — a silently swallowed judge error is indistinguishable from a clean result.
- **Working-tree hygiene:** the tree carries roughly 50 unrelated modified files from concurrent work. Stage only the files you touch.

## Review Focus

Five input classes the spec implies but that are easy to leave untested. Each is pinned by a test in the task that owns the code:

1. **Provider accepts the connection then stalls.** Expect: the search completes in about 4s, not 30s, unfiltered, with the judge-unavailable footer. Pinned in Task 3.
2. **TypeSafe unconfigured, keyless, or a non-TypeSafe factory result.** Expect: output byte-identical to today, no footer, no outbound request. Pinned in Tasks 3 and 4.
3. **Model omits `intent`** (lazy model, or a replayed pre-change session whose recorded call has no such argument). Expect: no `Decide` call at all, unfiltered, and an `intent-missing` debug line. Pinned in Task 3.
4. **Repo-wide grep matching hundreds of files.** Expect: exactly 40 judged, every remaining match still present, and the footer naming how many were not judged. Pinned in Task 4.
5. **A correct-looking match that the judge vetoes** — a `_test.go` when the intent never mentioned tests. Expect: the veto is disclosed in the footer rather than silent, so the model can re-run. Pinned in Task 4.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `internal/tool/search_judge.go` | create | the `SearchResult` / `SearchJudgeRequest` / `SearchResultJudge` contract and the context plumbing |
| `internal/tool/search.go` | modify | `GrepTool` and `GlobTool` produce `SearchResult` sets, judge, then format; `intent` added to both schemas |
| `internal/tool/rgrep.go` | modify | `RgrepTool` groups rg's JSON records into `SearchResult` sets, replacing the raw re-decode in `format()` |
| `internal/agent/typesafe.go` | modify | add `DecideCtx`; `Decide` becomes a wrapper |
| `internal/agent/search_typesafe.go` | create | judge state builder, rubric, `(*Agent).searchResultJudge()` |
| `internal/agent/agent.go` | modify | attach the judge to `toolCtx` in `executeToolCallWithContext` |
| `internal/agent/prompt.go` | modify | reinforce the required `intent` beside the existing grep/glob mention |
| `TODO.md` | modify | track the deferred `list` follow-up |
| `CHANGES.md` | modify | user-facing entry |
| `skills/ocode-tools/SKILL.md` | modify | document the seam, the `intent` argument, the footer contract |

---

### Task 1: Tool-side judge contract and context plumbing

**Files:**
- Create: `internal/tool/search_judge.go`
- Test: `internal/tool/search_judge_test.go`

**Interfaces:**
- Produces: `tool.SearchResult{Path, Summary string; Count int}`, `tool.SearchJudgeRequest{Tool, Intent string; Query map[string]string; Results []SearchResult}`, `tool.SearchResultJudge func(SearchJudgeRequest) (kept []SearchResult, vetoed int, err error)`, `tool.WithSearchResultJudge(ctx, judge) context.Context`, `tool.SearchJudgeFromContext(ctx) tool.SearchResultJudge`. Exact field and signature forms are in spec §4 — copy them from there, do not improvise.

**Steps:**
- [ ] Write the failing test: a context round-trip returns the judge that was attached; a nil judge and a nil context both return nil without panicking.
- [ ] Run `go test ./internal/tool/ -run SearchJudge` and confirm it fails on the missing functions.
- [ ] Implement the types and the two context functions, mirroring the shape and the nil-handling comment style of `internal/tool/workdir_ctx.go`.
- [ ] Document the contract on the types, specifically recording that the judge's `err` return exists so a failed judge stays distinguishable from one that legitimately kept everything, and that callers must render all results when `err` is non-nil.
- [ ] Re-run the test and confirm it passes.
- [ ] Commit.

### Task 2: Context-aware TypeSafe decide

**Files:**
- Modify: `internal/agent/typesafe.go`
- Test: `internal/agent/typesafe_ctx_test.go`

**Interfaces:**
- Produces: `(*TypesafeClient).DecideCtx(ctx context.Context, state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error)`.
- Consumes: nothing new. `Decide` keeps its current signature and its 30s budget for the three existing callers (`permission_typesafe.go`, `autocontinue_typesafe.go`, `relevance_typesafe.go`).

**Steps:**
- [ ] Write the failing test: a server that never responds is abandoned when the context deadline fires; an already-cancelled context returns without issuing a request.
- [ ] Run the test and confirm it fails.
- [ ] Add `DecideCtx`, moving the request construction onto the context, and reduce `Decide` to a wrapper that keeps the existing `typesafeRequestTimeout` behaviour.
- [ ] Re-run and confirm it passes, then run the full `go test ./internal/agent/ -run Typesafe` to confirm the three existing judge paths are unaffected.
- [ ] Commit.

### Task 3: Agent-side relevance judge

**Files:**
- Create: `internal/agent/search_typesafe.go`
- Test: `internal/agent/search_typesafe_test.go`
- Modify: `internal/agent/agent.go` — attach the judge in `executeToolCallWithContext`, on the same `toolCtx` that already receives the snapshot store, `tool.WithWorkDir` and the full-output flag.

**Interfaces:**
- Consumes: `tool.SearchResultJudge` and `tool.WithSearchResultJudge` from Task 1; `DecideCtx` from Task 2; the existing `judgeRelevanceQuestions` helper, which already owns the floor, per-candidate fail-open, side-usage accounting and debug lines.
- Produces: `(*Agent).searchResultJudge() tool.SearchResultJudge`, returning nil when TypeSafe is not connected.

**Steps:**
- [ ] Write the failing pure-function tests: the state builder produces the request/tool/query/candidates shape from spec §6; the per-candidate summary is capped; match line numbers survive into the summary; the query map carries only pattern, path and include and never secret material.
- [ ] Write the failing judge tests against a fake client: veto-all, keep-all, a missing or non-`noul` answer for one candidate, and a transport error. Assert the fail-open outcomes from spec §10.
- [ ] Write the failing test for an empty `intent`: no `Decide` request is issued at all, and an `intent-missing` debug line is emitted.
- [ ] Write the failing test for the timeout budget: a stalled server is abandoned at roughly the 4s budget rather than the 30s default.
- [ ] Write the failing wiring test: attaching a nil judge is a no-op and tool dispatch is unaffected.
- [ ] Implement `buildSearchJudgeState`, the per-candidate `noul` question text (including the two code-specific rubric guards — do not veto a file merely for being a test or fixture, and do not veto on a substring match unrelated to the request), `judgeSearchResults` delegating to `judgeRelevanceQuestions`, and `searchResultJudge` gated on `discoveryJudgeClient()`. Apply the 4s budget via `DecideCtx`.
- [ ] Attach the judge to `toolCtx` in `executeToolCallWithContext`.
- [ ] Re-run the focused tests, then `go build ./...` and `go vet ./internal/agent/`.
- [ ] Commit.

### Task 4: `grep` — the first live path

**Files:**
- Modify: `internal/tool/search.go` — `GrepTool`
- Test: `internal/tool/search_judge_grep_test.go`

**Interfaces:**
- Consumes: the Task 1 contract; `SearchJudgeFromContext` to reach the judge, and the tool name `"grep"` for the request.
- Produces: the `intent` property, added to both `properties` and `required`.

**Steps:**
- [ ] FIRST, before changing any code, capture golden output for the judge-absent case: all three `output_mode` values over a fixed fixture tree, plus the truncated-input and no-match messages. These are the refactor guard.
- [ ] Write the failing test: with no judge attached, output is byte-identical to the captured golden output.
- [ ] Write the failing tests: the judge receives one `SearchResult` per matching file with the right count and a bounded summary; a vetoed file's lines are absent from every `output_mode`; the cap boundary at 39, 40 and 41 candidates including the unjudged-count footer; the all-vetoed message with its actionable tail; the judge-error footer; and `intent` present in the schema's required list.
- [ ] Replace the tool's internal per-file result type with `tool.SearchResult`, run the judge between collection and formatting, move the format switch to run on the kept set, and keep `truncateOutput` last. Add the `intent` property and required entry.
- [ ] Re-run every test above, including the golden comparison.
- [ ] Commit.

### Task 5: `glob`

**Files:**
- Modify: `internal/tool/search.go` — `GlobTool`
- Test: `internal/tool/search_judge_glob_test.go`

**Interfaces:**
- Consumes: the Task 1 contract; the tool name `"glob"`.
- Produces: the `intent` property, added to both `properties` and `required`.

**Steps:**
- [ ] Capture judge-absent golden output for the existing capped and uncapped result shapes, including the existing "N files matched, showing first 100" note, before changing code.
- [ ] Write the failing tests: byte-identical output with no judge; the mtime ordering is preserved as the pre-judge order the cap samples from; the cap boundary and its unjudged-count footer; the all-vetoed message; the judge-error footer; `intent` in the schema.
- [ ] Convert the match slice to `[]tool.SearchResult`, keeping the mtime sort, then judge, then format.
- [ ] Re-run all tests and confirm the golden comparison passes.
- [ ] Commit.

### Task 6: `rgrep`

**Files:**
- Modify: `internal/tool/rgrep.go` — `RgrepTool` and its `format` method
- Test: `internal/tool/search_judge_rgrep_test.go`

**Interfaces:**
- Consumes: the Task 1 contract; the tool name `"rgrep"`.
- Produces: the `intent` property, added to both `properties` and `required`.

**Steps:**
- [ ] FIRST capture golden judge-absent output for all three `output_mode` values, including a line longer than the truncation limit, a file with multiple matches (to pin `files_with_matches` dedup and `count`), and a run whose output hits the capture cap (to pin the truncated-tail behaviour).
- [ ] Write the failing golden-comparison test.
- [ ] Write the failing judge tests: the judge receives correctly grouped per-file results with counts and bounded summaries; a vetoed file is absent from every mode; cap boundary and footer; all-vetoed; judge error; `intent` in the schema.
- [ ] Replace the raw-JSON re-decode in `format` with a grouping pass. This is tractable because the rg invocation already passes `--sort path`, so records for one path are contiguous. Preserve exactly: per-line truncation at the existing limit, path ordering, `files_with_matches` deduplication, and the truncated-tail behaviour of the capture buffer. Then share the formatter with the other search tools rather than keeping a third copy.
- [ ] Re-run every test, including the golden comparison.
- [ ] Commit.

### Task 7: Wiring regressions and prompt reinforcement

**Files:**
- Test: `internal/agent/search_wiring_test.go`
- Modify: `internal/agent/prompt.go` — the directive line that already names grep/glob
- Modify: `internal/tool/search_judge_test.go` if a shared assertion belongs there instead

**Steps:**
- [ ] Write the seam regression test: a parent agent, a sub-agent, and a transient advisor agent each have their searches judged by their own agent, and a child coming and going leaves the parent's judging intact. This pins the defect that ruled out a judge field on the tool struct — a child inheriting the parent's tool objects and overwriting the parent's judge.
- [ ] Write the coverage regression test: scan the tree for non-agent code that calls `Execute` or `ExecuteCtx` on `grep`, `rgrep` or `glob` outside the agent dispatch chain, and fail loudly if any is found, since such a caller would silently bypass filtering.
- [ ] Write the failing prompt test asserting the directive line instructs the model to pass `intent` on code-search calls.
- [ ] Update the directive line to reinforce the required `intent`.
- [ ] Re-run the focused tests, then the full `go test ./internal/agent/ ./internal/tool/` and `go build ./...`.
- [ ] Commit.

### Task 8: Documentation and the deferred follow-up

**Files:**
- Create: `docs/concepts/code-search-relevance-judge.md`
- Modify: `skills/ocode-tools/SKILL.md`, `CHANGES.md`, `TODO.md`

**Steps:**
- [ ] Write the concept page through the context sub-agent (it is the sole writer of the `docs/` bundle): the operator-facing description of what is filtered, when the judge is live, the fail-open contract, the candidate cap and its disclosure, the footer strings, and the `list` deferral. Cover the code-search relevance dimensions a reader needs and cross-link `concepts/doc-search-relevance-judge.md`.
- [ ] Update `skills/ocode-tools/SKILL.md` with the context seam, the `intent` argument, and the footer contract, and add the corresponding regression-test line to its test inventory.
- [ ] Add a `CHANGES.md` entry describing the user-visible behaviour.
- [ ] Confirm the `TODO.md` entry for the deferred `list` tool is present and still accurate (it was added during planning on 2026-09-28, before implementation, so the executor should verify rather than re-add it).
- [ ] Re-derive every `file:line` anchor you cite in the new docs against the current source, and carry symbol names alongside the numbers so they survive later edits.
- [ ] Commit.

---

## Execution notes

- Tasks 1 through 3 land the contract and the judge with no tool consuming them yet; Task 4 is the first end-to-end path, and Tasks 5 and 6 apply the same shape to the remaining two tools.
- Every test in this plan is expected to be mutation-verified before its task is considered done: temporarily break the behaviour, confirm the test fails, restore. A test that cannot fail is not evidence.
- Do not stage the unrelated dirty files in the working tree.
