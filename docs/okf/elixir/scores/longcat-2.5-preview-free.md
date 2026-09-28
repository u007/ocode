---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `longcat-2.5-preview-free` has no slash, so the filename is
     unchanged. -->

# Scorecard — longcat-2.5-preview-free on elixir

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | binding + MatchError correct; never says `=` is not assignment (no `1 = x` style equality check) |
| elixir-pm-02 | pattern-matching | 3 | 3 | 1 | 0.33 | top-to-bottom first-match ordering correct; no guard restriction (whitelist of allowed expressions), no `FunctionClauseError` when nothing matches |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | rubric points present; imprecise claim that `send` delivery is "not guaranteed" |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 1 | 0.50 | out-of-band messages correct; only plain `send` as example — no `:DOWN` monitors, no `send_after` timers, no `{:noreply, state}` return. Wrong: says system messages go to `handle_info`, and that cast is tagged with a unique ref |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | KeyError + Map.put insert/overwrite correct; never states both return a new map (no mutation) |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 1 | 0.50 | first-argument rule correct; nothing on flattening nested calls into a readable pipeline |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | non-first-arg case + named-variable fix; no `then/2` but rubric accepts either |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 2 | 0.67 | supervisor restart from clean state + avoid defensive rescue; no process-isolation point (one crash doesn't take down the system); doesn't add "still use tuples for expected errors" |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | intermediate-list point via "fuse into a single pass" |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 1 | 0.50 | `@callback` + `@impl` correct; WRONG on `@behaviour` — says it goes in the module that defines the behaviour (it goes in the implementing module, which then gets missing-callback warnings) |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | calls behaviour dispatch "compile time" (it is a runtime call on an explicitly named module) but the explicit-module + type-vs-module axis is clear |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | compile-time callback/arity check + intent present; misattributes the missing-callback check to `@impl` (that is `@behaviour`'s job) |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | async/await + propagation + 5s timeout present; no running-several-in-parallel usage. Wrong: `Task.await` returns the bare result, not `{:ok, result}`; failure propagates via the link, not a re-raise |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | rubric points present; wrong that ETS has no cleanup on process death (a table is owned by a process and deleted when the owner dies unless an heir is set) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.70 | 4 | ok | **derive** |
| processes-otp | 0.86 | 6 | ok | omit (strong) |
| immutability-data | 0.93 | 5 | ok | omit (strong) |
| pipe-with | 0.88 | 4 | ok | omit (strong) |
| error-handling | 0.93 | 6 | ok | omit (strong) |
| enum-stream | 1.00 | 4 | ok | omit (strong) |
| protocols-behaviours | 0.88 | 4 | ok | omit (strong) |
| concurrency | 0.93 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 62.67 / 71 = 88%
```

## Derivation targets

Tags below threshold (`< 0.75`): **pattern-matching** → feed into
`derived/elixir.longcat-2.5-preview-free.SKILL.md`.

Pattern in the pattern-matching misses: the model explains the happy path of
matching (binding, pin, clause ordering, map subset matching) but leaves out
the language-level facts around it. It never says `=` is not assignment,
never names `FunctionClauseError` for a call no clause matches, and treats
guards as if they could hold any expression.

Outside the derived tag, several factual errors to watch if a future run
drifts: `@behaviour` placed in the wrong module (proto-02, echoed in proto-04);
`Task.await` said to return `{:ok, result}` and to "re-raise" (conc-02); system
messages and cast refs mis-described (otp-03); ETS said to survive owner death
(conc-04). Each sits in a tag that still clears 0.75, so none is derived.

Contamination check: no leak. No answer is a near-verbatim match to the
reference: wording, ordering and examples differ, and several answers miss
points the reference states outright (FunctionClauseError, guard whitelist,
`:DOWN`/`send_after`). The `@behaviour` error contradicts the reference
directly. Consistent with a genuine closed-book run (orchestrator audit: 1
user + 1 assistant turn, zero tool calls).
