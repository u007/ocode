# nvidia/nemotron-3.5-super-vl — closed-book answers for elixir (openrouter, 2026-10-16); answerer saw only _prompts/elixir.md
- id: elixir-pm-01
  answer: |
    It pattern-matches the result of `fetch()` against `{:ok, value}`. If `fetch()` returns `{:ok, something}`, `value` is bound to `something`; if the right side has any other shape — e.g. `{:error, :notfound}` — the match fails and a `MatchError` is raised.
- id: elixir-pm-02
  answer: |
    Function-head clauses are tried top to bottom; the first clause whose pattern matches the arguments AND whose guard evaluates to true runs. Guards let one pattern split into multiple cases (for example, `def f(x) when is_integer(x) and x > 0`, `def f(x) when is_integer(x)`, `def f(_)`), replacing nested `if/cond` with declarative dispatch.
- id: elixir-pm-03
  answer: |
    The pin operator `^x` pins the existing value of variable `x`: the match succeeds only if the right side equals that value, and `x` is not rebound. A bare `x` on the left always matches and rebinds `x` to the right-hand value.
- id: elixir-pm-04
  answer: |
    `%{name: n} = user` matches any map containing the `:name` key and binds `n` to its value — extra keys are allowed, so it does NOT require exactly those keys. List and tuple matching, by contrast, requires exact structure and arity.
- id: elixir-otp-01
  answer: |
    `spawn/1` creates a new lightweight process and returns its PID. `send/2` (or `pid <- msg`) puts a message in that process's mailbox. `receive do ... end` blocks the current process waiting for a message matching one of its clauses; if no message matches, it blocks indefinitely unless an `after` timeout is supplied.
- id: elixir-otp-02
  answer: |
    `handle_call` handles synchronous requests: the caller blocks until a reply is sent, and the callback returns `{:reply, reply, state}` or `{:noreply, state}` (optionally with a timeout). `handle_cast` handles asynchronous messages: no reply is expected, and it returns `{:noreply, state}` (optionally with a timeout).
- id: elixir-otp-03
  answer: |
    `handle_info` is the catch-all for anything that is not a call or cast: plain `send` messages, system messages, and timeout messages (`Process.send_after/4`, etc.). `handle_call`/`handle_cast` are part of the GenServer protocol with explicit reply contracts; `handle_info` has no reply contract.
- id: elixir-otp-04
  answer: |
    `:one_for_one` restarts only the crashed child. `:one_for_all` restarts all children. `:rest_for_one` restarts the crashed child and every child started after it. A child spec is a map (usually from `child_spec/1`) describing how to start, stop, and supervise a child — keys include `:id`, `:start`, `:restart`, `:shutdown`, `:type`, and `:modules`.
- id: elixir-data-01
  answer: |
    `list` is unchanged — still `[1, 2, 3]`. `List.delete(list, 2)` returns a new list `[1, 3]`; the original is never mutated. Rebinding a variable just makes the name refer to a new value; the old value remains intact (and is garbage-collected if unreferenced).
- id: elixir-data-02
  answer: |
    Maps: unordered key-value stores with fast lookup and any key type, for general data. Keyword lists: lists of two-tuples `[{:k, v}]`, ordered, allowing duplicate keys, conventionally used for options. Structs: maps with a fixed `__struct__` field defined by `defstruct`, with compile-time-known keys, used as custom typed records.
- id: elixir-data-03
  answer: |
    `%{map | key: val}` updates only existing keys and raises `KeyError` if `key` is missing. `Map.put(map, key, val)` adds the key if absent or updates it if present; it never raises for a missing key.
- id: elixir-data-04
  answer: |
    `get_in/2`, `put_in/3`, and `update_in/3` take a list of keys (a path) and read, write, or update nested structures immutably, returning new structures. They avoid deeply nested manual `Map.update`/`put` boilerplate, compose via the `Access` behaviour, and keep the code readable while preserving immutability.
- id: elixir-pipe-01
  answer: |
    `|>` takes the result of the left expression and passes it as the first argument to the function call on the right: `a |> b(c)` becomes `b(a, c)`. The one rule: the piped value is always inserted as the first argument of the right-hand call.
- id: elixir-pipe-02
  answer: |
    `with` avoids nested `case` when chaining operations that return `{:ok, value}` / `{:error, reason}`. Each `pat <- expr` must match; if every clause matches, the `do:` block runs with the bound values. Any mismatch short-circuits and returns that value immediately.
- id: elixir-pipe-03
  answer: |
    A value that fails to match a `<-` clause is returned immediately — the whole `with` short-circuits with that value. The `else` block contains clauses that pattern-match on that non-matching value; if no `else` clause matches either, an error is raised.
