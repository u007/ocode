---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-19
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. muse-spark-1.2 has no "/", unchanged. -->

# Scorecard — muse-spark-1.2 on elixir

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | missed explicit "not assignment / 1=x checks equality" contrast |
| elixir-pm-02 | pattern-matching | 3 | 3 | 2 | 0.67 | missed "guards restricted to a whitelist of pure expressions" |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | missed "both return a new map, no mutation" |
| elixir-data-04 | immutability-data | 2 | 2 | 1 | 0.50 | named the functions but didn't detail update_in taking a function / get_in reading by path |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 2 | 0.67 | missed "still use tuples for expected errors" nuance |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | missed the "fan out several tasks in parallel" case |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.80 | 4 | ok | omit (strong) |
| processes-otp | 0.93 | 6 | ok | omit (strong) |
| immutability-data | 0.83 | 5 | ok | omit (strong) |
| pipe-with | 1.00 | 4 | ok | omit (strong) |
| error-handling | 0.93 | 6 | ok | omit (strong) |
| enum-stream | 1.00 | 4 | ok | omit (strong) |
| protocols-behaviours | 1.00 | 4 | ok | omit (strong) |
| concurrency | 0.93 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.671 / 71 = 92.5%
```

## Derivation targets

No tags fell below threshold (`< 0.75`). No derived skill was written.
