# elixir benchmark — answers (WITH derived skill active)
# Model: novita-ai/tencent/hy3   Skill: elixir.tencent__hy3.SKILL.md
# Closed-book (no answer key); the derived skill body was injected as active guidance.

- id: elixir-pm-01
  answer: |
    `=` is the match operator, not assignment. `{:ok, value} = fetch()` asserts that the
    value returned by `fetch()` has the shape `{:ok, something}`; if it matches, the
    variable `value` is bound to the second element of the tuple. Critically, `=` is a
    two-way match/equality assertion, not a store-into-a-cell operation — for example
    `1 = x` is legal and does NOT assign; it checks that `x` already equals `1` and
    raises `MatchError` if not. A bare variable on the left rebinds, which is exactly
    why the "it's assignment" mental model is wrong.

    If the right side is `{:error, :notfound}`, the pattern `{:ok, value}` does not
    match that shape, so the match fails and Elixir raises `** (MatchError) no match of
    right hand side value: {:error, :notfound}`.

- id: elixir-pm-02
  answer: |
    Multiple function-head clauses let you define the same function name/arity several
    times, each with its own pattern and optional guard. They replace conditional logic
    (if/case) by dispatching based on the *shape and properties* of the arguments.

    What decides which clause runs:
    1. Clauses are tried top-to-bottom, in source order.
    2. The first clause whose pattern matches the arguments AND whose guard passes wins
       and executes.
    3. Guards are NOT arbitrary code — a `when` clause is limited to a fixed whitelist of
       pure, side-effect-free functions and operators: comparison operators, boolean
       operators (`and`/`or`/`not`, plus `&&`/`||`/`!`), type checks (`is_integer/1`,
       `is_atom/1`, `is_list/1`, …), arithmetic (`+`/`-`/`*`/`div`/…), `in`, `elem/2`,
       `tuple_size/1`, `map_size/1`, `length/1`, `abs/1`, bitstring size, etc. You
       CANNOT call arbitrary user functions inside a guard. A guard that raises does not
       raise — it simply fails and the next clause is tried.
    4. If no clause's pattern-plus-guard matches, the call raises
       `** (FunctionClauseError) no function clause matching in <module>`.

- id: elixir-pm-03
  answer: |
    The pin operator `^` forces the variable to be used as a value to match against
    rather than as a fresh binding target. In `^x = expr`, Elixir requires that `expr`
    is equal to the *current* value of `x`; if it isn't, the match raises `MatchError`.

    A bare `x` on the left of a match is a binding — it always succeeds and rebinds `x`
    to whatever is on the right. `^x` instead says "use the existing value of `x` as a
    literal to compare against." This matters in function clauses and case patterns:
    e.g. `case val do ^x -> :same; _ -> :different end` compares `val` to the already
    bound `x`, whereas `case val do x -> :anything end` would just rebind `x` and match
    everything.

- id: elixir-pm-04
  answer: |
    Map patterns match by *subset*, not exact equality. `%{name: n} = user` requires
    that `user` is a map containing the key `name` (binding `n` to its value); it does
    NOT require the map to have exactly one key. The map may carry any number of
    additional keys and still match. This is unlike tuple and list matching, which are
    *exact* in length/shape: `[a, b] = list` requires exactly a two-element list, and
    `{a, b} = tuple` requires exactly a two-element tuple. (Matching a list against a
    cons pattern `[head | tail]` does match any non-empty list, but a fixed-length list
    pattern is exact.)

    Note that the update syntax `%{map | name: new}` IS exact: the key `name` must
    already exist, or you get a `** (KeyError)`.

- id: elixir-otp-01
  answer: |
    These are the raw building blocks of BEAM processes:
    - `spawn/1` (and `spawn/3`) creates a new lightweight process running a given
      function (or module/function/args) and returns its PID immediately; the process
      runs concurrently.
    - `send(dest, message)` asynchronously sends a message (any term) to a process
      identified by PID or registered name; it returns the message sent.
    - `receive` blocks the current process waiting for a message in its mailbox that
      matches one of its patterns, optionally with guards and a timeout
      (`after`): `receive do pattern -> ... after n -> ... end`.

    When `receive` runs and no message currently in the mailbox matches any clause, the
    process *blocks* (waits) for a new message to arrive, rather than falling through —
    unless an `after` timeout is given, in which case the timeout clause runs and the
    process continues. A blocked receive consumes no CPU.

