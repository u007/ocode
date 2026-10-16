# nvidia/nemotron-3.5-super-vl — with-skill answers for elixir (openrouter, 2026-10-16); answerer saw only _prompts/elixir.md; derived/elixir.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: elixir-pm-01
  answer: |
    `{:ok, value} = fetch()` is a pattern match, not an assignment. Elixir evaluates `fetch()` and asserts the result matches `{:ok, value}`: on success `value` is bound to the second element of the tuple and the expression evaluates to the tuple. If the right side is `{:error, :notfound}`, `:ok` does not match `:error`, so the match fails and raises `MatchError`. Likewise `1 = x` is legal and asserts that `x` already equals 1.
- id: elixir-pm-02
  answer: |
    Multiple function-head clauses are tried top to bottom. The first clause whose pattern matches the arguments and whose `when` guard passes is the one whose body runs; the rest are skipped. Guards only admit guard-safe expressions (comparisons, boolean and arithmetic operators, `is_*/1` checks, `in` with literals, and a small Kernel subset like `length/1`); your own functions are never allowed, pure or not. This lets each clause encode one case instead of using `if`/`cond`. If no clause matches, the call raises `FunctionClauseError` unless you deliberately add a catch-all.
- id: elixir-pm-03
  answer: |
    The pin operator `^x` matches against the current value of the variable `x` without rebinding it. A bare `x` on the left of a match always matches anything and rebinds `x` to the matched value. So `^x = y` succeeds only when `y` equals the existing `x`, whereas `x = y` always succeeds and makes `x` refer to `y`.
- id: elixir-pm-04
  answer: |
    Map matching matches on a subset of keys: `%{name: n} = user` requires `user` to be a map containing at least `:name`, binds `n` to `user[:name]`, and does not require the map to have exactly those keys — extra keys are fine. List matching requires exact length and element order; tuple matching requires exact arity and element structure. So maps assert the presence of named keys, not exclusivity.
- id: elixir-otp-01
  answer: |
    `spawn` creates a new lightweight BEAM process and returns its pid. `send(pid, msg)` puts a message in that process's mailbox and returns immediately — it is asynchronous. `receive` blocks the current process and scans its mailbox for a message matching one of its clauses; if no message matches, it waits indefinitely unless an `after` timeout is provided. It does not raise when nothing matches; it simply keeps waiting.
- id: elixir-otp-02
  answer: |
    `handle_call` handles synchronous requests: the caller blocks until a reply is sent back, so the callback must include a reply in its return, typically `{:reply, reply, state}`. `handle_cast` handles asynchronous messages: no reply is expected, the caller does not wait, and the return is `{:noreply, state}`. Both callbacks return an updated server state.
- id: elixir-otp-03
  answer: |
    `handle_info` handles everything that is not a call or a cast: plain messages sent with `send/2`, system messages, timeout messages from `Process.send_after/3`, and `:timeout` from `receive ... after`. Unlike `handle_call`/`handle_cast`, there is no protocol-level reply mechanism, so the sender gets nothing back. It is the catch-all for out-of-band communication.
- id: elixir-otp-04
  answer: |
    The three main restart strategies are `:one_for_one` (restart only the crashed child), `:one_for_all` (terminate and restart all children), and `:rest_for_one` (restart the crashed child plus every child started after it). A child spec is a map (usually returned by `child_spec/1`) describing how to supervise a child: `:id`, `:start` (module, function, args), `:restart` (`:permanent`, `:temporary`, `:transient`), `:shutdown`, `:type` (`:worker` or `:supervisor`), and `:modules`.
- id: elixir-data-01
  answer: |
    Nothing happens to `list` — Elixir data is immutable, so `List.delete(list, 2)` leaves `[1, 2, 3]` untouched and returns a new list, `[1, 3]`. Rebinding (`list = List.delete(list, 2)`) only makes the variable name refer to the new list; the old list still exists until garbage collection. A variable is a label, not a box.
- id: elixir-data-02
  answer: |
    Maps are general key-value stores with any key types; use them for hashes and dictionaries. Keyword lists are lists of two-element tuples `{:key, value}` with atom keys: ordered, allowing duplicates, typically used for options and optional arguments. Structs are maps with a `__struct__` field defined by `defstruct`, with fixed fields and compile-time guarantees; use them for your own domain entities. Use maps for arbitrary data, keyword lists for options, structs for named records.