- id: elixir-pipe-04
  answer: |
    Piping is the wrong tool when the value must land in a non-first argument position or when combining several independent values (for example, `Enum.zip(a, b)` or `Map.merge(map1, map2)`), since the pipe can only feed the first argument — it reads awkwardly and hides the real structure. Fix it with a plain function call, `with`, or an explicit capture.
- id: elixir-error-01
  answer: |
    Use tagged tuples (`{:ok, value}` / `{:error, reason}`) for expected, recoverable failures the caller should handle (validation, not found, timeouts). Raise for exceptional, programmer-error, or unrecoverable conditions (bad arguments, violated invariants, internal bugs).
- id: elixir-error-02
  answer: |
    A trailing `!` means "raises on failure instead of returning an `{:ok, :error}` tuple." `File.read/1` returns `{:ok, body}` or `{:error, reason}`; `File.read!/1` returns `body` directly or raises an exception.
- id: elixir-error-03
  answer: |
    `try/rescue/after` evaluates a body; `rescue` pattern-matches on exceptions raised inside that body (e.g. `rescue e in EOFError -> ...`), while `catch` is the older mechanism for caught throws/exits. `after` always runs — whether the body succeeded, raised, or was rescued — and is used for cleanup, running before the result or exception continues.
- id: elixir-error-04
  answer: |
    "Let it crash" means isolating risky work in a process and letting it crash on failure instead of defensively rescuing every error; a supervisor restarts it to a known-good state. It is better because defensive masking hides bugs and corrupts state, while crashes are cheap, processes are isolated, and restarts restore consistency.
- id: elixir-enum-01
  answer: |
    `Enum` is eager: it walks the collection immediately and returns a concrete result. `Stream` is lazy: it builds a composable recipe and computes nothing until the stream is enumerated (for example by `Enum.to_list/1`).
- id: elixir-enum-02
  answer: |
    Infinite or very large sequences (`Stream.iterate/2`, `Stream.cycle/1`, reading a huge file line by line), and chaining many transformations over big collections without building intermediate lists in memory.
- id: elixir-enum-03
  answer: |
    `Enum.reduce(collection, acc, fun)` applies `fun` to each element while carrying an accumulator. It is the fundamental building block because `map`, `filter`, `sum`, `max`, and similar can all be implemented as reductions.
- id: elixir-enum-04
  answer: |
    `x <- list` generates each element; `rem(x, 2) == 0` filters to keep evens; `into: %{}` collects results into a map; `do: {x, x * x}` produces each entry as a key-value pair. The result is a map of each even number to its square.
- id: elixir-proto-01
  answer: |
    A protocol defines a set of functions that dispatch on the type of their first argument. `defprotocol/2` declares the protocol and its functions; `defimpl/2` provides an implementation for a particular type. Calling a protocol function dispatches to the impl for the argument's runtime type — runtime polymorphism.
- id: elixir-proto-02
  answer: |
    A behaviour is a module-level contract (like an interface) listing required callbacks. `@callback/2` declares a callback name and spec; `@behaviour/1` marks a module as implementing a behaviour; `@impl true` or `@impl MyBehaviour` marks a function as implementing a behaviour callback and enables compile-time checks.
- id: elixir-proto-03
  answer: |
    Protocols dispatch on the data type at runtime — any type can implement a protocol via `defimpl`, even types you do not control. Behaviours are compile-time module contracts — a module either exports the required callbacks or not. Protocols are about data types; behaviours are about module structure.
- id: elixir-proto-04
  answer: |
    `@impl true` (or `@impl MyBehaviour`) tells the compiler the function is meant to implement a behaviour callback. It verifies the name and arity match and warns on mismatches, catching typos and documenting intent.
- id: elixir-conc-01
  answer: |
    The BEAM runs thousands of lightweight, isolated processes, each with its own heap. They share no memory and communicate only by sending messages; each process is garbage-collected independently. This matters because there are no locks, no shared-state corruption, and a crash in one process cannot corrupt another's memory.
- id: elixir-conc-02
  answer: |
    `Task.async/1` spawns a process running `fun` and returns a `%Task{}`. `Task.await/2` blocks until that process sends its result and returns it, raising if the task raised. With a `:timeout`, `await` raises if the deadline passes, and the task may be shut down depending on options.
- id: elixir-conc-03
  answer: |
    An `Agent` is a minimal process holding state, accessed with `Agent.get/3` and `Agent.update/3` — state is just a value, no callbacks. A `GenServer` is a full OTP server with `handle_call`, `handle_cast`, and `handle_info` callbacks, supporting sync and async messages, timeouts, and supervision.
- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is an in-memory key-value table with concurrent read/write access, owned by the process that created it. Use it instead of a GenServer/Agent when many processes need fast shared state — reads and writes do not serialize through a single mailbox, so it scales better under high contention.
