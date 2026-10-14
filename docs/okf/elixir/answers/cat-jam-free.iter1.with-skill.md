# cat-jam-free — WITH-SKILL closed-book answers for elixir (opencode-go, 2026-10-14)
# Delivery: derived/elixir.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/elixir.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: elixir-pm-01
  answer: |
    `{:ok, value} = fetch()` matches fetch()'s result against `{:ok, value}`: it binds `value` only if the tuple's first element is `:ok`. If the result is `{:error, :notfound}`, no clause matches and Elixir raises `MatchError`, binding nothing.

- id: elixir-pm-02
  answer: |
    Multiple clauses are tried top to bottom. A clause runs only when its patterns match the arguments and its `when` guard, if present, is true. If patterns/guards all fail, Elixir raises `FunctionClauseError`.

- id: elixir-pm-03
  answer: |
    `^x` in a pattern means "match this position against the current value of x" rather than bind it. A bare `x` in a pattern binds x to whatever is there, shadowing/rebinding the name.

- id: elixir-otp-01
  answer: |
    `spawn` starts a BEAM process and returns its pid; `send` delivers a message asynchronously; `receive` blocks until a message matches a clause. If nothing matches, it waits, using an optional `after` timeout if provided.

- id: elixir-otp-02
  answer: |
    `handle_call` handles synchronous `GenServer.call` and returns something like `{:reply, reply, state}`; `handle_cast` handles asynchronous `GenServer.cast` and returns `{:noreply, state}` because there is no caller to reply to.

- id: elixir-otp-03
  answer: |
    `handle_info` handles messages sent directly to the process, such as raw `send`, timers, and monitor messages. Unlike call/cast wrappers, it is asynchronous and has no client expecting a reply.

- id: elixir-otp-04
  answer: |
    Strategies: `:one_for_one` restarts only the crashed child; `:one_for_all` restarts all children; `:rest_for_one` restarts the crashed child plus children started after it. A child spec describes id, start module/function, restart policy, shutdown, type, and modules.

- id: elixir-data-01
  answer: |
    `list` remains `[1, 2, 3]`; `List.delete/2` returns a new list. Rebinding a variable only changes that name to point at a new immutable value; the old data is never mutated.

- id: elixir-data-02
  answer: |
    Use maps for general key-value data, keyword lists for ordered atom keys and duplicate keys such as function options, and structs for typed records with fixed fields and defaults.

- id: elixir-data-03
  answer: |
    `%{map | key: val}` requires `key` to already exist and raises `KeyError` otherwise. `Map.put(map, key, val)` inserts the key if absent or updates it if present.

- id: elixir-data-04
  answer: |
    `get_in/2` reads a nested path, while `put_in/3` and `update_in/3` immutably replace or transform nested values. They are better than manual nesting because the path is concise, composable, and avoids rebuilding every container by hand.

- id: elixir-pipe-01
  answer: |
    `x |> f(a)` evaluates to `f(x, a)`: the piped value becomes the first argument. Its purpose is flattening nested calls so data flows top to bottom; piping a single call adds nothing.

- id: elixir-pipe-02
  answer: |
    `with` chains steps with `<-` and runs the `do` block only if every pattern matches. It solves nested `{:ok, _}` handling by returning the `do` result on success and short-circuiting on the first non-match.

- id: elixir-pipe-03
  answer: |
    A non-matching `<-` value is returned as the result of the whole `with`, unchanged. An `else` block can transform those failures; if `else` is present and no clause matches, `WithClauseError` is raised.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the value is not the first parameter or the flow branches, e.g. `foo(x) |> bar(y, _)` is not direct. Fix it with an intermediate variable or `then(x, fn v -> bar(y, v) end)`.

- id: elixir-error-01
  answer: |
    Use tagged result tuples for expected, recoverable outcomes that callers branch on normally. Raise exceptions for unexpected or exceptional failures, invariant violations, and programmer errors.

- id: elixir-error-02
  answer: |
    A trailing `!` names a function that raises on failure instead of returning `{:error, reason}`. The non-bang version returns the tagged tuple so callers can handle the failure.

- id: elixir-error-03
  answer: |
    `try` runs a block; `rescue` handles exceptions, optionally by type; `catch` can also handle thrown values and exits. `after` runs cleanup on normal completion or when an exception is rescued, but not if the process is killed.

- id: elixir-error-04
  answer: |
    "Let it crash" isolates faults in processes and lets supervisors restart them with clean state. Crashing avoids partial corruption and defensive complexity; rescue only where meaningful local recovery is needed.

- id: elixir-enum-01
  answer: |
    `Enum` is eager: transformations run immediately and build concrete results. `Stream` is lazy: transformations compose without running until a consumer pulls values.

- id: elixir-enum-02
  answer: |
    Use `Stream` for very large or infinite inputs where memory matters, and when you want early termination without computing or allocating every element first.

- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` folds a collection into an accumulator, updating it for each element. Map, filter, and sum are specialized versions of that same accumulator-update pattern.

- id: elixir-enum-04
  answer: |
    In `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`, `x <- list` generates values, `rem(x,2)==0` filters, `into: %{}` collects into a map, and `do: {x, x*x}` produces each key-value entry.

- id: elixir-proto-01
  answer: |
    A protocol defines polymorphic behavior dispatched by the type of a value. `defprotocol` declares the functions; `defimpl` provides an implementation for a specific type.

- id: elixir-proto-02
  answer: |
    A behaviour is a module contract. `@callback` declares a required function signature; `@behaviour` marks a module as implementing it; `@impl` annotates and verifies the implementing function.

- id: elixir-proto-03
  answer: |
    Protocols provide runtime polymorphism over data types. Behaviours are compile-time contracts between modules checked by the compiler; they do not perform runtime dispatch.

- id: elixir-proto-04
  answer: |
    `@impl true` or `@impl MyBehaviour` tells the compiler to verify that the function matches a declared callback, catching typos and signature mismatches while documenting intent.

- id: elixir-conc-01
  answer: |
    BEAM runs many lightweight, preemptively scheduled processes with no shared mutable memory; they communicate by message passing. Isolation prevents races, limits fault impact, and enables supervision and restart.

- id: elixir-conc-02
  answer: |
    `Task.async` spawns a process to run work and returns a handle; `Task.await` waits for the result with a timeout. If the task crashes or times out, await raises/propagates failure instead of returning a normal result.

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple process wrapping state with get/update helpers. A full `GenServer` gives explicit callbacks, request/reply semantics, and lifecycle control; use Agent for simple state and GenServer for custom behavior.

- id: elixir-conc-04
  answer: |
    ETS is an in-memory table for shared data with concurrent access. Use it instead of a process for read-heavy, shared, or large data to avoid serializing access through one process mailbox.
- id: elixir-pm-04
  answer: |
    No. Map pattern matching is partial: `%{name: n} = user` matches any map that contains a
    `:name` key (and binds `n` to its value), ignoring any additional keys. This differs from
    tuple matching (`{a, b} = t`), which requires the tuple to have exactly that arity, and from
    list matching (`[a, b] = l`), which requires the list to have exactly that length. A map
    pattern therefore asserts "the map has at least these keys" rather than "the map equals this
    shape exactly."
