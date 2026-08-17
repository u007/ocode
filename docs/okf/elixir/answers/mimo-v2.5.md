```yaml
- id: elixir-pm-01
  answer: |
    `{:ok, value} = fetch()` pattern-matches the return value of `fetch()` against the
    tuple `{:ok, value}`. If the right side is `{:ok, some_value}`, the match succeeds
    and the variable `value` is bound to `some_value`. If the right side is
    `{:error, :notfound}`, the match fails because the tuple shape does not match (the
    atom in the first position is `:error` instead of `:ok`), and a `MatchError` is
    raised at runtime.

- id: elixir-pm-02
  answer: |
    You define a function with multiple clauses separated by semicolons, each optionally
    with a `when` guard. Elixir tries the clauses top to bottom; the first clause whose
    head pattern matches AND whose guard evaluates to `true` wins. For example:

        def status(%{active: true}) when count > 0, do: :running
        def status(%{active: true}), do: :idle
        def status(_), do: :unknown

    The guards act as additional boolean filters, so you avoid nested `if/case` blocks.
    If no clause matches, a `FunctionClauseError` is raised.

- id: elixir-pm-03
  answer: |
    The pin operator `^` pins a variable to its current value instead of rebinding it.
    Inside a pattern on the left of `=`, a bare variable `x` is a new binding — it will
    match anything and rebind `x`. Writing `^x` means "match against the value `x` already
    holds." For example, `^x = 42` succeeds only if `x` is already `42`; a bare `x = 42`
    always succeeds and sets `x` to `42`.

- id: elixir-pm-04
  answer: |
    Map pattern matching is partial — the pattern only requires that the listed keys
    exist in the map. Additional keys are ignored. So `%{name: n} = user` matches any
    map that contains a `:name` key (or a `name` key, depending on syntax), regardless of
    what other keys the map has. This is different from tuples, which require an exact
    shape and arity, and from lists, which match prefix/suffix with the `|` tail syntax.

- id: elixir-otp-01
  answer: |
    `spawn/1` takes a zero-arity function and creates a new, independent process that
    runs it; it returns the new process's PID. `send/2` (also written `pid ! message`)
    sends a message to a process by PID. `receive/0` (or `receive/1` with a timeout) sits
    in the current process's mailbox and blocks until a message matching one of its
    pattern-matched clauses arrives. If no message in the mailbox matches and no timeout
    is specified, `receive` blocks indefinitely waiting for a future message. If a
    `timeout:` is given, it returns `:timeout` when the deadline expires without a match.

- id: elixir-otp-02
  answer: |
    `handle_call/3` handles synchronous requests — the caller blocks until a reply is
    returned. Its signature is `handle_call(request, from, state)` and it must return
    `{:reply, reply, new_state}` (or variants like `{:stop, reason, state}`). `handle_cast/2`
    handles asynchronous requests — the caller does not wait. Its signature is
    `handle_cast(request, state)` and it must return `{:noreply, new_state}` (or
    `{:stop, reason, new_state}`). The distinction is whether the caller expects and waits
    for a reply.

- id: elixir-otp-03
  answer: |
    `handle_info/2` handles messages that arrive in the process's mailbox but were NOT
    sent through the GenServer's `call` or `cast` API — i.e., raw `send` messages,
    messages from monitors or links, system messages, or messages from other processes
    using the raw PID. `handle_call` and `handle_cast` specifically handle the structured
    `{:call, ...}` and `{:cast, ...}` messages that go through `GenServer.call/2` and
    `GenServer.cast/2`. `handle_info` is the catch-all for everything else.

- id: elixir-otp-04
  answer: |
    The three main strategies are:
    1. `:one_for_one` — only the crashed child is restarted.
    2. `:one_for_all` — if any child crashes, all children are terminated and restarted.
    3. `:rest_for_one` — the crashed child and all children started after it are restarted.

    A child spec is a map (or a tuple returned by a `child_spec/1` function) that
    describes a child: its module, arguments, restart strategy (`:permanent`, `:transient`,
    `:temporary`), shutdown behaviour, and type. It tells the supervisor how to start,
    stop, and restart each child process.

- id: elixir-data-01
  answer: |
    The original `list` variable is unchanged — it still points to `[1, 2, 3]`.
    `List.delete(list, 2)` returns a new list `[1, 3]` without modifying the original.
    Rebinding (`list = List.delete(list, 2)`) just makes the variable name point to the
    new list; the old list still exists in memory (and may be garbage-collected once no
    references remain). This is how all Elixir data is immutable — operations return new
    values rather than mutating in place.

- id: elixir-data-02
  answer: |
    - **Keyword lists**: Ordered list of `{atom, value}` tuples. Used for optional
      configuration and options (e.g., `Application.get_env`), where the caller can
      supply only the keys they care about. Support only atom keys.
    - **Maps**: Unordered key-value collections (`%{key => value}`). Use when you need
      fast, polymorphic lookups, when keys are not necessarily atoms, or as the general
     -purpose associative data structure.
    - **Structs**: Named, compile-time-enforced maps with a `__struct__` key. Use when
      you want a data type with known fields, compile-time guarantees, and protocol
      dispatch. They are built on top of maps but provide a fixed schema.
```

