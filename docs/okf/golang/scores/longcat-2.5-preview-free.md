---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: golang
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. longcat-2.5-preview-free has no "/", so the filename is unchanged. -->

# Scorecard — longcat-2.5-preview-free on golang

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 | |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 | states "blocks until at least one case becomes ready"; no random-choice or leak / `select {}` mention, but the blocking concept is present (same bar as prior golang scorecards) |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 | no "close only once", but sender-owns-close + send-on-closed panic + zero/ok=false after drain + range termination all present |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 2 | 0.67 | shared-variable cause and 1.22 per-iteration fix present; missed the pre-1.22 workaround (`v := v` shadow or pass as argument) |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 | |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 | |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 | |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-03 | errors | 2 | 2 | 1 | 0.50 | defines the sentinel correctly; says compare with `errors.Is` "or == for direct comparison, though errors.Is is preferred" — endorses `==` and never gives the reason (`==` fails once the sentinel is wrapped), so the "Is, not ==, survives wrapping" point is not awarded |
| go-errors-04 | errors, interfaces | 3 | 3 | 3 | 1.00 | (type,value) pair, non-nil type slot, return-untyped-nil fix all present |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-01 | generics, interfaces | 2 | 2 | 2 | 1.00 | runtime polymorphism vs type safety without boxing/assertions |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 | example uses `constraints.Ordered` (x/exp; stdlib is `cmp.Ordered` since 1.21) — minor, not a rubric point |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-04 | generics | 1 | 2 | 2 | 1.00 | |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 2 | 0.67 | cooperative observe-Done-and-return and "runtime does not kill or interrupt" present; missed propagation of cancellation to derived child contexts |
| go-context-02 | context | 3 | 2 | 2 | 1.00 | |
| go-context-03 | context | 2 | 2 | 2 | 1.00 | request-scoped vs not-for-params/dependencies present; did not mention an unexported custom key type |
| go-context-04 | context | 1 | 2 | 2 | 1.00 | |
| go-slices-01 | slices-maps | 3 | 3 | 2 | 0.67 | shared-backing-array aliasing and assign-the-return-value present; missed how to get an independent slice (`copy` / `slices.Clone` / full-slice expression) |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 | |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 | |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 | randomized order + `&m[k]` illegal (entries move on growth) present; did not mention sorting keys for stable order |
| go-defer-01 | defer-panic | 3 | 2 | 2 | 1.00 | |
| go-defer-02 | defer-panic | 2 | 2 | 2 | 1.00 | |
| go-defer-03 | defer-panic, goroutine-leaks | 2 | 2 | 2 | 1.00 | |
| go-defer-04 | defer-panic, errors | 2 | 2 | 2 | 1.00 | |
| go-testing-01 | testing | 2 | 2 | 2 | 1.00 | |
| go-testing-02 | testing | 2 | 2 | 2 | 1.00 | "after all non-parallel tests complete" is a loose phrasing of pause-until-parent-returns; accepted |
| go-testing-03 | testing | 1 | 2 | 2 | 1.00 | no LIFO mention for Cleanup |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| concurrency | 0.92 | 5 | ok | omit (strong) |
| sync | 0.92 | 5 | ok | omit (strong) |
| errors | 0.92 | 5 | ok | omit (strong) |
| interfaces | 1.00 | 4 | ok | omit (strong) |
| generics | 1.00 | 4 | ok | omit (strong) |
| context | 0.89 | 4 | ok | omit (strong) |
| slices-maps | 0.90 | 4 | ok | omit (strong) |
| defer-panic | 1.00 | 4 | ok | omit (strong) |
| goroutine-leaks | 0.90 | 4 | ok | omit (strong) |
| testing | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70 / 74 = 94.6%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag clears 0.75, so no
`derived/golang.longcat-2.5-preview-free.SKILL.md` is generated from this
scorecard. Deductions are four missing secondary points: the pre-1.22
loop-variable workaround (go-concurrency-04), sentinel comparison that
tolerates `==` without the wrapping rationale (go-errors-03), cancellation
propagation to child contexts (go-context-01), and the `copy`/`slices.Clone`
detach remedy (go-slices-01). Pattern: explains the mechanism, omits the
remedy/rationale — not a conceptual gap.

## Contamination check

No leak. Checked each answer against its reference `answer:` block with a
5-word-shingle overlap (share of reference 5-grams reused): median 0.01, 17 of
33 answers at 0.00, max 0.17 (go-testing-03). The highest overlaps are stock Go
phrasing, not copied answers: go-testing-03 shares "marks the calling function
as a test helper" (go doc wording for `t.Helper`); go-slices-03 shares
"copies min(len(dst), len(src)) elements"; go-concurrency-01 (0.09) shares "an
unbuffered channel has no capacity" / "blocks only when the buffer is full",
the standard textbook description, with sentence order and the rest of the text
different. The answers also include details the key does not have (the race
detector requires cgo; `errors.New`/`ErrNotFound` examples) and leave out
remedies the key spells out (`v := v`, `slices.Clone`, child-context
propagation). An answerer that saw the key would not show that pattern.
