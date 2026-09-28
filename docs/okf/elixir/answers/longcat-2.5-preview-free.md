- id: elixir-pm-01
  answer: |
    `{:ok, value} = fetch()` attempts to pattern match the return value of `fetch()` against the tuple `{:ok, value}`. If `fetch()` returns `{:ok, 42}`, the match succeeds and `value` is bound to `42`. If `fetch()` returns `{:error, :notfound}`, the match fails because the first element `:error` does not match `:ok`, and a `MatchError` is raised at runtime.

- id: elixir-pm-02
  answer: |
    Multiple function-head clauses let you define the same function name with different patterns and guards. Elixir tries each clause top-to-bottom at call time; the first clause whose argument patterns all match and whose guard expressions all evaluate to true is the one that runs. Guards (introduced with `when`) let you test properties like type, value, or arithmetic conditions without embedding `if` inside the function body.

- id: elixir-pm-03
  answer: |
    The pin operator `^` prevents rebinding a variable on the left side of a match and instead uses the variable's existing value as the pattern. With `^x`, the left side matches only if the right side equals the current value of `x`. A bare `x` on the left always matches anything and rebinds `x` to that value.

- id: elixir-pm-04
  answer: |
    Map pattern matching is partial: `%{name: n} = user` matches as long as `user` is a map containing the key `:name`; other keys are ignored. List and tuple matching, by contrast, are exact — `[a, b] = [1, 2]` requires exactly two elements, and `{a, b} = {1, 2}` requires exactly a two-tuple.

- id: elixir-otp-01
  answer: |
    `spawn` creates a new process and returns its PID. `send` sends a message to a process (by PID or name) and returns the message; delivery is asynchronous and not guaranteed. `receive` blocks the calling process until a message arrives that matches one of its clauses; if no message matches, the process waits indefinitely (or until a timeout is specified with `after`).

- id: elixir-otp-02
  answer: |
    `handle_call` handles synchronous requests made with `GenServer.call`; it must return a reply, typically `{:reply, response, new_state}` (or `{:stop, reason, reply, new_state}`). `handle_cast` handles asynchronous requests made with `GenServer.cast`; it never replies and returns `{:noreply, new_state}` (or a stop tuple). The key difference is that `call` blocks the caller waiting for a reply, while `cast` is fire-and-forget.

- id: elixir-otp-03
  answer: |
    `handle_info` handles messages that were sent directly to the process mailbox — i.e., messages sent with `send` or a bare PID, not through `GenServer.call`/`cast`. This includes messages from other processes, system messages, and timeout signals. `handle_call` and `handle_cast` only handle messages sent through the GenServer API, which tag the message with a unique reference so the GenServer can route them to the correct callback.

- id: elixir-otp-04
  answer: |
    The three main restart strategies are `:one_for_one` (only the crashed child is restarted), `:one_for_all` (if one child crashes, all children are terminated and then restarted in order), and `:rest_for_one` (the crashed child and all children started after it are terminated and restarted). A child spec is a map (or tuple) describing how to start, identify, and supervise a child process — it includes the child's ID, start function, restart type, and shutdown strategy.

- id: elixir-data-01
  answer: |
    `List.delete(list, 2)` returns a new list `[1, 3]`; the original `list` variable still points to `[1, 2, 3]` because Elixir data is immutable. Rebinding a variable does not mutate the underlying data — it creates a new binding of the same name to a different value. The old value still exists as long as something references it.

- id: elixir-data-02
  answer: |
    Maps are key-value stores with fast lookup, arbitrary keys, and no ordering guarantee; use them for structured data. Keyword lists are lists of two-element tuples where the first element is an atom; they preserve order, allow duplicate keys, and are commonly used for options and function arguments. Structs are maps with a fixed set of enforced keys and a `__struct__` field identifying their type; use them when you want compile-time guarantees about which fields exist.

