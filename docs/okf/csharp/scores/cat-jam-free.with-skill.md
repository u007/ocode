---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: csharp
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on csharp (with-skill)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark. Graded from `answers/cat-jam-free.with-skill.md`.
> Fractional awards (0.5 steps) were used where a point's sub-element was missing.

> **WITH-SKILL VALIDATION RUN.** These answers were produced closed-book with
> the derived tuning skill `../derived/csharp.cat-jam-free.SKILL.md`
> (`csharp-tuning-cat-jam-free`, target tags: **types-nullability, async,
> generics, disposal, collections-spans**) prepended to the answerer prompt as
> active guidance. Grading is independent and held to the same standard as the
> baseline `cat-jam-free.md`: a point is awarded only where the concept is
> present, and a materially wrong claim about the same construct withholds it.
> No derived skill is written from this run.

## Provenance

Answerer: `ocode run -m opencode-go/cat-jam-free -effort med` with the SKILL.md
body (front matter stripped) prepended to the `_prompts/csharp.md` sheet; isolated
HOME with every tool denied, cwd holding only the sheet. Session audited in sqlite:
1 user + 1 assistant message, 0 tool calls.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2.5 | 0.83 | compile-time only, `?` no runtime effect, `!` no runtime check, `#nullable enable` correct; `!!` never-shipped still missing despite the skill; no C# 8 (same as baseline) |
| csharp-null-02 | types-nullability | 3 | 3 | 3 | 1.00 | copy-vs-share, defensive copy (readonly field / collection element), and the value-vs-reference default point now present |
| csharp-null-03 | types-nullability | 3 | 3 | 3 | 1.00 | record class: value equality + `with` + init-only positional members; record struct mutable `get; set;` unless `readonly`; plain class identity equality. Baseline error fixed. No C# 9/10 version numbers |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | init + required + constructor-annotation escape hatch |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | still says missing coverage is "normally a compile-time error"; it is warning CS8509 + runtime `SwitchExpressionException` (same error as baseline) |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | no version numbers; concepts complete |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | definite assignment on the match path; `is not` not mentioned |
| csharp-linq-01 | linq | 3 | 3 | 2 | 0.67 | deferred vs immediate correct; still never says a deferred query re-runs on each enumeration (same as baseline) |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 2.5 | 0.83 | consume-once and `AsTask()` present; still only "avoid allocating a Task" — never says ValueTask is a struct vs Task a heap reference type, despite the skill directive |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | "await all the way up the call chain" fix now present |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | `WithCancellation` + `[EnumeratorCancellation]` both named |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | restrict + unlock (`new T()`, base/interface members, only `object` members otherwise) |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | out/in with examples, interfaces/delegates only, classes invariant |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1.5 | 0.75 | default(T) correct; `EqualityComparer<T>.Default` still missing despite the skill (same as baseline) |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 1.5 | 0.75 | Action/Func distinction present; lambdas/method groups converting to delegates not stated (same as baseline) |
| csharp-delegate-02 | delegates-events | 2 | 2 | 1.5 | 0.75 | event restrictions correct; plain delegate field exposure only implied (same as baseline) |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | try/finally exception safety now stated; using declaration present (no C# 8) |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 2 | 1.00 | SafeHandle preference + `GC.SuppressFinalize(this)` now present |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | Dispose(bool)/finalizer split, SuppressFinalize, `_disposed` guard all present |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-02 | collections-spans | 3 | 3 | 2.5 | 0.83 | ref struct view, no field/box/await, Memory heap-storable; `.Span` still missing despite the skill (same as baseline) |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-04 | collections-spans | 2 | 2 | 2 | 1.00 | fixed vs resizable with amortized O(1) Add (realloc+copy not spelled out); C# 13 params collections with Span/ReadOnlySpan/IEnumerable and per-call allocation avoidance |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | baseline | with-skill | n | target? | verdict |
|-----|---------:|-----------:|--:|:-------:|---------|
| types-nullability | 0.73 | 0.95 | 4 | **yes** | **PASS** — record struct mutability error fixed, defaults and `with` added; only `!!` still missing |
| async | 0.83 | 0.96 | 5 | **yes** | **PASS** — async-all-the-way and async-stream cancellation added; ValueTask-is-a-struct still missing |
| generics | 0.82 | 0.96 | 4 | **yes** | **PASS** — constraint "unlocks operations" added; `EqualityComparer<T>.Default` still missing |
| disposal | 0.81 | 1.00 | 4 | **yes** | **PASS** — try/finally, `GC.SuppressFinalize`, `_disposed` guard all absorbed |
| collections-spans | 0.83 | 0.94 | 4 | **yes** | **PASS** — C# 13 params collections + amortized growth added; `.Span` still missing |
| pattern-matching | 0.88 | 0.88 | 4 | no | unchanged (same pattern-01 exhaustiveness error) |
| linq | 0.88 | 0.88 | 5 | no | unchanged (same linq-01 re-run omission) |
| delegates-events | 0.89 | 0.89 | 4 | no | unchanged |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.25 / 73 ≈ 93.5%   (baseline 60.5/73 ≈ 82.9%)
```

## Verdict

**PASS.** Every target tag (types-nullability, async, generics, disposal,
collections-spans) is at or above the 0.85 threshold. The skill is validated for
`cat-jam-free` @ `alpha` on csharp.

Absorption was partial: four directives were not applied (`!!` never shipped,
ValueTask is a struct, `EqualityComparer<T>.Default`, `memory.Span`), each costing
0.5 on one question, but none pushes its tag below threshold. If the skill is ever
tightened, those four lines are the ones to sharpen. No non-target drift.

## Derivation targets

None. This is a validation run; no derived skill is written from it.

## Contamination check

No answer reads as a copy of the reference. Non-target answers keep the baseline's
independent errors and omissions (pattern-01 "compile-time error", linq-01 re-run,
delegate-01/02 omissions), and the target-tag answers track the skill's wording
(e.g. "zeroed, non-null instances … null only via Nullable<T>", "mutable
`get; set;` positional members") rather than the key's. Consistent with a genuine
closed-book run with the skill active.
