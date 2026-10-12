---
name: elixir-tuning-step-5-preview-free
description: >
  Corrective Elixir knowledge for step-5-preview-free, targeting its
  pattern-matching and immutable-data gaps: what actually limits a `when`
  guard, what happens when no function clause matches, and the exact failure
  behaviour of `%{map | key: val}` and the `*_in` path helpers.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `step-5-preview-free` AND the repository is an Elixir project
  (mix.exs present, or *.ex files containing `defmodule ` — per meta.yaml
  detection). For any other model or non-Elixir repo, do not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: elixir
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# Elixir corrections for step-5-preview-free

## Pattern matching: guards are a whitelist, and a miss raises

The model explains clause ordering well but misdescribes what limits a guard
and never says what happens when no clause matches. Apply the following
whenever you explain or write multi-clause functions.

### A guard is restricted to a fixed whitelist, not merely "side-effect free"

- `when` accepts only guard-safe expressions: comparison, boolean and
  arithmetic operators, type checks (`is_integer/1`, `is_map/1`,
  `is_binary/1`, …), `in` with a literal list or range, and a short list of
  Kernel functions such as `length/1`, `map_size/1`, `tuple_size/1`,
  `elem/2`, `hd/1`, `tl/1`, `rem/2`, `div/2`, `abs/1`, `byte_size/1`.
- Your own functions are NOT allowed in a guard, even when they are pure.
  Purity is not the criterion; membership in the whitelist is. Calling
  `my_check(x)` in a `when` is a compile error.
- Build reusable guards from the whitelist with `defguard`/`defguardp`:

```elixir
defguard is_pos_int(n) when is_integer(n) and n > 0
def f(n) when is_pos_int(n), do: :pos
```

- An error raised inside a guard makes that guard fail (the clause is
  skipped); it does not crash the call.

### No matching clause raises `FunctionClauseError`

- When every clause's pattern or guard fails, the call raises
  `FunctionClauseError` ("no function clause matching in …"). State this
  explicitly instead of only showing a `def f(_)` fallback.
- Either add a catch-all clause or let it crash on purpose under a
  supervisor; both are valid designs, but name the error that occurs.
- `case` behaves the same way with `CaseClauseError`; a bare `=` with
  `MatchError`.

```elixir
def area({:circle, r}) when is_number(r) and r > 0, do: 3.14159 * r * r
def area({:square, s}) when is_number(s), do: s * s
# area({:triangle, 1}) => ** (FunctionClauseError) no function clause matching in area/1
```

- A clause placed after a catch-all is unreachable and the compiler emits a
  warning ("this clause cannot match because a previous clause always
  matches"). Do not describe it as silent.

## Immutable data: exact failure behaviour of map update and the `*_in` helpers

The model has the happy paths right but invents behaviour on the failure
paths and names a function that does not exist. Apply the following.

### `%{map | key: val}` is a runtime check on a plain map

- `%{map | key: val}` updates `key` only if it already exists; otherwise it
  raises `KeyError` at RUNTIME, whether the key is a literal or a variable.
  There is no compile-time key check for plain maps.
- The compile-time check exists only for structs: `%User{user | nmae: x}`
  fails to compile because the struct's fields are known. Keep the two cases
  separate.
- `Map.put(map, key, val)` inserts or overwrites and never raises. Both forms
  return a new map; neither mutates.

```elixir
m = %{a: 1}
%{m | a: 2}        # %{a: 2}
%{m | b: 2}        # ** (KeyError) key :b not found in: %{a: 1}   (runtime)
Map.put(m, :b, 2)  # %{a: 1, b: 2}
```

### `put_in` / `update_in` raise on a missing intermediate; `get_in` returns nil

- `get_in(data, path)` returns `nil` when any step of the path is missing.
- `put_in(data, path, v)` and `update_in(data, path, fun)` do NOT tolerate a
  missing intermediate: when a step resolves to `nil` they raise
  `ArgumentError` ("could not put/update key … on a nil value"). Only the
  LAST key may be absent (`put_in` adds it; `update_in` passes `nil` to the
  function). Never say `update_in` "returns the current value" when the path
  is missing.
- To create intermediates on the fly, use `Access.key/2` with a default:
  `put_in(data, [Access.key(:user, %{}), Access.key(:address, %{}), :city], "KL")`.

```elixir
data = %{}
get_in(data, [:user, :city])              # nil
put_in(data, [:user, :city], "KL")        # ** (ArgumentError) could not put/update key :city on a nil value
put_in(data, [Access.key(:user, %{}), :city], "KL")  # %{user: %{city: "KL"}}
```

### The path helpers that exist

- `get_in/2`, `put_in/2,3`, `update_in/2,3`, `get_and_update_in/2,3`,
  `pop_in/1,2`. There is no `fetch_in`; for a strict read use
  `Access.fetch/2` on one level or `get_in` with an explicit `nil` check.
- The macro forms take a dotted path (`put_in(data.user.city, "KL")`) and
  raise `KeyError` on a missing key, because `data.user` is strict field
  access; the function forms take a path list and use `Access`.
- Manual nesting is written as `%{data | user: %{data.user | address: %{data.user.address | city: "KL"}}}`:
  every level is re-stated from the root. Do not introduce bindings inside
  the update syntax; `%{user = data.user | ...}` is not valid Elixir.
