- id: elixir-pm-01
  answer: |
    The right-hand side is evaluated first, then its value is matched against the pattern on the left. `{:ok, value} = fetch()` succeeds only if `fetch()` returns a two-element tuple whose first element is `:ok`; on success, `value` is bound to the second element.

    If `fetch()` returns `{:error, :notfound}`, the pattern does not match and Elixir raises a `MatchError`.

- id: elixir-pm-02
  answer: |
    Multiple function-head clauses provide branching based on argument patterns and guards. Elixir examines the clauses from top to bottom and executes the first one whose parameters match and whose `when` guards all evaluate truthy. If no clause matches, it raises `FunctionClauseError`.

    ```elixir
    def sign(n) when n < 0, do: :negative
    def sign(0), do: :zero
    def sign(n) when n > 0, do: :positive
    ```

- id: elixir-pm-03
  answer: |
    The pin operator `^` makes a pattern match the exact value already bound to that variable. It does not create a new binding.

    ```elixir
    x = :expected
    {:ok, ^x} = fetch()  # matches only {:ok, :expected}
    {:ok, x} = fetch()   # x is a fresh pattern binding and can match any value
    ```

    A bare `x` in a match pattern is an unbound pattern variable, so it matches any value and binds or rebinds `x`.

- id: elixir-pm-04
  answer: |
    Map patterns match a required subset of a map's entries rather than requiring the map to contain exactly the listed keys. `%{name: n} = user` requires `user` to be a map containing `:name`, binds its value to `n`, and ignores additional keys. Use `^expected` to match an already-bound value.

- id: elixir-otp-01
  answer: |
    - `spawn(fun)` starts a new BEAM process, runs `fun` in it, and returns its PID. It links that process to the caller; when the function returns, the process exits normally, and an unhandled failure terminates it abnormally.
    - `send(pid, message)` places a term in the process's mailbox and returns the sent message.
    - `receive` pattern-matches messages already in the mailbox. A matching message is consumed and its clause's value is returned. Nonmatching messages remain in the mailbox. If nothing matches, `receive` waits until a matching message arrives or an optional timeout's `after` clause produces a timeout result.

- id: elixir-otp-02
  answer: |
    `handle_call` handles synchronous requests made with `GenServer.call/3`. The caller waits for a reply, and the callback normally returns `{reply, new_state}`. It can instead return a stop tuple such as `{:stop, reason, reply, new_state}`.

    `handle_cast` handles asynchronous casts made with `GenServer.cast/2`. The caller does not wait for a reply, and the callback normally returns `:noreply` or `{:noreply, new_state}`. It can also return a stop tuple without a reply.

- id: elixir-otp-03
  answer: |
    `handle_info` handles ordinary informational messages that are not synchronous calls or casts. Examples include `send(pid, message)`, timeout notifications, tick messages, monitor `DOWN` notifications, and some linked-process exit notifications.

    `handle_call` and `handle_cast` are dedicated callbacks for `GenServer.call/3` and `GenServer.cast/2`, respectively. `handle_info` receives the general mailbox protocol, so it should be used deliberately rather than as a catch-all unless re-raising unexpected messages is acceptable.

- id: elixir-otp-04
  answer: |
    The three standard restart strategies are:

    - `:one_for_one`: restart only the child that terminated; its siblings continue.
    - `:one_for_all`: if one child terminates, terminate and restart all the supervisor's children.
    - `:rest_for_one`: restart the terminated child and all children started after it; earlier siblings continue.

    A child spec is configuration that tells a supervisor how to identify, start, and manage a child. Its required entries are `:id` and `:start`; common optional entries include `:restart`, `:shutdown`, and `:type`. For example:

    ```elixir
    %{
      id: MyWorker,
      start: {MyWorker, :start_link, []}
    }
    ```

- id: elixir-data-01
  answer: |
    `List.delete(list, 2)` returns a new list containing `[1, 3]`; it does not alter the original list, which remains bound as `[1, 2, 3]`.

    Rebinding a variable associates that name with another value. Thus:

    ```elixir
    list = List.delete(list, 2)
    ```

    makes `list` refer to `[1, 3]`, but it does not mutate the original data structure. The old value can still be held by another variable.

