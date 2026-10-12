---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: elixir
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on elixir

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book: the answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/elixir.md` sheet attached; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. A
first attempt failed at the provider (429) with no output and was re-run.
Graded from `../answers/step-5-preview-free.md` (32/32 ids present, no
duplicates, no truncation). Answers are in the model's own wording, typically
2–3× longer than the reference, and carry several factual slips the answer key
does not contain (wrong `Task.await` return shape, a non-existent `fetch_in/2`,
a compile-time check that does not exist) — no sign of answer-key contamination.

Grading rule applied throughout: a point is withheld when the answer states the
concept but also makes a materially wrong claim about the same construct;
tangential wrong asides are noted only. Three such calls decide the two
derivation targets below and are marked `(judgment)` in the notes.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 3 | 1.00 | |
| elixir-pm-02 | pattern-matching | 3 | 3 | 1 | 0.33 | never mentions `FunctionClauseError`; guard restriction given as "must be side-effect free", not the allowed-expression whitelist (judgment); aside: says a dead clause after a catch-all "generates no error" — the compiler warns |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | aside: lists OTP system messages under `handle_info` (the gen loop handles those) |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | `__struct__` tag not named, concept present |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | KeyError point withheld: claims the missing-key check is "at compile time for literal keys" — for a plain map it is always runtime (judgment) |
| elixir-data-04 | immutability-data | 2 | 2 | 1 | 0.50 | update_in/get_in point withheld: says `update_in` "returns the current value if an intermediate node is missing" (it raises `ArgumentError`); also invents `fetch_in/2` and writes an invalid manual-nesting example (judgment) |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | aside: "scoping tool … without shadowing" claim is meaningless |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | correctly adds `WithClauseError` when `else` has no match |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | examples garbled but both fixes (`then/2`, named intermediate) present |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | aside: "the compiler even warns on unmatched shapes" is not true for plain tuples |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 1 | 0.50 | contract/@callback point withheld: asserts "optional and default implementations are not a thing" — `@optional_callbacks` and `defoverridable` defaults exist |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | aside: `@impl` does not check specs |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | async/await point withheld: says `Task.await` returns `{:ok, result}` / `{:exit, reason}` — that is `Task.yield`'s shape; `await` returns the bare result |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.80 | 4 | ok | **derive** |
| processes-otp | 1.00 | 6 | ok | omit (strong) |
| immutability-data | 0.83 | 5 | ok | **derive** |
| pipe-with | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 6 | ok | omit (strong) |
| enum-stream | 1.00 | 4 | ok | omit (strong) |
| protocols-behaviours | 0.88 | 4 | ok | omit |
| concurrency | 0.93 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.67 / 71 = 92.5%
```

## Derivation targets

Tags below threshold (`< 0.85`): **pattern-matching, immutability-data** → feed into
`derived/elixir.step-5-preview-free.SKILL.md`.
