---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: csharp
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on csharp (with-skill)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark. Graded from
> `answers/nvidia__nemotron-3.5-super-vl.with-skill.md`. Fractional awards (0.5 steps)
> were used where a point's sub-element was missing.

> **WITH-SKILL VALIDATION RUN.** These answers were produced closed-book with the
> derived tuning skill `../derived/csharp.nvidia__nemotron-3.5-super-vl.SKILL.md`
> (`csharp-tuning-nvidia-nemotron-3.5-super-vl`, target tags: **types-nullability,
> async**) prepended to the answerer prompt as active guidance. Grading is
> independent and held to the same standard as the baseline
> `nvidia__nemotron-3.5-super-vl.md`: a point is awarded only where the concept is
> present, and a materially wrong claim about the same construct withholds it. No
> derived skill is written from this run.

## Provenance

Answerer: `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
with the SKILL.md body (front matter stripped) prepended to the `_prompts/csharp.md`
sheet; isolated HOME with every tool denied (no tools advertised), cwd holding only
the sheet, one fresh session. Session audited in sqlite: 1 user + 1 assistant
message, 0 tool calls. All 32 ids returned once, none empty. No answer cites a tool
or skill the question never named; no thinking-aloud, duplicates or fabricated tool
output. The OpenRouter `:free` route variant is not part of the model id.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2.5 | 0.83 | compile-time only, erased at runtime, `?`/`!`/`#nullable` correct; `!!` never-shipped still missing despite the skill directive; no C# 8 version this run |
| csharp-null-02 | types-nullability | 3 | 3 | 3 | 1.00 | value-type zeroed vs reference-type null default now present (skill wording); copy-vs-share and defensive copies (readonly field, `List<T>` indexer CS1612, foreach) correct |
| csharp-null-03 | types-nullability | 3 | 3 | 3 | 1.00 | record struct positional members MUTABLE unless `readonly record struct` now stated; record class init-only + value equality + `with`; plain class identity. Names the generated method "PrintToString" (it is `PrintMembers`/`ToString`), minor |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | **regressed: now says the compiler errors (CS8509) on a non-exhaustive switch expression** — it is a warning plus runtime `SwitchExpressionException`; baseline had this right (non-target) |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | no version numbers; concepts complete |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 1.5 | 0.75 | test+bind in one step correct; **wrongly says `c` is assigned its default on the failed path** — it is simply not definitely assigned there; half of the scope point withheld (non-target; baseline was correct) |
| csharp-linq-01 | linq | 3 | 3 | 2 | 0.67 | deferred vs immediate correct; the "re-runs on each enumeration / reflects source at enumeration time" point dropped this run (baseline had it; non-target) |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 3 | 1.00 | struct vs allocated `Task<T>`, consume-once, and `.AsTask()` for storage/fan-out now present |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | "async all the way — await up the call chain" fix now present |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | `WithCancellation` + `[EnumeratorCancellation]` both named |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | the baseline's invented `where T : any` is gone; kinds now correct |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | |
| csharp-generics-03 | generics | 2 | 2 | 1.5 | 0.75 | inference from arguments and explicit-when-undetermined correct; **claims type arguments can be inferred "from how the result is used via target typing in newer C#"** — generic method inference never uses the return context; half withheld (non-target; baseline was correct) |
| csharp-generics-04 | generics | 1 | 2 | 1.5 | 0.75 | `EqualityComparer<T>.Default` idiom missing (same as baseline) |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 1.5 | 0.75 | Action/Func distinction present; lambdas/method groups converting to delegates not stated (same as baseline) |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 2 | 1.00 | `SafeHandle` preference present this run; `GC.SuppressFinalize` not mentioned here (baseline had the reverse) |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-02 | collections-spans | 3 | 3 | 2.5 | 0.83 | `.Span` on `Memory<T>` still missing (same as baseline) |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | "no zero-fill guarantee" aside is only true under `SkipLocalsInit`; not withheld |
| csharp-span-04 | collections-spans | 2 | 2 | 1.5 | 0.75 | array vs List with amortized O(1) append present; **params collections dated C# 12** (they are C# 13) and no allocation-avoidance rationale; half withheld (non-target; baseline had the version right) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | baseline | with-skill | n | target? | verdict |
|-----|---------:|-----------:|--:|:-------:|---------|
| types-nullability | 0.82 | 0.95 | 4 | **yes** | **PASS** — type defaults and record struct mutability absorbed; only `!!` never-shipped still missing |
| async | 0.83 | 1.00 | 5 | **yes** | **PASS** — `.AsTask()`, async-all-the-way and `[EnumeratorCancellation]` all absorbed |
| pattern-matching | 1.00 | 0.81 | 4 | no | drift (pattern-01 exhaustiveness "error", pattern-04 default-on-failure claim) |
| linq | 0.96 | 0.88 | 5 | no | drift (linq-01 re-run point dropped) |
| generics | 0.89 | 0.89 | 4 | no | unchanged net (generics-01 fixed, generics-03 target-typing claim) |
| delegates-events | 0.94 | 0.94 | 4 | no | unchanged |
| disposal | 1.00 | 1.00 | 4 | no | unchanged |
| collections-spans | 0.89 | 0.89 | 4 | no | unchanged net (span-04 C# 12 instead of allocation rationale) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.75 / 73 ≈ 92.8%   (baseline 66.75/73 ≈ 91.4%)
```

## Verdict

**PASS.** Both target tags (types-nullability 0.95, async 1.00) are above the 0.85
threshold. The skill is validated for `nvidia/nemotron-3.5-super-vl` @ `3.5` on
csharp.

Absorption: five of six directives were applied. The one not applied is "`!!` never
shipped" (null-01, costs 0.5 on one question); if the skill is ever tightened, that
is the line to sharpen. Non-target drift is single-sample resampling noise per the
validation guide and is left alone: pattern-matching dropped to 0.81 on two new
wrong claims the baseline did not make, and linq dropped one omission. Neither tag
is a target of this skill; if either looks weak again on a fresh baseline it earns
its own section then.

## Derivation targets

None. This is a validation run; no derived skill is written from it.

## Contamination check

No answer reads as a copy of the reference. Target-tag answers track the skill's
wording ("zeroed, non-null instance … only `Nullable<T>`", "not the same code
generation … the mutability default differs", "two halves"), not the key's.
Non-target answers carry independent errors and omissions the key contradicts
(pattern-01 "errors", pattern-04 default-on-failure, generics-03 target typing,
span-04 C# 12, no `.Span`, no `EqualityComparer<T>.Default`). Consistent with a
genuine closed-book run with the skill active.