- id: elixir-data-02
  answer: |
    - Maps are key-value collections with arbitrary term keys. Use them for dynamic lookups, updates, nested data, and keys that are not a fixed set of known fields.
    - Keyword lists are ordered lists of two-element tuples, conventionally with atom keys. Use them for option lists, configuration, and APIs that benefit from ordered keyword syntax.
    - Structs are maps with a known `__struct__` key, a module-defined shape, and optional defaults or enforced keys. Use them for domain records whose fields should be known statically and represented with `%Struct{...}` syntax and struct patterns.

- id: elixir-data-03
  answer: |
    `%{map | key: val}` is map-update syntax and behaves like `Map.update!/3`. If `:key` already exists, it returns a map with that key replaced. If the key is absent, it raises a `KeyError`.

    `Map.put(map, :key, val)` returns a map with the value inserted or replaced and adds the key when it does not already exist.

- id: elixir-data-04
  answer: |
    - `get_in(data, path)` retrieves a value through a nested key or `Access` path.
    - `put_in(data, path, value)` returns updated nested data with a new value inserted along that path.
    - `update_in(data, path, fun)` retrieves the current nested value, applies `fun` to it, and returns the data with the resulting value placed back along the path.

    These functions provide concise, reusable nested-data operations. They centralize access and reconstruction logic that would otherwise require separate calls to `get`, update the result, and manually rebuild nested maps or lists.

- id: elixir-pipe-01
  answer: |
    The pipe operator takes the value on its left and inserts it as the **first argument** of the function on its right. Thus `a |> f(b)` is exactly `f(a, b)`.

    Its purpose is to flatten nested calls into a readable, top-to-bottom data pipeline. This is why core APIs conventionally take data as their first argument:

    ```elixir
    list
    |> Enum.map(&(&1 * 2))
    |> Enum.filter(&(&1 > 4))
    |> Enum.sum()
    ```

    This reads more clearly than the equivalent `Enum.sum(Enum.filter(Enum.map(...)))` nesting.

- id: elixir-pipe-02
  answer: |
    The `with` special form expresses a sequence of pattern matches and bindings. It is especially useful for a linear happy path in which several operations return `{:ok, value}` tuples.

    ```elixir
    with {:ok, user} <- fetch_user(id),
         {:ok, order} <- fetch_order(user) do
      {:ok, order}
    end
    ```

    Each right-hand side is evaluated in order. When its value matches the `<-` pattern, the pattern's variables are bound and evaluation continues. The `do` block is evaluated only after all clauses match, and its value is the result of the successful `with`.

- id: elixir-pipe-03
  answer: |
    The first `<-` clause whose pattern does not match short-circuits the remaining clauses. The **nonmatching value itself becomes the result of the whole `with` expression**, returned unchanged when there is no `else` block. Thus an `{:error, reason}` from a step flows directly out.

    An `else` block pattern-matches those failure values to handle or transform them:

    ```elixir
    with {:ok, user} <- fetch_user(id) do
      user
    else
      {:error, :not_found} -> :anonymous
      {:error, reason} -> {:fetch_failed, reason}
    end
    ```

    If no `else` clause matches a failure value, Elixir raises `WithClauseError`.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the piped value belongs in a later argument position rather than the function's first argument. For example, this incorrectly suggests the piped value will be the string argument:

    ```elixir
    value |> Regex.replace(~r/foo/, "bar")
    ```

    `Regex.replace/3` expects `(pattern, string, replacement)`. If a pipe is still desired, explicitly place the value with `then/2`:

    ```elixir
    value
    |> then(fn v -> Regex.replace(~r/foo/, v, "bar") end)
    ```

    Alternatively, bind the value to a descriptive variable and make a plain call. A one-call pipe such as `x |> f()` is also no clearer than `f(x)`.

    `IO.inspect/1` and `IO.inspect/2` are safe inside pipelines because they return the inspected value unchanged. `IO.puts/1` returns `:ok`, so inserting it into a data pipeline would replace the data.

