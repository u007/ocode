---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: elixir
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on elixir

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/elixir.md` sheet, one fresh session;
the session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
All 32 ids came back once, none empty. Headless runs inject the bundled
skill-catalogue digest into the system prompt; no answer cites a tool or skill
the question never named. The OpenRouter `:free` route variant is not part of
the model id. Graded from `../answers/nvidia__nemotron-3.5-super-vl.md`.

Answers are terse (one short paragraph each, roughly the length of the
reference or shorter), in the model's own wording, with no thinking-aloud, no
duplicated answers and no fabricated tool output. Two small factual slips the
answer key does not contain (`pid <- msg` offered as a send form; `Task.await`
"may shut the task down depending on options") rule out answer-key
contamination. The dominant failure mode is omission, not error: the model
states the mechanism and skips the named failure mode, the restriction, or the
"why" half of a rubric point.

Grading rule applied throughout: a point is withheld when the concept is
absent, or when the answer states the concept but also makes a materially
wrong claim about the same construct; tangential wrong asides are noted only.
Compound points whose primary concept is present with a secondary clause
omitted are awarded and the omission noted. Calls that decide a derivation
target are marked `(judgment)`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | "not assignment" point withheld: restates binding + `MatchError` but never makes the match-vs-store distinction (no `1 = x`-style equality check) |
| elixir-pm-02 | pattern-matching | 3 | 3 | 1 | 0.33 | ordering point only; guards shown but their restriction to a whitelist of allowed expressions is never stated; `FunctionClauseError` never mentioned (shows a `def f(_)` fallback instead) |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | "exact structure and arity" covers tuple/list; no `[h \| t]` example |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 2 | 0.67 | spawn/send point withheld (judgment): `send` is never described as asynchronous/non-blocking, and `pid <- msg` is offered as an alternative send form — that operator does not exist in Elixir |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | return shape `{:noreply, state}` not stated; aside: lists "system messages" under `handle_info` (the gen loop handles those) |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | "both return a new map / no mutation" never stated |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | `update_in` taking a function is not mentioned; new-structure, path read and avoid-manual-nesting all present |
| elixir-pipe-01 | pipe-with | 2 | 2 | 1 | 0.50 | first-argument rule correct; the readability point (flattening nested calls into a left-to-right pipeline) is absent — the answer is only the rewrite rule (judgment) |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | correctly notes an error is raised when no `else` clause matches (`WithClauseError`, unnamed) |
| elixir-pipe-04 | pipe-with | 1 | 2 | 1 | 0.50 | non-first-argument case correct; fix point withheld (judgment): offers "a plain function call, `with`, or an explicit capture" — neither `then/2` nor a named intermediate, and `with` is not a tool for argument placement |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 1 | 0.50 | returns-vs-raises contrast correct; never says when to choose the bang form (crash rather than handle) |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | aside: calls `catch` "the older mechanism" — it is a different mechanism (throw/exit), not an older one |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | "still use tagged tuples for expected errors" caveat not stated |
| elixir-enum-01 | enum-stream | 3 | 3 | 2 | 0.67 | eager/lazy correct; the intermediate-list vs element-at-a-time consequence is never mentioned |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | terse but all three concepts present |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | multiple-generator nesting not mentioned |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | open/extensible-to-new-types stated under proto-03 rather than here |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | "warns on missing callbacks" is only implied by "compile-time checks" |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | explicit-module dispatch not stated; core axis present |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | fan-out/await-several point absent; linked-failure and timeout point awarded but imprecise: `await` exits rather than raises, default 5 s not given, and "may be shut down depending on options" is not a `Task.await` option (that is `Task.yield`/`Task.shutdown`) |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.70 | 4 | ok | **derive** |
| processes-otp | 0.95 | 6 | ok | omit (strong) |
| immutability-data | 0.93 | 5 | ok | omit (strong) |
| pipe-with | 0.81 | 4 | ok | **derive** |
| error-handling | 0.93 | 6 | ok | omit (strong) |
| enum-stream | 0.89 | 4 | ok | omit |
| protocols-behaviours | 1.00 | 4 | ok | omit (strong) |
| concurrency | 0.87 | 5 | ok | omit |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 62.5 / 71 = 88.0%
```

## Derivation targets

Tags below threshold (`< 0.85`): **pattern-matching, pipe-with** → feed into
`derived/elixir.nvidia__nemotron-3.5-super-vl.SKILL.md`.
