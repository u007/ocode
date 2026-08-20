# elixir knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: elixir-pm-01
  answer: |
    `=` is the match operator. `{:ok, value} = fetch()` asserts that `fetch()` returns a 2-tuple whose first element is `:ok`, and if so binds `value` to the second element. It also returns the right-hand side on success. If the right side is `{:error, :notfound}` the pattern does not match, so it raises a `MatchError` (no binding occurs). It does not return nil or false - it crashes the current process unless rescued.

- id: elixir-pm-02
  answer: |
    Instead of `if/case` inside one function body, you define multiple heads of the same function with different patterns and optional `when` guards. At call time Elixir tries clauses top-to-bottom; the first clause where the arguments structurally match the patterns AND the guard expression evaluates to true is executed. If no clause matches you get a `FunctionClauseError`. Example: `def handle({:ok, x}) when is_integer(x) do ...` vs `def handle({:error, _}) do ...` - the shape and guard decide, not manual conditionals inside.

- id: elixir-pm-03
  answer: |
    `^` is the pin operator. It prevents rebinding and means "match against the current value of this variable". Bare `x` on the left of `=` always matches and (re)binds `x` to whatever is on the right. `^x` requires the right side to equal the existing value of `x`, otherwise a MatchError. E.g. `x = 1; x = 2` rebinds to 2, but `x = 1; ^x = 2` fails, while `^x = 1` succeeds without changing x.

- id: elixir-pm-04
  answer: |
    Map matching is subset/partial: `%{name: n} = user` succeeds if `user` is a map containing at least key `:name` (with any other keys allowed), binding `n`. It does NOT require exact keys. List/tuple matching is exact: `[a, b] = [1,2,3]` fails (different length), `{a,b} = {1,2,3}` fails (different arity). Maps only check that the keys mentioned in the pattern exist and match.

- id: elixir-otp-01
  answer: |
    `spawn/1,3` creates a new lightweight BEAM process returning its pid. `send(pid, msg)` (or `pid <- msg`) asynchronously enqueues `msg` in that process's mailbox and returns the message. `receive do pattern1 -> ... pattern2 -> ... after timeout -> ... end` scans the mailbox in order for the first message matching any pattern, removes it and executes that branch. If no message matches, `receive` blocks the process until a matching message arrives (or until the optional `after` timeout fires). Messages remain queued until consumed.

- id: elixir-otp-02
  answer: |
    `handle_call(request, from, state)` handles synchronous `GenServer.call/2,3` - caller blocks for a reply. It must return `{:reply, reply, new_state}`, `{:reply, reply, new_state, timeout/hibernate_opts}`, `{:noreply, new_state}` (to reply later via `GenServer.reply/2`), or `{:stop, reason, ...}`. `handle_cast(request, state)` handles asynchronous `GenServer.cast/2` - fire-and-forget, no reply to caller. It returns `{:noreply, new_state}` or `{:stop, reason, new_state}` (and noreply variants with timeout). `call` provides backpressure and result, `cast` does not.

- id: elixir-otp-03
  answer: |
    `handle_info(msg, state)` handles all ordinary messages arriving in the GenServer's mailbox that were not sent via `call`/`cast` - i.e. `send/2`, `Process.send_after`, monitors (`:DOWN`), links (`:EXIT`), `handle_continue`, etc. `handle_call` and `handle_cast` are only for the `GenServer.call/cast` protocol messages (`$gen_call`, `$gen_cast`). In effect, `handle_info` is the catch-all for raw BEAM messages. It returns like `handle_cast`: `{:noreply, new_state}` or `{:stop, ...}`.

- id: elixir-otp-04
  answer: |
    Three main strategies: `:one_for_one` - only the crashed child is restarted; `:one_for_all` - all children are terminated and restarted when one crashes; `:rest_for_one` - the crashed child and all children started *after* it are terminated and restarted (those started before are left alone). A child spec is a map describing how the supervisor starts/monitors a child: `:id`, `:start` `{Mod, fun, args}`, `:restart` (`:permanent/:transient/:temporary`), `:shutdown`, `:type` (`:worker/:supervisor`), `:modules`.

