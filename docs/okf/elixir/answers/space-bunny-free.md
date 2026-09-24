- id: elixir-pm-01
  answer: |
    `fetch()` is evaluated first, and its result is pattern-matched against `{:ok, value}`. If the result is `{:ok, something}`, `value` is bound to `something`. If it is `{:error, :notfound}`, the match fails and raises a `MatchError` in the current process; it does not return the error tuple automatically.

- id: elixir-pm-02
  answer: |
    Define several clauses with the same function name and arity, optionally guarded. Elixir tries the clauses in their source order and runs the first clause whose arguments match and whose guards evaluate to true. Guards are restricted expressions such as comparisons, boolean logic, and pattern checks. If no clause matches, the call raises `FunctionClauseError`.

- id: elixir-pm-03
  answer: |
    `^x` in a pattern refers to the value already bound to `x` and requires the matched value to equal it. A bare `x` on the left of a match is treated as a pattern variable: it binds or rebinds `x` to whatever value is matched, rather than comparing against its previous value. Use pinning when the old value matters.

- id: elixir-pm-04
  answer: |
    Map patterns match required keys and bind values for keys listed in the pattern. The map may contain additional keys; `%{name: n} = user` requires `user` to have a `:name` key and binds `n` to its value, but it does not require the map to contain exactly `:name`. A missing required key causes the match to fail.

- id: elixir-otp-01
  answer: |
    `spawn(fun)` starts a new, unlinked process that evaluates `fun` and returns the new process identifier. `send(pid, message)` places a message in that process's mailbox. `receive` searches the mailbox for the first message matching one of its patterns, removes it, and evaluates the corresponding body. If no message matches, it blocks until one arrives unless an `after` timeout is supplied; with a timeout, it returns `:timeout`, leaving unmatched messages in the mailbox.

- id: elixir-otp-02
  answer: |
    `handle_call/3` handles a synchronous `GenServer.call/3`; the caller waits for a reply. A common return is `{:reply, reply, new_state}`; it can also return `{:noreply, new_state}` or a `{:stop, ...}` tuple. `handle_cast/2` handles an asynchronous `GenServer.cast/2`, which returns control to the sender without waiting for a reply. It returns `{:noreply, new_state}` or `{:stop, reason, new_state}`.

- id: elixir-otp-03
  answer: |
    `handle_info/2` handles messages that were not handled as calls or casts, including ordinary sends from other processes, monitor `DOWN` notifications, and other informational messages. It returns `{:noreply, state}`, optionally with a timeout, or a stop tuple. It does not provide the synchronous request-and-reply behavior of `handle_call`, nor the explicit asynchronous cast protocol.

- id: elixir-otp-04
  answer: |
    `:one_for_one` restarts only the child that crashed. `:one_for_all` restarts every child when one crashes. `:rest_for_one` restarts the crashed child and all children started after it, leaving earlier children running. A child spec is a map describing a supervised child, including its unique `:id`, how to `:start` it, its `:restart` policy, shutdown behavior, and type. Restart policies include `:permanent` (always restart), `:transient` (restart on abnormal exit), and `:temporary` (never restart).

- id: elixir-data-01
  answer: |
    `List.delete(list, 2)` returns a new list, `[1, 3]`; the original `list` remains `[1, 2, 3]`. Rebinding a variable associates that variable name with a new value; it does not mutate the old value or any structure that contains it. Existing immutable data remains unchanged unless a new version is explicitly returned and used.

- id: elixir-data-02
  answer: |
    Use maps for general keyed data, with arbitrary keys, updates, and pattern matching. Use keyword lists for ordered, small collections of atom-keyed options, especially option lists accepted by functions; they are lists of two-element tuples and may contain duplicate keys. Use structs for data with a predefined set of fields and a defining module, giving it a known shape, dot access, and compile-time structure.

- id: elixir-data-03
  answer: |
    `%{map | key: val}` is the map update syntax and requires `key` to already exist; it raises `KeyError` if it does not. `Map.put(map, key, val)` returns a map with the key set to `val`, adding the key when it is absent and replacing its value when it is present.

