---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: php
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — mimo-v2.5 on php

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | |
| php-types-02 | types | 3 | 3 | 3 | 1.00 | juggling example matches reference framing; also asserted `0 == ""` is true, which is stale for 8.0+ (not penalized, required example present elsewhere) |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | omits "declaration order" nuance for cases(), core concept present |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | |
| php-oop-02 | oop | 3 | 3 | 2 | 0.67 | missed runtime-set-not-a-constant / readonly-class-extended-only-by-readonly-class point |
| php-oop-03 | oop | 2 | 3 | 2 | 0.67 | LSB via `static::` dispatch explained, but missed the `new static()` vs `new self()` instantiation illustration |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | |
| php-closures-02 | closures | 2 | 2 | 1 | 0.50 | missed that `(...)` replaces string/array callables with a type-safe reference |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | |
| php-null-03 | null-safety | 1 | 2 | 1 | 0.50 | missed the lazy-RHS-evaluation nuance of `??=` |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | |
| php-match-03 | match-control | 1 | 2 | 1 | 0.50 | fallthrough point covered; incorrectly claimed match arms can hold multiple statements "with braces" |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 1.00 | 5 | ok | omit (strong) |
| enums | 1.00 | 4 | ok | omit (strong) |
| oop | 0.81 | 5 | ok | omit (above threshold) |
| closures | 0.86 | 4 | ok | omit (above threshold) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 0.94 | 4 | ok | omit (above threshold) |
| match-control | 0.93 | 4 | ok | omit (above threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 62.33 / 66 = 94.4%
```

## Derivation targets

No tag scored below threshold (`< 0.75`). Every tag cleared 0.75, so **no
derived skill is written** for `mimo-v2.5` on `php` — see `rubric-guide.md`
("Tags with subscore ≥ 0.75 are omitted"). Weakest spots observed (all still
above threshold, kept here as scorecard notes only, not skill content):
`oop` (0.81 — missed the runtime-set/readonly-class-extension nuance on
`readonly`, and the `new static()` vs `new self()` instantiation illustration
of late static binding), `closures` (0.86 — missed that first-class callable
syntax replaces the old string/array callable forms with a type-safe
reference), `match-control` (0.93 — incorrectly claimed `match` arms can hold
multiple statements "with braces", contradicting the single-expression rule).
