- id: elixir-pm-01
  answer: |
    `{:ok, value} = fetch()` is a pattern match. It attempts to match the return value of `fetch()` against the tuple pattern `{:ok, value}`. If `fetch()` returns `{:ok, some_value}`, the match succeeds and `value` is bound to `some_value`. If `fetch()` returns `{:error, :notfound}`, the match fails because the first element `:error` does not match `:ok`, and Elixir raises a `MatchError` at that line.

- id: elixir-pm-02
  answer: |
    Multiple function-head clauses plus guards replace conditional logic by letting the runtime select which function head to execute based on pattern matching and guard expressions. You define several heads for the same function name/arity, each with different parameter patterns and optional `when` guards. At call time, Elixir tries each clause top-to-bottom; the first clause whose parameter patterns all match the arguments AND whose guard expression evaluates to `true` is the one that runs. If no clause matches, a `FunctionClauseError` is raised. This replaces `if`/`case` inside a single function body with declarative, data-driven dispatch.

- id: elixir-pm-03
  answer: |
    The pin operator `^` in a pattern means "match against the current value of this variable" rather than rebinding it. With `^x`, the pattern only succeeds if the right-hand side equals the current value of `x`; it never reassigns `x`. With a bare `x` on the left of `=`, the variable is (re)bound to whatever the right-hand side is, regardless of its previous value. So `^x` is for asserting equality with an existing binding, while bare `x` is for creating a new binding.

- id: elixir-pm-04
  answer: |
    Map pattern matching is a subset match: `%{name: n} = user` only requires that `user` is a map containing at least the key `:name`; extra keys are ignored and `n` is bound to the value of `:name`. This differs from tuple and list matching, which require an exact structural match — `{a, b} = tuple` requires exactly two elements, and `[h | t] = list` requires a non-empty list. Maps are the only common structure where partial key matching is the default behavior.

- id: elixir-otp-01
  answer: |
    `spawn` creates a new lightweight process and returns its PID. `send` sends a message to a process (by PID or name); it is asynchronous and returns the message immediately. `receive` blocks the calling process, scanning its mailbox for a message that matches one of its clauses; when a match is found, that clause executes. If no message matches, `receive` blocks indefinitely (or until the optional `timeout` expires, after which the `after` clause runs). Messages that don't match any clause remain in the mailbox for future `receive` calls.

- id: elixir-otp-02
  answer: |
    `handle_call` handles synchronous requests made via `GenServer.call`. The client blocks waiting for a response. It returns a 3-tuple like `{:reply, response, new_state}` (reply and continue), `{:reply, response, new_state, timeout_or_hibernate}`, or `{:stop, reason, reply, new_state}` (stop the server). `handle_cast` handles asynchronous requests made via `GenServer.cast`; the client does not wait. It returns `{:noreply, new_state}` or `{:stop, reason, new_state}`. The key difference is that `handle_call` must reply to the caller, while `handle_cast` never replies.

- id: elixir-otp-03
  answer: |
    `handle_info` handles raw messages that arrive in the GenServer's mailbox via `send/2`, `Kernel.send`, or messages sent directly to the PID — anything that is not a `GenServer.call` or `GenServer.cast`. `handle_call` and `handle_cast` handle the specific request/response protocol messages that the GenServer behaviour intercepts and wraps. `handle_info` is the catch-all for everything else, including messages from other processes, timeouts (`:timeout`), and monitor `:DOWN` messages.

- id: elixir-otp-04
  answer: |
    The three main Supervisor restart strategies are: `:one_for_one` — if a child crashes, only that child is restarted; `:one_for_all` — if any child crashes, all children are terminated and then all are restarted; `:rest_for_one` — if a child crashes, that child and all children started after it (in start order) are terminated and restarted. A child spec is a map (or tuple) describing how a supervisor manages a child: its start function (`{module, :start_link, args}`), restart type (`:permanent`, `:temporary`, `:transient`), shutdown strategy, and unique id.

