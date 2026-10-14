---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: elixir
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on elixir

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

`cat-jam-free` is an opencode-go free/alpha model with no published version; it
self-identifies as "CatJam, developed by LitterBox AI". Closed-book: the
answerer was `ocode run -m opencode-go/cat-jam-free -effort med` in an empty
scratch cwd with only the `_prompts/elixir.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. Headless
runs inject ocode's bundled skill-catalogue digest into the system prompt.

The model skipped `elixir-pm-04` in the sheet run; it was re-asked alone
(single-question sheet, tool-free isolated HOME) and appended to the answers
file. The sheet run also leaked one stray line after the YAML fence ("Need final
answer only."), which is not part of any answer and was not graded.

Graded from `../answers/cat-jam-free.md` (32/32 ids present, no duplicates, no
truncation). Answers are terse (usually 2–3 sentences), in the model's own
wording, and miss rubric points by omission rather than by stating wrong facts —
no sign of answer-key contamination.

Grading rule: a point is withheld when the answer states the concept but also
makes a materially wrong claim about the same construct, and a point is not
awarded for merely echoing the question's own premise. Calls that decide a
derivation target are marked `(judgment)`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | bind + MatchError correct; "not assignment" only restates the question's premise, no `1 = x`-style equality check (judgment; tag is below threshold either way) |
| elixir-pm-02 | pattern-matching | 3 | 3 | 1 | 0.33 | top-to-bottom first-match correct; no guard whitelist/restriction, no `FunctionClauseError` |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | re-asked alone after the sheet run skipped it |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | send / monitor `:DOWN` / timeout examples present; `{:noreply, state}` return not stated — awarded on examples |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | `__struct__` tag not named, concept present |
| elixir-data-03 | immutability-data | 2 | 3 | 3 | 1.00 | says "raises" without naming `KeyError` |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 1 | 0.50 | first-argument rule correct; nothing on flattening nested calls into a readable pipeline |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 2 | 0.67 | flat happy path + proceeds-while-matching correct; never says the first non-matching clause short-circuits and returns that value (it says so only in the separate pipe-03 answer) (judgment; awarding it would put pipe-with at 0.875) |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | non-first-position case + intermediate binding fix; no `then/2` |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 1 | 0.50 | returns-value-vs-raises contrast correct; nothing on when to choose the bang form (prefer crashing over handling) |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | "still use tuples for expected errors" not restated; avoid-defensive-rescue concept present |
| elixir-enum-01 | enum-stream | 3 | 3 | 2 | 0.67 | eager vs lazy correct; no intermediate-list / element-at-a-time point |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | generator/filter/into/do all identified; pattern-match filtering in generators and nesting not mentioned |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 1 | 0.50 | runtime first-arg dispatch correct; no open/extensible (new types can implement) point |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | aside: "isolated mutable state" is loose wording; no shared memory / message passing / race-free consequence all present |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | async/await + link propagation + timeout present; no running-several-in-parallel usage; default 5s not stated |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.70 | 4 | ok | **derive** |
| processes-otp | 1.00 | 6 | ok | omit (strong) |
| immutability-data | 1.00 | 5 | ok | omit (strong) |
| pipe-with | 0.75 | 4 | ok | **derive** |
| error-handling | 0.87 | 6 | ok | omit |
| enum-stream | 0.89 | 4 | ok | omit |
| protocols-behaviours | 0.88 | 4 | ok | omit |
| concurrency | 0.93 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 62.33 / 71 = 87.8%
```

## Derivation targets

Tags below threshold (`< 0.85`): **pattern-matching, pipe-with** → feed into
`derived/elixir.cat-jam-free.SKILL.md`.
