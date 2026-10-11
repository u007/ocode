---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: csharp
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on csharp (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Baseline rerun 2026-10-05, graded from
> `answers/longcat-2.5-preview-free.rerun-2026-10-05.md`. An earlier 2026-09-28 scorecard exists beside it.
> Fractional awards (0.5 steps) were used where a point's sub-element was missing.
> The answers file had one malformed record (csharp-span-02 lacked its `answer:` key); parsed by `- id:` anyway.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2.5 | 0.83 | missed `!!` never shipped |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | no default-value point |
| csharp-null-03 | types-nullability | 3 | 3 | 2 | 0.67 | record struct mutable positional members not covered |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | claims non-exhaustive is compile error; actually CS8509 warning + runtime SwitchExpressionException |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | no `is not` inversion |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 2 | 0.67 | no .AsTask() to store/fan out |
| csharp-async-02 | async | 3 | 3 | 2 | 0.67 | no async-all-the-way fix |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 1.5 | 0.75 | wrong API names (ThrowIfCancellationCancellationRequested, GetAsyncEnumeratorAsync, MoveAsyncNextAsync); no WithCancellation/[EnumeratorCancellation] |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1.5 | 0.75 | no EqualityComparer<T>.Default |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 1.5 | 0.75 | no SafeHandle / SuppressFinalize in this answer |
| csharp-dispose-04 | disposal | 2 | 2 | 1.5 | 0.75 | no disposed flag |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-02 | collections-spans | 3 | 3 | 3 | 1.00 | answers-file record malformed (missing `answer:`); content parsed |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-04 | collections-spans | 2 | 2 | 1 | 0.50 | no params collections (C# 13) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-nullability | 0.77 | 4 | ok | omit (just above threshold) |
| pattern-matching | 0.88 | 4 | ok | omit (strong) |
| linq | 1.00 | 5 | ok | omit (strong) |
| async | 0.79 | 5 | ok | omit (just above threshold) |
| generics | 0.96 | 4 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| disposal | 0.89 | 4 | ok | omit (strong) |
| collections-spans | 0.89 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 88.7%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill written.
