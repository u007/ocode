- id: elixir-pm-01
  answer: |
    `=` is the match operator, not assignment. `{:ok, value} = fetch()` asserts that the return value of `fetch()` matches the pattern `{:ok, value}` — it destructures the tuple, binds `value` to the second element, and verifies the first element is the literal `:ok`. If the right side is `{:error, :notfound}`, the pattern does not match and Elixir raises a `MatchError` at runtime. This is the key distinction: a literal on the left side of `=` is checked for equality, not bound.

- id: elixir-pm-02
  answer: |
    Multiple function-head clauses with guards replace `if`/`case` conditional logic. Each `def` is a separate clause. Clauses are tried top to bottom at runtime; the first clause whose pattern matches the arguments and whose `when` guard evaluates to `true` runs. If no clause matches, a `FunctionClauseError` is raised. This means you should put specific clauses before general ones. Guards are restricted to a whitelist of guard-safe expressions: comparison operators, boolean and arithmetic operators, type-check functions (`is_integer/1`, `is_map/1`, etc.), `in` with compile-time lists/ranges, and a few Kernel functions like `length/1`, `map_size/1`, `elem/2`, `hd/1`, `rem/2`, `abs/1`. Arbitrary function calls (including your own) are not allowed in guards. `defguard` builds reusable guard macros from those same allowed expressions.

- id: elixir-pm-03
  answer: |
    The pin operator `^` in a pattern matches against the *current* value of a variable rather than rebinding it. `^x` on the left of a match asserts that the right-hand value equals the existing value of `x`. A bare `x` on the left always succeeds and rebinds `x` to whatever is on the right. For example, if `x = 1`, then `^x = 1` succeeds but `^x = 2` raises `MatchError`, whereas `x = 2` simply rebinds `x` to `2`.

- id: elixir-pm-04
  answer: |
    Map pattern matching is *partial* — `%{name: n} = user` only requires that the map contains the `:name` key; any other keys are ignored and allowed. This is different from tuple matching, which requires an exact structural match (same arity, same positions), and list matching, which requires a specific head/tail structure. So `%{name: n}` will match `%{:name => "Alice", :age => 30}` and bind `n` to `"Alice"`, whereas `{name, age} = {"Alice", 30}` requires exactly a 2-tuple.

- id: elixir-otp-01
  answer: |
    `spawn` creates a new lightweight process and returns its PID. `send` sends a message to a process identified by PID (or name); it is asynchronous and non-blocking — it puts the message in the target's mailbox and returns immediately. `receive` blocks the current process, waiting for a message that matches one of its pattern clauses. If no message matches, the process waits indefinitely (unless an `after` timeout clause is provided, in which case the timeout body runs). Messages that don't match any clause remain in the mailbox for future `receive` calls.

- id: elixir-otp-02
  answer: |
    `handle_call` handles synchronous requests made via `GenServer.call/2,3`. The caller blocks waiting for a reply. It returns `{:reply, response, new_state}` (or `{:reply, response, new_state, timeout/hibernate}`). `handle_cast` handles asynchronous requests made via `GenServer.cast/2`. The caller does not block. It returns `{:noreply, new_state}` (or `{:noreply, new_state, timeout/hibernate}`). The key difference: `call` is request-response with a reply; `cast` is fire-and-forget.

- id: elixir-otp-03
  answer: |
    `handle_info` handles raw messages that arrive in the GenServer's mailbox but were not sent through `GenServer.call` or `GenServer.cast`. These are messages sent directly via `send/2` (or `Kernel.send/2`) to the process PID — for example, messages from other processes, `Process.send_after/3` timers, or `:DOWN` messages from `Process.monitor`. Unlike `handle_call`/`handle_cast`, which are part of the GenServer protocol dispatch, `handle_info` is the catch-all for everything else. It returns the same shape as `handle_cast`: `{:noreply, new_state}`.

- id: elixir-otp-04
  answer: |
    The three main Supervisor restart strategies are:
    1. `:one_for_one` — if a child crashes, only that child is restarted.
    2. `:one_for_all` — if any child crashes, all children are terminated and then all are restarted.
    3. `:rest_for_one` — if a child crashes, that child and all children started *after* it are terminated and restarted.
    A child spec is a map (or tuple) that describes a child process to the Supervisor. It contains keys like `:id`, `:start` (a `{module, function, args}` tuple), `:restart` (`:permanent`, `:temporary`, or `:transient`), `:shutdown`, and `:type` (`:worker` or `:supervisor`). It tells the Supervisor how to start, restart, and stop the child.