- id: elixir-data-03
  answer: |
    There is no difference. Both `%{map | key: val}` and `Map.put(map, key, val)` produce the identical result whether or not the key already exists. The update syntax `%{map | ...}` is syntactic sugar that desugars to `Map.put` (or more precisely, it calls the `Map` protocol's `update` or directly uses Erlang's map update). Neither form mutates in place; both return a brand-new map with the key set to the given value. The only practical difference is readability: the update syntax is more concise for simple merges, while `Map.put` works as a function and can be used in pipelines.

- id: elixir-data-04
  answer: |
    `get_in`, `put_in`, and `update_in` operate on nested data structures by accepting a list of keys (or a dynamic path) to drill into arbitrarily deep maps (or structs). For example, `get_in(map, [:a, :b, :c])` is equivalent to `map.a.b.c` but works with computed key lists. `put_in(map, [:a, :b], value)` sets a deeply nested value without manually reconstructing each intermediate map. `update_in(map, [:a, :b], fun)` applies a function to a deeply nested value.

    They are better than manual nesting because with immutable data you must rebuild every intermediate map along the path. Manually you'd write something like `%{map | a: %{map.a | b: %{map.a.b | c: new_val}}}` which is verbose, error-prone, and hard to read. The kernel functions handle all the intermediate reconstruction automatically, keeping code clean and correct even when the nesting depth is dynamic or deep.

- id: elixir-pipe-01
  answer: |
    The pipe operator `|>` takes the value on its left side and passes it as the **first argument** to the function call on its right side. That is the one rule: the piped value always becomes the first argument. So `x |> f(y)` is equivalent to `f(x, y)`. This enables chaining functions in a readable top-to-bottom or left-to-right flow, where each step's output feeds into the next function's first parameter.

- id: elixir-pipe-02
  answer: |
    The `with` special form solves the problem of chaining operations that can each fail (typically returning `{:ok, value}` or `{:error, reason}` tuples) without deeply nesting `case` statements or `if` chains. Each `<-` clause pattern-matches the result of the expression on the right. If the match succeeds, the matched value is bound and execution continues to the next clause. If any clause fails to match, `with` short-circuits and falls through to the `else` block (if present) or returns the non-matching value directly. This keeps error-handling linear and flat rather than pyramid-shaped.

- id: elixir-pipe-03
  answer: |
    In a `with` expression, when a value does not match the pattern on the left side of a `<-` clause, `with` immediately stops evaluating subsequent clauses and returns that unmatched value. If an `else` block is present, the unmatched value is passed to it, and the `else` block can transform it (for example, wrapping it in an `{:error, reason}` tuple). If there is no `else` block, the raw unmatched value is simply returned from the `with` expression. The `else` block is therefore the place to handle or convert unexpected/non-matching results into a consistent error format.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the piped value is not the first argument, or when the function takes multiple arguments and the piped value isn't the primary subject. For example:

    `Enum.map(list, &(&1 + 1)) |> Enum.filter(&(&1 > 5))` — here the first pipe is fine, but consider:

    `value |> IO.inspect(label: "debug") |> process()` — the label keyword is fine, but if you needed to pass `value` as a non-first argument, piping becomes awkward.

    A concrete bad case: `result |> SomeModule.do_thing(arg1, arg2)` where `result` is actually the third argument. This misleads readers because the pipe convention is "first argument." You would fix it by either writing a helper that rearranges arguments, using an anonymous function `&SomeModule.do_thing(&3, &1, &2)` (ugly), or simply calling the function directly without the pipe: `SomeModule.do_thing(arg1, arg2, result)`. The fix is to reserve piping for cases where the piped value truly is the first argument.

- id: elixir-error-01
  answer: |
    Use tagged tuples (`{:ok, value}` / `{:error, reason}`) when the failure is an expected part of the domain logic — things like "file not found," "validation failed," or "user not authenticated." These are ordinary outcomes, not bugs. Tagged tuples make the caller handle the error explicitly, are composable, work well with `with` and pattern matching, and carry no performance overhead.

    Use raising (exceptions) when the error represents a genuine bug or an unrecoverable situation — for example, a programming mistake like calling a function with the wrong type, or a system-level failure like running out of memory. Exceptions are for exceptional circumstances. Raising forces the caller to deal with the issue (or lets the supervisor crash and restart), which is appropriate when the program cannot meaningfully continue.

    The convention in Elixir libraries is to provide both: a non-raising version returning tagged tuples (e.g., `File.read/1`) and a raising `!` version (e.g., `File.read!/1`) for convenience when you are confident the call will succeed or want to crash on failure.