- id: elixir-data-01
  answer: |
    `List.delete(list, 2)` returns a new list `[1, 3]`; the original `list` is unchanged because Elixir data is immutable — no function mutates in place. Rebinding a variable (e.g., `list = List.delete(list, 2)`) does not mutate the old value; it creates a new binding where the name `list` now points to a different (new) value in memory. The old value still exists until garbage collected, and any other reference to it is unaffected.

- id elixir-data-02
  answer: |
    Maps are key-value stores with arbitrary keys, O(1) access, and no ordering guarantee; use them for general-purpose structured data. Keyword lists are lists of `{key, value}` tuples where keys are atoms, preserving order and allowing duplicate keys; use them for function options and small ordered configurations. Structs are maps with a fixed set of known keys enforced at compile time, plus default values; use them when you want a map with a guaranteed shape (e.g., a struct enforces that only declared fields exist and provides dot-access syntax).

- id: elixir-data-03
  answer: |
    `%{map | key: val}` is the update syntax for maps; it raises a `KeyError` if `key` does not already exist in the map, because its purpose is to update existing keys. `Map.put(map, key, val)` does not raise — it inserts the key if absent or overwrites it if present. So the difference is that the pipe-update syntax requires the key to pre-exist, while `Map.put` is unconditional.

- id: elixir-data-04
  answer: |
    `put_in`, `update_in`, and `get_in` provide functional access to nested immutable data structures. `get_in` traverses a path of keys to read a value; `put_in` sets a value at a path, returning a new top-level structure; `update_in` applies a function to the value at a path. They are better than manual nesting because they handle the boilerplate of rebuilding each intermediate level of the structure for you, they share unchanged subtrees for efficiency, and they read cleanly as a single expression rather than a chain of nested `Map.get`/`Map.put` calls.

- id: elixir-pipe-01
  answer: |
    The pipe operator `|>` takes the value on its left and inserts it as the first argument of the function call on its right. The one rule is: the piped value always becomes the first argument of the next function. So `x |> f(a, b)` is `f(x, a, b)`, and `x |> g()` is `g(x)`. This lets you write data transformations as a left-to-right pipeline.

- id: elixir-pipe-02
  answer: |
    The `with` special form solves the problem of chaining multiple pattern matches where each step depends on the previous one succeeding, without deep nesting `case` expressions. You write a sequence of `<-` bindings; each right-hand side is evaluated and matched against the left pattern. If all match, the `do` block runs with all bindings in scope. If any fails to match, `with` short-circuits and returns the non-matching value immediately, letting you handle failures in an `else` block.

- id: elixir-pipe-03
  answer: |
    In a `with` expression, when a `<-` clause's right-hand side fails to match the left pattern, the entire `with` expression immediately returns that non-matching value (e.g., `{:error, reason}`) without evaluating any subsequent clauses or the `do` block. The `else` block is for pattern-matching on those failure values — it lets you transform different failure reasons into a common shape, so you can handle `{:error, :notfound}` and `{:error, :unauthorized}` differently, for example.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the data flow is not linear — for example, when you need to use an intermediate value in multiple later steps, or when the natural first-argument rule produces awkward code like `value |> Enum.map(&process(&1, config))` where `config` is not the first argument. Another case is when a step needs two previously computed values. The fix is to use intermediate `let`-style bindings (assign meaningful names to intermediate results) or to restructure the functions so the primary data argument comes first, making the pipeline read top-to-bottom.

- id: elixir-error-01
  answer: |
    Use tagged tuples (`{:ok, value}` / `{:error, reason}`) for expected, recoverable errors that are part of normal control flow — things like validation failures, missing records, or user input errors. The caller explicitly handles both cases. Use `raise` (or functions ending in `!`) for exceptional, unexpected situations that indicate a bug or unrecoverable state — things like a missing configuration file at startup or a violated invariant. The principle: tuples for expected failures, exceptions for truly exceptional ones.