- id: elixir-data-01
  answer: |
    Data is immutable, so `List.delete(list, 2)` does NOT mutate `list`; it returns a new list `[1,3]` and `list` remains `[1,2,3]`. Rebinding `list = List.delete(list, 2)` does not mutate the original data; it simply makes the variable `list` point to the new list value. The old list value still exists unchanged (and will be GC'd if no longer referenced). Variables are just labels, not mutable slots.

- id: elixir-data-02
  answer: |
    Keyword lists: list of `{atom, value}` tuples, ordered, allows duplicate keys, slow lookup, used for options/DSLs where order matters: `[port: 4000, host: "localhost"]`. Maps: key->value store, any key type, unique keys, unordered, efficient lookup, general associative data: `%{name: "Jo", age: 30}`. Structs: maps with fixed keys, a `__struct__` field, compile-time guarantees - defined via `defstruct` inside a module, used for typed/domain entities: `%User{name: "Jo"}` - you get defaults, enforced keys, and can dispatch on them.

- id: elixir-data-03
  answer: |
    `%{map | key: val}` is the update syntax; it requires `key` to already exist in `map`, otherwise raises `KeyError`. It is assertive and slightly faster. `Map.put(map, key, val)` always succeeds - if `key` exists it updates, if not it inserts. Use `|` when you want to ensure you are not accidentally adding a new key (e.g. struct update).

- id: elixir-data-04
  answer: |
    `get_in`, `put_in`, `update_in` (and macros `get_and_update_in`) access/update nested immutable structures via Access protocol without manual copy-every-level. E.g. `put_in(user, [:address, :city], "Paris")` returns a new root with only the nested path changed, sharing unchanged structure. Better than ` %{user | address: %{user.address | city: "Paris"}}` because it is concise, composable, works polymorphically over maps/structs/keyword lists via Access, and supports functions/lenses (`Access.key`, `Access.all`).

- id: elixir-pipe-01
  answer: |
    `|>` pipes the result of the left expression as the first argument to the function call on the right. `a |> b(c, d)` rewrites to `b(a, c, d)`. The one rule: the piped value is always inserted as the first argument; if you need it elsewhere use an anonymous capture or explicit call. It is just syntactic sugar for nesting, enabling left-to-right data flow.

- id: elixir-pipe-02
  answer: |
    `with` solves nested `case {:ok,_}` happy-path pyramids (railway-oriented programming). It chains steps that may fail with `{:ok, _}` / `{:error, _}` without deep nesting: `with {:ok, a} <- step1(), {:ok, b} <- step2(a), do: {:ok, b}`. If all `<-` matches succeed, the `do` block runs. It lets you write a linear sequence of dependent failable operations, handling errors in one place, much cleaner than `case` inside `case`.

- id: elixir-pipe-03
  answer: |
    With `pattern <- expr`, if `expr` returns a value that does NOT match `pattern`, the `with` short-circuits: that non-matching value is immediately returned as the result of the whole `with` (it does not continue to next clauses). The optional `else` block lets you handle those non-matching values: `else` clauses pattern-match on the failed value to transform it, e.g. `else {:error, r} -> {:error, r}; _ -> {:error, :unknown} end`. Without `else`, the raw non-matching value is returned.

- id: elixir-pipe-04
  answer: |
    Piping is wrong when you are not threading a single value through transformations, or when readability suffers: multiple branches, functions needing the piped value not as first arg, or side-effect-only steps. Bad: `data |> Enum.filter(...) |> (if length(...) > 0, do: ... else: ...)` or `x |> foo(a,b)` where `x` should be second arg, requiring awkward `|> then(&foo(a, &1, b))`. Fix: break with a variable, use `with`/`case`, or use `then/1`/`tap/1` sparingly, or just call `foo(a, x, b)` directly. Single-value pipelines with arity-1 transforms are ideal; conditional logic or non-first-arg insertion is not.

- id: elixir-error-01
  answer: |
    Use `{:ok, value} | {:error, reason}` tagged tuples for expected, recoverable failures that are part of domain logic - file not found, validation error, external call failure - where caller should handle both branches. Raise exceptions (`raise`, `throw`) only for programmer errors, contract violations, or truly exceptional/unrecoverable conditions where you cannot continue. Elixir libraries provide both: `File.read` returns tuples, `File.read!` raises on error for when you want to crash/fast-fail.

- id: elixir-error-02
  answer: |
    Trailing `!` is the convention for the raising variant of a function. `File.read(path)` returns `{:ok, binary} | {:error, reason}` safely. `File.read!(path)` does the same work but returns the bare `binary` on success and raises an exception on failure. Use `!` when failure should be exceptional and you want to crash or let the caller rescue, not pattern-match.

- id: elixir-error-03
  answer: |
    `try do ... rescue e in [SomeError] -> handle ... after cleanup end` . `rescue` catches only exceptions raised with `raise` (i.e. `** (RuntimeError)` etc.) and you can pattern-match on exception type/struct. `catch` (in `try/catch`) catches throws (`throw/1`), exits (`exit/1`), and can catch any term, not just exceptions. `after` is guaranteed to run whether the `try` succeeded, rescued, or caught - typically for cleanup (close file, release resource) - even if no exception occurred.

- id: elixir-error-04
  answer: |
    "Let it crash" says don't defensively program every process to never fail; instead isolate work in supervised processes, write the happy path, and if something unexpected happens let the process crash and let its Supervisor restart it to a known good state. Because BEAM processes share no memory and are cheap/isolated, a crash doesn't corrupt global state. This leads to simpler code (no tangled error guards everywhere) and better resilience - supervisors handle recovery policy, rather than burying errors with `rescue` that leaves process in unknown state.

- id: elixir-enum-01
  answer: |
    `Enum` is eager - it traverses the whole enumerable immediately and returns a concrete list/map/result. `Stream` is lazy - it builds a composable recipe (a stream struct) with deferred computation; nothing executes until you call a terminal `Enum` function or `Stream.run`. Enum does work now, Stream does work on demand, enabling fusion and handling infinite/large data without intermediate lists.

- id: elixir-enum-02
  answer: |
    1) Large or infinite collections where eager `Enum` would allocate huge intermediate lists or never finish - e.g. `File.stream!("huge.log") |> Stream.map(...) |> Stream.filter(...) |> Enum.take(10)` or infinite `Stream.iterate(0, & &1+1)`. 2) Pipeline of multiple transforms where laziness fuses them and avoids intermediate lists - e.g. `1..1_000_000 |> Stream.map(&expensive/1) |> Stream.filter(&pred/1) |> Enum.take(5)` stops early instead of mapping all 1M eagerly. Also I/O resources that should be processed incrementally.