- id: elixir-error-02
  answer: |
    The trailing `!` convention distinguishes the raising version of a function from the non-raising version. `File.read(path)` returns `{:ok, contents}` or `{:error, reason}`, letting the caller handle the error. `File.read!(path)` returns the value directly on success or raises a `File.Error` exception on failure. The `!` signals "this will raise on error" so the caller knows to either wrap it in `try/rescue`, use it in a context where the supervisor handles crashes, or only call it when failure is not expected. It is a naming convention, not a language-level distinction — any function can have a `!` variant.

- id: elixir-error-03
  answer: |
    `try/rescue/after` is Elixir's mechanism for handling exceptions (raised errors, not tagged tuples). The `try` block contains code that might raise. The `rescue` clause catches exceptions by type: `rescue e in RuntimeError -> ...` binds the exception to `e` and lets you handle it. The `after` block runs no matter what — whether `try` succeeded, raised and was rescued, or raised and was not rescued — making it useful for cleanup (similar to `finally` in other languages). `after` is guaranteed to execute after the `try`/`rescue` blocks complete.

    `rescue` differs from `catch` in what it handles. `rescue` catches **exceptions** (values that are structs implementing the `Exception` behaviour, raised via `raise`). `catch` catches **non-exception throws** — values passed to `throw/1` or exits via `exit/1`. In practice, `rescue` is for errors and `catch` is for flow control with `throw` or `exit`.

- id: elixir-error-04
  answer: |
    "Let it crash" is the philosophy that instead of defensively rescuing every possible error and trying to recover, you let processes fail when they encounter unexpected states and rely on supervisors to restart them. This works because Elixir/Erlang runs on the BEAM, where processes are lightweight, isolated (no shared memory), and cheap to create. A supervisor tree monitors processes and restarts them according to a strategy (one-for-one, rest-for-one, one-for-all) when they crash.

    This is often better than defensive error handling because: (1) it avoids masking bugs — an exception propagates the problem rather than hiding it behind recovery code that may itself be buggy; (2) it keeps code simpler — you don't need to handle every edge case if the process can just restart in a known-good initial state; (3) it is more resilient — a supervisor restarts only the failed process, not the whole application, so other parts continue unaffected. Defensive rescuing can lead to corrupted state that silently propagates, whereas crashing and restarting from a clean slate is often the safest recovery.

```yaml
- id: elixir-enum-01
  answer: |
    `Enum` is eager — it evaluates the entire collection immediately when the function is called, consuming memory proportional to the data. `Stream` is lazy — it builds a pipeline of operations that produce values one at a time on demand. Work in `Stream` only happens when the stream is enumerated (via `Enum.to_list`, `Stream.run`, or similar). This means `Stream` can represent infinite sequences and avoids intermediate allocations.

- id: elixir-enum-02
  answer: |
    1. When working with very large or infinite collections — a `Stream` avoids loading the entire data set into memory at once, producing elements one at a time. For example, reading a multi-gigabyte file line by line with `File.stream!`.
    2. When composing a long chain of transformations — `Stream` defers all intermediate steps until final enumeration, avoiding the creation of multiple intermediate lists that `Enum` would allocate at each step.

- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` iterates through an enumerable, carrying an accumulator, and applies a function `fun.(element, acc)` at each step, returning the final accumulator value. It is the fundamental building block because most other `Enum` functions can be implemented in terms of it: `map` passes transformed elements into a new list via the accumulator, `filter` conditionally appends to the accumulator, `sum` adds each element to a running total, and so on. Every enumerable-consuming operation ultimately reduces to threading state through each element.

- id: elixir-enum-04
  answer: |
    In `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`:
    - `x <- list` is a generator: it binds `x` to each element of `list` in turn.
    - `rem(x, 2) == 0` is a filter (guard): only elements satisfying this condition proceed; odd values are skipped.
    - `into: %{}` tells the comprehension to collect results into a map instead of a list.
    - `do: {x, x * x}` is the expression evaluated for each passing element, producing a `{key, value}` tuple that gets inserted into the map.
    The result is a map of even numbers to their squares.

- id: elixir-proto-01
  answer: |
    A protocol in Elixir defines a set of functions that can be implemented for different data types. `defprotocol` declares the protocol with its function signatures. `defimpl` provides concrete implementations for specific types (e.g., `defimpl MyProto, for: String` or `for: Any`). Polymorphism is achieved at runtime by dispatching on the type of the argument passed to a protocol function — Elixir uses the first argument's `@derive` or struct module to look up the correct implementation. This lets you write generic code that works across types without coupling to any specific one.

