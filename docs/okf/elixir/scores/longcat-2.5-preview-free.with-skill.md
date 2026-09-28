---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

<!-- WITH-SKILL VALIDATION RUN. The derived skill
     `elixir-tuning-longcat-2.5-preview-free`
     (derived/elixir.longcat-2.5-preview-free.SKILL.md) was prepended to the
     question sheet while these answers were produced closed-book via
     `ocode run` from an isolated dir (orchestrator audit: 1 user + 1 assistant
     turn, zero tool calls). Graded by an independent grader to the same strict
     standard as the baseline. This is validation, not derivation: no new
     derived skill is written from this run.
     Filename: `longcat-2.5-preview-free` has no slash, so it is unchanged. -->

# Scorecard — longcat-2.5-preview-free on elixir (WITH-SKILL validation)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.
>
> **WITH-SKILL run:** derived skill `elixir-tuning-longcat-2.5-preview-free`
> active during answering. Baseline: `longcat-2.5-preview-free.md` (88%).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 3 | 1.00 | match + bind, explicitly "not assignment" with literal-on-left equality check, MatchError — all present (baseline 0.67) |
| elixir-pm-02 | pattern-matching | 3 | 3 | 3 | 1.00 | top-to-bottom first match, guard whitelist, FunctionClauseError — all present (baseline 0.33) |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | bare rebinds vs `^` matches existing value, with example |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | map subset vs exact tuple arity / list head-tail shape |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | spawn→pid, async send to mailbox, receive matches + blocks (after clause); non-matches stay in mailbox |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | out-of-band msgs; send, `send_after`, `:DOWN` examples; `{:noreply, state}` (baseline 0.50) |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | three strategies distinct + child spec (id, start MFA, restart) |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | all three distinguished; bad example — `File.read(path, [:binary])` is not a real keyword-list options call; keyword keys not said to be atoms |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | KeyError + Map.put insert/overwrite correct; again never states both return a new map. Wrong: calls the update syntax a "compile-time-checked" assertion (it raises at runtime) |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | put_in rebuilds each level (new structure); update_in fn; get_in reads |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | first-arg rule + nested-call flattening now present (baseline 0.50) |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 3 | 1.00 | |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | non-first-arg case; `then/2` and named-variable fixes |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | rescue vs catch(throw/exit) and after present; muddled aside that rescue catches "throw-style errors" before correctly separating them |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | supervisor restart to known-good state, isolation, avoid defensive rescue (baseline 0.67) |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | `Stream.iterate` argument order wrong in example (start value comes first); outside rubric points |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | generator / filter / into / do all correct |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 1 | 0.50 | `@callback` + `@impl` correct; WRONG on `@behaviour` again — says it goes in the module that defines the behaviour (it goes in the implementing module, which then warns on missing callbacks). Same error as baseline |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | type-vs-module axis clear; imprecise "behaviours dispatch on the module that calls them", corrected by "calling code explicitly references the module" |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | name/arity check + intent present. Wrong extras: says `@impl` checks types, and again attributes missing-callback detection to `@impl` (that is `@behaviour`) |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | async/await, link propagation, 5s default present; no running-several-in-parallel usage. Wrong (same as baseline): `Task.await` returns `{:ok, result}` (it returns the bare result); timeout "returns `{:exit, :timeout}`" (it exits the caller); says `await` links (the link is made by `async`) |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | minor slip: Agent calls do take timeouts |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | direct multi-process access vs GenServer mailbox bottleneck |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | target? |
|-----|---------:|-----------:|--:|-------|---------|
| pattern-matching | 0.70 | 1.00 | 4 | ok | **target** |
| processes-otp | 0.86 | 1.00 | 6 | ok | no |
| immutability-data | 0.93 | 0.93 | 5 | ok | no |
| pipe-with | 0.88 | 1.00 | 4 | ok | no |
| error-handling | 0.93 | 1.00 | 6 | ok | no |
| enum-stream | 1.00 | 1.00 | 4 | ok | no |
| protocols-behaviours | 0.88 | 0.88 | 4 | ok | no |
| concurrency | 0.93 | 0.93 | 5 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.67 / 71 = 97%   (baseline 88%)
```

## Target tags

| tag | baseline | with-skill | verdict |
|-----|---------:|-----------:|---------|
| pattern-matching | 0.70 | 1.00 | **PASS** |

The skill landed. Both pattern-matching misses from the baseline are fixed:
pm-01 now says `=` is not assignment and that a literal on the left checks
equality; pm-02 now names `FunctionClauseError` and gives the guard whitelist.
The wording of those two answers follows the skill closely. That is expected
(the skill is meant to be in context for these tasks) and shows the guidance
was absorbed. No target tag failed, so no skill tweak is needed.

## Non-target drift (informational, not a failure)

- Gains from resampling: processes-otp 0.86→1.00 (otp-03 now lists `:DOWN` and
  `send_after`), pipe-with 0.88→1.00 (pipe-01 now covers flattening),
  error-handling 0.93→1.00 (error-04 now covers isolation). The skill covers
  none of these, so treat them as single-run variance.
- Errors that persisted from the baseline in tags that still clear 0.75:
  `@behaviour` placed in the wrong module (proto-02, echoed in proto-04);
  `Task.await` said to return `{:ok, result}`, plus a wrong timeout return
  (conc-02); data-03 still omits "returns a new map". None is a target tag, so
  per HOW-TO-EVALUATE none is bolted onto this skill. If a fresh baseline pushes
  protocols-behaviours below 0.75, derive a section for it then.

## Derivation targets

None. This is a validation run. No derived skill is written or changed.

Contamination check: no answer-key leak. Non-target answers differ from the
reference in wording and examples and repeat baseline errors that the
reference contradicts (`@behaviour` placement, `Task.await` return shape). The
only close phrasing is in pm-02's guard whitelist, which comes from the injected
skill, not from questions.yaml.