- id: elixir-enum-03
  answer: |
    `Enum.reduce(enumerable, acc, fun)` folds the enumerable by iteratively calling `fun.(element, accumulator)` and threading the accumulator, returning the final accumulator. It's fundamental because any traversal can be expressed as a fold: `map` is `reduce` with `acc ++ [f(x)]`, `filter` is `reduce` keeping only matching, `sum` is `reduce` with `+/2`, etc. It captures the generic recursion pattern over a collection.

- id: elixir-enum-04
  answer: |
    `for x <- list` is the generator (enumerates `list`, binds each `x`). `rem(x,2)==0` is a filter (keeps only even x; filters are bare boolean expressions after generator). `into: %{}` is a collector - injects results into the given collectable (here a map). `do: {x, x*x}` is the body expression producing one element per iteration (here a tuple that becomes a map entry). So it iterates list, filters evens, maps each to `{x, square}`, and collects into a map `%{2=>4, 4=>16...}`.

- id: elixir-proto-01
  answer: |
    A protocol is Elixir's polymorphism for data types - dispatch based on the type of a value. `defprotocol Stringifiable do def to_string(data) end` declares the interface. `defimpl Stringifiable, for: MyStruct do def to_string(s), do: ... end` provides the implementation for a specific type (or `for: Any` with fallback). At runtime `Stringifiable.to_string(value)` dispatches to the impl for `value`'s type. Built-ins like `Enumerable` and `Inspect` are protocols.

