---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `mimo-v2.5` has no slash, so the filename is unchanged. -->

# Scorecard — mimo-v2.5 on elixir

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | never states "= is not assignment" (e.g. `1 = x`); has bind + MatchError |
| elixir-pm-02 | pattern-matching | 3 | 3 | 2 | 0.67 | omits guard whitelist restriction; has ordering + FunctionClauseError |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 1 | 0.50 | has "non-call/cast" concept but no concrete example (:DOWN/send_after) or return shape |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | |
| elixir-data-03 | immutability-data | 2 | 3 | 1 | 0.33 | wrong: claims `%{m \| k: v}` and `Map.put` are identical; missed KeyError-on-missing-key behavior |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 1 | 0.50 | identifies non-first-arg case, but fix is "drop the pipe" rather than `then/2`/named var |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 2 | 0.67 | has restart/isolation; doesn't restate that tagged tuples remain for expected errors |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | has linking/timeout; omits fan-out/parallel-await usage |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.80 | 4 | ok | omit (strong) |
| processes-otp | 0.86 | 6 | ok | omit (strong) |
| immutability-data | 0.87 | 5 | ok | omit (strong) |
| pipe-with | 0.94 | 4 | ok | omit (strong) |
| error-handling | 0.93 | 6 | ok | omit (strong) |
| enum-stream | 1.00 | 4 | ok | omit (strong) |
| protocols-behaviours | 1.00 | 4 | ok | omit (strong) |
| concurrency | 0.93 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 91%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag cleared 0.75, so no
corrective skill is derived for `mimo-v2.5` on `elixir` at this time — a
`derived/elixir.mimo-v2.5.SKILL.md` would have nothing to say (DRY,
cache-friendly). The two lowest-scoring individual questions worth watching if
a future run drifts down: `elixir-data-03` (claimed `%{m | k: v}` and
`Map.put/3` behave identically — factually wrong, misses `KeyError` on a
missing key) and `elixir-otp-03` (no concrete `handle_info` example or return
shape).