- id: elixir-data-03
  answer: |
    `%{map | key: val}` updates only when `key` already exists; if the key is missing it raises `KeyError`. `Map.put(map, key, val)` inserts the key when missing and updates it when present, so it never raises for an absent key. Use `Map.put` when you might be inserting; use the `|` form when you want to assert the key already exists.
- id: elixir-data-04
  answer: |
    `get_in/2` reads a nested value through a path of keys or indices, `put_in/2` updates a nested value and returns a new structure, and `update_in/2` applies a function to a nested value. They use access keys such as `[:foo, 0, :bar]` so you avoid manual nested `case`/`with` or `Map.update!` chains. Because the data is immutable, manual nesting forces you to rebuild every level by hand; these macros traverse and rebuild automatically, reading top to bottom.
- id: elixir-pipe-01
  answer: |
    `x |> f(a)` rewrites to `f(x, a)`: the value on the left of `|>` becomes the first argument of the call on the right. The operator exists for readability — a pipeline reads as a top-to-bottom sequence of transformations instead of inside-out nesting, like `list |> Enum.map(f) |> Enum.filter(p)` versus the equivalent nested calls.
- id: elixir-pipe-02
  answer: |
    `with` solves chaining steps that return tagged tuples, usually `{:ok, value}`, without nesting a `case` after every step. Each `<-` clause unwraps a matching value and binds it; later clauses may use earlier bindings. If every clause matches, the `do:` block runs with all values bound. If any `<-` clause fails, the `with` short-circuits and returns (or routes to `else`) the failing value.
- id: elixir-pipe-03
  answer: |
    When a `<-` clause's pattern does not match, execution stops immediately, later clauses are skipped, and the non-matching value becomes the result of the `with` expression. The optional `else` block receives that failing value and matches it against its own clauses, commonly `{:error, reason}`, letting you convert failures into a controlled result instead of propagating the raw value.
- id: elixir-pipe-04
  answer: |
    Piping is the wrong tool when the value must land in a non-first argument or when you are combining several independent values, such as `Map.merge(m1, m2)` or `Enum.zip(a, b)` — there is no single value flowing through. It also reads badly for a lone call (`x |> f()` is just `f(x)`). Fix it with a plain call, `then/2` such as `key |> then(&Map.get(opts, &1))`, or a named intermediate variable; never use `with` just to fix argument order.
- id: elixir-error-01
  answer: |
    Use tagged tuples (`{:ok, value}`, `{:error, reason}`) for expected, recoverable failures that the caller should handle as control flow, such as a missing file or invalid input. Raise exceptions for truly exceptional or programmer-error conditions — bugs, violated invariants, arguments that can never be valid — where crashing is preferable to inventing a return value. Libraries often expose both: `File.read` for handled errors, `File.read!` for "this must work".
- id: elixir-error-02
  answer: |
    The trailing `!` means the function raises an exception on failure instead of returning an `{:error, reason}` tuple. `File.read/1` returns `{:ok, body} | {:error, reason}`; `File.read!/1` returns the body or raises. The convention is to provide the non-bang version for callers that want to handle failure, and the bang version for callers where failure is exceptional and should crash, often under a supervisor.
- id: elixir-error-03
  answer: |
    `try ... rescue` runs the body and catches exceptions raised inside it; `rescue` clauses pattern-match on exception modules, such as `rescue in ArgumentError -> ...`. `catch` is broader: it catches `:throw`, `:exit`, and `:error` signals, not just runtime exceptions. `after` is guaranteed to run no matter how the block exits — normal return, rescued exception, or caught throw/exit — making it the place for cleanup such as closing files.
- id: elixir-error-04
  answer: |
    "Let it crash" means: do not write defensive code to catch every possible error inside a process; let the process crash when something unexpected happens and let its supervisor restart it into a clean known state. BEAM processes are isolated — no shared memory, cheap to create — so one crash cannot corrupt other processes' state. Crashing is often better than rescuing everything because defensive code misses edge cases and can leave state inconsistent, while a fresh restart guarantees consistency. Pair crashes with supervision trees instead of bare `try/rescue` everywhere.
- id: elixir-enum-01
  answer: |
    `Enum` is eager: it traverses the enumerable immediately and returns a concrete result, usually a list. `Stream` is lazy: it returns a stream that composes transformations but does no work until something enumerates it, such as `Enum.to_list/1`. Each `Stream` operation builds a chain of functions, and enumeration pulls elements through the chain one at a time.
