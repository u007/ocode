---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: golang
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. space-bunny-free has no "/", so the filename is unchanged. -->

# Scorecard — space-bunny-free on golang

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 | |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 | states "blocks until one becomes ready"; did not spell out the leak / `select {}` consequence, but the blocking concept is present |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 | never says "close only once" / close-of-closed panics, but sender-owns-close + send-on-closed panic + drain/ok=false + range termination all present |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 2 | 0.67 | got the shared-variable cause and the 1.22 per-iteration fix; missed the pre-1.22 workaround (`v := v` shadow or pass as argument) |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 | |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 | |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 | |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-03 | errors | 2 | 2 | 2 | 1.00 | |
| go-errors-04 | errors, interfaces | 3 | 3 | 3 | 1.00 | (type,value) pair, non-nil type slot, and the return-untyped-nil fix all present |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-01 | generics, interfaces | 2 | 2 | 2 | 1.00 | names the in/out type-relationship preservation and avoiding assertions |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-04 | generics | 1 | 2 | 2 | 1.00 | |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 2 | 0.67 | got cooperative observe-Done-and-return and "does not forcibly kill"; missed propagation of cancellation to derived child contexts |
| go-context-02 | context | 3 | 2 | 2 | 1.00 | |
| go-context-03 | context | 2 | 2 | 2 | 1.00 | |
| go-context-04 | context | 1 | 2 | 2 | 1.00 | |
| go-slices-01 | slices-maps | 3 | 3 | 2 | 0.67 | correct in-capacity-append aliasing mechanism and use-the-return-value; missed how to get an independent slice (`copy` / `slices.Clone` / full-slice expression) |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 | |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 | |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 | did not mention sorting keys for stable order, nor the rehash/move reason for non-addressability; core concepts (unspecified order, `&m[k]` illegal, workarounds) present |
| go-defer-01 | defer-panic | 3 | 2 | 2 | 1.00 | |
| go-defer-02 | defer-panic | 2 | 2 | 2 | 1.00 | |
| go-defer-03 | defer-panic, goroutine-leaks | 2 | 2 | 2 | 1.00 | |
| go-defer-04 | defer-panic, errors | 2 | 2 | 2 | 1.00 | |
| go-testing-01 | testing | 2 | 2 | 2 | 1.00 | |
| go-testing-02 | testing | 2 | 2 | 2 | 1.00 | |
| go-testing-03 | testing | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| concurrency | 0.92 | 5 | ok | omit (strong) |
| sync | 0.92 | 5 | ok | omit (strong) |
| errors | 1.00 | 5 | ok | omit (strong) |
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
stack_score = Σ(normalized×weight) / Σ(weight) = 71 / 74 = 95.9%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag clears 0.75, so no
`derived/golang.space-bunny-free.SKILL.md` is generated from this scorecard.
The only deductions are three missing secondary points, each on a different
weight-3 question: the pre-1.22 loop-variable workaround (go-concurrency-04),
cancellation propagation to child contexts (go-context-01), and the
`copy`/`slices.Clone` detach remedy (go-slices-01). The pattern is "explains
the mechanism, omits the remedy/workaround", not a conceptual gap.

## Contamination check

Answers are in the model's own wording with different structure from the
reference answers and include details the key does not contain (e.g. the
`-parallel` limit for `t.Parallel`, Go 1.20 interface types satisfying
`comparable`, `delete` on a nil map being a no-op). No answer reads as a
verbatim or near-verbatim copy of the reference.
