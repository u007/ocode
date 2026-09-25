---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

<!-- WITH-SKILL VALIDATION RUN. The derived skill
     `derived/elixir.space-bunny-free.SKILL.md` (name: elixir-tuning-space-bunny-free,
     target tag: pipe-with) was prepended to the answerer prompt as active
     guidance while these answers were produced. Graded by an independent
     grader (Claude Fable 5.1) to the same strict standard as the baseline
     `scores/space-bunny-free.md` — points awarded only where the concept is
     genuinely and correctly present. This is validation, not derivation: no
     derived skill is written or modified from this run. -->

# Scorecard — space-bunny-free on elixir (WITH-SKILL validation)

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.
>
> **WITH-SKILL run:** derived skill `elixir-tuning-space-bunny-free`
> (`derived/elixir.space-bunny-free.SKILL.md`, target tag **pipe-with**) active
> during answering; compared against baseline `scores/space-bunny-free.md`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | match + bind and MatchError present; still never makes the "= is not assignment" point (no `1 = x` style check) — same miss as baseline |
| elixir-pm-02 | pattern-matching | 3 | 3 | 2 | 0.67 | top-to-bottom first match+guard and FunctionClauseError present; nothing on guards being restricted to a whitelist of expressions (baseline had it) |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | `^` matches existing value; bare `x` "binds or rebinds" |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 1 | 0.50 | map subset match correct; never contrasts with tuple/list exact-shape matching — same miss as baseline. Stray `^expected` remark is off-topic |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | pid / mailbox / receive matches / blocks-until-match-or-after all present. Factual slip outside rubric: claims `spawn` links to the caller (that is `spawn_link`) |
| elixir-otp-02 | processes-otp | 3 | 3 | 2 | 0.67 | sync vs async correct; `{:noreply, state}` for cast present; handle_call return given as `{reply, new_state}` (missing the `:reply` tag) — point not awarded. Also offers bare `:noreply` as a valid cast return (wrong). Baseline scored 1.00 here |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | out-of-band messages; send / timers / :DOWN examples. Return shape `{:noreply, state}` not stated (secondary half of point 2) |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | three strategies distinct; child spec id / start MFA / restart |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | keyword-list duplicate keys not mentioned but all three roles correct |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | KeyError vs Map.put insert correct; never states both return a new map / no mutation — same miss as baseline. "behaves like `Map.update!/3`" is loose (update! takes a fun) |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | first-argument rule AND flattening-nested-calls purpose now both present (baseline 0.50) |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 2 | 0.67 | flat happy path + proceeds-while-matching present; the answer covers ONLY the success path — never says the first non-matching clause short-circuits and returns that value. Same score as baseline; the short-circuit fact landed in pipe-03 instead |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | non-matching value becomes the `with` result, returned as-is; `else` transforms it. Correct bonus: `WithClauseError` when no else clause matches |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | non-first-arg + single-call cases; `then/2` and named-variable fixes both given (baseline 0.50). `IO.inspect` fact now correct |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | returns-vs-raises contrast; "when the caller wants immediate failure behavior" accepted as the choose-bang-to-crash guidance (weakly stated; baseline 0.50) |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | rescue vs catch (throw/exit); after always runs, with a correct kill/halt caveat |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-enum-01 | enum-stream | 3 | 3 | 2 | 0.67 | eager vs lazy ok (hedged "generally defer"); still omits intermediate-list-per-step vs element-at-a-time — same miss as baseline |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | infinite source with partial consumption (early termination) + large file to bound memory |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | explicit-module dispatch not named, but the data-type vs module-contract axis is clear (as in baseline) |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | linked failure propagation + timeout exit ok; no fan-out / await-several-in-parallel usage (naming `Task.async_stream` as a control alternative is not that) — same miss as baseline |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.70 | 4 | ok | non-target; see comparison note |
| processes-otp | 0.93 | 6 | ok | omit (strong) |
| immutability-data | 0.83 | 5 | ok | omit (strong) |
| pipe-with | 0.88 | 4 | ok | **TARGET — passes (≥ 0.75)** |
| error-handling | 0.93 | 6 | ok | omit (strong) |
| enum-stream | 0.89 | 4 | ok | omit (strong) |
| protocols-behaviours | 1.00 | 4 | ok | omit (strong) |
| concurrency | 0.93 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Baseline vs with-skill comparison

