---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: php
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on php

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | |
| php-types-02 | types | 3 | 3 | 3 | 1.00 | uses the PHP 7 `"abc" == 0` case and notes the 8.0 change |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | intersection "primarily for interfaces" (class/interface-only restriction implicit, not stated); "nullable types can be used in union types" is loose — `?` cannot prefix a multi-member union |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-03 | enums | 2 | 3 | 2 | 0.67 | implicitly-final point withheld: says enums "cannot extend a class" (wrong direction), never that an enum cannot be extended; also wrongly claims enums cannot use `__call`/`__callStatic` (they can) |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | rubric points present, but claims enum cases are usable as array keys — objects are illegal array offsets (TypeError); not penalized, flagged |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | |
| php-oop-02 | oop | 3 | 3 | 3 | 1.00 | readonly class extended only by readonly class present |
| php-oop-03 | oop | 2 | 3 | 3 | 1.00 | |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | |
| php-closures-02 | closures | 2 | 2 | 1 | 0.50 | produces-a-Closure point present; never says it replaces string/array callables (`'strlen'`, `[$obj, 'm']`) with a type-safe reference; example `Closure::fromCallable(strlen)` is missing quotes |
| php-closures-03 | closures | 1 | 2 | 1 | 0.50 | scope-grants-private-access present; describes bind/bindTo as rebinding "the closure's" `$this`, missing that they return a NEW closure and leave the original unchanged |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | return override stated; throw-in-finally not mentioned |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | internals claim of a "doubly-linked list" for order is inaccurate for PHP 7+ (ordered bucket array); not penalized |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | "only int/string keys" not stated outright, but `"1"`→1 coercion present; `false`→0 omitted |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | |
| php-null-03 | null-safety | 1 | 2 | 1 | 0.50 | only-if-null / no-op point present; frames the difference as avoiding a write, never states the right side is evaluated only when needed; calls the two "semantically equivalent" |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | |
| php-match-03 | match-control | 1 | 2 | 1 | 0.50 | fallthrough reason present; never states match arms are single expressions so switch fits multi-statement branches |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 1.00 | 5 | ok | omit (strong) |
| enums | 0.90 | 4 | ok | omit (above threshold) |
| oop | 1.00 | 5 | ok | omit (strong) |
| closures | 0.79 | 4 | ok | omit (above threshold) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 0.94 | 4 | ok | omit (above threshold) |
| match-control | 0.93 | 4 | ok | omit (above threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 62.83 / 66 = 95.2%
```

## Derivation targets

No tag scored below threshold (`< 0.75`). Every tag cleared 0.75, so **no
derived skill is written** for `longcat-2.5-preview-free` on `php` — see
`rubric-guide.md` ("Tags with subscore ≥ 0.75 are omitted").

Contamination check: no leak. Answers are not near-verbatim copies of the
reference — wording and examples diverge throughout (types-02 leads with the
PHP 7 `"abc" == 0` case and `null == []`; match-02 uses `match(true) { 1 => }`,
absent from the key), and the answer file contains errors the key does not
(enums-03 "cannot use `__call`/`__callStatic`", enums-04 enum cases as array
keys, arrays-01 "doubly-linked list"). Session audit: 1 user + 1 assistant
turn, zero tool calls. The high score reads as genuine PHP knowledge.

Weakest spots (all above threshold, kept as scorecard notes only):
`closures` (0.79 — closest to threshold; missed that bind/bindTo return a new
closure and that `(...)` replaces string/array callables), `match-control`
(0.93 — missed the single-expression-arm reason for choosing `switch`),
`enums` (0.90 — never stated enums are implicitly final), `null-safety`
(0.94 — missed the lazy right-hand side of `??=`).
