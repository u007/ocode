---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: golang
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. glm-5.3-flash has no "/", so the filename is unchanged. -->

# Scorecard — glm-5.3-flash on golang

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 | |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 | frames the no-default/no-ready case as a deadlock rather than a leak, but the blocks-forever concept is present |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 | says a closed channel "returns immediately with the zero value" — omits that buffered values drain first; core zero/ok=false concept present |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 3 | 1.00 | |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 | |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 | |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 | |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-03 | errors | 2 | 2 | 2 | 1.00 | |
| go-errors-04 | errors, interfaces | 3 | 3 | 3 | 1.00 | |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-01 | generics, interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-04 | generics | 1 | 2 | 2 | 1.00 | |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 3 | 1.00 | child propagation only implied ("parent cancellation" as a trigger); "ignoring it leaks" stated as "cannot be killed by force / must cooperate" |
| go-context-02 | context | 3 | 2 | 2 | 1.00 | |
| go-context-03 | context | 2 | 2 | 2 | 1.00 | |
| go-context-04 | context | 1 | 2 | 2 | 1.00 | |
| go-slices-01 | slices-maps | 3 | 3 | 2 | 0.67 | correct aliasing mechanism (in-capacity append writes in place) and the assign-the-return-value point; missed the detach point — no mention of `copy`, `slices.Clone`, or a full-slice expression to get an independent slice |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 | |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 | |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 | |
| go-defer-01 | defer-panic | 3 | 2 | 2 | 1.00 | both points present, but the aside claiming `defer log(*p)` "logs the current value at return time" is wrong — `*p` is dereferenced at the defer statement like any other argument |
| go-defer-02 | defer-panic | 2 | 2 | 2 | 1.00 | |
| go-defer-03 | defer-panic, goroutine-leaks | 2 | 2 | 2 | 1.00 | |
| go-defer-04 | defer-panic, errors | 2 | 2 | 2 | 1.00 | |
| go-testing-01 | testing | 2 | 2 | 2 | 1.00 | |
| go-testing-02 | testing | 2 | 2 | 2 | 1.00 | |
| go-testing-03 | testing | 1 | 2 | 2 | 1.00 | LIFO order of Cleanup not mentioned; registers-teardown-at-test-end concept present |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| concurrency | 1.00 | 5 | ok | omit (strong) |
| sync | 1.00 | 5 | ok | omit (strong) |
| errors | 1.00 | 5 | ok | omit (strong) |
| interfaces | 1.00 | 4 | ok | omit (strong) |
| generics | 1.00 | 4 | ok | omit (strong) |
| context | 1.00 | 4 | ok | omit (strong) |
| slices-maps | 0.90 | 4 | ok | omit (above threshold) |
| defer-panic | 1.00 | 4 | ok | omit (strong) |
| goroutine-leaks | 1.00 | 4 | ok | omit (strong) |
| testing | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 73 / 74 = 98.6%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag clears 0.75, so no
`derived/golang.glm-5.3-flash.SKILL.md` is generated from this scorecard.
The only lost point is go-slices-01's missing detach idiom (`copy` /
`slices.Clone`); `slices-maps` (0.90) is the sole tag under 1.00.

Contamination check: the near-ceiling score was sanity-checked per the
closed-book rule. The answers are in the model's own wording, consistently
more verbose than the key, include material the key does not (vector clocks
for `-race`, Go 1.20 interface-satisfies-comparable, `go vet` lostcancel),
and contain one substantive miss plus one factual error in an aside
(go-defer-01's `*p` claim) — not the signature of a copied key.
