---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: php
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. muse-spark-1.2 has no "/", so the filename is unchanged. -->

# Scorecard — muse-spark-1.2 on php

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | |
| php-types-02 | types | 3 | 3 | 3 | 1.00 | gave own juggling examples ("123"==123, "0e12345"=="0e54321", in_array) |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | |
| php-types-05 | types | 1 | 2 | 2 | 1.00 | |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | |
| php-oop-02 | oop | 3 | 3 | 2 | 0.67 | got write-once + readonly-class semantics but never contrasted "runtime-set, not a constant" nor the readonly-class-extended-only-by-readonly-class rule |
| php-oop-03 | oop | 2 | 3 | 3 | 1.00 | |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | some visible self-correcting hedging mid-answer but both concepts land |
| php-closures-02 | closures | 2 | 2 | 1 | 0.50 | got "produces a Closure without invoking", missed that it replaces the old string/array-callable forms with a type-safe reference |
| php-closures-03 | closures | 1 | 2 | 1 | 0.50 | got scope-binding grants private/protected access, missed that bind/bindTo return a NEW closure leaving the original unchanged |
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
| php-null-03 | null-safety | 1 | 2 | 2 | 1.00 | |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | gave the requested isset/empty divergence example (0) unprompted |
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
| oop | 0.89 | 5 | ok | omit (strong) |
| closures | 0.79 | 4 | ok | omit (above threshold) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 1.00 | 4 | ok | omit (strong) |
| match-control | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.51 / 66 = 96.2%
```

## Derivation targets

No tag fell below threshold (`< 0.75`) — the lowest, `closures` at 0.79, still
clears it. No derived skill file was written.

## Contamination check

The overall score (96.2%) is high but not suspicious:

- **Closures scored 0.79**, the weakest tag, with two questions losing half
  credit each (`php-closures-02`, `php-closures-03`) on real, specific gaps
  (Closure-vs-old-callable-form contrast; bind/bindTo returning a new closure)
  — not a flat, evenly-distributed near-100% that would suggest key exposure.
- Answers use the model's own phrasing throughout, not rubric language —
  e.g. it reasons out loud with visible uncertainty and self-correction
  (`php-closures-01`: "Actually by-value copy is taken when arrow is
  defined? ... but effectively value-bound"; `php-arrays-02`: "integer keys
  are still reindexed sequentially? ... In array unpacking ints are
  reindexed"; `php-null-04`: works through the isset/empty example live
  rather than stating a pre-packaged answer). This is inconsistent with
  reciting a memorized answer key and consistent with a strong model
  reasoning from training knowledge.
- PHP 8.0–8.4 core language features (types, enums, closures, match,
  null-safety) are extremely well-documented, high-frequency material in
  public PHP docs/tutorials/RFCs — a strong general-purpose coding model
  scoring ~96% on it, while still missing specific secondary details
  (readonly-class extension rule, Closure-vs-string-callable contrast,
  bind/bindTo immutability), reads as plausible genuine competence rather
  than corpus leakage.
- Answerer log confirms zero tool invocations (pure isolated LLM completion,
  no repo/web access) — see `/tmp/kaizen-php-answer/raw_output.log`.

No contamination flag raised; the score is accepted as-is.
