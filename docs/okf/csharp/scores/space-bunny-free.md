---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: csharp
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on csharp

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2 | 0.67 | `#nullable enable` covered but never mentions the `!!` param-null-check that was pulled before C# 11 shipped |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | copy-vs-share and defensive-copy points present (hedged "may be rejected or use a defensive copy"); no default-value point (value type = zeroed, reference type = null) |
| csharp-null-03 | types-nullability | 3 | 3 | 2 | 0.67 | says "neither form is automatically immutable" and positional record-class props are only "commonly" init-only — they ARE init-only by default; never states record struct members are mutable unless `readonly record struct` |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | exhaustiveness wrong: claims "otherwise compilation fails"; it is warning CS8509 + runtime `SwitchExpressionException` |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | no C# version numbers but every concept present |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | definite-assignment-on-match-path explained; `is not` inversion not called out explicitly but core concept present |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 3 | 1.00 | never says ValueTask is a struct outright, but allocation-avoidance / consume-once / `AsTask()` all present |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | `WithCancellation` given; `[EnumeratorCancellation]` omitted (rubric accepts either) |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1 | 0.50 | no `EqualityComparer<T>.Default` for comparing an arbitrary T to its default |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 1 | 0.50 | SafeHandle preference present but omits `GC.SuppressFinalize` pairing with Dispose |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | "modern C#" instead of C# 12, concepts complete |
| csharp-span-02 | collections-spans | 3 | 3 | 2 | 0.67 | Memory<T> as heap-storable form covered; never mentions obtaining a Span via `.Span` |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | extra claim "stackalloc memory should not be assumed zeroed" is spec-true but misleading in practice (compiler zero-inits unless `[SkipLocalsInit]`); not a rubric item |
| csharp-span-04 | collections-spans | 2 | 2 | 2 | 1.00 | params collections stated vaguely ("collection and span forms"), no C# 13 version, but allocation-avoidance concept present |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| linq | 1.00 | 5 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| generics | 0.93 | 4 | ok | omit (strong) |
| disposal | 0.89 | 4 | ok | omit (strong) |
| collections-spans | 0.89 | 4 | ok | omit (strong) |
| pattern-matching | 0.88 | 4 | ok | omit (strong) |
| types-nullability | 0.73 | 4 | ok | **derive** |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.5/73 ≈ 91.1%
```

## Derivation targets

Tags below threshold (`< 0.75`): **types-nullability** →
`../derived/csharp.space-bunny-free.SKILL.md`. All other tags (linq, async,
delegates-events, generics, disposal, collections-spans, pattern-matching) are
omitted from the skill — the model already knows them.

## Contamination check

No answer reads as a copy of the reference. Wording and structure differ
throughout, several answers carry errors the key does not (pattern-01
exhaustiveness, null-03 record mutability), and version numbers the key states
inline are consistently absent. Consistent with a genuine closed-book run.