- id: elixir-proto-02
  answer: |
    A behaviour in Elixir is a module-level contract that specifies which callback functions a module must implement, plus optional macros.
    - `@callback` (used inside a `defmodule ... @behaviour` or in a behaviour definition module) declares a required callback function with its expected arity and typespec.
    - `@behaviour MyBehaviour` (placed in the implementing module) declares that the module intends to implement the callbacks defined in `MyBehaviour`; the compiler checks for missing callbacks.
    - `@impl true` (or `@impl MyBehaviour`) annotates a function definition to indicate it is a callback implementation; it enables compile-time warnings if the function signature doesn't match the declared `@callback`.

- id: elixir-proto-03
  answer: |
    The fundamental difference is that protocols dispatch based on the *type* of the data at runtime (struct name, built-in type), while behaviours define a *contract* that a module opts into and is checked at compile time. A protocol lets you add new implementations externally without modifying the protocol itself (open for extension), whereas a behaviour requires the implementing module to explicitly declare `@behaviour` and is about module-level interface conformance. Protocols work on data values; behaviours work on modules.

- id: elixir-proto-04
  answer: |
    `@impl true` (or `@impl MyBehaviour`) tells the compiler "this function is an implementation of a callback." This buys you:
    1. Compile-time verification that the function's arity and typespec match the declared `@callback` in the behaviour.
    2. A warning if you write a function named after a callback but with a wrong signature, catching typos or drift early.
    3. Documentation clarity — readers know the function exists to satisfy the behaviour contract, not just as a regular module function.

- id: elixir-conc-01
  answer: |
    The BEAM runs each Elixir process as a lightweight, isolated unit of execution with its own heap and stack, scheduled cooperatively across OS threads by the VM. There is no shared mutable memory between processes — the only way to share data is by sending immutable messages via `send/2`. This matters because it eliminates data races, eliminates the need for locks/mutexes, and makes process failures isolated (a crash in one process doesn't corrupt another's state). It also enables massive concurrency — millions of processes can coexist because each is tiny and copying messages between them is cheap.

- id: elixir-conc-02
  answer: |
    `Task.async/1` spawns a new process that runs the given function concurrently and returns a `%Task{}` struct holding the PID and ref. `Task.await/1` (or `Task.await/2` with a timeout) blocks the calling process until the spawned task completes, then returns its result. If the task process raises an exception, `Task.await` re-raises that exception in the calling process. If the task times out (exceeds the `Task.await` timeout, default 5000ms), `Task.await` raises an `exit` with `{:timeout, ...}`. The dead task process is cleaned up; if it was started with `Task.async`, the caller is also linked to it, so an uncaught crash in the task can propagate.
```

- id: elixir-conc-03
  answer: |
    An Agent is a simpler abstraction built on top of GenServer that wraps common state-management patterns. Instead of implementing GenServer callbacks (init, handle_call, handle_cast, handle_info), you use the Agent API directly: `Agent.start_link/1`, `Agent.get/2`, `Agent.put/3`, `Agent.update/3`. It stores state internally and you interact with it through function calls that return the value or the result of a transformation function.

    Key differences from GenServer:
    - Agents hide the GenServer boilerplate; no callbacks to implement.
    - Agents expose a small API focused on state get/set/update.
    - GenServers let you handle arbitrary messages, define custom reply logic, use state machines, manage timeouts, and pattern-match on different message types.
    - Agents are best when you just need a process holding mutable state and don't need fine-grained control over message handling or custom dispatch.

- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is a built-in, in-memory key-value store that lives in the Erlang VM. Tables can be set to store any Erlang term as the value (no serialization needed for reads), and they support multiple table types (`:set`, `:ordered_set`, `:bag`, `:duplicate_bag`) and ownership models (`:public`, `:protected`, `:private`).

    Use ETS instead of GenServer/Agent when:
    - You have read-heavy workloads and multiple processes need fast concurrent reads without the overhead of message passing.
    - The data is relatively static or changes infrequently (reference tables, caches, configuration lookups).
    - You want lock-free, low-latency reads—ETS reads don't go through the GenServer mailbox.
    - You need very high throughput for lookups and can tolerate eventual consistency or handle writes carefully.
    - The data structure is best modeled as a key-value store rather than a complex state machine.

    Caveats: ETS tables are owned by the creating process and are destroyed if that process crashes unless you trap exits. Writes are not atomic across tables and don't provide the serialized consistency guarantees a GenServer offers. For data that changes frequently with complex invariants, a GenServer is often safer and easier to reason about.

