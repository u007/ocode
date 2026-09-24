---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: php
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on php

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | |
| php-types-02 | types | 3 | 3 | 3 | 1.00 | examples `"0" == 0` / `false == 0` differ from the reference's `0 == "foo"` framing; both correct |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | states constant types are invariant (must declare the same type); compatible-with-parent concept present |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 2 | 0.67 | `cases()` point withheld: claimed it returns an array "with case names as array keys" — it returns a plain list in declaration order; from/tryFrom failure behavior correct |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | |
| php-oop-02 | oop | 3 | 3 | 3 | 1.00 | readonly class extended only by readonly class present |
| php-oop-03 | oop | 2 | 3 | 3 | 1.00 | |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | "captured at definition time" only implicit, but by-value vs auto-capture contrast is clear |
| php-closures-02 | closures | 2 | 2 | 2 | 1.00 | |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | rubric points present, but the example `[$head, ...$tail] = $values;` is invalid PHP (spread is not allowed in destructuring) — not penalized, flagged |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | |
| php-null-03 | null-safety | 1 | 2 | 2 | 1.00 | calls `??=` "semantically equivalent" to `$x = $x ?? $y`; misses the evaluate-target-once nuance, but both rubric points present |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | |
| php-match-03 | match-control | 1 | 2 | 1 | 0.50 | fallthrough / shared-block reason present; never states that match arms are single expressions so switch fits multi-statement branches |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 1.00 | 5 | ok | omit (strong) |
| enums | 0.90 | 4 | ok | omit (above threshold) |
| oop | 1.00 | 5 | ok | omit (strong) |
| closures | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 1.00 | 4 | ok | omit (strong) |
| match-control | 0.93 | 4 | ok | omit (above threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.83 / 66 = 98.2%
```

## Derivation targets

No tag scored below threshold (`< 0.75`). Every tag cleared 0.75, so **no
derived skill is written** for `space-bunny-free` on `php` — see
`rubric-guide.md` ("Tags with subscore ≥ 0.75 are omitted").

Contamination check: answers are not verbatim copies of the reference. Wording
and examples diverge throughout (types-02 uses `"0" == 0` where the key uses
`0 == "foo"`; closures-02 mentions `call_user_func()`, absent from the key), and
the answer file contains errors the key does not (enums-02 "case names as array
keys", arrays-02 spread inside destructuring). The near-perfect score reads as
genuine PHP knowledge, not key leakage. Weakest spots (all above threshold,
kept as scorecard notes only): `enums` (0.90 — wrong `cases()` return shape),
`match-control` (0.93 — missed the single-expression-arm reason for choosing
`switch`).
