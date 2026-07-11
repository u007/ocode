---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: csharp
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on csharp

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.

Grader: independent (Claude Opus 4.8), closed-book. Answers were produced
CLOSED-BOOK (no corpus access); points awarded only where genuinely present,
0 for wrong/version-wrong claims.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 3 | 1.00 | compile-time/erased, `?`/`!`/`#nullable` all correct; didn't mention `!!` never shipped (not required, no wrong claim) |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | copy-vs-share + struct-copy mutation correct; **omitted the default rule** (value type = zeroed non-null, reference = null) |
| csharp-null-03 | types-nullability | 3 | 3 | 3 | 1.00 | record class / record struct / class equality + `with` + init-vs-mutable all present |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | init + required both correct, combination right |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | **WRONG: claims non-exhaustive switch expression is a compile error.** It is a CS8509 warning + runtime `SwitchExpressionException`, not a compile error |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | property + positional patterns, Deconstruct, var binding all correct |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | relational + logical + list/slice patterns all correct |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | test+bind in one, scoped to matching path; didn't cover `is not` fall-through (parenthetical, core present) |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | deferred vs immediate operators + re-evaluation all correct |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | IEnumerable delegates vs IQueryable expression-tree, AsEnumerable trap correct |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | double-enumeration re-runs pipeline, materialize with ToList correct |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | First/Single/OrDefault + value-type default(T)=0 gotcha correct |
| csharp-async-01 | async | 3 | 3 | 3 | 1.00 | ValueTask struct avoids alloc, consume-once, AsTask all correct |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | state machine + captured-context deadlock + "blocking is bad practice" all present |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | resumes off captured context, library-code rationale correct |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | cooperative cancellation + async streams; missed WithCancellation/[EnumeratorCancellation] (core present) |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | where restricts + enables ops, lists class/struct/new()/base/interface |
| csharp-generics-02 | generics | 2 | 2 | 2 | 1.00 | out/covariance + in/contravariance + interfaces/delegates-only correct |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | inference from arguments + when to specify explicitly correct |
| csharp-generics-04 | generics | 1 | 1 | 1 | 1.00 | default(T) zero value + generic-code need correct; missed EqualityComparer<T>.Default (core present) |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | Func/Action/Predicate + type-safe method reference correct |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | event restricts to +=/-=, plain field breaks encapsulation correct |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | capture-by-reference, for shared-var trap + per-iteration copy, foreach safe since C# 5 |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | multicast invocation list, last-return-kept, throw-stops-rest correct |
| csharp-dispose-01 | disposal | 3 | 3 | 3 | 1.00 | using try/finally at block end + using declaration at enclosing scope correct |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | DisposeAsync/await using + use-when-teardown-is-async correct |
| csharp-dispose-03 | disposal | 2 | 2 | 2 | 1.00 | Dispose vs finalizer, only-unmanaged-needs-finalizer correct; omitted SafeHandle + SuppressFinalize (headline present, both covered in dispose-04) |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | Dispose(bool)+SuppressFinalize+finalizer split, managed/unmanaged semantics; omitted the _disposed idempotency flag (core present) |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | collection expressions target-typed + spread `..` correct |
| csharp-span-02 | collections-spans | 3 | 3 | 3 | 1.00 | ref struct stack-only, no field/await, Memory<T> heap form; didn't name `.Span` accessor (core present) |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | stackalloc on stack, no-escape + stack-overflow caveats correct |
| csharp-span-04 | collections-spans | 2 | 2 | 2 | 1.00 | array-vs-List + params collections (C# 13) avoiding array alloc correct |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-nullability | 0.91 | 4 | ok | omit (strong) |
| pattern-matching | 0.88 | 4 | ok | omit (strong) |
| linq | 1.00 | 5 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| generics | 1.00 | 4 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| disposal | 1.00 | 4 | ok | omit (strong) |
| collections-spans | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
(linq includes csharp-delegate-01; async includes csharp-dispose-02.)

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71.0 / 73 = 97%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. All eight tags score ≥ 0.88, so
**no derivation** — no `derived/csharp.tencent__hy3.SKILL.md` is produced. The
model's only outright miss (switch-expression exhaustiveness as a "compile
error", csharp-pattern-01) and its one omission (default-value rule,
csharp-null-02) are isolated and do not pull any tag below 0.75.
