---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: csharp
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on csharp

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark. Graded from `answers/cat-jam-free.md`.
> Fractional awards (0.5 steps) were used where a point's sub-element was missing.

## Provenance

opencode-go free/alpha model; no version published. The model self-identifies as
"CatJam, developed by LitterBox AI". Closed-book: the answerer was
`ocode run -m opencode-go/cat-jam-free -effort med` in an empty scratch cwd with only
the `_prompts/csharp.md` sheet attached; the session sqlite shows exactly 1 user +
1 assistant message and 0 tool calls. Headless runs inject ocode's bundled
skill-catalogue digest into the system prompt.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2.5 | 0.83 | compile-time/erased and `!` no-runtime-effect correct; `!!` never-shipped missing; no C# 8 version |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | copy-vs-share and defensive copy (readonly field, indexer copy) present; value-type vs reference-type default omitted |
| csharp-null-03 | types-nullability | 3 | 3 | 1.5 | 0.50 | value equality on both correct, but no `with`/C# 9; **wrongly says record struct positional members are init-only** (they are mutable unless `readonly record struct`) and hedges record class to "often init-only" — mutability point withheld |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | **wrongly says non-exhaustive switch expression is a compile error**; it is warning CS8509 + runtime `SwitchExpressionException` |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | gives `Point { X: 0, Y: var y }` as a positional example (it is a property pattern); `(0, var y)` correct |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | definite assignment on the true path stated; `is not` inversion not mentioned |
| csharp-linq-01 | linq | 3 | 3 | 2 | 0.67 | deferred vs immediate correct; never says a deferred query re-runs on each enumeration / reflects source at enumeration time |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 2.5 | 0.83 | consume-once and `AsTask()` present; never says Task is a heap-allocated reference type vs ValueTask a struct |
| csharp-async-02 | async | 3 | 3 | 2 | 0.67 | state machine + deadlock mechanism correct; no "async all the way" fix |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 1.5 | 0.75 | cooperative cancellation correct; async-stream token only "enumerator/token pairing" — no `WithCancellation` / `[EnumeratorCancellation]` |
| csharp-generics-01 | generics | 2 | 2 | 1 | 0.50 | constraint kinds listed (incl. `allows ref struct`); never says a constraint unlocks operations on `T` (`new T()`, member calls) |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1.5 | 0.75 | `EqualityComparer<T>.Default` comparison idiom missing |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 1.5 | 0.75 | Action/Func distinction present; lambdas/method groups converting to delegates and passing as values not stated |
| csharp-delegate-02 | delegates-events | 2 | 2 | 1.5 | 0.75 | event restrictions correct; plain delegate field overwrite/invoke exposure only implied |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 1.5 | 0.75 | using statement + declaration present; exception safety (try/finally) never stated |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 1.5 | 0.75 | SafeHandle present; `GC.SuppressFinalize` not mentioned |
| csharp-dispose-04 | disposal | 2 | 2 | 1.5 | 0.75 | Dispose(bool)/finalizer split correct; no `_disposed` idempotency guard |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-02 | collections-spans | 3 | 3 | 2.5 | 0.83 | getting a Span from Memory via `.Span` missing |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-04 | collections-spans | 2 | 2 | 1 | 0.50 | no amortized growth / realloc+copy; params collections vague — no C# 13, no Span/ReadOnlySpan, no allocation-avoidance |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-nullability | 0.73 | 4 | ok | **derive** |
| pattern-matching | 0.88 | 4 | ok | omit (strong) |
| linq | 0.88 | 5 | ok | omit (strong) |
| async | 0.83 | 5 | ok | **derive** |
| generics | 0.82 | 4 | ok | **derive** |
| delegates-events | 0.89 | 4 | ok | omit (strong) |
| disposal | 0.81 | 4 | ok | **derive** |
| collections-spans | 0.83 | 4 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 60.5 / 73 = 82.9%
```

## Derivation targets

Tags below threshold (`< 0.85`): **types-nullability, async, generics, disposal,
collections-spans** → feed into `derived/csharp.cat-jam-free.SKILL.md`.

Note: async, generics and collections-spans sit at 0.82–0.83, one missed sub-element
from the threshold; the failures there are omissions (missing idioms/caveats), not
wrong claims. The two materially wrong claims are on types-nullability (record struct
mutability) and pattern-matching (exhaustiveness "error"); pattern-matching stays above
threshold on the strength of its other three answers.

## Contamination check

No verbatim or near-verbatim matches to the reference answers. Answers are terse and
miss exactly the details a copier would carry (`!!` never shipped, `with`,
`EqualityComparer<T>.Default`, `GC.SuppressFinalize`, C# 13 params collections), and
contain two errors the key contradicts. Consistent with a genuine closed-book run.