- id: elixir-otp-02
  answer: |
    Both are GenServer callbacks that handle incoming messages, but they differ in
    whether a reply is expected:
    - `handle_call(request, from, state)` handles a *synchronous* call (`GenServer.call`).
      The caller blocks until a reply. It must return
      `{:reply, reply, new_state}` (or `{:reply, reply, new_state, opts}` /
      `{:stop, reason, reply, new_state}` / `{:noreply, new_state, opts}` etc.).
    - `handle_cast(request, state)` handles an *asynchronous* cast (`GenServer.cast`).
      The caller does not wait for a reply. It returns
      `{:noreply, new_state}` (or `{:stop, reason, new_state}` / `{:noreply, new_state, opts}`).

    In short: call = request/response (reply included), cast = fire-and-forget (no reply
    to the caller). Both return the new state so the server loop continues.

- id: elixir-otp-03
  answer: |
    `handle_info(info, state)` handles *all* messages sent to the GenServer that were
    NOT generated by `GenServer.call`/`cast` — i.e. messages that arrive in the
    process mailbox from `send`, from other processes, from ports/connected drivers, or
    OTP system messages. It returns `{:noreply, new_state}` (or stop tuples), just like
    `handle_cast`.

    The difference: `handle_call`/`handle_cast` are tied to GenServer's client API
    (they only fire for `call`/`cast` invocations and give you a typed request arg),
    whereas `handle_info` is the catch-all for anything else. You typically use it for
    timeouts, monitor `:DOWN` messages, `:ssl`/`Port` data, or custom `send`s.

- id: elixir-otp-04
  answer: |
    Supervisor restart strategies (`:strategy` in the child spec / `Supervisor.start_link`):
    - `:one_for_one` — if a child crashes, only that child is restarted. Others are
      unaffected. Best when children are independent.
    - `:one_for_all` — if any child crashes, *all* children are terminated and then all
      are restarted (in start order). Used when children depend on each other.
    - `:rest_for_one` — if a child crashes, that child and all children started *after*
      it are terminated and restarted; earlier children are left alone.

    A **child spec** is the description of how to start, restart, and supervise a child
    process: it includes `:id`, `:start` (the `{module, fun, args}` to launch it),
    `:restart` (`:permanent`/`temporary`/`transient`), `:shutdown` (timeout or `:brutal_kill`),
    `:type` (`:worker`/`:supervisor`), and `:modules`. Supervisors use these specs to
    start and know how to restart their children.

- id: elixir-data-01
  answer: |
    Data is immutable. `list = [1, 2, 3]` binds `list` to that list; calling
    `List.delete(list, 2)` returns a *new* list `[1, 3]` and leaves the original `[1, 2, 3]`
    completely unchanged — `list` still refers to the same old list until you rebind it.

    "Rebinding" a variable (e.g. `list = List.delete(list, 2)`) does NOT mutate the
    prior value. It simply makes the variable name point at a different, newly-created
    term. The old list still exists (until garbage-collected if nothing references it).
    There are no in-place updates — every "modification" produces a new data structure.

- id: elixir-data-02
  answer: |
    - **Maps** (`%{key => val}`): the general-purpose key-value store; keys can be any
      term; O(log n) access; great default for structured, arbitrary data. Pattern match
      by subset of keys.
    - **Keyword lists** (`[{:key, val}, ...]`): a list of two-tuples where keys are
      atoms; ordered, allows duplicate keys, and integrates with the keyword-syntax
      sugar `key: val`. Use them for options/arguments to functions (e.g. `Repo.get(User, id, preload: [:posts])`)
      and small ordered config, where order and duplicates matter.
    - **Structs** (`%User{...}`): a map with a fixed set of fields and a mandatory
      `__struct__` key (defined via `defstruct`). Use them for well-defined domain
      records where you want compile-time guarantees about fields/defaults and a clear
      type tag. They give structure + defaults, while maps stay flexible.

    Rule of thumb: options → keyword list; typed domain data → struct; ad-hoc/unknown
    keys → map.

