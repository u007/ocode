# Elixir — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 32

---

### elixir-pm-01

In Elixir, `=` is the match operator, not assignment. What does `{:ok, value} = fetch()` actually do, and what happens if the right side is `{:error, :notfound}`?

### elixir-pm-02

How do multiple function-head clauses plus guards replace conditional logic? Show what decides which clause runs.

### elixir-pm-03

What does the pin operator `^` do in a pattern, and how does `^x` differ from a bare `x` on the left of a match?

### elixir-pm-04

How does map pattern matching differ from list/tuple matching — specifically, does `%{name: n} = user` require the map to have exactly those keys?

### elixir-otp-01

Describe the raw process primitives `spawn`, `send`, and `receive`. What does `receive` do when no message matches?

### elixir-otp-02

In a GenServer, what is the difference between `handle_call` and `handle_cast`, and what does each callback return?

### elixir-otp-03

What kind of messages does `handle_info` handle, and how does that differ from `handle_call`/`handle_cast`?

### elixir-otp-04

Name the three main Supervisor restart strategies and what each does when one child crashes. What is a child spec?

### elixir-data-01

Elixir data is immutable. If `list = [1, 2, 3]` and you call `List.delete(list, 2)`, what happens to `list`? What does rebinding a variable actually do?

### elixir-data-02

Compare maps, keyword lists, and structs. When do you use each?

### elixir-data-03

What is the difference between `%{map | key: val}` and `Map.put(map, key, val)` when `key` does not already exist?

### elixir-data-04

How do `put_in`, `update_in`, and `get_in` help with nested immutable data, and why are they better than manual nesting?

### elixir-pipe-01

What does the pipe operator `|>` do, and what is the one rule about how the piped value is passed to the next function?

### elixir-pipe-02

What problem does the `with` special form solve, and how does it chain a sequence of `{:ok, _}` steps?

### elixir-pipe-03

In a `with` expression, what happens to a value that fails to match a `<-` clause, and what is the `else` block for?

### elixir-pipe-04

Give a case where piping is the wrong tool or reads badly, and how you'd fix it.

### elixir-error-01

Elixir favors `{:ok, value}` / `{:error, reason}` tuples over exceptions. When do you use tagged tuples versus raising?

### elixir-error-02

What is the convention behind a trailing `!` in function names like `File.read!` versus `File.read`?

### elixir-error-03

Explain `try/rescue/after` and how `rescue` differs from a `catch`. When is `after` guaranteed to run?

### elixir-error-04

Explain the "let it crash" philosophy. Why is crashing a process often better than defensively rescuing every error?

### elixir-enum-01

What is the core difference between the `Enum` and `Stream` modules regarding when work happens?

### elixir-enum-02

Give two situations where `Stream` is clearly the right choice over `Enum`.

### elixir-enum-03

What does `Enum.reduce/3` do, and why is it considered the fundamental building block behind `map`, `filter`, `sum`, etc.?

### elixir-enum-04

Explain the parts of a comprehension like `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`.

### elixir-proto-01

What is a protocol in Elixir, and how do `defprotocol` and `defimpl` provide polymorphism?

### elixir-proto-02

What is a behaviour, and what do `@callback`, `@behaviour`, and `@impl` each do?

### elixir-proto-03

Protocols and behaviours both look like "interfaces." What is the fundamental difference between them?

### elixir-proto-04

Why annotate a callback implementation with `@impl true` (or `@impl MyBehaviour`) — what does it buy you?

### elixir-conc-01

Describe the BEAM concurrency model. Why is there no shared mutable memory between processes, and why does that matter?

### elixir-conc-02

How do `Task.async` and `Task.await` run work concurrently, and what happens if the task raises or times out?

### elixir-conc-03

What is an `Agent`, and how does it differ from writing a full `GenServer`?

### elixir-conc-04

What is ETS and when would you use it instead of a process (GenServer/Agent) to hold shared data?
