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

> **WITH-SKILL VALIDATION RUN.** These answers were produced closed-book with
> the derived tuning skill `../derived/csharp.space-bunny-free.SKILL.md`
> (`csharp-tuning-space-bunny-free`, target tag: **types-nullability**)
> prepended to the answerer prompt as active guidance. Grading is independent
> and held to the same strict standard as the baseline
> `space-bunny-free.md`: a point is awarded only where the concept is genuinely
> present. No derived skill is written from this run.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 3 | 1.00 | C# 8 named; erased at runtime; `!` no runtime effect; `!!` "removed before release and does not exist in any shipped C# version" — the baseline miss is now covered |
| csharp-null-02 | types-nullability | 3 | 3 | 3 | 1.00 | copy-vs-share, defensive copy stated plainly (readonly field, foreach), and the default-value point (zeroed non-null vs null) now present. Side note: claims `list[i].X = ...` acts on a copy — in fact that is compile error CS1612, not a silent no-op; not a rubric item |
| csharp-null-03 | types-nullability | 3 | 3 | 3 | 1.00 | record class positional members "init-only by default"; record struct "mutable by default", `readonly record struct` makes them immutable; value equality + `with` + plain-class reference equality all present. No C# 9/10 version numbers (not required by the baseline either) |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | init (C# 9) + required (C# 11) + `[SetsRequiredMembers]`; combination explained |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | still claims non-exhaustive arms mean "compilation fails"; it is warning CS8509 + runtime `SwitchExpressionException` (same miss as baseline) |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | C# 11 for list patterns; no C# 9 for relational/logical, concepts complete |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | definite assignment on the match path, `&&`/`||` flow explained |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 3 | 1.00 | struct-based / allocation avoidance, consume-once, `AsTask()` all present |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | both `WithCancellation` and `[EnumeratorCancellation]` given this run |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-02 | generics | 2 | 3 | 2 | 0.67 | out/in correct with examples; says "value types remain invariant" and "does not apply to generic method type parameters" but never states variance is limited to interfaces/delegates and that classes / `List<T>` are invariant (the baseline answer did state this) |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1 | 0.50 | no `EqualityComparer<T>.Default` for comparing an arbitrary T to its default (same miss as baseline) |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | "one shared captured local" = capture-by-variable; per-iteration copy fix; foreach safe since C# 5 |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 1 | 0.50 | SafeHandle preference present but omits `GC.SuppressFinalize` pairing with Dispose (same miss as baseline) |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | C# 12 named this run |
| csharp-span-02 | collections-spans | 3 | 3 | 2 | 0.67 | Memory<T> as heap-storable / across-await form covered; never mentions obtaining a Span via `.Span` (same miss as baseline) |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | same "not guaranteed initialized" side claim as baseline; not a rubric item |
| csharp-span-04 | collections-spans | 2 | 2 | 2 | 1.00 | C# 13 params collections named with ReadOnlySpan/IEnumerable and allocation saving |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-nullability | 1.00 | 4 | ok | omit (strong) — **validation target, passed** |
| linq | 1.00 | 5 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| disposal | 0.89 | 4 | ok | omit (strong) |
| collections-spans | 0.89 | 4 | ok | omit (strong) |
| pattern-matching | 0.88 | 4 | ok | omit (strong) |
| generics | 0.83 | 4 | ok | omit (strong) |

## Baseline vs with-skill comparison

| tag | baseline | with-skill | target? | verdict |
|-----|---------:|-----------:|:-------:|---------|
| types-nullability | 0.73 | 1.00 | **yes** | **PASS** — crossed 0.75; all three baseline misses (`!!` never shipped, value/reference defaults, record class init-only vs record struct mutable) now answered |
| linq | 1.00 | 1.00 | no | unchanged |
| async | 1.00 | 1.00 | no | unchanged |
| delegates-events | 1.00 | 1.00 | no | unchanged |
| generics | 0.93 | 0.83 | no | non-target drift (−0.10, one dropped point on generics-02); single-sample noise, no action |
| disposal | 0.89 | 0.89 | no | unchanged |
| collections-spans | 0.89 | 0.89 | no | unchanged |
| pattern-matching | 0.88 | 0.88 | no | unchanged (same pattern-01 exhaustiveness error) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.83/73 ≈ 94.3%   (baseline 66.5/73 ≈ 91.1%)
```

## Derivation targets

None. This is a validation run; no derived skill is written from it. Every
tag is at or above the 0.75 threshold. The skill's sole target tag,
**types-nullability**, moved 0.73 → 1.00 — the skill is validated for
`space-bunny-free` @ `alpha` on csharp.

## Contamination check

No answer reads as a copy of the reference. Non-target answers keep the same
independent errors as the baseline (pattern-01 "compilation fails", the
missing `.Span`, `GC.SuppressFinalize`, `EqualityComparer<T>.Default`), and
the target-tag answers track the skill's directives rather than the key's
wording (e.g. the skill-sourced `list[i].X = ...` claim, which the key does
not contain). Consistent with a genuine closed-book run with the skill active.
