---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: php
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on php

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | includes the caller-side-decides nuance and int→float widening |
| php-types-02 | types | 3 | 3 | 3 | 1.00 | correct 8.0 `0 == "foo"` change; `"1e3" == "1000"` example |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | states covariant narrowing rule (correct per RFC) |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | |
| php-oop-02 | oop | 3 | 3 | 3 | 1.00 | all three points incl. readonly-class extension rule and 8.3 `__clone` |
| php-oop-03 | oop | 2 | 3 | 3 | 1.00 | |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | |
| php-closures-02 | closures | 2 | 2 | 1 | 0.50 | produces-a-Closure point present (equates to `Closure::fromCallable`); missed that it replaces string/array callable forms with a type-safe, scope-respecting reference |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | both points present; minor inaccuracy: claims `bindTo()` lacks a scope argument (it accepts `$newScope` too) — not a rubric point, not penalized |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | full `ErrorException` handler shown incl. `error_reporting()` guard |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | omits int-key renumbering nuance; core concepts present |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | correctly notes only the nullsafe link short-circuits |
| php-null-03 | null-safety | 1 | 2 | 2 | 1.00 | both points present, though framed as "purely syntactic sugar" |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | gives `0` (isset true, empty true) as required, but the surrounding prose is muddled about what "disagree" means |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | |
| php-match-03 | match-control | 1 | 2 | 2 | 1.00 | |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 1.00 | 5 | ok | omit (strong) |
| enums | 1.00 | 4 | ok | omit (strong) |
| oop | 1.00 | 5 | ok | omit (strong) |
| closures | 0.86 | 4 | ok | omit (above threshold) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 1.00 | 4 | ok | omit (strong) |
| match-control | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65 / 66 = 98.5%
```

## Derivation targets

No tag scored below threshold (`< 0.75`). Every tag cleared 0.75, so **no
derived skill is written** for `glm-5.3-flash` on `php` — see `rubric-guide.md`
("Tags with subscore ≥ 0.75 are omitted"). The only lost point was on
`closures` (0.86): first-class callable syntax was described as producing a
Closure (correct) but not as the type-safe replacement for the old
string/array callable forms. Note: a near-perfect sweep is a contamination
red flag per the closed-book protocol — worth confirming the answerer saw
only `_prompts/php.md`.
