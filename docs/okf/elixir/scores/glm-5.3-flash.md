---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `glm-5.3-flash` has no slash, so the filename is unchanged. -->

# Scorecard — glm-5.3-flash on elixir

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | has bind + MatchError; never states "= is not assignment" (no `1 = x`-style equality-check framing) |
| elixir-pm-02 | pattern-matching | 3 | 3 | 3 | 1.00 | |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | concrete examples (send, send_after, :DOWN, :EXIT) present; return shape not restated — awarded on examples |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | KeyError + Map.put insert-or-update correct; never says both return a new map (no-mutation point) |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 2 | 0.67 | says it "short-circuits and stops" but never that the non-matching value becomes the `with` result |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 2 | 1.00 | fix via subject-first wrapper / named variable (then/2 mentioned only as the smell) |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 2 | 0.67 | has restart + isolation + anti-defensive-rescue; doesn't say tagged tuples remain for expected errors |
| elixir-enum-01 | enum-stream | 3 | 3 | 3 | 1.00 | |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 3 | 1.00 | |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.90 | 4 | ok | omit (strong) |
| processes-otp | 0.93 | 6 | ok | omit (strong) |
| immutability-data | 0.93 | 5 | ok | omit (strong) |
| pipe-with | 0.88 | 4 | ok | omit (strong) |
| error-handling | 0.87 | 6 | ok | omit (strong) |
| enum-stream | 1.00 | 4 | ok | omit (strong) |
| protocols-behaviours | 1.00 | 4 | ok | omit (strong) |
| concurrency | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.33 / 71 = 95%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag cleared 0.75, so no
corrective skill is derived for `glm-5.3-flash` on `elixir` at this time — a
`derived/elixir.glm-5.3-flash.SKILL.md` would have nothing to say (DRY,
cache-friendly). All four deductions share one shape: the mechanics are right
but a framing sentence the rubric wants is left implicit — "= is not
assignment" (`elixir-pm-01`), "both return a new map" (`elixir-data-03`), "the
non-matching value IS the `with` result" (`elixir-pipe-02`), and "expected
errors still use tagged tuples" (`elixir-error-04`). No factual errors were
found in any answer.
