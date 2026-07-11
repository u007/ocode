---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

<!-- WITH-SKILL VALIDATION RUN. The derived skill `elixir-tuning-tencent-hy3`
     was active in the model's context while these answers were produced. This
     scorecard was graded by an independent grader (Opus 4.8, 1M) to the same
     strict standard as a normal eval — points awarded only where the concept is
     genuinely and correctly present. This is validation, not derivation: no new
     derived skill is written from this run. -->

# Scorecard — tencent/hy3 on elixir (WITH-SKILL validation)

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.
>
> **WITH-SKILL run:** derived skill `elixir-tuning-tencent-hy3` active during
> answering; grader independent (Opus 4.8).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 3 | 1.00 | match+bind, not-assignment (1=x), MatchError all present |
| elixir-pm-02 | pattern-matching | 3 | 3 | 3 | 1.00 | top-to-bottom ordering, guard whitelist, FunctionClauseError all present |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | bare rebinds vs `^` matches existing value — correct contrast |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | map subset vs exact tuple/list shape; cons `[h\|t]` noted |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | spawn→pid, async send, receive matches + blocks when nothing matches |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | call sync/`{:reply,…}`, cast async/`{:noreply,…}` |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | out-of-band msgs; :DOWN/timers/send examples; `{:noreply}` return |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | all three strategies distinct + child spec (id/start MFA/restart) |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | returns new value; rebinding repoints name, no mutation |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | map / keyword-list (ordered, dup atoms, options) / struct all correct |
| elixir-data-03 | immutability-data | 2 | 3 | 3 | 1.00 | `%{m\|k}` KeyError vs Map.put insert; both return new map |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | put_in/update_in new nested at path; update_in fn; get_in reads |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | first-argument insertion + flattens nested calls |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | `<-` flat chain, proceeds while matching, first non-match short-circuits |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | unmatched value returned as-is; else transforms failures |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | don't pipe single/non-first-arg; named intermediate variable as fix |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | tuples for expected/recoverable, raise for exceptional |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | non-bang tuple vs bang raw/raise; choose bang to crash |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | rescue=raise, catch=throw/exit, after always runs |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | supervisor restart from clean state, isolation, avoid defensive rescue |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | Enum eager, Stream lazy/terminal, intermediate lists vs element-at-a-time |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | large/infinite sources + multi-step pipelines / early termination |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | fold via (element,acc)→acc, threads accumulator, others are special cases |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | generator/filter/into+do all correct (stray "lazily-friendly" flourish, outside rubric points) |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | runtime dispatch on first-arg type; defimpl per-type/extensible |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | @callback contract, @behaviour adopts, @impl marks callback |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | runtime data-type vs compile-time explicit-module — core axis present |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | compiler verifies vs @callback + documents intent/consistency |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | isolated own-heap processes, copy messages, no races/crash isolation |
| elixir-conc-02 | concurrency | 2 | 3 | 3 | 1.00 | async→Task, await blocks, linked failure propagates, 5s timeout (names "Task.Error" for timeout — actually an exit; peripheral to rubric point) |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | Agent get/update state; GenServer for custom message handling |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | ETS direct multi-process access vs GenServer bottleneck (minor trade-off slips outside rubric points) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 1.00 | 4 | ok | omit (strong) |
| processes-otp | 1.00 | 6 | ok | omit (strong) |
| immutability-data | 1.00 | 5 | ok | omit (strong) |
| pipe-with | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 6 | ok | omit (strong) |
| enum-stream | 1.00 | 4 | ok | omit (strong) |
| protocols-behaviours | 1.00 | 4 | ok | omit (strong) |
| concurrency | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 100%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag scores 1.00 with the
`elixir-tuning-tencent-hy3` skill active — including the previously-weak
**pattern-matching** tag, which is now 1.00 (n=4, ≥0.75). This validation run
confirms the derived skill; no corrective skill is (re)written from it.
