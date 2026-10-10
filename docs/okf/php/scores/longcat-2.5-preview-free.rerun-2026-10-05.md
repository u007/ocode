---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: php
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on php (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark.
>
> Baseline re-run of 2026-10-05 (full 34-question sweep, closed-book). An earlier
> 2026-09-28 scorecard exists beside this one; it was not consulted.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | |
| php-types-02 | types | 3 | 3 | 3 | 1.00 | |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-03 | enums | 2 | 3 | 2 | 0.67 | no mention of implicit final / cannot be extended (only said enums cannot extend) |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | |
| php-oop-02 | oop | 3 | 3 | 2 | 0.67 | missed runtime-set/not-a-constant and readonly-class-extension rule |
| php-oop-03 | oop | 2 | 3 | 2 | 0.67 | no `new static()` vs `new self()` contrast |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | |
| php-closures-02 | closures | 2 | 2 | 1 | 0.50 | produces Closure, but no point on replacing string/array callables |
| php-closures-03 | closures | 1 | 2 | 1 | 0.50 | scope/private access ok; missed that bind returns a NEW closure |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | wording says set_error_handler itself converts to ErrorException; the example throws it correctly |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | dupes/renumbering detail omitted; dubious claim about string keys in calls |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | |
| php-null-03 | null-safety | 1 | 2 | 2 | 1.00 | |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 1 | 0.50 | muddled example; no must-cast/avoids-loose-false-matches point |
| php-match-03 | match-control | 1 | 2 | 1 | 0.50 | fallthrough ok; no single-expression vs statement-block point |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 1.00 | 5 | ok | omit (strong) |
| enums | 0.90 | 4 | ok | omit (strong) |
| oop | 0.82 | 5 | ok | omit (strong) |
| closures | 0.79 | 4 | ok | omit (above threshold) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 1.00 | 4 | ok | omit (strong) |
| match-control | 0.79 | 4 | ok | omit (above threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 60.67 / 66 = 91.9%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — no derived skill is warranted.
Nearest to threshold: closures (0.79) and match-control (0.79).
