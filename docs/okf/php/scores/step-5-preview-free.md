---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: php
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on php

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book: the answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/php.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | |
| php-types-02 | types | 3 | 3 | 3 | 1.00 | |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | invents `abstract const`; harmless to the rubric |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | all three points present; side claims shaky (enum constants "since 8.2" — they are 8.1; "cannot be serialized" — they can) |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | both points present; wrongly says enum cases are valid array keys (they are an illegal offset type) |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | bogus "callable promoted params before 8.2" caveat; not rubric-relevant |
| php-oop-02 | oop | 3 | 3 | 2 | 0.67 | no runtime-set/not-a-constant contrast and no readonly-class-extends-only-readonly rule; also wrongly says readonly properties may declare defaults |
| php-oop-03 | oop | 2 | 3 | 3 | 1.00 | |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | trailing sentence about `$this` is garbled but the two points are clean |
| php-closures-02 | closures | 2 | 2 | 2 | 1.00 | |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | mid-sentence self-correction ("extends Error... actually extends nothing") lands on the right hierarchy |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | claims `$previous` arrived "since PHP 8.0" (it is 5.3); not rubric-relevant |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | spread/8.1 string keys/dupes-overwrite and keyed destructuring present; wrongly says duplicate int keys are a fatal error (they are renumbered) |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | all four coercions right; wrongly says whitespace-padded integer strings are cast (`" 1"` stays a string) |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | both points present; wrongly says `?->` supports writes (write context is a compile error) |
| php-null-03 | null-safety | 1 | 2 | 1 | 0.50 | only-if-null/no-op present; never states the right-hand side is evaluated only when needed (focused entirely on single evaluation of the left-hand side) |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | record key is `Answer:` not `answer:` (see Anomalies); arms written in `case` syntax but both concepts present |
| php-match-03 | match-control | 1 | 2 | 2 | 1.00 | |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 1.00 | 5 | ok | omit (strong) |
| enums | 1.00 | 4 | ok | omit (strong) |
| oop | 0.89 | 5 | ok | omit (above threshold) |
| closures | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 0.94 | 4 | ok | omit (strong) |
| match-control | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.5 / 66 = 97.7%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none** — no derived skill is warranted.
Nearest to threshold: oop (0.89), null-safety (0.94).

## Anomalies

- `php-match-02` is keyed `Answer:` (capital A). A strict id→`answer` loader would
  report that record as missing; graded from the text anyway.
- The file is not valid YAML as a whole: a stray closing code fence and an
  `<!--END-->` marker follow the last record, then a chatty sign-off offering a
  "second sheet" of less-certain items. Grading was unaffected.
- No contamination signals: answers are far longer than the references, use
  different examples (`"10" == "1e1"`, grade bands for `match(true)`), and carry
  factual errors absent from the key (enum array keys, readonly defaults,
  duplicate-int-key fatal, nullsafe writes). Nothing reads as a copy.
- Recurring pattern: every rubric concept is hit, but roughly a third of the
  answers add a confident, wrong side claim. None of those claims touched a
  rubric point, so they did not move the score; they are listed in the notes
  column for anyone revisiting this model's reliability on PHP edge rules.