- id: elixir-data-03
  answer: |
    `%{map | key: val}` requires `key` to already exist in the map; if it does not, it raises a `KeyError`. `Map.put(map, key, val)` works whether or not the key exists — it inserts a new key if absent and overwrites if present. So the update syntax is a safe assertion that the key is already there, while `Map.put` is the general-purpose setter.

- id: elixir-data-04
  answer: |
    `put_in`, `update_in`, and `get_in` provide concise access to nested immutable data. `get_in` reads a value at a path, `put_in` replaces it, and `update_in` applies a function to it. They are better than manual nesting because they handle the boilerplate of rebuilding each intermediate layer of the data structure automatically, and they return the full updated structure rather than requiring you to manually reconstruct each level.

- id: elixir-pipe-01
  answer: |
    The pipe operator `|>` takes the value on its left and inserts it as the first argument of the function call on its right. The one rule is that the piped value always becomes the first argument of the next function, so you must design or call functions with that argument order in mind.

- id: elixir-pipe-02
  answer: |
    The `with` special form solves the problem of chaining multiple steps that can each fail (returning `{:error, reason}`) without deeply nested `case` expressions. It chains a sequence of `<-` bindings: if every pattern matches, the `do` block runs with all bound variables in scope. If any pattern fails to match, `with` short-circuits and returns the non-matching value directly.

- id: elixir-pipe-03
  answer: |
    When a `<-` clause in a `with` fails to match, the `with` expression immediately returns the non-matching value (e.g., `{:error, :notfound}`) and does not evaluate any subsequent clauses or the `do` block. The `else` block provides pattern-matching clauses to handle those non-matching values explicitly, letting you transform or handle different failure reasons before they propagate outward.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the data flow is not linear — for example, when you need to use a value in multiple branches, when intermediate steps require several arguments that don't fit the first-argument convention, or when the pipeline becomes so long that naming intermediate results would aid clarity. The fix is to break the pipeline into named intermediate variables or use `with` for branching logic, so each step is explicit and the data flow is easy to follow.

- id: elixir-error-01
  answer: |
    Tagged tuples (`{:ok, value}` / `{:error, reason}`) are used for expected, recoverable outcomes — the kind of failure that is part of normal program flow and that callers should handle explicitly. Raising is reserved for truly exceptional situations where continuing is impossible or would produce incorrect results, such as programming errors, invalid internal state, or unrecoverable resource failures.

- id: elixir-error-02
  answer: |
    The trailing `!` convention indicates a "bang" version of a function that raises an exception on failure instead of returning an error tuple. `File.read` returns `{:ok, content}` or `{:error, reason}`; `File.read!` returns the content directly on success or raises on failure. The non-bang version is for when you want to handle errors explicitly; the bang version is for when an error is truly exceptional and you want it to crash.

- id: elixir-error-03
  answer: |
    `try/rescue/after` provides structured exception handling. `rescue` catches exceptions raised in the `try` body and matches them by type or pattern, similar to `catch` in other languages but specifically for exceptions (not thrown values or exits). `after` runs regardless of whether the body succeeded or raised, making it suitable for cleanup. `rescue` differs from `catch` in that `catch` handles `throw` and `exit` signals, while `rescue` handles only exceptions (errors).

- id: elixir-error-04
  answer: |
    "Let it crash" means that instead of defensively rescuing every possible error inside a process, you let the process fail and rely on a supervisor to restart it in a clean state. This is better because it avoids complex, error-prone defensive code, keeps the happy path clean, and ensures that after a crash the process restarts from a known-good state rather than continuing with potentially corrupted internal state.

- id: elixir-enum-01
  answer: |
    `Enum` is eager — it processes the entire collection immediately and returns a concrete result (a list, a sum, etc.). `Stream` is lazy — it returns a composable data structure that describes the computation but does not execute it until it is enumerated (e.g., by passing it to `Enum`). This means `Stream` can represent infinite or very large sequences and can fuse multiple operations into a single pass.