- id: elixir-error-01
  answer: |
    Use tagged tuples such as `{:ok, value}` and `{:error, reason}` for expected, operation-specific failures that callers can reasonably handle. A function can return a failure to a caller that may retry, choose another approach, or propagate it with `with`.

    Use exceptions for exceptional conditions, failed invariants, programmer errors, or circumstances where continuing is not meaningful. Expected recoverable outcomes are better represented explicitly; unexpected failures can be allowed to crash an isolated process so supervision can recover it. An API may also raise at a boundary after accumulating lower-level tagged errors.

- id: elixir-error-02
  answer: |
    A trailing `!` conventionally indicates a variant that returns useful data directly but raises an exception on failure.

    ```elixir
    File.read(path)   # {:ok, contents} | {:error, reason}
    File.read!(path)  # contents, or raises on failure
    ```

    The non-bang function makes failure part of its normal return contract, while the bang function is a shorter convenience when the caller wants immediate failure behavior.

- id: elixir-error-03
  answer: |
    `try/rescue/after` provides guarded execution, exception handling, and cleanup. `rescue` clauses handle raised errors or exceptions, commonly by matching exception modules with `e in SomeError -> ...`. `catch` clauses can match the signal kind and value, such as `:throw, value` or `:exit, reason`; `catch` can also handle errors, but it is chiefly useful here for thrown values and exits.

    An `after` expression runs after the `try` body or its handled outcome, whether the body returns normally, raises, throws, or exits, and before the overall expression delivers its result or remaining exception. It is not guaranteed to run if the process is forcibly killed or the VM halts. An exception raised by the `after` expression replaces the pending result or exception.

- id: elixir-error-04
  answer: |
    “Let it crash” means allowing unexpected failures to terminate the process in which they occur instead of rescuing every error and continuing with potentially invalid state. This preserves stack traces, avoids silent error swallowing, and works well with supervision: a supervisor can restart a failed process according to its restart strategy while other processes continue.

    Process isolation makes this safer than handling failures globally. It does not mean ignoring every error: expected, recoverable failures should still be returned, handled, or transformed explicitly, while unexpected invariant violations should fail loudly.

- id: elixir-enum-01
  answer: |
    `Enum` functions eagerly traverse an enumerable and perform the work when the function is called, producing a completed result immediately.

    `Stream` functions return a composable stream and generally defer work until data is demanded by an enumerating operation. Streams can therefore represent large, expensive, or even infinite sequences without eagerly producing all of their elements.

- id: elixir-enum-02
  answer: |
    - An infinite or virtually unbounded source where only part will be consumed, such as:

      ```elixir
      1
      |> Stream.iterate(&(&1 * 2))
      |> Stream.take(10)
      |> Enum.to_list()
      ```

    - A large or resource-backed source, such as a file read line by line, that should be transformed incrementally to avoid holding the entire input in memory:

      ```elixir
      File.stream!("large.csv")
      |> Stream.map(&String.trim/1)
      |> Stream.each(&IO.puts/1)
      ```

- id: elixir-enum-03
  answer: |
    `Enum.reduce(enumerable, initial_accumulator, fun)` folds over the enumerable. By default it calls `fun.(element, accumulator)` from left to right and returns the final accumulated value.

    It is a fundamental building block because many collection operations can be expressed as folds: a `map` creates a new accumulated collection, a `filter` conditionally changes it, a `sum` accumulates numbers, and custom reductions can express aggregation patterns not directly provided by the standard library.

- id: elixir-enum-04
  answer: |
    ```elixir
    for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}
    ```

    - `x <- list` is the generator: it takes values from `list` and binds each to `x`.
    - `rem(x, 2) == 0` is a filter: only even values continue through the comprehension.
    - `into: %{}` specifies that generated results should be collected into a map rather than the default list.
    - `do: {x, x * x}` generates each result tuple, using the even value as the map key and its square as the value.

    If duplicate map keys are generated, the later generated value replaces the earlier one.

