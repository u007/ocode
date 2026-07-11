---
name: elixir-tuning-tencent-hy3
description: >
  Corrective Elixir knowledge for tencent/hy3, targeting the pattern-matching
  gaps this model showed on the closed-book elixir benchmark (guard
  restrictions, the FunctionClauseError / MatchError failure modes, and the
  "match is not assignment" framing).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `tencent/hy3` AND the repository is an Elixir project (mix.exs
  present, or *.ex files containing `defmodule ` — per meta.yaml detection).
  For any other model or non-Elixir repo, do not load.
tuned_for: tencent/hy3
tuned_version: "3.0"
stack: elixir
source_scorecard: ../scores/tencent__hy3.md
threshold: 0.75
revalidate_when: model_version changes
---

# Elixir corrections for tencent/hy3

hy3 is strong across most of the Elixir stack (94% overall; immutability,
processes/OTP, protocols/behaviours, enum/stream, concurrency all ≥ 0.88).
The one area below threshold is **pattern-matching** (subscore 0.70). The
sections below target only the specific mistakes it made — everything else it
already knows, so nothing else is restated here.

## Pattern-matching: state the failure modes and the guard whitelist

hy3 explains *how* matching binds variables but repeatedly omits the hard
edges: the exception names, the "not assignment" framing, and what guards are
actually allowed to contain. Always include these.

### `=` is a match, NOT assignment (elixir-pm-01)

hy3 correctly said `{:ok, value} = fetch()` binds `value` and raises on a
`{:error, _}` shape, but never made the core conceptual point that `=` is not
assignment. Always add it explicitly:

- `=` asserts the left pattern matches the right value; it is a two-way
  **match/equality assertion**, not a store into a cell.
- Give the giveaway example: `1 = x` is legal — it does **not** assign, it
  checks that `x` already equals `1`, and raises `MatchError` if not.
- A variable already bound can still appear on the left; a bare variable
  rebinds, which is exactly why the "it's assignment" mental model is wrong.

### Guards use `when` and are restricted to a whitelist (elixir-pm-02)

This is hy3's weakest answer (0.33). It described function-head clauses and
top-to-bottom ordering correctly, but called guards merely "boolean
expressions" and never stated the two facts the rubric wants:

1. **Guards are not arbitrary code.** `when` clauses are limited to a fixed
   whitelist of pure, side-effect-free functions and operators — comparison
   and boolean operators, type checks (`is_integer/1`, `is_atom/1`, …),
   arithmetic, `in`, `elem/2`, `map_size/1`, etc. You **cannot** call arbitrary
   user functions in a guard. A guard that raises does not error — it simply
   fails and the next clause is tried.
2. **Name the failure mode.** When no clause's pattern-plus-guard matches the
   arguments, the call raises `**FunctionClauseError**`. Always name it.

So the full picture hy3 must give: clauses tried top-to-bottom, first whose
pattern matches *and* whose whitelisted guard passes wins, otherwise
`FunctionClauseError`.

### Always name the exception for a failed match

Generalizing the two misses above — pattern-matching questions expect the
concrete failure name, not just "it fails":

- A failed `=` / bad shape → `MatchError`.
- No function clause matches → `FunctionClauseError`.
- Update syntax `%{m | k: v}` on a missing key → `KeyError` (hy3 got this one
  right; keep doing it).

Do not answer a pattern-matching question with only the happy path — the shape
that *fails* and the exception it raises is half the point.
