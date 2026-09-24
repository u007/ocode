---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: elixir
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `space-bunny-free` has no slash, so the filename is unchanged. -->

# Scorecard — space-bunny-free on elixir

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| elixir-pm-01 | pattern-matching | 3 | 3 | 2 | 0.67 | has bind + MatchError; never makes the "= is not assignment" point (no `1 = x` style example) |
| elixir-pm-02 | pattern-matching | 3 | 3 | 3 | 1.00 | |
| elixir-pm-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| elixir-pm-04 | pattern-matching, immutability-data | 2 | 2 | 1 | 0.50 | map subset match correct; never contrasts with tuple/list exact-shape matching, which the question explicitly asked |
| elixir-otp-01 | processes-otp, concurrency | 2 | 3 | 3 | 1.00 | rubric points present; factual slip: claims `receive ... after` "returns `:timeout`" (the `after` body runs; nothing is returned automatically) |
| elixir-otp-02 | processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-otp-03 | processes-otp | 2 | 2 | 2 | 1.00 | |
| elixir-otp-04 | processes-otp | 3 | 4 | 4 | 1.00 | |
| elixir-data-01 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-data-02 | immutability-data | 2 | 3 | 3 | 1.00 | |
| elixir-data-03 | immutability-data | 2 | 3 | 2 | 0.67 | KeyError + Map.put insert correct; never states both return a new map / no mutation |
| elixir-data-04 | immutability-data | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-01 | pipe-with | 2 | 2 | 1 | 0.50 | first-argument rule correct; nothing on flattening nested calls into a readable pipeline |
| elixir-pipe-02 | pipe-with, error-handling | 3 | 3 | 2 | 0.67 | flat happy path + proceeds-while-matching ok; says "exits early" but never that the non-matching value becomes the `with` result; final sentence garbled |
| elixir-pipe-03 | pipe-with, error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-pipe-04 | pipe-with | 1 | 2 | 1 | 0.50 | identifies "value isn't the natural first arg"; no `then/2` / named-variable fix. Example is factually wrong: `IO.inspect/1` returns its argument, not `:ok` |
| elixir-error-01 | error-handling | 3 | 2 | 2 | 1.00 | |
| elixir-error-02 | error-handling | 2 | 2 | 1 | 0.50 | returns-vs-raises contrast correct; no guidance on when to choose bang (crash vs handle) |
| elixir-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| elixir-error-04 | error-handling, processes-otp | 3 | 3 | 3 | 1.00 | |
| elixir-enum-01 | enum-stream | 3 | 3 | 2 | 0.67 | eager vs lazy ok (hedged "mostly lazy"); omits the intermediate-list-per-step vs element-at-a-time detail |
| elixir-enum-02 | enum-stream | 2 | 2 | 2 | 1.00 | |
| elixir-enum-03 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-enum-04 | enum-stream | 2 | 3 | 3 | 1.00 | |
| elixir-proto-01 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-02 | protocols-behaviours | 2 | 2 | 2 | 1.00 | |
| elixir-proto-03 | protocols-behaviours | 3 | 3 | 3 | 1.00 | does not say behaviour dispatch is by naming the module explicitly, but the data-type vs module-contract axis is clear |
| elixir-proto-04 | protocols-behaviours | 1 | 2 | 2 | 1.00 | |
| elixir-conc-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| elixir-conc-02 | concurrency | 2 | 3 | 2 | 0.67 | linked failure propagation + timeout ok; no fan-out / await-several-in-parallel usage |
| elixir-conc-03 | concurrency, processes-otp | 1 | 2 | 2 | 1.00 | |
| elixir-conc-04 | concurrency | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pattern-matching | 0.80 | 4 | ok | omit (strong) |
| processes-otp | 1.00 | 6 | ok | omit (strong) |
| immutability-data | 0.83 | 5 | ok | omit (strong) |
| pipe-with | 0.69 | 4 | ok | **derive** |
| error-handling | 0.87 | 6 | ok | omit (strong) |
| enum-stream | 0.89 | 4 | ok | omit (strong) |
| protocols-behaviours | 1.00 | 4 | ok | omit (strong) |
| concurrency | 0.93 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.17 / 71 = 89%
```

## Derivation targets

Tags below threshold (`< 0.75`): **pipe-with** → feed into
`derived/elixir.space-bunny-free.SKILL.md`.

Pattern in the pipe-with misses: the model states the mechanical rule (first
argument, `<-` matching) but stops short of the *why* and the *fix* — it never
says pipes exist to flatten nested calls, never says a failed `with` clause
*returns* its non-matching value, and offers no `then/2` / intermediate-variable
remedy for non-first-arg piping. Its one concrete pipe example was also wrong
(`IO.inspect/1` returns its argument, not `:ok`).

Outside the derived tag, two factual errors worth watching if a future run
drifts: `elixir-otp-01` (claims `receive ... after` returns `:timeout`) and the
same `IO.inspect` slip above. Answers are generally terse and hedged ("mostly
lazy", "generally performed"), which cost the intermediate-list point on
`elixir-enum-01`.

Contamination check: no answer reads as a copy of the reference — wording,
examples and ordering all differ from `questions.yaml`, and several answers miss
points the reference states verbatim. Consistent with a genuine closed-book run.
