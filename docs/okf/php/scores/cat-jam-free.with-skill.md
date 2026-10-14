---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: php
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on php (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `php/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the derived skill `derived/php.cat-jam-free.SKILL.md`
> (`php-tuning-cat-jam-free`) prepended to the answerer prompt as active guidance
> (audited zero tool calls). Baseline: `scores/cat-jam-free.md`. No new derived skill
> is produced from this run.

> Target tags for this skill (baseline < 0.85): **oop**.

Graded against `php/questions.yaml` corpus_rev 1. Grading rule applied (same
strictness as the baseline): a `point` whose core concept is genuinely present earns
full credit (minor missing sub-items are noted); a `point` contradicted by a materially
wrong claim about the same construct earns 0. Points are whole; fractions come only
from a listed partial. Grader judgment calls are marked "(judgment)".

## Provenance

Closed-book. The answerer was `ocode run -m opencode-go/cat-jam-free -effort med` with
the SKILL.md body (front matter stripped) prepended to the `_prompts/php.md` sheet, in an
isolated HOME with every tool denied and a cwd holding only the sheet. The session sqlite
shows exactly 1 user + 1 assistant message and 0 tool calls. All 34 answers came back.

## History

- Earlier with-skill scorecard: none (first with-skill validation for cat-jam-free on php).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | first statement, file-only scope; strict TypeError vs coercive conversion |
| php-types-02 | types | 3 | 3 | 3 | 1.00 | example now correct: `0 == "foo"` true in PHP 7, false since PHP 8. (judgment) also lists `'' == 0` among loose surprises, which is false in PHP 8; the correct 8.0-change example carries the point |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | 8.3 present; override rule stated as "same type / invariant" — stricter than PHP's actual compatibility rule, credited as the baseline credited "changing the declared type is not allowed" |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | "cannot be extended or subclassed" now present (baseline 0.67) |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | visibility/readonly on a constructor param declares and assigns the property |
| php-oop-02 | oop | 3 | 3 | 3 | 1.00 | typed, write-once from declaring scope, `Error` on rewrite; readonly class = all readonly + no dynamic props; readonly-only extension rule. Baseline's "constant default at declaration" error is gone |
| php-oop-03 | oop | 2 | 3 | 3 | 1.00 | self compile-time vs static runtime/LSB. (judgment) third point awarded for "factory and fluent methods in parent classes incorrectly creating ... the parent instead of a subclass" — the new self()/new static() contrast in prose, though neither expression is written out |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | "catches the bug" for typos/upstream renames; compile-time not stated, same as baseline |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | |
| php-closures-02 | closures | 2 | 2 | 1 | 0.50 | produces a Closure without calling; no string/array-callable replacement (same as baseline) |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | "copy" with new `$this`; scope grants private/protected access |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | catching Exception misses Error not stated explicitly (same as baseline) |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | numeric-key reindexing now mentioned |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | "numeric strings become integers" slightly over-broad (only canonical int strings) |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | no-notice for `??` only implied by contrast with `?:` |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | |
| php-null-03 | null-safety | 1 | 2 | 1 | 0.50 | only-if-null present; lazy right-hand side never stated (difference framed as "does not write") — same as baseline |
| php-null-04 | null-safety | 2 | 3 | 2 | 0.67 | isset/empty definitions ok; disagreement example `$x = 'abc'` (isset true, empty false) is not a falsy-but-set value — they agree it holds something. **Regression** vs baseline (which gave `$x = 0`, 1.00) |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | `match(0)` hits only int 0, not `false`/`null`/`'0'`; loose `switch` groups those under `case 0` — correct in PHP 8. Baseline's PHP 7 `switch(0)` / `'foo'` error is gone |
| php-match-03 | match-control | 1 | 2 | 2 | 1.00 | |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| types | 0.91 | 1.00 | +0.09 | 5 | ok | no |
| enums | 0.90 | 1.00 | +0.10 | 4 | ok | no |
| oop | 0.70 | 1.00 | +0.30 | 5 | ok | **yes** |
| closures | 0.86 | 0.86 | +0.00 | 4 | ok | no |
| error-handling | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| arrays | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| null-safety | 0.94 | 0.85 | −0.09 | 4 | ok | no |
| match-control | 0.86 | 1.00 | +0.14 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: types 11/11; enums 7/7; oop 9/9; closures 6/7; error-handling 8/8;
arrays 9/9; null-safety 6.83/8 (0.854); match-control 7/7.

Sensitivity: if the php-oop-03 judgment call were withheld, oop = 8.33/9 = 0.93 — still
above threshold.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.83 / 66 = 96.7%
```

(Losses: closures-02 −1.0, null-04 −0.67, null-03 −0.5.) Baseline 59.17 / 66 = 89.6%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| oop | 0.70 | 1.00 | **PASS** |

**PASS.** The only target tag reaches 0.85. The skill is validated for cat-jam-free @ alpha.

The skill's two corrections were absorbed: php-oop-02 drops the baseline's false
"readonly property with a constant default" claim and now states typed, write-once,
`Error` on rewrite, and the readonly-class extension rule (0.33 → 1.00); php-oop-03 adds
the parent-vs-subclass factory consequence of LSB (0.67 → 1.00, judgment).

Non-target drift (not a reason to edit the skill): types, enums and match-control moved
up — notably the PHP 7 number-vs-string error from the baseline (types-02, match-02) did
not recur this sample. null-safety moved down 0.94 → 0.85 on one question (null-04 picked
a non-falsy disagreement example); it still sits at the threshold and is a single-sample
resample on a question the skill does not cover. closures-02 (callable-replacement point)
missed again in both runs; watch on the next baseline.
