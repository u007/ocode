---
name: elixir-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective Elixir knowledge for nvidia/nemotron-3.5-super-vl, targeting its
  pattern-matching and pipe gaps: `=` is a match assertion rather than
  assignment, guards are a fixed whitelist, a clause miss raises
  `FunctionClauseError`, and how to place a piped value in a non-first
  argument position (`then/2` or a named intermediate, never `with`).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `nvidia/nemotron-3.5-super-vl` (e.g.
  `openrouter/nvidia/nemotron-3.5-super-vl` → `nvidia/nemotron-3.5-super-vl`;
  a `:free` route suffix is not part of the id) AND the repository is an
  Elixir project (mix.exs present, or *.ex files containing `defmodule ` — per
  meta.yaml detection). For any other model or non-Elixir repo, do not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: elixir
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes
---
# Elixir corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
**Match, not assignment:** `=` asserts that the left pattern matches the right value and binds the pattern's variables; it is not a store. `1 = x` is legal and checks that `x` already equals 1. Say this whenever you explain `=`, and name `MatchError` as the failure.

**Guards are a whitelist:** a `when` guard admits only guard-safe expressions — comparison, boolean and arithmetic operators, `is_*/1` type checks, `in` with a literal, and a short Kernel list (`length/1`, `map_size/1`, `tuple_size/1`, `elem/2`, `hd/1`, `tl/1`, `rem/2`, `div/2`, `abs/1`, `byte_size/1`). Your own functions are never allowed, pure or not; use `defguard` to compose. A guard that raises simply fails and the next clause is tried.

**No matching clause raises `FunctionClauseError`:** when every clause's pattern or guard fails, the call raises `FunctionClauseError`. Name it; a `def f(_)` fallback is a design choice, not the default behaviour. `case` raises `CaseClauseError`, `with` without a matching `else` raises `WithClauseError`.

**Pipe for readability, not only rewriting:** `a |> f(b)` is `f(a, b)`, and the point of a pipeline is to flatten nested calls into a top-to-bottom data transformation. Say both halves.

**Non-first argument:** when the piped value is not the first argument, use `then/2` (`x |> then(&g(a, &1))`) or bind a named intermediate. `with` is for chaining tagged results; it is never a fix for argument placement. Do not pipe a single call.
<!-- /kaizen:digest -->

## Pattern matching: state the assertion, the whitelist and the exception

- `=` is a pattern match that doubles as a runtime shape assertion. When you
  describe `{:ok, value} = fetch()`, include all three facts: it binds `value`
  on success, it is not assignment (`1 = x` checks equality), and a mismatch
  raises `MatchError`.
- A `when` guard is restricted to a fixed whitelist of expressions. User
  functions are not allowed in guards even when pure; purity is not the
  criterion, whitelist membership is. Compose reusable guards with
  `defguard`/`defguardp`.

```elixir
defguard is_pos_int(n) when is_integer(n) and n > 0
def f(n) when is_pos_int(n), do: :pos
def f(n) when is_integer(n), do: :non_pos
# f(:a) => ** (FunctionClauseError) no function clause matching in f/1
```

- Clauses are tried top to bottom; the first whose pattern matches and whose
  guard passes runs. When none does, the call raises `FunctionClauseError`.
  State the exception by name instead of silently adding a catch-all clause.
  Letting it crash under a supervisor and adding a catch-all are both valid
  designs; name the error either way.
- A clause placed after a catch-all is unreachable and the compiler warns
  about it.

## Pipes: give the readability rationale and the right fix

- `x |> f(a)` rewrites to `f(x, a)`: the piped value becomes the FIRST
  argument. Add the reason the operator exists: a pipeline reads as a
  top-to-bottom sequence of transformations instead of inside-out nested
  calls (`list |> Enum.map(f) |> Enum.filter(p) |> Enum.sum()`), which is why
  core APIs take the data as their first argument.
- When the value must land in a non-first position, use `then/2` or a named
  intermediate variable. Do not suggest `with`: it chains tagged-tuple steps
  and has nothing to do with argument order.

```elixir
# value is the SECOND argument of Map.get/2
key |> then(&Map.get(opts, &1))
# or
key = compute_key(x)
Map.get(opts, key)
```

- Do not pipe a single call: `x |> f()` adds nothing over `f(x)`. Do not pipe
  into a call that combines several independent values (`Enum.zip(a, b)`,
  `Map.merge(m1, m2)`); write the plain call.
