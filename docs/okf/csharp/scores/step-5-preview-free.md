---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: csharp
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on csharp

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Graded from `answers/step-5-preview-free.md`.
> Fractional awards (0.5 steps) were used where a point's sub-element was missing.

## Provenance

Closed-book: the answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/csharp.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. A first attempt timed
out at the provider with no output and was re-run; only the successful run is graded.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2.5 | 0.83 | `#nullable` context covered; `!!` never-shipped not mentioned |
| csharp-null-02 | types-nullability | 3 | 3 | 3 | 1.00 | |
| csharp-null-03 | types-nullability | 3 | 3 | 2.5 | 0.83 | record class positional init-only not stated; record struct mutability only implied via `readonly record struct`. Also wrongly claims (hedged) that record struct does not synthesize `==`/`!=` — it does |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | example `d is <= DateTime.Now` is invalid (relational patterns need a constant); not a rubric point |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | no `is not` inversion, but definite-assignment scoping explained |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 2.5 | 0.83 | `.AsTask()` called an "interop escape hatch", not presented as the way to store / await repeatedly / fan out |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1.5 | 0.75 | `EqualityComparer<T>.Default` name-dropped but not as the way to compare `T` against its default; `x is default` is not a valid pattern |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | notation slip `Func<TResult, ...>`; example and prose correct |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | idempotency stated as a rule; no explicit `_disposed` flag in the code |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | wrongly says an untyped `[...]` defaults to an array (collection expressions have no natural type); not a rubric point |
| csharp-span-02 | collections-spans | 3 | 3 | 3 | 1.00 | |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | says safe `stackalloc` since C# 7.3 (it is 7.2); zeroing hedged |
| csharp-span-04 | collections-spans | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-nullability | 0.91 | 4 | ok | omit (strong) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |
| linq | 1.00 | 5 | ok | omit (strong) |
| async | 0.96 | 5 | ok | omit (strong) |
| generics | 0.96 | 4 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| disposal | 1.00 | 4 | ok | omit (strong) |
| collections-spans | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71.25 / 73 = 97.6%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none**. No derived skill written.

## Contamination check

No verbatim or near-verbatim matches to the reference answers. The answers are
longer, differently structured, and miss exactly the details a copier would have
carried over (`!!` never shipped on null-01, `.AsTask()` as the store/fan-out route
on async-01, `EqualityComparer<T>.Default` as the default-comparison idiom on
generics-04). The few factual slips noted above are the model's own.