- id: elixir-proto-02
  answer: |
    A behaviour is an interface for modules - dispatch based on module, like abstract callbacks. `@callback my_fun(arg :: type) :: return_type` inside a `defmodule MyBehaviour` declares a required function signature. A module adopts it with `@behaviour MyBehaviour` (gives compile warnings if callbacks missing). `@impl true` (or `@impl MyBehaviour`) marks the following `def` as implementing a behaviour callback, enabling compiler checks that the function действительно exists in the behaviour and has correct arity.

- id: elixir-proto-03
  answer: |
    Protocol = polymorphism on *data type* (what the value is) - one function name dispatches to different code per struct/type; caller doesn't choose module. Behaviour = polymorphism on *module* (who implements the interface) - different modules each provide the same set of callbacks; caller chooses module. E.g. `Inspect` protocol dispatches on struct, `GenServer` behaviour requires any server module to implement `handle_call`. Protocols are about extending operations to new data, behaviours about ensuring modules conform to a contract.

- id: elixir-proto-04
  answer: |
    `@impl true` tells the compiler to verify the next `def` actually implements a callback from the declared `@behaviour`. If you typo the name/arity or the behaviour changes, you get a compile warning/error instead of silently defining an unused function. `@impl MyBehaviour` does the same but explicitly names which behaviour (useful with multiple behaviours). It also documents intent and enables tooling/dialyzer.

- id: elixir-conc-01
  answer: |
    BEAM runs many ultra-lightweight processes (actors) scheduled preemptively on schedulers, each with private heap, stack, and mailbox; they communicate only by copying messages (`send/receive`), no shared heap or mutable memory. No shared mutable memory avoids data races, locks, and the need for mutexes/GIL; isolation means one process crashing cannot corrupt another's state, enabling supervision, hot code reload, and scaling across cores/nodes transparently. State is held per-process and shared only via messaging or ETS.

- id: elixir-conc-02
  answer: |
    `Task.async(fn -> work end)` spawns a linked process to run `work` concurrently and returns a `%Task{}` struct (containing pid/ref). `Task.await(task, timeout \\ 5000)` blocks the caller waiting for the task's reply message. If the task raises, `await` re-raises the exception in the caller (exit propagates due to link). If it exceeds timeout (default 5s), `await` exits with timeout, and the task remains running unless shut down. `Task.yield`/`shutdown` allow non-raising checks.

- id: elixir-conc-03
  answer: |
    `Agent` is a simple state holder - a GenServer abstraction for just keeping and updating state via `Agent.get/update/get_and_update` and `start_link`. No need to write `handle_call/cast` callbacks. Use it when you only need synchronous access to shared mutable state (counter, cache). A full `GenServer` is needed when you need custom messages, async casts, handle_info, complex logic, queuing, or to implement a process protocol beyond simple get/update. Agent is less boilerplate for trivial state.

- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is an in-memory, mutable, concurrent key-value store owned by a process but accessible by any process without message passing. Tables can be `public`/`protected` for direct concurrent reads/writes, with `:set/:ordered_set/:bag`. Use ETS when a GenServer/Agent mailbox would be a bottleneck for shared read-heavy data (caches, lookup tables, counters, session store) - it gives O(1) concurrent access without serialization through one process. Tradeoff: data is mutable outside process isolation, not automatically persisted/not replicated, needs owner handling.
```
