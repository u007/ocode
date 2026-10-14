# cat-jam-free — WITH-SKILL closed-book answers for elixir (opencode-go, 2026-10-14)
# Delivery: derived/elixir.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/elixir.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: elixir-pm-01
  answer: |
    `{:ok, value} = fetch()` is a pattern match: it asserts that `fetch()` returns a two-tuple whose first element is `:ok`, binding `value` to the second element on success. If the right side is `{:error, :notfound}`, the patterns do not match, so Elixir raises `MatchError` and binds nothing.
- id: elixir-pm-02
  answer: |
    Multiple function-head clauses are tried top to bottom, in source order. The first clause whose argument pattern matches **and** whose `when` guard is true runs; the guard is limited to a fixed whitelist of guard-safe expressions. If no clause matches, the call raises `FunctionClauseError`.
- id: elixir-pm-03
  answer: |
    `^x` uses the current binding of `x` as a literal value to match against, so `^x = y` succeeds only when `y == x` and raises `MatchError` otherwise. A bare `x` on the left is a variable pattern: it matches any value and binds or rebinds `x`.
- id: elixir-pm-04
  answer: |
    No. Map patterns match a subset of keys and ignore extras, so `%{name: n} = user` succeeds if `user` contains at least `:name`, binding `n` while leaving the rest untouched. It does not require exactly those keys, unlike tuples or lists, whose shapes and lengths must match.
- id: elixir-otp-01
  answer: |
    `spawn/1` creates a new lightweight BEAM process and returns its pid. `send/2` delivers a message to that process's mailbox asynchronously. `receive` scans the mailbox for a message matching one of its clauses; if none matches, the process waits, re-scanning as new messages arrive, unless an `after` timeout fires.
- id: elixir-otp-02
  answer: |
    `handle_call` is synchronous request/reply: the caller waits for a reply, and the callback typically returns `{:reply, reply, new_state}`. `handle_cast` is fire-and-forget and returns `{:noreply, new_state}`. Use `call` when a result is needed, `cast` when it is not.
- id: elixir-otp-03
  answer: |
    `handle_info` handles messages that are not GenServer `call`/`cast` protocol messages, such as raw `send` messages, monitor `:DOWN` messages, and timer messages. Like `handle_cast`, it is asynchronous and returns `{:noreply, state}` or `{:stop, ...}`; unlike call/cast, it is not initiated by the GenServer client API.
- id: elixir-otp-04
  answer: |
    The three main strategies are `:one_for_one` (restart only the crashed child), `:one_for_all` (stop and restart all children when one crashes), and `:rest_for_one` (restart the crashed child and any children started after it). A child spec describes how to start and supervise a child, including id, start MFA, restart policy, shutdown timeout, type, and modules.
- id: elixir-data-01
  answer: |
    `list` remains `[1, 2, 3]`; `List.delete/2` returns a new list without mutating the original. Rebinding a variable, such as `list = List.delete(list, 2)`, only makes the name point to a new immutable value; it does not alter old data, and unreachable old values can be garbage-collected.
- id: elixir-data-02
  answer: |
    Maps are the general key-value store with fast lookup and unique keys. Keyword lists are ordered lists of `{atom, value}` pairs and allow duplicate keys, commonly used for options. Structs are compile-time maps with a fixed set of atom keys and defaults, used for domain records. Use maps for generic data, keyword lists for small ordered options, and structs for named entities.
- id: elixir-data-03
  answer: |
    `%{map | key: val}` requires `key` to already exist and raises `KeyError` if it does not. `Map.put(map, key, val)` inserts the key if absent or updates it if present, so it works in both cases.
- id: elixir-data-04
  answer: |
    `get_in`, `update_in`, and `put_in` traverse a nested path of keys or Access functions in one expression: read, transform, or replace a nested value. They are better than manual nesting because they keep updates as a single immutable expression, reduce boilerplate, and compose with keys, indices, and dynamic path elements.
- id: elixir-pipe-01
  answer: |
    `|>` passes the value on the left as the first argument of the call on the right, so `x |> f(a)` is `f(x, a)`. Its purpose is to turn nested expressions into a top-to-bottom data pipeline, which is why Elixir APIs usually take data as the first argument. A single call like `x |> f()` adds nothing over `f(x)`.
- id: elixir-pipe-02
  answer: |
    `with` chains fallible steps, commonly returning `{:ok, _}` tuples, so the happy path can be written linearly. Each `<-` clause must match; if all match, the `do` block runs and its value is returned. If a step fails to match, the chain short-circuits and that non-matching value becomes the result of the `with` unless `else` transforms it.
- id: elixir-pipe-03
  answer: |
    When a value fails to match a `<-` clause, the `with` stops immediately and returns that value unchanged. The optional `else` block pattern-matches those failure values to transform them; if `else` is present and none of its clauses match, `WithClauseError` is raised.