- id: elixir-error-02
  answer: |
    The trailing `!` convention means the function will raise an exception on error instead of returning a `{:error, reason}` tuple. `File.read(path)` returns `{:ok, content}` or `{:error, reason}`; `File.read!(path)` returns the content directly or raises on error. The convention provides a pair: a safe version for handling errors explicitly, and a bang version for when you want to crash on failure (often used in scripts, tests, or when an error is truly unexpected).

- id: elixir-error-03
  answer: |
    `try/rescue/after` is Elixir's exception handling. The `try` block contains code that might raise. `rescue` catches exceptions (raised with `raise` or `throw`) and handles them by pattern-matching on the exception struct. `after` contains cleanup code that always runs, whether the `try` block succeeded, raised, or was exited — similar to `finally` in other languages. `rescue` differs from `catch`: `rescue` only catches exceptions (runtime errors), while `catch` (used with `try/catch`) catches thrown values (`throw`) and process exits (`exit`), which are not exceptions.

- id: elixir-error-04
  answer: |
    "Let it crash" is the philosophy that instead of defensively rescuing every possible error inside a process, you let the process crash when something unexpected happens and rely on a supervisor to restart it in a clean state. Crashing is often better because: (1) processes are isolated, so a crash does not corrupt other processes; (2) defensive code adds complexity and can hide bugs by silently continuing in a broken state; (3) a supervisor restarting from a known-good initial state is more reliable than trying to recover from an unknown corrupted state; (4) it separates business logic from error recovery, keeping code cleaner.

- id: elixir-enum-01
  answer: |
    The core difference is eager vs. lazy evaluation. `Enum` functions are eager: they process the entire collection immediately and return a concrete result (e.g., a list). `Stream` functions are lazy: they return a `Stream` struct that describes the computation but does nothing until you enumerate it (e.g., with `Enum.to_list` or `Enum.reduce`). This means `Stream` can represent infinite or very large collections and only computes values as needed.

- id: elixir-enum-02
  answer: |
    Two situations where `Stream` is clearly the right choice: (1) Working with large or infinite data sources — e.g., reading a huge file line-by-line with `File.stream!` or generating an infinite sequence — where eager `Enum` would load everything into memory. (2) When you only need a portion of the result — e.g., `Stream.filter(...) |> Enum.take(5)` stops processing after finding 5 matches, whereas `Enum.filter(...) |> Enum.take(5)` would process the entire collection first.

- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` takes a collection, an initial accumulator, and a function that maps each element and the current accumulator to a new accumulator. It folds the collection into a single value. It is the fundamental building block because `map` (transform each element, collecting results), `filter` (keep elements matching a predicate), `sum` (add all elements), `count`, and many others can all be implemented in terms of `reduce` — they are essentially `reduce` with specific accumulator logic and result shaping.

- id: elixir-enum-04
  answer: |
    In `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`: `x <- list` is the generator, producing each element of `list` in turn. `rem(x, 2) == 0` is a filter, keeping only even numbers. `into: %{}` specifies that results are collected into a map (instead of the default list). `do: {x, x * x}` is the body, producing a `{key, value}` tuple for each element — so the result is a map where each even number maps to its square.

- id: elixir-proto-01
  answer: |
    A protocol in Elixir is a polymorphism mechanism that defines a set of functions (a contract) that can be implemented for different data types. `defprotocol` declares the protocol and its function signatures. `defimpl` provides a concrete implementation of those functions for a specific type. When you call a protocol function, the runtime dispatches to the appropriate `defimpl` based on the type of the first argument, giving you polymorphism based on data type rather than module hierarchy.

- id: elixir-proto-02
  answer: |
    A behaviour is a module that defines a set of required functions (callbacks) that other modules must implement — it is a contract for module authors. `@callback` declares a function signature (name, arity, typespec) that implementors must follow. `@behaviour` is placed in a module to declare that it implements a particular behaviour, enabling compile-time checks. `@impl` (or `@impl true` / `@impl MyBehaviour`) is placed above a function to tell the compiler it implements a specific callback, so the compiler can verify the signature matches.

- id: elixir-proto-03
  answer: |
    The fundamental difference is dispatch mechanism. Protocols dispatch on the data type of the first argument — the same protocol function call runs different code depending on what type the data is (e.g., `to_string(1)` vs `to_string([])`). Behaviours dispatch on the module — the caller explicitly calls a function on a specific module, and that module must implement the behaviour's callbacks. Protocols are about "what is this data?"; behaviours are about "who is responsible for this?"

- id: elixir-proto-04
  answer: |
    Annotating with `@impl true` (or `@impl MyBehaviour`) tells the compiler that the function implements a specific callback from a behaviour. This buys you compile-time verification: the compiler checks that the function's signature matches the `@callback` declaration, catching mismatches in arity or typespec early. Without `@impl`, the compiler cannot distinguish callback implementations from regular functions, so it cannot verify correctness.

- id: elixir-conc-01
  answer: |
    The BEAM (Erlang VM) concurrency model uses lightweight processes that are isolated from each other — each process has its own heap and stack, and there is no shared mutable memory between processes. Processes communicate exclusively via asynchronous message passing. This matters because it eliminates entire classes of concurrency bugs: no data races, no deadlocks from lock contention, no need for mutexes or semaphores. A crash in one process cannot corrupt the memory of another.

- id: elixir-conc-02
  answer: |
    `Task.async` spawns a lightweight process to run a function and returns a `%Task{}` struct (a reference to the process). `Task.await` blocks the caller until the task process sends back its result (or times out). If the task function raises an exception, `Task.await` re-raises that exception in the calling process. If the task does not complete within the timeout (default 5 seconds), `Task.await` exits the caller, and the task process is automatically shut down.

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple wrapper around a GenServer that holds state. You start it with an initial value, then read (`Agent.get`) and update (`Agent.update`) it with functions. It differs from a full GenServer in that it is much less boilerplate — you do not write `init`, `handle_call`, or `handle_cast` — but it is also less flexible: you cannot handle custom messages, implement custom termination logic, or use OTP features like hot code upgrades. Use an Agent for simple shared state; use a GenServer when you need custom behaviour.

- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is an in-memory key-value store built into the VM that supports concurrent read and write access from multiple processes without going through a process. You would use ETS instead of a process (GenServer/Agent) when: (1) you need very fast concurrent access to shared data without message-passing overhead; (2) the data does not need complex update logic or serialization; (3) you want multiple processes to read/write simultaneously without bottlenecking on a single process mailbox. ETS tables are not garbage collected until deleted, and they survive process crashes.
- id: elixir-data-02
  answer: |
    **Maps** are the general-purpose key-value store. Keys can be any type (atoms, strings, tuples, etc.), and access is O(1) for larger maps (backed by HAMT). Use maps when you have dynamic or unknown keys, arbitrary key types, or data shaped like JSON (external APIs, config files, protocol messages). Pattern matching works naturally. Trade-off: no compile-time guarantee about which keys exist.

    **Keyword lists** are lists of `{atom, value}` tuples, written `[key: val]` for ergonomics. They preserve order and allow duplicate keys. Use them for function options and DSLs — anywhere you want a lightweight, ordered bag of named settings passed to a function (e.g., `def foo(opts \\ [])`). They are O(n) to access, so avoid them for large datasets. They are the idiomatic choice for optional parameters and configuration.

    **Structs** are maps with a compile-time-enforced schema. They have a fixed set of fields defined in `defstruct`, provide default values, and carry a `__struct__` tag for pattern matching on type. Use structs for domain models where the shape is known and stable — you get compile-time field-name checking, defaults, and the ability to pattern match on the struct type itself. You cannot add arbitrary fields to a struct (unlike a map), which is the point: it enforces a contract.

    **Rule of thumb:** keyword lists for ordered options/DSLs, structs for fixed-schema domain entities, maps for everything else (dynamic keys, external data, large datasets).
