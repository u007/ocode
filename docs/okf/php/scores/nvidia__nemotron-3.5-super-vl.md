---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: php
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on php

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/php.md` sheet, one fresh session; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 34
ids came back once, none empty. Headless runs inject the bundled skill-catalogue
digest into the system prompt; no answer cites a tool or skill the question never
named. The OpenRouter `:free` route variant is not part of the model id.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | |
| php-types-02 | types | 3 | 3 | 2 | 0.67 | example withheld: claims `0 == "abc"` is true; since PHP 8.0 a non-numeric string compares as a string, so it is false (verified on php 8.5). `null == false` is correct but the headline example rests on PHP 7 semantics |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | "bottom type" not named; never-returns concept present |
| php-types-05 | types | 1 | 2 | 1 | 0.50 | override point withheld (judgment): says the type "must match exactly — constant types are invariant, covariance forbidden". Typed class constants are covariant: `const int\|string X` → child `const int X` compiles (verified on php 8.5); only widening is an error |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | `->value` not named; scalar backing value + persistence use present |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | declaration order not mentioned |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | minor: adds "cannot be serialized" — `serialize()` supports enums since 8.1; not a rubric point |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | modifier trigger shown only via the `public` example |
| php-oop-02 | oop | 3 | 3 | 2 | 0.67 | third point withheld: says readonly classes "cannot be extended" — a readonly class may be extended by another readonly class (verified); runtime-set/not-a-constant never stated; typed requirement omitted |
| php-oop-03 | oop | 2 | 3 | 2 | 0.67 | $this/self/static and compile-time vs runtime present; contrasts `static::create()`/`static::CONST` with `self::` but never `new static()` vs `new self()` (judgment: the rubric point is the instantiation case) |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 1 | 0.50 | arrow-fn point withheld (judgment): says arrow fns capture "by value only (no by-reference option)" and auto-bind `$this`, but never that they capture enclosing variables implicitly with no `use` clause — the differentiator the question asks for |
| php-closures-02 | closures | 2 | 2 | 1 | 0.50 | produces a Closure without invoking; no mention that it replaces string/array callables with a type-safe, scope-respecting reference |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | "clone a closure rebound" = new closure; scope governs private/protected access |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | throw-in-finally not mentioned; return override present |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | `getPrevious()` not named; "does not lose the root cause" satisfies the alternative |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | dupes-overwrite / int-key renumbering not mentioned |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 1 | 0.50 | second point withheld (judgment): prose says "booleans and null become strings"; the worked example then gives `true` → int `1`. Bools become ints; the self-contradicting rule is a materially wrong claim about the construct |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | |
| php-null-03 | null-safety | 1 | 2 | 1 | 0.50 | only-if-null present; never says the right-hand side is evaluated only when needed ("writes only once" is about the left target) |
| php-null-04 | null-safety | 2 | 3 | 2 | 0.67 | disagreement example withheld: picks `null` (isset false, empty true), which is the complementary case; never names a set-but-falsy value such as `0`/`"0"` where both are true |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | concrete miss (`match(0) { "0" => }`) is correct; the upside (no loose false matches) is not mentioned |
| php-match-03 | match-control | 1 | 2 | 2 | 1.00 | |
| php-match-04 | match-control | 1 | 2 | 1 | 0.50 | first point withheld (judgment): says each arm is "evaluated as a boolean" and "any truthy condition yields `true === true`"; `match(true) { 1 => … }` falls to `default` (verified) — arms must be real booleans |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 0.86 | 5 | ok | omit (borderline) |
| enums | 1.00 | 4 | ok | omit (strong) |
| oop | 0.81 | 5 | ok | **derive** |
| closures | 0.71 | 4 | ok | **derive** |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 0.89 | 4 | ok | omit (above threshold) |
| null-safety | 0.85 | 4 | ok | omit (borderline: 0.854) |
| match-control | 0.93 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Per-tag arithmetic: types 9.5/11, enums 7/7, oop 7.33/9, closures 5/7,
error-handling 8/8, arrays 8/9, null-safety 6.83/8, match-control 6.5/7.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 58.17 / 66 = 88.1%
```

## Derivation targets

Tags below threshold (`< 0.85`): **oop, closures** →
`derived/php.nvidia__nemotron-3.5-super-vl.SKILL.md`.
Nearest to threshold: null-safety (0.854), types (0.864).

## Notes

- Deciding judgment calls: `php-oop-03` (static-vs-self shown on method/constant
  dispatch, not on `new`) and `php-closures-01` (implicit capture never stated).
  Awarding either flips its tag above threshold (oop → 0.89, closures → 0.86).
  Both were withheld for consistency with the `cat-jam-free` baseline, which
  withheld the same points for the same omissions. `php-oop-02`'s "cannot be
  extended" error is a real correction regardless of the oop-03 call.
- Recurring error class: PHP 7-era comparison semantics (`0 == "abc"` true), the
  same wrong rule the `cat-jam-free` baseline carried. It cost a point in `types`
  only; the tag still passes, so it is not in the derived skill.
- Four claims were verified against a local php 8.5 CLI before withholding:
  `0 == "abc"` is false; typed-constant narrowing compiles; `match(true)` with an
  int arm hits `default`; a readonly class extends a readonly class.
- Answers are one tight paragraph each, no thinking-aloud, no duplicated answers,
  no fabricated tool output, no cited tools or skills. Misses are mostly omitted
  secondary points (RHS laziness of `??=`, callable-replacement role of `(...)`),
  plus five materially wrong claims: `0 == "abc"`, invariant typed constants,
  readonly classes unextendable, bool keys become strings, truthy `match(true)`
  arms.
- No contamination signals: wording and examples differ from the key, and the
  answers contain errors the key does not.