- id: elixir-data-03
  answer: |
    - `Map.put(map, key, val)` works whether or not `key` exists: it adds the key if
      missing or replaces it if present, always returning a new map.
    - `%{map | key: val}` is the *update* syntax and requires that `key` already exists
      in `map` as a key. If it does not, Elixir raises `** (KeyError) key :key not found`.

    So for a missing key, `Map.put` succeeds (adds it) while `%{map | ...}` raises a
    `KeyError`. (Both are immutable — both return a new map and leave the original
    untouched; `%{map | ...}` is just a more efficient, compile-checked update for
    known keys, often used with structs.)

- id: elixir-data-04
  answer: |
    `get_in`, `put_in`, and `update_in` are access macros that navigate *nested*
    immutable structures (maps, structs, keyword lists, and even custom accessors via
    the `Access` behaviour) using a path of keys/indices:
    - `get_in(data, [key1, key2, ...])` reads a deeply nested value.
    - `put_in(data, [path], value)` returns a new structure with the value replaced at
      the path.
    - `update_in(data, [path], fn existing -> ... end)` applies a function to the
      existing nested value.

    They are better than manual nesting because they (a) handle the combinatorial
    boilerplate of copying every level of the structure on the way down, (b) avoid the
    subtle bug of forgetting to re-wrap an intermediate level, and (c) keep the code
    declarative about *what* changes rather than *how* to rebuild each layer. There's
    also `get_and_update_in` and the `data[key][key]` dynamic-access form.

- id: elixir-pipe-01
  answer: |
    The pipe operator `|>` takes the result of the expression on its left and passes it
    as the *first argument* to the function call on its right. So `a |> f(b)` is
    `f(a, b)`. It lets you read data transformations left-to-right instead of nested
    inside-out.

    The one rule: the piped value is **always inserted as the first argument** of the
    next function call. If you want it somewhere else you must use an inline
    `fn`/anonymous function, e.g. `data |> (fn x -> Enum.map(x, &String.upcase/1) end).()`
    — or rely on the function taking the data first (which is the Elixir convention for
    pipe-friendly functions).

- id: elixir-pipe-02
  answer: |
    `with` solves the "pyramid of nested case/if" problem when you have a sequence of
    steps that each can fail, and you want to stop at the first failure without deeply
    nesting happy-path code.

    It chains bindings/matches using `<-` (and `=`): each `<-` clause expects a
    `{:ok, value}` (or otherwise *matching*) result; if it matches, the bound variable
    is available to later steps; if a `<-` clause's pattern does NOT match, the whole
    `with` expression immediately returns that non-matching value (short-circuiting).
    Example:
    ```
    with {:ok, user} <- fetch_user(id),
         {:ok, repo} <- fetch_repo(user) do
      {:ok, repo}
    else
      {:error, reason} -> {:error, reason}
    end
    ```
    So a chain of `{:ok, _}` steps reads flat, and the first failing step bails out.

- id: elixir-pipe-03
  answer: |
    In a `with`, a value that fails to match a `<-` clause is returned directly as the
    result of the entire `with` expression — the `do` block is skipped and execution
    short-circuits at that clause. The unmatched value is returned untouched (it is NOT
    wrapped or raised).

    The `else` block exists precisely to handle those non-matching (typically error)
    values in one place. If provided, it receives the unmatched value as its argument
    and lets you pattern-match on the failure reasons and return a normalized result.
    Without an `else`, the raw non-matching value just bubbles out of the `with`.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the "flow" isn't a simple left-to-right transformation of
    one data value, e.g.:
    - When the same value must be used in several unrelated branches, or you need it as
      a non-first argument in many steps — forcing nested `fn` wrappers defeats the
      readability win.
    - When steps are conditional/mutually exclusive (a `case`/`if` is clearer than a
      `with` with awkward patterns).
    - When you're building something where the subject changes type confusingly at each
      step and a temporary variable name aids clarity.

    Fix: use a `with`/`case`/`if`, or bind intermediate results to named variables
    (`user = fetch_user(id)`), or restructure functions so the data naturally lands as
    the first argument. Piping is for linear data transformation; reach for other forms
    when control flow dominates.