- id: elixir-enum-02
  answer: |
    Two situations where `Stream` is clearly the right choice: (1) working with large or infinite data sources such as files or network responses where you want to process items one at a time without loading everything into memory; (2) chaining multiple transformations (map, filter, etc.) over a large collection where you want to fuse them into a single pass rather than creating intermediate lists at each step.

- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` takes an enumerable, an accumulator, and a function, and folds the enumerable into a single value by applying the function to each element and the running accumulator. It is the fundamental building block because `map`, `filter`, `sum`, `count`, and many other `Enum` functions can all be implemented in terms of `reduce` — they are essentially `reduce` with different accumulator logic and result extraction.

- id: elixir-enum-04
  answer: |
    In `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`: `x <- list` is the generator that binds `x` to each element; `rem(x, 2) == 0` is a filter that keeps only even numbers; `into: %{}` specifies that results are collected into a map; `do: {x, x * x}` is the body that produces a key-value tuple for each surviving element. The result is a map of even numbers to their squares.

- id: elixir-proto-01
  answer: |
    A protocol is a polymorphism mechanism that defines a set of functions that can be implemented for different types. `defprotocol` declares the protocol and its function signatures. `defimpl` provides a concrete implementation of those functions for a specific type. Together they let you write generic code that dispatches to the correct implementation at runtime based on the type of the first argument.

- id: elixir-proto-02
  answer: |
    A behaviour is a module that defines a set of function callbacks that other modules must implement, establishing a contract. `@callback` declares the expected signature of a callback function. `@behaviour` is placed in the module that defines the behaviour, declaring which callbacks it expects. `@impl` is placed in an implementing module to mark that the following functions are implementations of a specific behaviour's callbacks, enabling compile-time checks.

- id: elixir-proto-03
  answer: |
    The fundamental difference is dispatch: protocols dispatch on the type of the first argument at runtime, allowing you to add new implementations for new types without modifying existing code. Behaviours dispatch at compile time based on the module that is passed as an argument — the caller explicitly names the implementing module, and the functions are called directly on that module. Protocols are about extending behaviour to types; behaviours are about defining a contract that modules explicitly fulfill.

- id: elixir-proto-04
  answer: |
    Annotating with `@impl true` or `@impl MyBehaviour` tells the compiler that the following functions are implementations of a specific behaviour. This buys you compile-time verification that you have implemented all required callbacks with the correct arity, and it makes the code's intent explicit. Without it, the compiler cannot check that your module satisfies the behaviour's contract.

- id: elixir-conc-01
  answer: |
    The BEAM (Erlang VM) runs lightweight processes that are isolated from each other — each has its own heap and stack, and there is no shared mutable memory. Communication between processes happens only via message passing. This matters because it eliminates data races and the need for locks, making concurrent programs easier to reason about and more resilient to failure.

- id: elixir-conc-02
  answer: |
    `Task.async` spawns a process to run a function concurrently and returns a task reference. `Task.await` blocks the caller until the task completes, returning `{:ok, result}` on success. If the task raises an exception, `Task.await` re-raises it in the calling process. If the task does not complete within the timeout (default 5 seconds), `Task.await` exits the caller.

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple abstraction around a GenServer that holds state and supports `get` and `update` operations. It differs from a full GenServer in that it is purpose-built for the single use case of maintaining shared state — you provide an initial value and functions to read and mutate it, without needing to define callbacks, handle messages, or manage a process loop. Use an Agent when you only need shared mutable state; use a GenServer when you need custom message handling, complex state transitions, or lifecycle management.

- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is an in-memory key-value store built into the VM that can be accessed by any process without going through a GenServer. You would use ETS instead of a process when you need high-performance, concurrent read/write access to shared data and can tolerate that the data lives outside the process supervision tree — for example, caching, lookup tables, or session storage. Unlike a GenServer, ETS does not provide built-in serialization of writes or automatic cleanup on process death.
