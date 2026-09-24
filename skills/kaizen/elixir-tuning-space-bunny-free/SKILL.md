---
name: elixir-tuning-space-bunny-free
description: >
  Corrective Elixir knowledge for space-bunny-free, targeting the pipe / with
  gaps this model showed on the closed-book elixir benchmark (why pipelines
  exist, what a failed `with` clause returns, and how to pipe into a
  non-first argument).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `space-bunny-free` AND the repository is an Elixir project (mix.exs
  present, or *.ex files containing `defmodule ` — per meta.yaml detection).
  For any other model or non-Elixir repo, do not load.
tuned_for: space-bunny-free
tuned_version: "alpha"
stack: elixir
source_scorecard: ../scores/space-bunny-free.md
threshold: 0.75
revalidate_when: model_version changes
---
# Elixir corrections for space-bunny-free

## Pipe and `with`: give the purpose, the return value, and the fix

The model states the mechanics of `|>` and `with` correctly but stops there.
It leaves out why the pipe exists, what a `with` expression actually evaluates
to on failure, and how to pipe when the value is not the first argument. Its
one concrete pipe example was also factually wrong. Apply the following.

### `|>` exists to flatten nesting, not just to "pass the first argument"

`a |> f(b)` rewrites to `f(a, b)` — the left value always becomes the FIRST
argument. Always add the point of it: a chain of pipes turns inside-out nested
calls into a top-to-bottom data pipeline.

```elixir
Enum.sum(Enum.filter(Enum.map(list, &(&1 * 2)), &(&1 > 4)))
# reads as:
list |> Enum.map(&(&1 * 2)) |> Enum.filter(&(&1 > 4)) |> Enum.sum()
```

This is also why core APIs take the data as their first argument. When
explaining the pipe, state both halves: the first-argument rule AND the
readability/flattening purpose.

### A failed `<-` clause makes `with` RETURN that value

"Exits early" is not enough. The precise behaviour:

- `with` proceeds while every `<-` pattern matches.
- The first clause whose pattern does NOT match short-circuits, and the
  non-matching value **becomes the result of the whole `with` expression**,
  returned as-is (so an `{:error, reason}` from any step flows straight out).
- An `else` block pattern-matches those failure values to transform or
  handle them; without `else`, they are returned untouched.

```elixir
with {:ok, user} <- fetch_user(id),
     {:ok, order} <- fetch_order(user) do
  {:ok, order}
end
# fetch_user returning {:error, :not_found} => the with returns {:error, :not_found}
```

Never describe the failure path as "raises" or merely "stops" — name the value
that comes out.

### When the value is not the first argument, use `then/2` or a named variable

Do not answer "call it directly" or "drop the pipe" as the only fix. The
idiomatic remedies for a step that needs the piped value in a non-first
position are:

- `then/2`: `value |> then(fn v -> Map.get(config, v) end)`
- a named intermediate variable and a plain call.
- an anonymous-function capture that places the argument:
  `value |> (&Regex.replace(~r/x/, &1, "y")).()` is legal but `then/2` is
  clearer.

Also do not pipe a single call: `x |> f()` adds nothing over `f(x)`.

### Get the `IO.inspect` fact right

`IO.inspect/1` and `IO.inspect/2` **return their argument unchanged** — that
is exactly why they are safe to drop into the middle of a pipeline:

```elixir
list |> Enum.map(&(&1 * 2)) |> IO.inspect(label: "doubled") |> Enum.sum()
```

It is `IO.puts/1` that returns `:ok`. Never use `IO.inspect` as an example of a
function that "breaks" a pipeline by returning `:ok`.