- id: elixir-error-01
  answer: |
    Elixir prefers returning `{:ok, value}` / `{:error, reason}` tagged tuples for
    *expected, routine* failures (file not found, validation error, id not found,
    network hiccup) — these are part of normal control flow and callers should handle
    them without crashing. The `!` variants (or raising) are for *exceptional*
    situations that shouldn't normally happen and indicate a bug or truly abnormal
    condition (e.g. a config file required at boot is missing, an invariant violated).

    Rule of thumb: use tagged tuples when failure is a normal, anticipated outcome the
    caller will branch on; raise (or use `!` functions) when the failure is unexpected
    and should propagate as an error up to a supervisor / top-level handler ("let it
    crash"). Never use exceptions for routine branching.

- id: elixir-error-02
  answer: |
    The trailing `!` marks the "bang" / raising variant of a function. `File.read(path)`
    returns `{:ok, contents}` or `{:error, reason}`; `File.read!(path)` returns the
    contents directly but **raises** an exception (`File.Error`) on failure instead of
    returning an error tuple.

    Convention: a non-`!` function gives you a tagged tuple to handle gracefully; the
    `!` twin either succeeds with the unwrapped value or raises. Use `!` when a failure
    is exceptional/unexpected and you'd rather crash than handle it locally, or in
    scripts/setup where aborting is fine. Use the non-bang form when you want to control
    the failure path yourself.

- id: elixir-error-03
  answer: |
    `try/rescue/after` handles exceptions:
    - `try` wraps code that may raise.
    - `rescue` catches **exceptions** (raised values, e.g. `RuntimeError`, `ArgumentError`)
      by pattern matching on the exception struct/class, binding it for inspection.
    - `catch` catches **throws** (`throw value`) and **exits** (`exit reason`) — a
      different mechanism from raises; `rescue` does NOT catch throws/exits.
    - `after` runs whether the block succeeded, raised, or was thrown — it's like a
      `finally`. It's guaranteed to run for cleanup (closing files, releasing
      resources) as long as the process isn't brutally killed.

    So `rescue` = exceptions from `raise`; `catch` = `throw`/`exit`; `after` = always-run
    cleanup.

- id: elixir-error-04
  answer: |
    "Let it crash" means you generally should NOT defensively wrap every operation in
    try/rescue to handle every possible error locally. Instead, let the process fail
    when something unexpected happens, and rely on the Supervisor to restart it (and its
    siblings as the strategy dictates) in a known-good initial state.

    Why this is better: defensive rescuing scatters fragile error handling through the
    code, hides bugs, and leaves the system in partially-corrupted states. Crashing
    isolates the failure to one (cheap, isolated) process, surfaces the real error for
    logging/debugging, and the supervisor restores clean state — turning error handling
    into a coarse-grained, declarative supervision tree rather than fine-grained,
    error-prone guards. Combined with immutability and no shared memory, a crash is
    contained and cheap.

- id: elixir-enum-01
  answer: |
    `Enum` is **eager**: each `Enum` function realizes the collection and performs its
    work immediately, producing an intermediate result (often a new list) at every step.
    Chaining `Enum.map |> Enum.filter |> Enum.count` builds and throws away several
    intermediate lists.

    `Stream` is **lazy**: `Stream` functions return a *composable, enumerable
    description* of the computation that does no work until you run a terminal
    operation (`Enum.to_list`, `Enum.count`, `Enum.each`, `Stream.run`, etc.). Work then
    happens element-by-element, pulling one item through the whole pipeline at a time —
    so you avoid building intermediate collections. `Enum` = work now; `Stream` = work
    later/as-needed.

