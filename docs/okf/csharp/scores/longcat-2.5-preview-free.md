---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: csharp
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on csharp

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2 | 0.67 | compile-time-only, `?`/`!` no runtime effect, `#nullable`/`<Nullable>` all correct; never mentions that the `!!` param-null-check never shipped |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | copy-vs-share and defensive-copy (readonly field / collection indexer) present; no default-value point (value type = zeroed, reference type = null) |
| csharp-null-03 | types-nullability | 3 | 3 | 3 | 1.00 | record class positional props init-only; record struct value equality; `readonly record struct` for immutability (record-struct mutable default implied, not stated outright); class reference equality. No C# 9/10 versions |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | exhaustiveness wrong: claims the compiler "enforces" it and CS8509 is an *error*; it is a warning + runtime `SwitchExpressionException` (never mentioned) |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | extended `Address.City` form shown, no C# 10 version |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | concepts present, but lists invalid `is == 5` / `is != 0` as relational patterns and claims they work on "any type that supports the operator" (constants of numeric/char/enum only) |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | test+bind in one, scoped to the matching path incl. `&&`; `is not` inversion not covered (core present) |
| csharp-linq-01 | linq | 3 | 3 | 2 | 0.67 | deferred and immediate operator lists correct; never says a deferred query re-runs on each enumeration / reflects the source at enumeration time |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 3 | 1.00 | allocation avoidance on sync path, consume-once, `AsTask()`; never says ValueTask is a struct outright |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | state machine + captured-context deadlock; fix only as "blocking on async code is still bad practice" (accepted). Minor: says the state machine "implements IAsyncMethodBuilder" (it is `IAsyncStateMachine`; the builder is separate) |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | stale extra: says ASP.NET controller actions want the request context back "to access HttpContext" — ASP.NET Core has no SynchronizationContext |
| csharp-async-04 | async | 2 | 2 | 1 | 0.50 | cooperative cancellation correct; async streams covered but no token wiring (`WithCancellation` / `[EnumeratorCancellation]`), and says you "produce elements with `await foreach`" (that consumes) |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | uses IComparer for contravariance instead of Action; correct |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 2 | 1.00 | `EqualityComparer<T>.Default` present; spurious `void` bullet |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | minor syntax slip `await using (var x = ...;)` |
| csharp-dispose-03 | disposal | 2 | 2 | 1 | 0.50 | SafeHandle preference present but omits `GC.SuppressFinalize` pairing with Dispose (it appears only in dispose-04) |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | example `var combined = [1, 2, ..list, 6, 7];` does not compile (collection expressions have no natural type) |
| csharp-span-02 | collections-spans | 3 | 3 | 2 | 0.67 | ref-struct restrictions and Memory<T> as heap-storable form covered; never mentions getting a Span via `.Span`; invents `span.AsReadOnly()` |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-04 | collections-spans | 2 | 2 | 2 | 1.00 | params-Span allocation avoidance present, but mis-dated as "C# 12+" (params collections are C# 13) and lists only Span forms |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| generics | 1.00 | 4 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| linq | 0.92 | 5 | ok | omit (strong) |
| async | 0.92 | 5 | ok | omit (strong) |
| disposal | 0.89 | 4 | ok | omit (strong) |
| collections-spans | 0.89 | 4 | ok | omit (strong) |
| pattern-matching | 0.88 | 4 | ok | omit (strong) |
| types-nullability | 0.82 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66/73 ≈ 90.4%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill is written for
`longcat-2.5-preview-free` on csharp. Recurring misses that stay above threshold
(recorded here only): switch-expression exhaustiveness severity (pattern-01),
nullability/value-type defaults (null-01 `!!`, null-02 default values),
deferred re-execution (linq-01), async-stream token wiring (async-04),
`GC.SuppressFinalize` (dispose-03), `Memory<T>.Span` (span-02).

## Contamination check

No answer reads as a copy of the reference. Wording, structure and examples
differ throughout (e.g. IComparer instead of Action for contravariance, long
operator lists, code samples the key does not have), and several answers carry
errors the key does not (CS8509 called an error, `is == 5` as a relational
pattern, params collections dated C# 12, `var` collection expression).
Consistent with a genuine closed-book run.
