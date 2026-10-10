---
name: elixir-tuning-longcat-2.5-preview-free
description: >
  Corrective Elixir knowledge for longcat-2.5-preview-free, targeting its
  pattern-matching gaps: `=` as a match (not assignment), what happens when no
  function clause matches, and what guards may contain.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `longcat-2.5-preview-free` AND the repository is an Elixir project
  (mix.exs present, or *.ex files containing `defmodule ` — per meta.yaml
  detection). For any other model or non-Elixir repo, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: elixir
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# Elixir corrections for longcat-2.5-preview-free

## Pattern matching: cover the failure paths and limits, not just the happy path

The model describes successful matches correctly but leaves out what `=`
really is, what happens when no clause matches, and what a guard may contain.
Apply the following.

### `=` is the match operator, not assignment

- `=` asserts that the left-hand pattern matches the right-hand value. When it
  matches, variables in the pattern are bound. When it doesn't, it raises
  `MatchError`.
- Say explicitly that this is not assignment. A literal on the left is legal
  and just checks equality:

```elixir
x = 1
1 = x        # ok: asserts x == 1
2 = x        # ** (MatchError) no match of right hand side value: 1
```

- So `{:ok, value} = fetch()` both destructures and asserts the result's
  shape at runtime.

### Function clauses: ordering, guard limits, and FunctionClauseError

When you explain multi-clause functions, always state all three:

1. **Ordering.** Clauses are tried top to bottom. The first one whose pattern
   matches and whose guard passes runs. Put specific clauses before general
   ones.
2. **Guards are restricted.** A `when` guard may only use a fixed whitelist of
   guard-safe expressions: comparison, boolean and arithmetic operators,
   type checks (`is_integer/1`, `is_map/1`, …), `in` with compile-time lists and
   ranges, and a few Kernel functions such as `length/1`, `map_size/1`,
   `elem/2`, `hd/1`, `rem/2`, `abs/1`. Arbitrary function calls, including
   your own functions, are not allowed. `defguard` builds reusable guards from
   those same allowed expressions. An error raised inside a guard makes the
   guard fail; it doesn't crash.
3. **No match raises `FunctionClauseError`.** If no clause matches, the call
   raises `FunctionClauseError`. Either add a catch-all clause or let it crash
   on purpose.

```elixir
def area({:circle, r}) when is_number(r) and r > 0, do: 3.14159 * r * r
def area({:square, s}) when is_number(s), do: s * s
# area({:triangle, 1}) => ** (FunctionClauseError) no function clause matching in area/1
```

The same idea applies to `case`: if no branch matches, it raises
`CaseClauseError`.

## Protocols and behaviours: state the extensibility and intent points

### Protocols

- A protocol dispatches at runtime on the data type of the first argument.
  `defprotocol` declares the functions; `defimpl Proto, for: Type` supplies one
  implementation per type.
- Protocols are open: any module can add `defimpl` for an existing protocol and
  any type, including third-party structs and built-in types, without modifying
  the protocol or the type's source. Say this explicitly.
- `for: Any` plus `@derive` or `@fallback_to_any true` provides a fallback.

### Behaviours

- A behaviour is a contract for a module: `@callback` declares the required
  functions with typespecs, and the implementing module adopts it with
  `@behaviour`. Missing callbacks produce a compile-time warning.
- Dispatch is by explicit module (an adapter passed in or read from config),
  not by data type. Contrast: protocol = polymorphism over data types;
  behaviour = swappable modules sharing one API.
- `GenServer` and `Supervisor` are behaviours.

### `@impl`

- `@impl true` (or `@impl MyBehaviour`) marks a function as a deliberate
  callback implementation. The compiler verifies it matches a declared
  `@callback`, so a typo in name or arity warns instead of silently defining an
  ordinary function.
- It documents intent: readers see the function satisfies a behaviour.
- It is all-or-nothing per module: once one callback has `@impl`, every other
  callback in that module must have it too, or the compiler warns.
