---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: php
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on php

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book: the answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch dir with only the `_prompts/php.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. Headless runs
inject ocode's bundled skill-catalogue digest into the system prompt. The model is an
opencode-go free/alpha model with no published version; it self-identifies as
"CatJam, developed by LitterBox AI".

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | omits the caller-file rule and int→float widening; not rubric points |
| php-types-02 | types | 3 | 3 | 2 | 0.67 | example withheld: claims `0 == "php"` is true; since PHP 8.0 a non-numeric string compares as strings, so it is false |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-03 | enums | 2 | 3 | 2 | 0.67 | says enums "cannot extend another class" — never that they are implicitly final / cannot BE extended |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | modifier trigger shown only via the `public` example |
| php-oop-02 | oop | 3 | 3 | 1 | 0.33 | write-once point withheld: wrongly says a readonly property may be initialized "with a constant default at declaration" (a readonly property cannot have a default); no typed requirement, no runtime-set/not-a-constant contrast, no readonly-class extension rule |
| php-oop-03 | oop | 2 | 3 | 2 | 0.67 | $this/self/static and LSB present; never contrasts `new static()` with `new self()` |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | |
| php-closures-02 | closures | 2 | 2 | 1 | 0.50 | produces a Closure; no mention that it replaces string/array callables with a type-safe reference |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | "create a closure" = new closure; scope grants private access |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | does not state explicitly that catching Exception misses Error, but catch Throwable / Exception\|Error is correct |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | `getPrevious()` not named; "preserves the original error context" satisfies the alternative |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | int-key renumbering not mentioned |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | minor: says `?->` accesses an "element" (there is no nullsafe array access) |
| php-null-03 | null-safety | 1 | 2 | 1 | 0.50 | only-if-null present; never says the right-hand side is evaluated only when needed (focuses on single evaluation of the left side) |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 1 | 0.50 | no-juggling point present; second point withheld: its only concrete case, `switch(0)` hitting `case 'foo'`, is false in PHP 8; no exact-type/cast consequence |
| php-match-03 | match-control | 1 | 2 | 2 | 1.00 | |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 0.91 | 5 | ok | omit (above threshold) |
| enums | 0.90 | 4 | ok | omit (above threshold) |
| oop | 0.70 | 5 | ok | **derive** |
| closures | 0.86 | 4 | ok | omit (borderline) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 0.94 | 4 | ok | omit (strong) |
| match-control | 0.86 | 4 | ok | omit (borderline) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 59.17 / 66 = 89.6%
```

## Derivation targets

Tags below threshold (`< 0.85`): **oop** → `derived/php.cat-jam-free.SKILL.md`.
Nearest to threshold: closures (0.857), match-control (0.857).

## Notes

- Recurring error: pre-PHP-8 number-vs-string comparison. Both `php-types-02`
  (`0 == "php"` true) and `php-match-02` (`switch(0)` matching `case 'foo'`) rest
  on PHP 7 semantics. The same wrong rule cost points in two tags that both still
  pass, so it is not in the derived skill. Re-check it on the next baseline.
- Answers are short, one paragraph each, and miss secondary rubric points
  (`new static()` vs `new self()`, string/array callable replacement, lazy RHS of
  `??=`) rather than stating them wrongly.
- No contamination signals: wording and examples differ from the key, and the
  answers contain errors that are not in the key.
