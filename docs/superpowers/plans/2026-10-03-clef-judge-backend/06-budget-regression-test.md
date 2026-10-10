# Part 06 — Budget regression test

Turn the spec's measurement table into assertions. **No behaviour change.**

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently.
- Fail-open is a hard invariant.
- No silent backend fallback.
- Missing credential must be visible, not a silent nil.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance.
- Never hold a lock across a network call.
- `gofmt` clean, `go vet` clean. Tests named `Test<Subject>_<Behaviour>`.
- Never `git stash`, `reset`, `checkout --`, or `clean`.

## Why this part exists

The spec's table of judge payload sizes came from a throwaway harness that was deleted, so **nothing in the tree could reproduce those numbers**. This part makes them reproducible and, more importantly, makes them a tripwire: if a future change grows a judge's payload past what any backend accepts, this fails loudly instead of the problem surfacing as a mysteriously unhelpful judge.

It is a regression test, not a benchmark. Its assertions are about ceilings, not about timing.

## Files

- Create `internal/agent/clef_budget_test.go`

## What to build

Subtests that build each judge request at its documented worst case using the **real** state builders — the same functions production uses, not re-implementations — at the **real** production caps:

- `discovery`: the `SelectCap` worth of candidates (`internal/discovery/index.go`), with `discoveryJudgeSummaryCap`-length summaries and a `discoveryJudgeTailN`-length message tail keyed by doc id of the `skill:<name>` form.
- code search: the `searchJudgeMaxCandidates` worth of results (`internal/tool/search_judge_apply.go`), keyed by long nested file paths.
- `doc_search`: the same count, keyed by long concept-page paths, with bodies well past the summary cap.
- permission: a deliberately oversized `write` payload, asserting that the budget guard **refuses** it. This is the finding, so assert the guard fires rather than asserting the state fits.
- content guard and network guard: a maximum chunk and a long URL, asserting they stay well inside budget.

A shared helper marshals `{state, questions}` and reports question count, byte size, estimated tokens and percentage of budget, then asserts both ceilings.

## Steps

1. Write the test using the real builders and the real caps, importing the discovery, knowledge and tool packages as the builders' signatures require.
2. Run it. Every subtest should pass, with the sizes logged.
3. **Copy the logged numbers back into the spec's table** so document and test agree, then re-derive the spec's code anchors — this test touches `internal/agent` and `internal/discovery`, so line numbers cited by the spec may have moved.

## Checks

- `go build ./...` and `go vet ./...` clean.
- `gofmt -l` prints nothing.
- `go test ./internal/agent/ -run TestJudgePayloadBudgets -v -count=1` passes and prints the table.
- Every judge's question count is at or under the clef ceiling; every relevance judge is inside the byte budget.
- The spec's table and the test's logged output match.

## Review focus

Confirm the caps used are the production constants by name rather than by a copied literal — a test that hardcodes `40` instead of referring to the constant silently stops protecting the real ceiling when the constant changes.

Confirm the permission subtest asserts the guard fires. If it ever starts asserting that the state *fits*, the finding has been papered over rather than fixed.

## Done when

The spec's table is reproducible from the tree, and a payload that outgrows any backend fails the build.