| tag | baseline | with-skill | target? | verdict |
|-----|---------:|-----------:|:-------:|---------|
| pattern-matching | 0.80 | 0.70 | no | non-target drift (pm-02 lost the guard-whitelist point this sample); noise, no action |
| processes-otp | 1.00 | 0.93 | no | non-target drift (otp-02 handle_call return shape); noise, no action |
| immutability-data | 0.83 | 0.83 | no | unchanged |
| **pipe-with** | **0.69** | **0.88** | **yes** | **PASS** — crossed threshold (+0.19) |
| error-handling | 0.87 | 0.93 | no | unchanged within noise (error-02 gained) |
| enum-stream | 0.89 | 0.89 | no | unchanged |
| protocols-behaviours | 1.00 | 1.00 | no | unchanged |
| concurrency | 0.93 | 0.93 | no | unchanged |

**Verdict: SUCCESS.** The only target tag, `pipe-with`, moved 0.69 → 0.88 and
is above the 0.75 threshold. The skill is validated for `space-bunny-free` @
`alpha`.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.67 / 71 = 90%
```

(baseline: 63.17 / 71 = 89%)

## Derivation targets

None — this is a validation run. No derived skill is written or modified.

### What the skill fixed (pipe-with)

- `elixir-pipe-01` 0.50 → 1.00: the flattening / readability purpose of `|>`
  is now stated alongside the first-argument rule.
- `elixir-pipe-04` 0.50 → 1.00: `then/2` and a named intermediate variable are
  both offered as the fix for a non-first-argument step; the `IO.inspect`
  return-value fact is now correct (and `IO.puts` is correctly named as the
  `:ok`-returning one).
- `elixir-pipe-03` held at 1.00 with a sharper statement that the non-matching
  value *is* the `with` result.

### Residual pipe-with miss (not blocking; note for any future sharpening)

- `elixir-pipe-02` stayed at 0.67: when asked "what problem does `with` solve
  and how does it chain `{:ok, _}` steps", the answer describes only the success
  path and never mentions that the first non-matching clause short-circuits and
  returns its value. The skill's `with` section does state this, but the model
  applied it only when directly asked about failure (pipe-03). If the tag ever
  needs a further nudge, the directive is: *any* explanation of `with` must
  include the failure path, not just the happy path.

### Non-target observations (do not act on — single-sample variance)

- `elixir-otp-02` dropped from 1.00 to 0.67 on a wrong `handle_call` return
  shape (`{reply, new_state}` instead of `{:reply, reply, new_state}`) and a
  bogus bare `:noreply` cast return.
- `elixir-pm-02` dropped from 1.00 to 0.67 by omitting the guard-expression
  whitelist.
- New factual slip outside the rubric: `spawn/1` described as linking to the
  caller (`elixir-otp-01`).
- Persistent baseline misses unchanged by the skill (as expected, non-target):
  pm-01 "not assignment", pm-04 tuple/list exact-shape contrast, data-03 "both
  return a new map", enum-01 intermediate lists, conc-02 parallel fan-out.

Contamination check: answers outside `pipe-with` differ in wording, examples
and ordering from `questions.yaml` and still miss points the reference states
verbatim, consistent with a genuine closed-book run. The `pipe-with` answers
overlap the reference closely (the pipe-01 example and the "why core APIs take
data first" line), but that overlap is inherited from the skill text itself,
which is the intended absorption effect, not answer-key leakage.