- id: elixir-pipe-04
  answer: |
    Piping reads badly when the piped value must go somewhere other than the first argument, or when the same value is needed more than once. For example, `g(a, x)` cannot be expressed directly as `x |> g(a)`. Use `then(x, fn v -> g(a, v) end)` or a named intermediate variable, and avoid piping a single call.
- id: elixir-error-01
  answer: |
    Use `{:ok, value}` / `{:error, reason}` for expected, recoverable cases that are part of normal control flow, such as a missing file or validation failure. Raise exceptions for unexpected failures, invariant violations, and programmer errors. Routine branching should pattern-match errors rather than rescue them.
- id: elixir-error-02
  answer: |
    A trailing `!` means the function raises on failure instead of returning an error tuple: `File.read/1` returns `{:ok, binary}` or `{:error, reason}`, while `File.read!/1` returns the binary or raises. The plain version is for conditional handling; the bang version asserts success.
- id: elixir-error-03
  answer: |
    `try` runs guarded code; `rescue` catches exceptions by pattern or type, while `catch` also catches thrown values and exits. `after` runs cleanup whether the body succeeds or raises, making it the right place for resource cleanup. It is guaranteed on normal and exceptional propagation paths within the process, but not if the process is killed abruptly.
- id: elixir-error-04
  answer: |
    "Let it crash" means isolate work in processes and let supervisors restart them into a clean state, rather than defending every call with rescues that may hide bugs or leave corrupted partial state. Crashes are cheap and contained, and recovery restores invariants. Expected domain failures should still be represented as tagged tuples, not crashes.
- id: elixir-enum-01
  answer: |
    `Enum` is eager: each operation traverses its input and produces a result immediately, often allocating intermediate collections. `Stream` is lazy: transformations compose into a recipe and values are computed only when a terminal consumer pulls them, using roughly constant extra memory.
- id: elixir-enum-02
  answer: |
    Use `Stream` for very large or infinite collections when only part of the result is needed, such as taking the first N matches, and for pipelines where eager evaluation would create many intermediate lists. It is also appropriate for ongoing data sources such as file lines or events that should be processed on demand.
- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` traverses an enumerable while folding each element into an accumulator, returning the final accumulator. Map, filter, sum, and many other operations can be expressed as reductions, so it is the fundamental loop building block and can collapse multiple traversals into one pass.
- id: elixir-enum-04
  answer: |
    In `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`: `x <- list` is a generator over the enumerable; `rem(x, 2) == 0` is a filter; `do: {x, x * x}` builds each output term; and `into: %{}` collects those terms into a map instead of the default list. Comprehensions can also use multiple generators and pattern matching in generators.
- id: elixir-proto-01
  answer: |
    A protocol is a set of functions that can be implemented separately for each data type, providing runtime polymorphism. `defprotocol` declares the contract; `defimpl Protocol, for: Type` provides an implementation for a specific type, including structs and built-in types. Dispatch selects the implementation based on the value's runtime type.
- id: elixir-proto-02
  answer: |
    A behaviour is a contract of callbacks that a module should implement. `@callback` declares a required function signature; a module declares `@behaviour MyBehaviour` to opt in; and `@impl` marks which functions are intended to implement those callbacks. The compiler then checks arity and signature.
- id: elixir-proto-03
  answer: |
    Protocols dispatch polymorphically on the runtime type of a value, so implementations can be added for any type. Behaviours are module-level contracts: they state that a module must implement certain callbacks, and callers use that module directly rather than dispatching on data. In short, protocols answer "what type is this value?", while behaviours answer "does this module implement this API?"
- id: elixir-proto-04
  answer: |
    `@impl true` or `@impl Behaviour` tells the compiler that the following function is supposed to implement a behaviour callback. It then verifies that the function name and arity actually match a declared `@callback`, so typos, wrong arities, or stale implementations fail at compile time instead of becoming ordinary functions.
- id: elixir-conc-01
  answer: |
    BEAM processes are lightweight, preemptively scheduled, and share no mutable memory. They communicate by exchanging immutable messages. This avoids shared-state races, gives each process its own garbage collector and failure boundary, and makes fault isolation and scaling natural.
- id: elixir-conc-02
  answer: |
    `Task.async(fun)` spawns a process to run `fun` and returns a `%Task{}` reference. `Task.await(task, timeout)` waits for the result, re-raising if the task raised or exited and raising a timeout error if it is not finished in time. If it times out, the task may keep running unless you explicitly shut it down.
- id: elixir-conc-03
  answer: |
    An `Agent` is a simple process holding state, accessed through functions such as `get` and `update`, built on top of GenServer. It reduces boilerplate for straightforward shared state. A full GenServer is preferable for custom messages, timeouts, initialization, cleanup, or complex state transitions.
- id: elixir-conc-04
  answer: |
    ETS is an in-memory table shared by processes in the BEAM VM, with fast concurrent access depending on table type and access rules. Use it for hot shared state or caches where a GenServer mailbox would become a bottleneck; use a process when you need validation, invariants, or message-driven behavior.
