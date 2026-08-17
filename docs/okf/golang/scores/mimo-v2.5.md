---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: golang
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. mimo-v2.5 has no "/", so the filename is unchanged. -->

# Scorecard — mimo-v2.5 on golang

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 | |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 | |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 | |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 2 | 0.67 | missed pre-1.22 workaround (`v := v`) |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 | |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 | |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 | |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-03 | errors | 2 | 2 | 2 | 1.00 | |
| go-errors-04 | errors, interfaces | 3 | 3 | 2 | 0.67 | got the (type,value) nil explanation, missed the fix (return untyped nil, don't return a typed nil pointer) |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-01 | generics, interfaces | 2 | 2 | 1 | 0.50 | got interface-vs-generic at a high level but missed the type-relationship-preservation distinction (same in/out type, avoid any+assert) |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-04 | generics | 1 | 2 | 2 | 1.00 | |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 1 | 0.33 | got "goroutine must observe Done() and return"; missed propagation to child contexts AND the "context can't force-kill / ignoring it leaks" point |
| go-context-02 | context | 3 | 2 | 2 | 1.00 | |
| go-context-03 | context | 2 | 2 | 2 | 1.00 | |
| go-context-04 | context | 1 | 2 | 2 | 1.00 | |
| go-slices-01 | slices-maps | 3 | 3 | 1 | 0.33 | inverted the mechanism — claimed the overwrite happens "if append exceeds capacity and reallocates" when in fact overwrite-through-aliasing happens exactly when capacity is NOT exceeded (in-place write); got the "use append's return value" point |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 | |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 | |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 | |
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
| errors | 0.92 | 5 | ok | omit (strong) |
| interfaces | 0.78 | 4 | ok | omit (above threshold) |
| generics | 0.86 | 4 | ok | omit (strong) |
| context | 0.78 | 4 | ok | omit (above threshold) |
| slices-maps | 0.80 | 4 | ok | omit (above threshold) |
| defer-panic | 1.00 | 4 | ok | omit (strong) |
| goroutine-leaks | 0.80 | 4 | ok | omit (above threshold) |
| testing | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67 / 74 = 90.5%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag clears 0.75, so no
`derived/golang.mimo-v2.5.SKILL.md` corrective sections are generated from
this scorecard. `interfaces` (0.78) and `context` (0.78) are the closest to
the line — both driven by the same two weak questions noted above
(go-errors-04's missing fix, go-generics-01's missing type-relationship
point, and go-context-01's missing propagation/force-kill points) — worth a
spot-check on a future re-run but not a derivation trigger at the current
threshold.
