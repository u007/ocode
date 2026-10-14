---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: elixir
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
iteration: 2
---

# Scorecard — cat-jam-free on elixir (with-skill validation, iteration 2)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN — ITERATION 2.** Answers:
> `elixir/answers/cat-jam-free.with-skill.md`, produced closed-book with the
> derived skill `derived/elixir.cat-jam-free.SKILL.md`
> (`elixir-tuning-cat-jam-free`) prepended to the answerer prompt as active
> guidance. Baseline: `scores/cat-jam-free.md`. Iteration 1:
> `scores/cat-jam-free.with-skill.iter1.md` (FAIL: pattern-matching 0.80). The
> skill was then sharpened: the pattern-matching section now opens with
> must-include checklist items for `=` (assertion + `1 = x` equality check) and
> for guards (fixed whitelist of guard-safe expressions), mirroring the `with`
> wording that pipe-with absorbed in iteration 1. No new derived skill is
> produced from this run.

> Target tags for this skill (baseline < 0.85): **pattern-matching, pipe-with**.

## Provenance

The answerer was `ocode run -m opencode-go/cat-jam-free -effort med` with the
SKILL.md body (front matter stripped) prepended to the `_prompts/elixir.md`
sheet, in an isolated HOME with every tool denied and a cwd holding only the
sheet. The session was audited at 1 user + 1 assistant message and 0 tool calls.
Unlike the baseline and iteration 1, this run answered all 32 ids in one
session; no re-ask was needed (`elixir-pm-04` included). The model appended a
recap block after the YAML fence; the harvester excluded it and it was not
graded.

Graded from `../answers/cat-jam-free.with-skill.md`: 32/32 ids present, no
duplicates, no truncation. Answers are a little fuller than iteration 1 (two to
three sentences), still in the model's own words, and miss points by omission
rather than by stating wrong facts.

Grading rule (same strictness as the baseline and iteration 1): a point is
withheld when the answer states the concept but also makes a materially wrong
claim about the same construct, and no point is awarded for echoing the
question's own premise. Verdict-relevant calls are marked `(judgment)`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | bind + `MatchError` correct. Now frames `=` as "a pattern match: it asserts that…", but still never says it is not assignment and gives no `1 = x` / bound-value equality check; "asserts the first element is `:ok`" describes the shape match, as in iteration 1 (judgment; awarding it would put pattern-matching at 1.00 — the verdict does not depend on it) |
| elixir-pm-02 | pattern-matching | 3 | 3 | 3 | 1.00 | top-to-bottom + first pattern-and-guard wins; "the guard is limited to a fixed whitelist of guard-safe expressions" (iteration-1 gap closed); `FunctionClauseError` |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | answered in the sheet run this time |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | mailbox now named |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | `{:noreply, state}` now stated |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | `__struct__` not named, concept present (same as before) |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | `KeyError` + insert-or-update correct; still no "both return a new map" (same as iteration 1) |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | first-argument rule + "turn nested expressions into a top-to-bottom data pipeline" |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | short-circuits AND "that non-matching value becomes the result of the `with`" — both halves now in this answer |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | returned unchanged; `else` transforms; `WithClauseError` |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | non-first position + `then/2` / named variable + don't pipe a single call |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | returns-tuple-vs-raises + "plain version is for conditional handling; the bang version asserts success" (judgment; first time a when-to-choose line appears; non-target) |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | `after` now "whether the body succeeds or raises" (iteration-1 narrowing gone) |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | Enum "often allocating intermediate collections"; Stream computes values as pulled with roughly constant memory (gap from baseline/iteration 1 closed) |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | now mentions multiple generators and pattern matching in generators |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 1 | 0.50 | runtime type dispatch + per-type `defimpl` (incl. structs, built-ins); no open/extensible (new types can implement it) point (same as before) |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | async/await, re-raise, timeout; still no running-several-in-parallel point (same as before) |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | iter 1 | with-skill (iter 2) | n | trust | delta vs baseline | target? |
|-----|---------:|-------:|--------------------:|--:|-------|------:|---------|
| pattern-matching | 0.70 | 0.80 | 0.90 | 4 | ok | +0.20 | **yes** |
| processes-otp | 1.00 | 1.00 | 1.00 | 6 | ok | 0.00 | no |
| immutability-data | 1.00 | 0.93 | 0.93 | 5 | ok | −0.07 | no |
| pipe-with | 0.75 | 1.00 | 1.00 | 4 | ok | +0.25 | **yes** |
| error-handling | 0.87 | 0.93 | 1.00 | 6 | ok | +0.13 | no |
| enum-stream | 0.89 | 0.89 | 1.00 | 4 | ok | +0.11 | no |
| protocols-behaviours | 0.88 | 0.88 | 0.88 | 4 | ok | 0.00 | no |
| concurrency | 0.93 | 0.93 | 0.93 | 5 | ok | 0.00 | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pattern-matching 9/10; processes-otp 14/14; immutability-data
9.33/10; pipe-with 8/8; error-handling 15/15; enum-stream 9/9;
protocols-behaviours 7/8; concurrency 9.33/10.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.67 / 71 = 95.3%
```

(Losses: pm-01 −1, data-03 −0.67, proto-01 −1, conc-02 −0.67.) Baseline
62.33 / 71 = 87.8%; iteration 1 64.67 / 71 = 91.1%.

## Target-tag verdict

| tag | baseline | iter 1 | iter 2 | pass (≥ 0.85)? |
|-----|---------:|-------:|-------:|----------------|
| pattern-matching | 0.70 | 0.80 | 0.90 | **PASS** |
| pipe-with | 0.75 | 1.00 | 1.00 | **PASS** |

**PASS.** Both target tags reach the threshold on iteration 2. The sharpened
guard checklist item was absorbed: pm-02 now states the guard whitelist and
`FunctionClauseError`, closing both of its baseline gaps. pipe-with holds at
1.00, and pipe-02 now carries the full failure path in one answer.

One directive is still only partly absorbed: pm-01 picked up the "asserts"
framing but not the explicit "not assignment / `1 = x` checks equality"
statement, so it stays at 2/3 under the baseline's strictness. The tag passes
without that point (0.90); awarding it would give 1.00. If the skill is tuned
again, that is the line to watch, but no change is required for this verdict.

Non-target drift (not acted on): error-handling 0.87→1.00 (error-02 gained a
when-to-choose-bang line) and enum-stream 0.89→1.00 (enum-01 named the
intermediate collections). immutability-data stays at 0.93 because data-03
still omits "both return a new map", the same as iteration 1.
protocols-behaviours and concurrency are unchanged from baseline. This is
single-sample variance and was not acted on.
