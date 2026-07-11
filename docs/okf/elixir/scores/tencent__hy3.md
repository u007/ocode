---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on elixir

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded by an independent grader (Opus 4.8), STRICT, against
> `questions.yaml` (corpus_rev 1) as source of truth. Answers were produced
> CLOSED-BOOK (`ocode run -dir <empty>`, no corpus access).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | Got match+bind and MatchError. Missed the "= is NOT assignment" point — never gave the `1 = x` equality-assertion example. |
| elixir-pm-02 | pattern-matching | 3 | 3 | 1 | 0.33 | Got top-to-bottom first-match ordering. Missed guard restriction (guards limited to a whitelist of pure functions/operators, no arbitrary calls) and never named `FunctionClauseError` for no matching clause. |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | Bare rebinds vs `^` matches existing value — both correct. |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | Map subset match vs exact tuple/list shape, `[h\|t]` — correct. |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | spawn/pid, async send to mailbox, receive blocks with `after` — all correct. |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | call sync `{:reply,reply,state}`, cast async `{:noreply,state}` — correct return shapes. |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | handle_info for out-of-band msgs; `send`, `:DOWN`, timeouts, `{:noreply}` — correct. |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | All three strategies correct + child spec (id/start/restart types). No conflation. |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | Original unchanged, new list; rebinding repoints name — correct. |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | map / keyword list / struct all distinguished correctly. |
| elixir-data-03 | immutability-data | 2 | 3 | 3 | 1.00 | `%{m\|k:v}` KeyError, Map.put inserts, both return new map — correct. |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | put_in/update_in/get_in by path, avoids manual re-nesting — correct. |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | Value inserted as FIRST arg; flattens nested calls — correct. |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 2 | 0.67 | Got `<-` chaining flat happy path and proceeds-while-matching. Missed the short-circuit: never stated a failed clause returns that non-matching value and skips the rest. |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | Failed `<-` value propagates as result; `else` handles it — correct. |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | Non-first-arg case + then/2 / captures fix present (despite a muddled example). |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | Tuples for expected/recoverable, raise for exceptional/programmer errors — correct. |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | Non-bang tuple vs bang raw-or-raise; crash-vs-handle choice — correct. |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | rescue=exceptions, catch=throw/exit, after=cleanup — no conflation. |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | Supervisor restart from clean state, isolation, still tuples for expected — correct. |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | Enum eager / Stream lazy / intermediate-list avoidance — correct. |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | Infinite/large sources + fuse-into-one-pass — both valid. |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | reduce folds, threads accumulator, other Enum are special cases — correct (minor prose slip: wrote fn args as `(accumulator, element)`; Elixir's callback is `(element, acc)`). |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | generator / filter / into: / do: all broken down correctly. |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | Runtime dispatch on first-arg data type; defimpl per type — correct. |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | @callback / @behaviour / @impl roles all correct. |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | Data-type dispatch vs module contract vs core axis — clean distinction. |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | Compile-time verification against @callback + intent — correct. |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | Isolated lightweight processes, message-copy, no-races/crash-isolation — correct. |
| elixir-conc-02 | concurrency | 2 | 3 | 3 | 1.00 | async returns Task, await blocks (5s), raise/timeout propagate to caller — correct. |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | Agent holds state via get/update; GenServer for custom logic — correct. |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | ETS direct multi-process access, GenServer-bottleneck rationale — correct. |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.70 | 4 | ok | **derive** |
| immutability-data | 1.00 | 5 | ok | omit (strong) |
| processes-otp | 1.00 | 6 | ok | omit (strong) |
| pipe-with | 0.88 | 4 | ok | omit (strong) |
| error-handling | 0.93 | 6 | ok | omit (strong) |
| enum-stream | 1.00 | 4 | ok | omit (strong) |
| protocols-behaviours | 1.00 | 4 | ok | omit (strong) |
| concurrency | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.0 / 71 = 94%
```

## Derivation targets

Tags below threshold (`< 0.75`): **pattern-matching** → feed into
`derived/elixir.tencent__hy3.SKILL.md`.
