---
name: elixir-tuning-cat-jam-free
description: >
  Corrective Elixir knowledge for cat-jam-free, targeting its pattern-matching
  and pipe/with gaps: `=` as an equality assertion, what limits a `when` guard,
  what happens when no clause matches, why pipes exist, and how `with`
  short-circuits.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `cat-jam-free` AND the repository is an Elixir project (mix.exs
  present, or *.ex files containing `defmodule ` — per meta.yaml detection).
  For any other model or non-Elixir repo, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: elixir
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# Elixir corrections for cat-jam-free

## Pattern matching: `=` asserts, guards are a whitelist, a miss raises

- Every explanation of `=` must include both: it is a match ASSERTION, not
  assignment; and a literal or bound value on the left (`1 = x`) is an
  equality check that raises `MatchError` when it fails.
- Every explanation of guards must include: a guard accepts only a fixed
  whitelist of guard-safe expressions, so your own functions are a compile
  error there (use `defguard`).

### `=` is an assertion, not a store

- `=` matches the left pattern against the right value. Variables in the
  pattern get bound; everything else is CHECKED.
- A literal or bound value on the left is an equality check: `1 = x` succeeds
  only when `x` is 1 and raises `MatchError` otherwise. Say this when
  explaining `=`, don't just repeat "not assignment".
- A failed match raises `MatchError` and binds nothing.

### A guard is limited to a fixed whitelist

- `when` accepts only guard-safe expressions: comparison, boolean and
  arithmetic operators, type checks (`is_integer/1`, `is_map/1`,
  `is_binary/1`, …), `in` with a literal list or range, and a short list of
  Kernel functions (`length/1`, `map_size/1`, `tuple_size/1`, `elem/2`,
  `hd/1`, `rem/2`, `div/2`, `abs/1`, `byte_size/1`, …).
- Your own functions are not allowed in a guard, even pure ones. Calling one is
  a compile error. Build reusable guards with `defguard`/`defguardp`.
- An error raised inside a guard makes that guard fail (the clause is skipped);
  it does not crash the call.

### No matching clause raises `FunctionClauseError`

- When every clause's pattern or guard fails, the call raises
  `FunctionClauseError` ("no function clause matching in …"). Say so
  whenever you explain multi-clause dispatch.
- `case` with no matching branch raises `CaseClauseError`; a bare `=` raises
  `MatchError`.

```elixir
def area({:circle, r}) when is_number(r), do: 3.14159 * r * r
def area({:square, s}) when is_number(s), do: s * s
# area({:triangle, 1}) => ** (FunctionClauseError) no function clause matching in area/1
```

## Pipes and `with`: state the purpose and the failure path

### `|>` exists to flatten nested calls

- `x |> f(a)` is `f(x, a)`: the value becomes the FIRST argument.
- Its purpose: turn inside-out nesting like
  `Enum.sum(Enum.filter(Enum.map(list, f), g))` into a top-to-bottom pipeline
  `list |> Enum.map(f) |> Enum.filter(g) |> Enum.sum()`. That is also why
  Elixir APIs take the data as their first argument. State both the rule and
  the purpose.
- When the value must go somewhere other than the first argument, use
  `then(x, fn v -> g(a, v) end)` or a named intermediate variable. Don't pipe
  a single call (`x |> f()` adds nothing over `f(x)`).

### `with` short-circuits on the first non-match

- Every explanation of `with` must include the failure path, not only the
  happy path: the first `<-` clause whose pattern does NOT match stops the
  chain, and that non-matching value (e.g. `{:error, reason}`) is returned
  as the result of the whole `with`, unchanged.
- An `else` block pattern-matches those failure values to transform them. If
  `else` is present and none of its clauses match, `WithClauseError` is
  raised.

```elixir
with {:ok, a} <- step1(),
     {:ok, b} <- step2(a) do
  {:ok, a + b}
end
# step2 returns {:error, :bad} => the with evaluates to {:error, :bad}
```
