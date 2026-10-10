---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: elixir
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on elixir (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Baseline rerun 2026-10-05, graded closed-book from
> `answers/longcat-2.5-preview-free.rerun-2026-10-05.md`. (Answers file has one malformed
> record id — `id elixir-data-02` — and a duplicate elixir-data-02 record; both versions earn full marks.)

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | never says "not assignment" / `1 = x` checks equality |
| elixir-pm-02 | pattern-matching | 3 | 3 | 2 | 0.67 | missed guard restrictions |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | return shape not stated, examples present |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | did not say both return a new map |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | slip: said rescue catches `throw` |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 2 | 0.67 | no "still use tuples for expected errors" |
| elixir-enum-01 | enum-stream | 3 | 3 | 2 | 0.67 | no intermediate-list contrast |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 1 | 0.50 | no open/extensible point |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 1 | 0.50 | no documents-intent / consistency point |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | no parallel fan-out; said await re-raises (linked crash propagates) |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | slip: said ETS survives process crashes |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.80 | 4 | ok | omit |
| processes-otp | 0.93 | 6 | ok | omit (strong) |
| immutability-data | 0.93 | 5 | ok | omit (strong) |
| pipe-with | 1.00 | 4 | ok | omit (strong) |
| error-handling | 0.93 | 6 | ok | omit (strong) |
| enum-stream | 0.89 | 4 | ok | omit |
| protocols-behaviours | 0.81 | 4 | ok | omit |
| concurrency | 0.93 | 5 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.17 / 71 = 90.4%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill needed from this run.