- id: elixir-data-04
  answer: |
    `get_in(data, path)` retrieves a value from nested data, while `put_in(data, path, value)` returns updated nested data with a value inserted or replaced. `update_in(data, path, fun)` retrieves a value, applies a function to it, and writes the returned value back. They return new values rather than mutating the original, and often make nested map, struct, and access-based list operations clearer than manually constructing and matching each intermediate nested value.

- id: elixir-pipe-01
  answer: |
    `|>` pipes the value on its left into the first argument of the function on its right. For example, `value |> do_something(argument)` is equivalent to `do_something(value, argument)`. The single rule is that the piped value is always passed as the first argument.

- id: elixir-pipe-02
  answer: |
    `with` makes a sequence of dependent operations readable as a successful path. Each `<-` expression's result is matched, and successful matches can bind values used by later expressions, often such as `{:ok, user} <- fetch_user()`. If a match succeeds, evaluation continues; if it fails, the expression exits early. This avoids deeply nested `case` expressions and mixes success and failure cases.

- id: elixir-pipe-03
  answer: |
    If a value returned by a `<-` expression does not match its pattern, the remaining clauses are skipped and that unmatched value becomes the result of the `with` expression, unless an `else` block handles it. The `else` block matches the failed value and lets the code translate, recover from, or deliberately raise on the failure.

- id: elixir-pipe-04
  answer: |
    Piping is wrong when the function's natural argument is not the data flowing through the pipeline. For example, `IO.inspect(value) |> String.trim()` becomes `String.trim(IO.inspect(value))`, but `IO.inspect/1` returns `:ok`, not the inspected value. Call `String.trim(value)` directly, or pipe in the intended direction with `value |> String.trim() |> IO.inspect()`.

- id: elixir-error-01
  answer: |
    Use tagged tuples such as `{:ok, value}` and `{:error, reason}` for expected, recoverable failures that callers should handle, especially normal conditions such as a missing record or invalid input. They make failure explicit and composable. Raise exceptions for unexpected conditions, broken invariants, programming errors, or failures for which there is no meaningful recovery path. Exceptions should not normally be used as ordinary control flow.

- id: elixir-error-02
  answer: |
    A non-bang function generally returns a tagged result on failure. For example, `File.read(path)` returns `{:ok, contents}` or `{:error, reason}`. Its bang counterpart, `File.read!(path)`, returns the contents directly when successful and raises an exception when it fails. The trailing `!` signals that failure will be raised rather than returned as an error value.

- id: elixir-error-03
  answer: |
    `try/rescue/after` evaluates a protected expression, optionally handles raised exceptions with `rescue`, and always evaluates `after` during normal completion, a raised exception, a throw, or an exit handled while exiting the `try` expression. `rescue` handles exceptions, such as `ArgumentError`; `catch` handles throws, exits, and errors using its own clause forms. The `after` block is not a general guarantee against an uncatchable process kill or VM termination, and an exception raised in `after` can replace the original result or error.

- id: elixir-error-04
  answer: |
    “Let it crash” means allowing an unexpected failure to terminate the current process instead of defensively rescuing everything. Elixir processes are isolated, so supervisors can restart a failed process, while a clear crash and stack trace expose the actual problem. This avoids hiding bugs, incorrect recovery logic, and unnecessary complexity. Expected, recoverable failures should still be handled explicitly.

- id: elixir-enum-01
  answer: |
    `Enum` performs operations eagerly: it traverses the enumerable and computes the result immediately. `Stream` defines a mostly lazy pipeline: work is generally performed when the stream is consumed. Building a `Stream` itself does not necessarily traverse its source or execute all transformations.

- id: elixir-enum-02
  answer: |
    First, a `Stream` is useful for an infinite or effectively unbounded source, especially when only a limited portion is needed, such as `Stream.iterate/2` followed by `Enum.take/10`. Second, it is useful for large or incremental sources such as files, sockets, or paginated data, where transformations can process items with bounded memory instead of first materializing the entire collection.