- id: elixir-enum-02
  answer: |
    1. **Large or infinite collections** — e.g. `Stream.cycle/1`, `Stream.iterate/2`,
       `1..1_000_000 |> Stream.map(...)` — where materializing the whole list in memory
       would be wasteful or impossible. Laziness bounds memory to one element at a time.
    2. **Multi-step pipelines over big data** — where you want to avoid building many
       intermediate lists; composing `Stream.map |> Stream.filter |> Stream.take(n)` and
       only realizing at the end means each element is processed once through the whole
       chain. Also good for file/IO streaming (`File.stream!/3`) where you process lines
       without loading the whole file.

- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` takes a collection, an accumulator initial value, and a function
    `fn element, acc -> new_acc end`. It walks the collection once, feeding each element
    and the running accumulator into the function, and returns the final accumulator.

    It's the fundamental building block because `map`, `filter`, `sum`, `count`,
    `min`/`max`, `flat_map`, `group_by`, etc. are all special cases of folding: map
    reduces by building a list while transforming each element; filter reduces by
    keeping/ dropping into an accumulator; sum reduces by adding. Anything that
    "traverse once and accumulate a result" can be expressed as a reduce, so most other
    `Enum` functions are implemented in terms of `reduce`.

- id: elixir-enum-04
  answer: |
    A comprehension has these parts:
    - **Generators**: `x <- list` produces each element `x` from the enumerable `list`
      (you can have multiple, nested).
    - **Filters**: `rem(x, 2) == 0` is a boolean guard that keeps only elements for which
      it's truthy (items where it's falsy are skipped) — no `do` body runs for them.
    - **`into:` option**: `into: %{}` specifies the "sink" the results are collected
      into; here a map (default is a list).
    - **`do` block**: `do: {x, x * x}` is the expression evaluated for each
      (filtered) generated element; its result is placed into the `into` target.

    So this comprehension yields a map `%{x => x*x}` for every even `x` in `list`
    (e.g. `{2, 4}, {4, 16}, …`), built lazily-friendly and collected into a map.

- id: elixir-proto-01
  answer: |
    A **protocol** is a mechanism for ad-hoc polymorphism: it defines a named set of
    functions that can be implemented for *any* data type, dispatched on the type
    (first argument) of the value at runtime.

    - `defprotocol` declares the protocol and its function signatures, e.g.
      `defprotocol Size do def size(data) end`.
    - `defimpl` provides an implementation for a specific type, e.g.
      `defimpl Size, for: Map do def size(map), do: map_size(map) end`.

    At call time `Size.size(x)` looks up the implementation based on `x`'s type
    (struct/module or built-in type) and invokes the matching `defimpl`. This lets the
    same operation work across unrelated types without them sharing a base class.

- id: elixir-proto-02
  answer: |
    A **behaviour** is a way to define an interface/contract that a module must
    implement — used for callbacks like GenServer, Supervisor, Plug, etc.
    - `@callback` (in the behaviour module) declares a required function signature,
      e.g. `@callback init(term) :: {:ok, term}`.
    - `@behaviour` (in the implementing module) declares that this module adopts the
      named behaviour, so the compiler checks that all `@callback`s are implemented.
    - `@impl` (in the implementing module, above a def) marks which behaviour callback a
      given function satisfies, e.g. `@impl GenServer; def init(_), do: ...`.

    Together: behaviour module defines the contract; implementing module promises to
    fulfill it (`@behaviour`) and tags each implementing function (`@impl`).

- id: elixir-proto-03
  answer: |
    Both look like interfaces but differ fundamentally in *what dispatches them*:
    - **Protocols** dispatch on the **type/data** of the first argument at runtime —
      `Size.size(x)` picks an implementation based on what `x` *is* (a Map, a List, a
      custom struct). You can add implementations for new types *without touching the
      protocol or the original types* (open, extensible, data-oriented).
    - **Behaviours** dispatch on the **module** that was passed / named as the
      implementation — a module *declares* it implements a behaviour, and you call its
      functions directly (e.g. `MyModule.init(arg)`). It's a compile-time contract
      checking that a module provides certain functions; it's about *capability of a
      module*, not the type of a runtime value.

    In short: protocol = runtime polymorphism keyed on value type (open); behaviour =
    compile-time interface a module commits to (closed per module, used for OTP
    callbacks/plugins).

- id: elixir-proto-04
  answer: |
    Annotating an implementation with `@impl true` (or `@impl MyBehaviour`) tells the
    compiler this function is meant to satisfy a behaviour callback. It buys you:
    1. **Verification** — the compiler checks the function's name/arity actually matches
       a `@callback` in the behaviour, catching typos or mismatches.
    2. **Catching drift** — if the behaviour later changes its callbacks (added/removed
       or renamed), the compiler warns/errors that you're missing or have a stale
       implementation, helping you keep implementations in sync.
    3. **Clarity** — it documents, at the call site, that this `def` is a callback
       implementation rather than an ordinary helper.

    `@impl true` means "this implements some behaviour's callback"; `@impl MyBehaviour`
    names which one (useful when a module adopts multiple behaviours).

- id: elixir-conc-01
  answer: |
    The BEAM (Erlang VM) concurrency model runs many lightweight processes (not OS
    threads) scheduled preemptively across a few scheduler threads. Processes are cheap
    (small heap, quick spawn) and communicate only by **message passing** (`send`/
    `receive`) — there is **no shared mutable memory** between them.

    Why no shared memory: each process has its own heap and state; to share data you
    copy messages (or, for large binaries > 64 bytes, reference-counted sharing). This
    matters because it **eliminates data races and locks** — you simply cannot have two
    processes mutating the same location concurrently, so concurrent code is far easier
    to reason about and naturally fault-isolated (one process crashing can't corrupt
    another's state). Combined with immutability, concurrency becomes safe by default.

- id: elixir-conc-02
  answer: |
    `Task.async/1` spawns a process that runs the given function and immediately returns
    a `%Task{}` struct (it does NOT block). `Task.await(task)` blocks the calling
    process until that task finishes, then returns its result. Multiple `async`s
    started before their `await`s run truly concurrently across schedulers.

    Failure semantics:
    - If the task **raises**, `await` re-raises that exception in the calling process.
    - If the task **exits** (e.g. `exit` or a `:normal`/non-normal exit), `await`
      raises an error reflecting the exit.
    - If `await` is not called within the **timeout** (default 5_000 ms), it raises
      `Task.Error` (timeout) and the task is shut down/linked accordingly. Use
      `Task.await(task, timeout)` to customize. (For fire-and-forget without linking
      crashes to the caller, use `Task.start/1` or supervised tasks.)

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple abstraction for a stateful process that holds a single value
    and lets you get/update it via functions passed to `Agent.get`/`Agent.update`
    (running inside the agent process). It's the easiest way to wrap shared mutable-ish
    state.

    Difference from a full GenServer: an Agent *is* a GenServer under the hood but with
    a fixed, limited API — you can only get/put/update a single state value using
    anonymous functions; you can't handle custom message types, `handle_call`/`cast`/
    `info` logic, timeouts, or named client/server separation. For anything beyond
    "store-and-transform-a-value" (custom protocols, side-effecting calls, multiple
    message kinds, fine-grained replies), you write a full GenServer. Agents are
    convenience; GenServer is the general tool.

- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is an in-memory key-value store that lives *outside* any
    single process's heap — it's shared (within a node) and accessed by multiple
    processes, with read/write operations that are very fast and don't go through
    message passing.

    Use ETS instead of a GenServer/Agent to hold shared data when:
    - You need **high concurrency / low latency** reads or writes from many processes
      (a GenServer serializes everything through one process; ETS allows concurrent
      access, especially with `:concurrent_read`/`read_concurrency`/`write_concurrency`).
    - The data is a large table you query by key, and you don't need the sequential
      request/response serialization a process gives you.
    - Trade-off: ETS has no built-in supervision/restart and is not process-isolated
      (a crash elsewhere doesn't clean it up the way a supervised process state would),
      and it doesn't enforce access control — so for simple, owned, supervised state a
      GenServer/Agent is still often the better, safer choice.
