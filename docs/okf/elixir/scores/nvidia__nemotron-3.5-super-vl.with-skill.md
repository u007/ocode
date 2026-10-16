---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: elixir
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — nvidia/nemotron-3.5-super-vl on elixir (with-skill validation)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers:
> `elixir/answers/nvidia__nemotron-3.5-super-vl.with-skill.md`, produced
> closed-book with the current derived skill
> `../derived/elixir.nvidia__nemotron-3.5-super-vl.SKILL.md`
> (`elixir-tuning-nvidia-nemotron-3.5-super-vl`) prepended to the answerer
> prompt as active guidance (front matter stripped; audited 1 user + 1
> assistant message, zero tool calls). Baseline:
> `scores/nvidia__nemotron-3.5-super-vl.md`. No new derived skill is produced
> from this run and the existing one is unchanged.

> Target tags for this skill (baseline < 0.85): **pattern-matching, pipe-with**.

## Provenance

Closed-book (Rule 0). Same harness as the baseline:
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under
an isolated HOME whose config denies every tool, scratch cwd holding only the
`_prompts/elixir.md` sheet with the derived skill body prepended as active
guidance, one fresh session with exactly 1 user + 1 assistant message and 0
tool calls. All 32 ids came back once, none empty, no duplicates. No answer
cites a tool or skill the question never named; the OpenRouter `:free` route
variant is not part of the model id.

Answers are still one paragraph each but noticeably fuller than the baseline.
They remain the model's own wording: two fresh factual slips not in the answer
key or the skill (a non-existent `@on_type` fallback in proto-01; `Task.async`
linking described as optional in conc-02) rule out answer-key contamination.
The skill's content appears in the model's own phrasing on the four target
questions, not pasted.

Grading rule applied (same strictness as the baseline): a point is withheld
when the concept is absent, or when the answer states the concept but also
makes a materially wrong claim about the same construct; tangential wrong
asides are noted only. Compound points whose primary concept is present with a
secondary clause omitted are awarded and the omission noted. Where the
baseline made a `(judgment)` call, the same standard is applied here.

## History

- Earlier with-skill scorecard: none (first with-skill validation for
  nvidia/nemotron-3.5-super-vl on elixir).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 3 | 1.00 | now opens with "a pattern match, not an assignment" and gives `1 = x` as an equality assertion; `MatchError` named. Baseline's withheld point earned |
| elixir-pm-02 | pattern-matching | 3 | 3 | 3 | 1.00 | guard whitelist stated (operators, `is_*/1`, `in` with literals, Kernel subset; "your own functions are never allowed, pure or not"); `FunctionClauseError` named. Both baseline gaps closed |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | `send` now "returns immediately — it is asynchronous"; the baseline's invented `pid <- msg` form is gone. Baseline's withheld point earned (outside the skill's scope) |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | return shape still not stated; aside: a GenServer `:timeout` message comes from a callback's timeout return value, not from `receive ... after` |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | "both return a new map / no mutation" still never stated (same miss as baseline; not a skill target) |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | `update_in` "applies a function" now explicit |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | first-argument rule plus "exists for readability — top-to-bottom sequence of transformations instead of inside-out nesting". Baseline's withheld point earned |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | no longer mentions the no-matching-`else` error; not a rubric point |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | `then/2` with a worked example and a named intermediate both given; "never use `with` just to fix argument order"; single-call case added. Baseline's withheld point earned |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | now gives the choice rule: bang "for callers where failure is exceptional and should crash, often under a supervisor". Baseline's withheld point earned (outside the skill's scope) |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | `catch` kinds `:throw`/`:exit`/`:error` correct; the baseline's "older mechanism" aside is gone |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | "still use tuples for expected errors" still not stated (same as baseline) |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | now says enumeration "pulls elements through the chain one at a time"; the Enum-side intermediate-list cost is only implied. Baseline's withheld point earned on the element-at-a-time half |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | aside: an "`@on_type` fallback" does not exist (the fallback is `@fallback_to_any true` plus `defimpl ..., for: Any`); dispatch and per-type `defimpl` points intact |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | "warns on missing callbacks" still only implied by "the compiler checks" |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | fan-out/await-several point still absent. Linked-failure/timeout point awarded under the baseline standard but imprecise in a new way: says the task "keeps running unless it is linked to the caller" — `Task.async` always links, so on an `await` timeout the caller exits and the task dies with it; "raises the same exception" is really a linked exit; default 5 s still not given |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | delta | target? |
|-----|---------:|-----------:|--:|-------|------:|---------|
| pattern-matching | 0.70 | 1.00 | 4 | ok | +0.30 | **yes** |
| processes-otp | 0.95 | 1.00 | 6 | ok | +0.05 | no |
| immutability-data | 0.93 | 0.93 | 5 | ok | 0.00 | no |
| pipe-with | 0.81 | 1.00 | 4 | ok | +0.19 | **yes** |
| error-handling | 0.93 | 1.00 | 6 | ok | +0.07 | no |
| enum-stream | 0.89 | 1.00 | 4 | ok | +0.11 | no |
| protocols-behaviours | 1.00 | 1.00 | 4 | ok | 0.00 | no |
| concurrency | 0.87 | 0.93 | 5 | ok | +0.07 | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pattern-matching 10/10; processes-otp 14/14; immutability-data
9.33/10; pipe-with 8/8; error-handling 15/15; enum-stream 9/9;
protocols-behaviours 8/8; concurrency 9.33/10.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69.67 / 71 = 98.1%
```

(Loss: data-03 −0.67, conc-02 −0.67.) Baseline 62.5 / 71 = 88.0%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| pattern-matching | 0.70 | 1.00 | **PASS** |
| pipe-with | 0.81 | 1.00 | **PASS** |

Every target tag reaches 0.85. The skill is validated for
nvidia/nemotron-3.5-super-vl @ 3.5. All five baseline misses inside the targets
(pm-01 match-not-assignment, pm-02 guard whitelist and `FunctionClauseError`,
pipe-01 readability rationale, pipe-04 `then/2` / named intermediate) are
corrected in the model's own words, and the two `(judgment)` withholds that
decided derivation (pipe-01, pipe-04) are no longer close calls.

Non-target drift, all upward or flat: processes-otp 0.95 → 1.00 (otp-01 now
calls `send` asynchronous and drops the invented `<-` form), error-handling
0.93 → 1.00 (error-02 bang-choice rule), enum-stream 0.89 → 1.00 (enum-01
element-at-a-time), concurrency 0.87 → 0.93 (otp-01 only; conc-02 unchanged),
immutability-data unchanged (data-03 same miss). None of these is covered by
the skill; single-sample movement, not acted on.