- id: elixir-enum-03
  answer: |
    `Enum.reduce(enumerable, initial, fun)` starts with `initial`, applies `fun` to each element and the current accumulator, and uses each returned value as the next accumulator. It returns the final accumulator. Reduction is a fundamental building block because many operations, including mapping, filtering, summing, grouping, and folding, can be expressed as different accumulator updates over an enumerable.

- id: elixir-enum-04
  answer: |
    `x <- list` is the generator, so each `x` is taken from `list`. `rem(x, 2) == 0` is a filter, so only even values proceed. `do: {x, x * x}` evaluates the output expression for each selected value. `into: %{}` collects the outputs into a map, so this comprehension builds entries such as `2 => 4` and `4 => 16`. Without `into`, `for` returns a list.

- id: elixir-proto-01
  answer: |
    A protocol provides polymorphism based on the data type of its first argument. `defprotocol` declares a protocol and its functions, such as `def size(data)`. `defimpl ProtocolName, for: SomeType` supplies the implementation for that type. Calling the protocol dispatches to the implementation associated with the runtime type of the first argument, allowing different types to behave differently without a common inheritance hierarchy.

- id: elixir-proto-02
  answer: |
    A behaviour is a compile-time contract for a module. `@callback` declares a required function and its argument and return types. `@behaviour MyBehaviour` in a module states that the module implements that behaviour and lets the compiler check the contract. `@impl true`, or `@impl MyBehaviour`, marks a function as an implementation of a callback and associates it with the relevant behaviour.

- id: elixir-proto-03
  answer: |
    Protocols dispatch operations by the runtime data type of the first argument and are open: new types can gain implementations without changing the protocol's callers. Behaviours define a contract for modules; modules declare that they implement the behaviour, and the compiler checks the callbacks and their specifications. A behaviour is not data-type dispatch, and a protocol is not primarily a compile-time module contract.

- id: elixir-proto-04
  answer: |
    `@impl` tells the compiler that a function is meant to implement a declared callback, so it can check the implementation against the behaviour's specification and warn about missing or unexpected callback implementations. `@impl MyBehaviour` is useful when a module implements multiple behaviours or when the callback is otherwise ambiguous. It provides compile-time verification and clarity; it does not provide runtime dispatch or replace the behaviour contract.

- id: elixir-conc-01
  answer: |
    The BEAM runs many lightweight, preemptively scheduled processes, potentially across multiple scheduler threads. Each process has its own stack, heap, and mailbox, and Elixir has no shared mutable memory between processes. Processes communicate by sending immutable terms in messages, which are copied or transferred into the recipient's memory. This isolation eliminates ordinary shared-memory data races, makes communication explicit, and prevents a process's failure from corrupting another process's state.

- id: elixir-conc-02
  answer: |
    `Task.async` starts work in a separate linked process and returns a task handle, allowing that work to run concurrently with the caller or other tasks. `Task.await` waits for the task and returns its result. If the task raises or exits unexpectedly, awaiting propagates the task's failure to the caller; an ordinary `{:error, reason}` return is just a value unless the task itself exits. If the wait exceeds the timeout, `Task.await` exits with a timeout reason, according to the Task API.

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple process whose purpose is to hold and update state through operations such as `Agent.get/2` and `Agent.update/2`. Its updates are serialized by the agent process. A `GenServer` offers a more general abstraction with custom protocol callbacks, typed messages, timeouts, linked-process handling, and complex state machines. An Agent is convenient when that extra GenServer machinery is unnecessary.

- id: elixir-conc-04
  answer: |
    ETS is an in-memory Erlang Term Storage table owned by a process but accessed directly by other processes rather than through mailbox messages. It is useful for read-heavy or high-throughput shared data such as caches, lookup tables, indexes, and coordination structures where message round trips would be unnecessary or too slow. ETS tables are not durable by default, and their ownership, synchronization, and cleanup must be managed; a supervisor can manage the owning process, but a GenServer is unnecessary just to store the table.