- id: elixir-data-01
  answer: |
    `List.delete(list, 2)` returns a *new* list `[1, 3]`; the original `list` variable still points to `[1, 2, 3]`. Elixir data is immutable — no function ever modifies data in place. Rebinding a variable (e.g., `list = List.delete(list, 2)`) creates a new binding: the name `list` now refers to a different value. The original list `[1, 2, 3]` still exists in memory until garbage-collected; it is simply no longer referenced by `list`. Rebinding does not mutate anything — it changes what the variable name points to.

- id: elixir-data-02
  answer: |
    Maps are key-value stores with O(1) average lookup, supporting any key type. Use maps for structured data with named fields, especially when keys are known at runtime or there are many keys. Keyword lists are ordered lists of two-element `{key, value}` tuples. Use keyword lists for passing options to functions (e.g., `File.read(path, [:binary])`) and for small, ordered collections where duplicate keys are meaningful. Structs are maps with a fixed set of keys enforced at compile time, plus a `__struct__` key identifying the type. Use structs for domain entities where you want compile-time guarantees about which fields exist and want to catch typos early.

- id: elixir-data-03
  answer: |
    `%{map | key: val}` is the map update syntax — it requires that `key` already exists in the map. If the key does not exist, it raises a `KeyError`. `Map.put(map, key, val)` works whether or not the key already exists; it inserts a new key or updates an existing one. So the update syntax is a compile-time-checked assertion that the key is present, while `Map.put` is the general-purpose insert-or-replace operation.

- id: elixir-data-04
  answer: |
    `put_in`, `update_in`, and `get_in` provide concise, functional access to nested immutable data structures. `get_in` reads a value at a nested path. `put_in` sets a value at a nested path, reconstructing each intermediate map/struct along the way. `update_in` applies a function to the value at a nested path. They are better than manual nesting because they eliminate the boilerplate of manually rebuilding each level of the structure. Without them, you'd need to write nested `Map.put` calls or pattern-match each level, which is verbose, error-prone, and hard to read. These macros generate the correct structural reconstruction automatically.

- id: elixir-pipe-01
  answer: |
    The pipe operator `|>` takes the value on its left and inserts it as the *first* argument of the function call on its right. The one rule: the piped value always becomes the first argument of the next function. For example, `x |> f(a, b)` is equivalent to `f(x, a, b)`. This lets you write data transformations as a linear pipeline: `data |> step1() |> step2() |> step3()` instead of nested calls `step3(step2(step1(data)))`.

