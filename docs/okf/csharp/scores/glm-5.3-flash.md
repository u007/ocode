---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: csharp
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on csharp

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2 | 0.67 | compile-time/erased/`!` all correct; never mentions the `!!` param-null-check being pulled before C# 11 |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | copy-vs-share and defensive-copy correct; no default-value point (value type = zeroed, reference = null) |
| csharp-null-03 | types-nullability | 3 | 3 | 3 | 1.00 | |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | exhaustiveness backwards: claims "the compiler errors" and "every non-exhaustive switch expression becomes a compile error" — actually CS8509 warning + runtime `SwitchExpressionException` |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 3 | 1.00 | |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1 | 0.50 | zero-value and why-generic-code-needs-it correct; missing `EqualityComparer<T>.Default` comparison detail |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 1 | 0.50 | deterministic-vs-GC and SafeHandle correct; omits `GC.SuppressFinalize` pairing with Dispose (stated only in dispose-04, not here) |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-02 | collections-spans | 3 | 3 | 3 | 1.00 | |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-04 | collections-spans | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| linq | 1.00 | 5 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| collections-spans | 1.00 | 4 | ok | omit (strong) |
| generics | 0.93 | 4 | ok | omit (strong) |
| disposal | 0.89 | 4 | ok | omit (strong) |
| pattern-matching | 0.88 | 4 | ok | omit (strong) |
| types-nullability | 0.82 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.5/73 ≈ 93.8%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill is written for
`glm-5.3-flash` on csharp. The only substantive error is the switch-expression
exhaustiveness claim (pattern-01, weight 2); the remaining deductions are
omitted secondary details (`!!` history, value-type defaults,
`EqualityComparer<T>.Default`, `GC.SuppressFinalize`), none of which pull a
tag under threshold.
