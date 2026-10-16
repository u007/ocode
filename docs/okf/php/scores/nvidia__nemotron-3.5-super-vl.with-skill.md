---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: php
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on php (WITH derived skill)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

> **Validation run.** This scorecard validates
> `../derived/php.nvidia__nemotron-3.5-super-vl.SKILL.md` (target tags: **oop,
> closures**). Baseline: `nvidia__nemotron-3.5-super-vl.md`. No new derivation
> from this card; only target-tag movement is trusted (non-target tags resample
> with ordinary LLM variance).

## Provenance

Closed-book (Rule 0) plus the skill. Same setup as the baseline:
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool, scratch cwd with only the
`_prompts/php.md` sheet, one fresh session (1 user + 1 assistant message, 0 tool
calls). The derived skill body (front matter stripped) was prepended to the sheet
as active guidance; the answerer never saw `questions.yaml`. All 34 ids came back
once, none empty. No answer cites a tool or skill the question never named.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | minor: "widening/narrowing via implicit casts from int to float" — only int→float widening is allowed |
| php-types-02 | types | 3 | 3 | 2 | 0.67 | example withheld again: `0 == "abc"` is `true` (false since PHP 8.0); `"0" == false` and `true == 1` are correct but the headline example is PHP 7 |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | awarded (judgment): now says the override "must declare a compatible type"; the trailing "effectively invariant" is still wrong (narrowing is allowed) but is hedged and no longer the stated rule |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | declaration order not mentioned |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | minor: "cannot be unserialized normally" — enums serialize/unserialize since 8.1; not a rubric point |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | |
| php-oop-02 | oop | 3 | 3 | 3 | 1.00 | typed, write-once in scope, Error; readonly class rules; "only another readonly class may extend or be extended by it" — baseline's "cannot be extended" error corrected |
| php-oop-03 | oop | 2 | 3 | 3 | 1.00 | `new self()` builds the parent, `new static()` the subclass — the baseline's missing point now present |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | implicit by-value capture with no `use` clause now stated; closure explicit `use`, snapshot at definition |
| php-closures-02 | closures | 2 | 2 | 2 | 1.00 | Closure without invoking; replaces string/array callables with a typed, visibility-respecting reference |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | minor: lists `FatalError`, which is not a PHP class; not a rubric point |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | now includes re-indexing and later-wins |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | bool → int now correct; baseline's "booleans become strings" gone |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | |
| php-null-03 | null-safety | 1 | 2 | 1 | 0.50 | same as baseline: only-if-null present; "evaluates the left side only once" is about the target, never that the right side is evaluated only when needed |
| php-null-04 | null-safety | 2 | 3 | 2 | 0.67 | same as baseline: disagreement example is `null`/undefined (the complementary case); never a set-but-falsy value such as `0`/`"0"` |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | both concrete cases (`'0'` arm, `false` arm) are correct |
| php-match-03 | match-control | 1 | 2 | 2 | 1.00 | |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | baseline's "any truthy condition" claim gone; arms are boolean expressions compared strictly to `true` |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | target? | result |
|-----|---------:|-----------:|--:|-------|---------|--------|
| types | 0.86 | 0.91 | 5 | ok | no | drift (not trusted) |
| enums | 1.00 | 1.00 | 4 | ok | no | unchanged |
| oop | 0.81 | **1.00** | 5 | ok | **yes** | **reached threshold** |
| closures | 0.71 | **1.00** | 4 | ok | **yes** | **reached threshold** |
| error-handling | 1.00 | 1.00 | 4 | ok | no | unchanged |
| arrays | 0.89 | 1.00 | 4 | ok | no | drift (not trusted) |
| null-safety | 0.85 | 0.85 | 4 | ok | no | unchanged (0.854) |
| match-control | 0.93 | 1.00 | 4 | ok | no | drift (not trusted) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Per-tag arithmetic: types 10/11, enums 7/7, oop 9/9, closures 7/7,
error-handling 8/8, arrays 9/9, null-safety 6.83/8, match-control 7/7.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.83 / 66 = 96.7%   (baseline 88.1%)
```

## Derivation targets

None — validation run. Both target tags (oop 0.81 → 1.00, closures 0.71 → 1.00)
reached the 0.85 threshold, so the derived skill is validated as written; no
tweak-and-repeat iteration is needed.

## Notes

- Every point the skill addressed was absorbed: readonly-class extension rule,
  `new static()` vs `new self()`, implicit arrow-function capture, and the
  callable-replacement role of `(...)` all appear in the model's own wording.
- Non-target improvements (bool array keys, `match(true)` boolean arms, spread
  re-indexing) are not covered by the skill and are treated as resampling drift,
  not as evidence; they would need a fresh baseline to count.
- Persistent non-target misses, identical to baseline: `0 == "abc"` claimed true
  (types-02), RHS laziness of `??=` never stated (null-03), `null` chosen as the
  isset/empty disagreement example (null-04). All three tags remain at or above
  threshold, so per the validation rules nothing is bolted onto this skill; if
  types-02's PHP 7 comparison rule recurs on a fresh baseline it is a candidate
  for its own `types` section.
- No anomalies: no duplicated answers, no cited tools or skills, no
  thinking-aloud, no fabricated tool output.
