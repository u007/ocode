---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: elixir
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on elixir (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `elixir/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the derived skill
> `derived/elixir.cat-jam-free.SKILL.md` (`elixir-tuning-cat-jam-free`)
> prepended to the answerer prompt as active guidance. Baseline:
> `scores/cat-jam-free.md`. No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **pattern-matching, pipe-with**.

## Provenance

The answerer was `ocode run -m opencode-go/cat-jam-free -effort med` with the
SKILL.md body (front matter stripped) prepended to the `_prompts/elixir.md`
sheet, in an isolated HOME with every tool denied and a cwd holding only the
sheet. The session was audited at 1 user + 1 assistant message and 0 tool calls.
As in the baseline, the model skipped `elixir-pm-04` in the sheet run. It was
re-asked alone with the skill prepended (tool-free) and the answer was appended
to the answers file.

Graded from `../answers/cat-jam-free.with-skill.md`: 32/32 ids present, no
duplicates, no truncation. The answers are even terser than the baseline (one
to two sentences each), and they miss points by omission, not by stating wrong
facts.

Grading rule (same strictness as the baseline): a point is withheld when the
answer states the concept but also makes a materially wrong claim about the same
construct, and no point is awarded for echoing the question's own premise.
Verdict-relevant calls are marked `(judgment)`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | bind + `MatchError` correct. "Not assignment / `1 = x` checks equality" point withheld: the answer drops even the baseline's "not assignment" and never says a literal or bound value on the left is an equality assertion. "Binds `value` only if the first element is `:ok`" only describes the shape match (judgment; awarding it would put pattern-matching at 0.90). Aside: "no clause matches" is the wrong word for a bare `=` |
| elixir-pm-02 | pattern-matching | 3 | 3 | 2 | 0.67 | top-to-bottom + `FunctionClauseError` (baseline gap now closed). Still nothing on guards being limited to a restricted whitelist, though the skill states it explicitly |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | re-asked alone with skill; map subset vs exact tuple arity / list length |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | mailbox not named; receive matching + blocking + `after` present |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | raw send / timers / monitor examples; `{:noreply, state}` not stated, awarded on examples as in the baseline |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | `__struct__` not named, concept present (same as baseline) |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | `KeyError` now named. Dropped the baseline's "both return new maps; neither mutates" (point withheld) |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | first-argument rule + "flattening nested calls so data flows top to bottom". Baseline gap closed |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | now says it "short-circuit[s] on the first non-match". The "returns that value" half is stated in pipe-03, not here (judgment; withholding it would still leave pipe-with at 0.875) |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | non-match returned unchanged; `else` transforms; `WithClauseError` correct |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | non-first-position case + `then/2` / intermediate variable. Aside: `bar(y, _)` is not valid syntax (illustrative only) |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 1 | 0.50 | returns-tuple-vs-raises correct; still nothing on when to choose the bang form (same as baseline) |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | rescue vs catch (throws/exits) correct. Aside: `after` is said to run "on normal completion or when an exception is rescued", which narrows the always-runs guarantee (it also runs on an unrescued raise). Awarded as an omission, not a wrong claim |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-enum-01 | enum-stream | 3 | 3 | 2 | 0.67 | eager vs lazy correct; still no intermediate-list / element-at-a-time point (same as baseline) |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 1 | 0.50 | type-based dispatch + `defimpl` per type; no open/extensible point (same as baseline) |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | aside: "they do not perform runtime dispatch" is loose. Behaviour dispatch is by an explicit (often runtime-chosen) module |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | async/await + failure/timeout propagation; no running-several-in-parallel point (same as baseline) |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | delta | target? |
|-----|---------:|-----------:|--:|-------|------:|---------|
| pattern-matching | 0.70 | 0.80 | 4 | ok | +0.10 | **yes** |
| processes-otp | 1.00 | 1.00 | 6 | ok | 0.00 | no |
| immutability-data | 1.00 | 0.93 | 5 | ok | −0.07 | no |
| pipe-with | 0.75 | 1.00 | 4 | ok | +0.25 | **yes** |
| error-handling | 0.87 | 0.93 | 6 | ok | +0.07 | no |
| enum-stream | 0.89 | 0.89 | 4 | ok | 0.00 | no |
| protocols-behaviours | 0.88 | 0.88 | 4 | ok | 0.00 | no |
| concurrency | 0.93 | 0.93 | 5 | ok | 0.00 | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pattern-matching 8/10; processes-otp 14/14; immutability-data
9.33/10; pipe-with 8/8; error-handling 14/15; enum-stream 8/9;
protocols-behaviours 7/8; concurrency 9.33/10.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.67 / 71 = 91.1%
```

(Losses: pm-01 −1, pm-02 −1, data-03 −0.67, error-02 −1, enum-01 −1,
proto-01 −1, conc-02 −0.67.) Baseline 62.33 / 71 = 87.8%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| pattern-matching | 0.70 | 0.80 | **FAIL** |
| pipe-with | 0.75 | 1.00 | **PASS** |

**FAIL.** pipe-with passes. Both of its baseline gaps (the pipe's flattening
purpose and `with` short-circuiting) are now stated in the model's own words.
pattern-matching improves but stays below 0.85. The model absorbed only one of
the three pattern-matching directives (`FunctionClauseError` in pm-02). Two are
still missing:

- **pm-01**: the answer never says that `=` is an assertion and not a store, and
  never gives the `1 = x` equality check, even though the skill says "Say this
  when explaining `=`, don't just repeat 'not assignment'". The answer drops
  "not assignment" altogether.
- **pm-02**: the answer never says that guards are limited to a whitelist of
  guard-safe expressions, although the skill has a dedicated section on it.

The verdict depends on one call: awarding the pm-01 point would put the tag at
exactly 0.90 and the run would pass. Under the baseline's strictness the point
is withheld. Even without that call, pm-02's guard-whitelist omission is a clear
miss. Suggested tweak (not applied here): turn the two pattern-matching bullets
into must-include checklist items for answers about `=` and multi-clause
functions, as the `with` section does with "Every explanation of `with` must
include…". That wording is what pipe-with absorbed.

Non-target drift: immutability-data fell from 1.00 to 0.93 because data-03
dropped its "both return new maps" sentence. error-handling rose from 0.87 to
0.93 through pipe-02. This is single-sample variance in a terser answer set and
was not acted on.