- id: elixir-enum-02
  answer: |
    1. Infinite sequences — `Stream.iterate/2` or `Stream.cycle/1` produce endless streams you can `take` from; `Enum` would try to materialize the whole thing. 2. Huge or streaming data where you only need a prefix — chaining `Stream.map`, `Stream.filter`, then `Enum.take/2` avoids building intermediate lists for the full dataset.
- id: elixir-enum-03
  answer: |
    `Enum.reduce/3(enumerable, acc, fun)` calls `fun.(element, acc)` for each element, threading the return value as the next accumulator, and returns the final accumulator. It is fundamental because `map`, `filter`, `sum`, `max`, and others are all just reductions: map builds a new list into the accumulator, filter conditionally appends, sum adds to a number. Most `Enum` functions can be implemented on top of `reduce`.
- id: elixir-enum-04
  answer: |
    `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`: `x <- list` is the generator binding `x` to each element; `rem(x, 2) == 0` is a filter keeping only even numbers; `do: {x, x * x}` produces each output entry (key `x`, value its square); and `into: %{}` collects those entries into a map. The result is a map of even numbers to their squares. General form: `for generator(s), filter(s), into: collector, do: expression`.
- id: elixir-proto-01
  answer: |
    A protocol defines polymorphic behaviour: one or more functions that dispatch on the type of their first argument. `defprotocol` declares the protocol name and its function signatures. `defimpl` provides an implementation of those functions for a specific type (or an `@on_type` fallback). At runtime, calling a protocol function inspects the argument's type and invokes the matching `defimpl`, so the same call works differently for different data types.
- id: elixir-proto-02
  answer: |
    A behaviour is a module contract declaring which functions a module must export. `@callback` declares a callback's name, arguments, and return type. `@behaviour MyBehaviour` declares that this module implements that behaviour. `@impl true` marks a function as implementing a behaviour callback; `@impl MyBehaviour` names the behaviour explicitly. The compiler checks that `@impl` functions match actual callbacks in the declared behaviour.
- id: elixir-proto-03
  answer: |
    Protocols dispatch on data types (ad-hoc polymorphism): any type can implement a protocol, and the implementation is chosen by the argument's type at call time, without the type needing to know about the protocol. Behaviours are module contracts: a module explicitly declares `@behaviour` and exports the required functions, as with GenServer. Protocols are functions over types; behaviours are modules fulfilling an interface.
- id: elixir-proto-04
  answer: |
    `@impl true` tells the compiler the function is intended to implement a callback from the module's declared behaviour; the compiler verifies it matches an actual callback by name and arity, catching typos, wrong arities, and implementations of removed callbacks at compile time. `@impl MyBehaviour` scopes the check to a specific behaviour. It also documents intent for readers. It buys compile-time safety and clarity, not runtime behaviour.
- id: elixir-conc-01
  answer: |
    The BEAM runs thousands of isolated, lightweight processes, each with its own heap and stack, scheduled preemptively by the VM. Processes communicate only by copying messages through mailboxes; there is no shared mutable memory. That isolation means one process cannot corrupt another's state, crashes are contained to the failing process, garbage collection can run per-process, and processes can be distributed across nodes without changing the message-passing model. Locks are unnecessary because no two processes mutate the same memory.
- id: elixir-conc-02
  answer: |
    `Task.async/1` spawns a process to run a function and returns a task referencing it. `Task.await/1` blocks the calling process until that task finishes and returns its result; if the task raises, the same exception is raised in the awaiting process. `Task.await/2` accepts an optional timeout that bounds the wait; once it expires the caller stops waiting rather than returning the task's result (the task itself keeps running unless it is linked to the caller). Tasks are fire-and-forget computations you later join.
- id: elixir-conc-03
  answer: |
    An `Agent` is a process that holds a single piece of state and lets you read or mutate it with `Agent.get/3` and `Agent.update/2`: you pass a function that runs inside the agent process and receives the current state, and its return becomes the result or the new state. It is a GenServer specialized to state — less boilerplate, no `handle_call`/`handle_cast` to write. Use an Agent when all you need is shared state; use a full GenServer when you need casts, info messages, timeouts, or a custom lifecycle.
- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is an in-memory key-value table built into the BEAM, accessible concurrently by many processes with near-O(1) lookups. Use ETS instead of a GenServer/Agent when many processes need fast shared read/write access to the same data: an Agent serializes every access through one process, which becomes a bottleneck, while ETS lets processes read and write directly. The trade-off is that ETS is raw storage without OTP supervision or automatic cleanup unless tied to a process or table options.