- id: elixir-proto-01
  answer: |
    A protocol is a mechanism for polymorphic dispatch based on the type of a value. `defprotocol` declares a protocol and its function names. `defimpl ProtocolName, for: SomeType` provides implementations for a particular type:

    ```elixir
    defprotocol Describe do
      def describe(value)
    end

    defimpl Describe, for: Integer do
      def describe(value), do: "integer: #{value}"
    end
    ```

    Calling `describe(value)` dispatches to the implementation for the first argument's type, without requiring inheritance-based dispatch.

- id: elixir-proto-02
  answer: |
    A behaviour is a formal contract that describes functions a module is expected to implement.

    - `@callback` declares a behaviour function, including its name, arity, and optionally its specification.
    - `@behaviour MyBehaviour` tells the compiler that the current module implements that behaviour, enabling warnings for missing or invalid callbacks.
    - `@impl true`, or `@impl MyBehaviour`, marks a function as an implementation so the compiler can verify it against the declared contract.

- id: elixir-proto-03
  answer: |
    A behaviour is primarily a compile-time contract attached to a module. A module opts into a behaviour, and the compiler checks that it supplies the required callbacks.

    A protocol is primarily runtime data dispatch. The protocol selects an implementation according to the type of its first argument, and implementations can be defined separately for different types.

    Thus behaviours answer “does this module fulfill this contract?”, while protocols answer “which implementation should run for this value?” Protocols do not require every implementation to come from one module adopting a single contract.

- id: elixir-proto-04
  answer: |
    Annotating an implementation with `@impl true` or `@impl MyBehaviour` makes the contract relationship explicit. The compiler can then report missing callbacks, callbacks that do not match the declaration, and functions marked as implementations that are not part of the behaviour. This helps catch contract drift during compilation rather than at runtime.

    The annotation also improves documentation and tooling. `@impl MyBehaviour` is useful when a module implements multiple behaviours, because it identifies the specific contract an implementation fulfills.

- id: elixir-conc-01
  answer: |
    The BEAM runs many lightweight, independently scheduled Erlang/Elixir processes on a small number of operating-system threads. Scheduling is preemptive, so one process does not need to yield explicitly for others to run.

    Ordinary BEAM processes do not share mutable heaps. Each process owns its state, and processes communicate by sending immutable terms through their mailboxes. This means data is effectively copied between processes rather than concurrently modified in place, so ordinary communication avoids shared-memory race conditions and locks.

    Isolation also limits the effects of a crash and allows large numbers of concurrent tasks. Explicit shared mechanisms such as ETS are available when shared data is genuinely needed.

- id: elixir-conc-02
  answer: |
    `Task.async(fun)` starts the function in a separate spawned, linked, and monitored process, allowing it to run concurrently. `Task.await(task)` waits for the task to send its result and returns that result.

    If the task exits abnormally—for example, because it raises—the linked failure and `Task.await` cause the caller to exit as well; the task's failure reason is propagated. If the optional timeout elapses before a result arrives, `Task.await` exits with a timeout exit. A timed-out task is not necessarily cancelled, so controlled workflows may use `Task.yield/2` followed by `Task.shutdown/1`, or `Task.async_stream/3`.

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple process for storing and updating one piece of state. Updates run inside the agent process, so messages are serialized and the state is not directly mutated by callers. Common operations include `Agent.get/2`, `Agent.update/2`, and `Agent.get_and_update/2`.

    A full `GenServer` provides a general message-processing state machine with synchronous calls, asynchronous casts, timeouts, initialization, termination, selective message handling, and richer state transitions. Use an `Agent` for simple state, and a `GenServer` when those coordination features or multiple message types are needed.

- id: elixir-conc-04
  answer: |
    ETS, the Erlang Term Storage, provides fast, in-memory, key-value tables accessible by multiple BEAM processes without first exchanging messages with an owning process. It supports high read concurrency and concurrent access patterns well.

    Use ETS for caches, indexes, read-heavy shared data, or other cases where direct lookups are preferable to serializing every operation through a `GenServer` or `Agent`. A process is usually preferable when updates need ordered multi-step logic, callers need synchronous coordination, or state transitions must be serialized. ETS table ownership also matters: the table is normally deleted when its owner exits unless ownership is transferred or a heir is configured.
