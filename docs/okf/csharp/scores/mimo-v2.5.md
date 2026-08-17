---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: csharp
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — mimo-v2.5 on csharp

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2 | 0.67 | never mentions `!!` param-check being removed/never shipped |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | no default-value point (value type default vs reference type null) |
| csharp-null-03 | types-nullability | 3 | 3 | 2 | 0.67 | wrongly claims record class is "mutable by default unless init-only" — positional members are init-only by default |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | exhaustiveness backwards: claims a compile-time error; actually CS8509 warning + runtime SwitchExpressionException |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 1 | 0.50 | property pattern syntax wrong: `{ Property = subPattern }` instead of `{ Property: pattern }` |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 1 | 0.50 | misses the `is not` inversion / definite-assignment nuance |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 3 | 1.00 | |
| csharp-async-02 | async | 3 | 3 | 2 | 0.67 | omits the explicit "async all the way" fix |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 1 | 0.50 | streaming covered but omits `WithCancellation`/`[EnumeratorCancellation]` token wiring |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1 | 0.50 | missing `EqualityComparer<T>.Default` comparison detail |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 1 | 0.50 | omits `GC.SuppressFinalize` pairing with Dispose |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-02 | collections-spans | 3 | 3 | 2 | 0.67 | doesn't mention obtaining a `Span` from `Memory<T>` via `.Span` |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-04 | collections-spans | 2 | 2 | 1 | 0.50 | wrong version: says params collections are "C# 12+", actually C# 13 |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| linq | 1.00 | 5 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| generics | 0.93 | 4 | ok | omit (strong) |
| disposal | 0.89 | 4 | ok | omit (strong) |
| async | 0.83 | 5 | ok | omit (strong) |
| collections-spans | 0.78 | 4 | ok | omit (strong) |
| types-nullability | 0.73 | 4 | ok | **derive** |
| pattern-matching | 0.63 | 4 | ok | **derive** |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61.5/73 ≈ 84.2%
```

## Derivation targets

Tags below threshold (`< 0.75`): **types-nullability, pattern-matching** →
`../derived/csharp.mimo-v2.5.SKILL.md`. All other tags (linq,
delegates-events, generics, disposal, async, collections-spans) are omitted
from the skill — the model already knows them.