- id: elixir-pipe-02
  answer: |
    The `with` special form solves the problem of chaining multiple operations that each return `{:ok, value}` or `{:error, reason}` without deep nesting. Instead of nested `case` expressions, you write a sequence of `<-` bindings. Each `<-` clause pattern-matches on the result of the expression to its right. If the result is `{:ok, value}`, the value is bound and execution continues to the next clause. If the result is `{:error, reason}` (or anything that doesn't match the pattern), the `with` expression short-circuits and returns that error value immediately, skipping all remaining clauses.

- id: elixir-pipe-03
  answer: |
    In a `with` expression, if a `<-` clause fails to match (e.g., the expression returns `{:error, :notfound}` or `nil`), the `with` short-circuits and returns that failure value as the entire result of the `with` — no further clauses are evaluated. The `else` block is used to pattern-match on the failure value and transform it. For example, you can match different error reasons and convert them into a different error shape, or provide a default. Without an `else` block, the raw failure value is returned as-is.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the data flow is not linear — for example, when a step needs the piped value as a non-first argument combined with other values, or when there are conditional branches that don't fit a straight chain. For instance, `data |> transform(other_arg, :option)` is confusing because `data` is inserted as the first argument, not where you might expect. The fix is to break the pipeline into named intermediate variables for clarity, or to use `then/2` (Elixir 1.12+) which lets you place the piped value in any argument position: `data |> then(&transform(&1, other_arg, :option))`. Another case: when a step returns a value that needs to be used in multiple subsequent steps, a pipe forces you to either nest pipes or use intermediate bindings — named variables are clearer.

- id: elixir-error-01
  answer: |
    Use tagged tuples (`{:ok, value}` / `{:error, reason}`) for expected, recoverable errors — situations that are part of normal operation, like validation failures, missing records, or user input errors. The caller can pattern-match and handle them gracefully. Use `raise` for truly exceptional, unexpected conditions — bugs, unrecoverable states, contract violations — where continuing would produce incorrect results or corrupt state. Raising signals "this should never happen; something is fundamentally wrong." The convention is: if the caller can meaningfully recover, return a tuple; if not, raise.

- id: elixir-error-02
  answer: |
    The trailing `!` convention means the function raises an exception on error instead of returning a tagged tuple. `File.read(path)` returns `{:ok, content}` on success or `{:error, reason}` on failure. `File.read!(path)` returns `content` directly on success or raises a `File.Error` on failure. The `!` signals to the caller: "you will get the value directly, or the process will crash." Use the `!` version when an error is truly exceptional and you want the convenience of not pattern-matching; use the non-`!` version when you want to handle the error gracefully.

- id: elixir-error-03
  answer: |
    `try/rescue/after` is Elixir's exception-handling construct. The `try` block contains code that might raise. `rescue` catches exceptions (raised via `raise` or `throw`-style errors) and lets you handle them by pattern-matching on the exception type and reason. `rescue` is specifically for *exceptions* (things raised with `raise` or that raise implicitly, like `ArithmeticError`). `catch` is a separate mechanism for `throw` values and `exit` signals — `throw` is for non-local returns, `exit` is for process termination signals. The `after` block is guaranteed to run regardless of whether the `try` block succeeded, raised, or was caught — it is equivalent to `finally` in other languages, used for cleanup.

- id: elixir-error-04
  answer: |
    "Let it crash" is the philosophy that processes should not defensively rescue every possible error. Instead, when something goes wrong, the process crashes and a Supervisor restarts it to a known-good state. This is better than defensive rescuing because: (1) processes are isolated — a crash doesn't corrupt shared state or other processes; (2) rescuing can hide bugs and leave the system in an inconsistent state that's harder to debug; (3) the Supervisor provides a clean recovery mechanism with restart strategies; (4) code is simpler without defensive error handling everywhere. You write code for the happy path and let the infrastructure handle recovery.

- id: elixir-enum-01
  answer: |
    The core difference is *when* work happens. `Enum` is eager — it processes the entire collection immediately and returns a concrete result (a list, a sum, etc.). `Stream` is lazy — it returns a composable data structure (a `Stream` struct) that describes the computation but does not execute it until you enumerate it with an `Enum` function. This means `Stream` can represent infinite sequences and can chain multiple transformations without creating intermediate lists in memory.

- id: elixir-enum-02
  answer: |
    Two situations where `Stream` is clearly the right choice:
    1. Working with infinite or very large sequences where you only need a subset — e.g., `Stream.iterate(&(&1 + 1), 0) |> Enum.take(10)` generates only 10 elements from an infinite sequence. With `Enum` you'd need a finite list first.
    2. Chaining multiple transformations on large data without materializing intermediate lists — e.g., `large_list |> Stream.filter(...) |> Stream.map(...) |> Enum.to_list()` processes elements one at a time through the pipeline, using O(1) extra memory per step rather than creating a full intermediate list for each transformation.

- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` takes a collection, an initial accumulator, and a function. It iterates over the collection, calling the function with each element and the current accumulator, and the return value becomes the new accumulator for the next iteration. The final accumulator is the result. It is the fundamental building block because `map`, `filter`, `sum`, `count`, `flat_map`, and many others can all be implemented in terms of `reduce`: `map` accumulates transformed elements into a list, `filter` accumulates only elements matching a predicate, `sum` accumulates a running total. `reduce` captures the essence of folding a collection into a single result.

- id: elixir-enum-04
  answer: |
    In `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`:
    - `x <- list` is the *generator* — it binds `x` to each element of `list` in turn.
    - `rem(x, 2) == 0` is a *filter* — it keeps only elements where the expression is truthy (even numbers).
    - `into: %{}` specifies the *collectible* — the output is collected into a map (here, an empty map that accumulates results).
    - `do: {x, x * x}` is the *output expression* — for each surviving element, it produces a `{key, value}` tuple that is inserted into the map. The result is a map of even numbers to their squares.

- id: elixir-proto-01
  answer: |
    A protocol in Elixir defines a set of functions (an interface) that can be implemented for different data types. `defprotocol` declares the protocol and its function signatures. `defimpl` provides a concrete implementation of those functions for a specific type. This gives polymorphism: you call the same function name (e.g., `String.Chars.to_string(x)`) and the implementation is selected based on the type of the first argument. Protocols dispatch on data type — the same function call behaves differently depending on what type of data it receives.

- id: elixir-proto-02
  answer: |
    A behaviour is a contract that specifies a set of functions (callbacks) a module must implement. `@callback` defines a function signature — its name, arguments, and expected return type — that implementors must follow. `@behaviour` is placed in a module to declare that it defines a behaviour (i.e., it specifies callbacks that other modules will implement). `@impl` is placed before a function definition to mark it as implementing a callback from a behaviour — it can be `@impl true` (referencing the most recent `@behaviour`) or `@impl MyBehaviour` (explicit). `@impl` is optional but recommended: it enables compile-time checks that the function matches the callback signature.

- id: elixir-proto-03
  answer: |
    The fundamental difference is dispatch mechanism. Protocols dispatch on the *type of the first argument* — they are data-oriented polymorphism. When you call a protocol function, Elixir looks up the implementation for the type of the data you passed. Behaviours dispatch on the *module that calls them* — they are module-oriented polymorphism. A behaviour defines callbacks that a module implements, and the calling code explicitly references the module. Protocols answer "what type is this data and how should it behave?"; behaviours answers "what module is doing this work and what contract does it follow?"

- id: elixir-proto-04
  answer: |
    Annotating a callback implementation with `@impl true` (or `@impl MyBehaviour`) tells the compiler that this function is intended to implement a callback from the specified behaviour. This buys you compile-time verification: the compiler checks that the function name and arity match a callback defined in the behaviour, and that the types align. Without `@impl`, the compiler cannot distinguish a callback implementation from a regular function, so a typo in the function name or a missing callback would go unnoticed until runtime. `@impl` catches these errors at compile time.

- id: elixir-conc-01
  answer: |
    The BEAM (Erlang VM) concurrency model is based on lightweight processes that are completely isolated from each other. Each process has its own heap, stack, and mailbox. There is no shared mutable memory between processes — data is copied when sent as a message. This matters because it eliminates entire classes of concurrency bugs: no data races, no deadlocks from lock contention, no need for mutexes or semaphores. Processes communicate only via asynchronous message passing. If one process crashes, it cannot corrupt the memory of any other process. This isolation is the foundation of Erlang/Elixir's fault tolerance.

- id: elixir-conc-02
  answer: |
    `Task.async` spawns a new process to run a function concurrently and immediately returns a `%Task{}` struct (a reference to the running task). The caller can continue doing other work. `Task.await` blocks the calling process until the task completes, then returns `{:ok, result}` on success. If the task raises an exception, `Task.await` causes the calling process to exit (the exception propagates). If the task does not complete within the timeout (default 5000 ms), `Task.await` returns `{:exit, :timeout}` and the task process is killed. `Task.await` links to the task process, so a crash in the task also crashes the caller unless the caller is trapping exits.

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple abstraction around a GenServer that holds a single piece of state. It provides `Agent.get/2`, `Agent.update/2`, and `Agent.get_and_update/2` for reading and modifying that state. It differs from a full GenServer in that it has no `handle_call`/`handle_cast`/`handle_info` callbacks, no custom message handling, and no support for timeouts or hibernation. Use an Agent when you just need shared mutable state accessible by multiple processes. Use a GenServer when you need custom message handling, complex state transitions, or the full request-response pattern.

- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is an in-memory key-value store built into the BEAM that is accessible by any process without going through a single process's mailbox. Use ETS instead of a process (GenServer/Agent) when you need high-performance concurrent read and write access to shared data. A GenServer serializes all access through its mailbox — every request is handled one at a time, creating a bottleneck. ETS tables support concurrent reads and writes with configurable concurrency, making them suitable for caching, lookup tables, and shared state that needs low-latency access from many processes. The tradeoff is that ETS data is not garbage-collected until the table is deleted, and it lacks the fault-tolerance supervision that processes provide.
