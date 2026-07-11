# elixir knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: elixir-pm-01
  answer: |
    `=` is the match operator, so `{:ok, value} = fetch()` evaluates `fetch()`, then pattern-matches the resulting term against the left-hand pattern. If `fetch()` returns `{:ok, value}`, the atom `:ok` matches the literal `:ok` and `value` is bound to the second element of the tuple. If `fetch()` returns `{:error, :notfound}`, the match fails: the tuple's first element is the atom `:error`, which does not match the literal `:ok`, so Elixir raises a `MatchError`. Nothing is bound; the pattern simply does not match. (Note: the `value` on the left here is a new variable binding, not to be confused with any pre-existing `value`.)

- id: elixir-pm-02
  answer: |
    Instead of if/else or case trees, you define several clauses of the same function with different argument patterns, optionally followed by `when` guards (boolean expressions). At call time the runtime evaluates the clauses top to bottom: it picks the first clause whose argument pattern matches the actual arguments AND whose guard (if any) evaluates to true. The matching clause's body runs; the rest are not considered. So "which clause runs" is decided by (1) structural pattern match on the arguments and (2) guard evaluation, in declaration order.

- id: elixir-pm-03
  answer: |
    The pin operator `^` forces a variable on the left of a match to be treated as a literal value to match against, i.e. it asserts equality with the variable's *current* value rather than rebinding it. `^x = expr` means "the value of `expr` must equal the existing value of `x`." A bare `x` on the left, by contrast, is a new variable that will be (re)bound to whatever the right side matches — it does not compare against any prior value. So `^x` prevents rebinding and instead checks, while bare `x` captures/binds.

- id: elixir-pm-04
  answer: |
    Map patterns match a *subset* of keys, not the exact structure. `%{name: n} = user` only requires that `user` is a map containing the key `:name`; any additional keys are allowed and ignored, and `n` is bound to that key's value. This differs from tuples (`{a, b}` requires exactly that length and positions) and lists (`[a, b]` requires exactly those elements / `[head | tail]` requires non-empty). Maps are the only of the three that match by "has these keys" rather than "is exactly this shape." (There is a special `=%{}`/struct form `%Struct{...}` that can require more specificity, but a plain `%{}` map pattern is a subset match.)

- id: elixir-otp-01
  answer: |
    These are the raw BEAM concurrency primitives. `spawn(mod, fun, args)` (or `spawn(fn -> ... end)`) creates a new, isolated lightweight process running that function and returns its PID. `send(pid, message)` asynchronously delivers a message (any term) to that process's mailbox. `receive` blocks the current process and scans its mailbox for the first message matching one of its patterns (in order); when found it runs that clause and removes the message. If no message matches, `receive` blocks indefinitely waiting; you can add an `after` timeout clause that runs if no match arrives within the given time.

- id: elixir-otp-02
  answer: |
    `handle_call` handles *synchronous* requests made via `GenServer.call/2/3`: the caller blocks until a reply is returned. Its callback returns `{:reply, reply, new_state}` (or `{:reply, ...}` variants, or stop tuples). `handle_cast` handles *asynchronous*, fire-and-forget messages sent via `GenServer.cast/2`: the caller does not wait for a reply. Its callback returns `{:noreply, new_state}` (or stop tuples). In short, call = request/response with reply; cast = one-way, no reply.

- id: elixir-otp-03
  answer: |
    `handle_info` handles *all other* messages a GenServer receives that are not `call` or `cast` requests — i.e. messages sent with plain `send/2`, `Process.monitor` `:DOWN` notifications, `Process.exit/2` exit signals, `:timeout` messages, and similar. `handle_call`/`handle_cast` only fire for the specially wrapped call/cast messages; anything else falls through to `handle_info`. Like `handle_cast`, `handle_info` returns `{:noreply, new_state}` (or stop tuples) since it is asynchronous.

- id: elixir-otp-04
  answer: |
    The three main supervisor restart strategies:
    - `:one_for_one` — restart only the crashed child; others are unaffected.
    - `:one_for_all` — restart *all* children whenever any one crashes.
    - `:rest_for_one` — restart the crashed child and every child started *after* it (those that may depend on it), leaving earlier children alone.
    A child spec (child specification) is the data structure (map or via a module's `child_spec/1`) that tells the supervisor how to start, restart, and shut down a child — including `:id`, `:start`, `:restart` (e.g. `:permanent`, `:temporary`, `:transient`), `:shutdown`, and `:type`.

- id: elixir-data-01
  answer: |
    Data is immutable, so `List.delete(list, 2)` returns a *new* list `[1, 3]`; the original `list` variable still points at `[1, 2, 3]` and is unchanged. Rebinding a variable (e.g. `list = [1, 3]`) simply makes the name refer to a different value — it does not mutate the prior value, which remains intact as long as something references it (otherwise it becomes garbage). Variables are bindings/names, not mutable storage cells.

- id: elixir-data-02
  answer: |
    - Maps (`%{}`): the general-purpose key/value structure with any key type (atoms, strings, etc.); used for most dynamic/arbitrary data.
    - Keyword lists (`[{:key, val}, ...]`): lists of two-tuples whose keys are atoms; they preserve order, allow duplicate keys, and are used primarily for optional arguments / options passed to functions.
    - Structs: maps with a fixed set of keys plus a `__struct__` tag, defined via `defstruct`; they give compile-time guarantees (unknown keys error), defaults, and clear domain "shapes." Use structs for well-defined domain records, maps for flexible/ad-hoc data, and keyword lists for ordered, repeatable options.

- id: elixir-data-03
  answer: |
    `%{map | key: val}` is the *update* syntax and requires that `key` already exists in the map — if it does not, it raises a `KeyError`. `Map.put(map, key, val)` works whether or not the key exists: it updates the value if present and adds the key if absent, always returning a new map. So the former is strictly for changing existing keys; the latter can also insert new ones.

- id: elixir-data-04
  answer: |
    `get_in`, `put_in`, and `update_in` (plus `get_and_update_in`) traverse a nested immutable structure via a path (keys/indices, or custom `Access` implementations) and return either a value or a new copy with the nested change applied, handling all the intermediate copy-on-write automatically. This is far better than manual nesting (e.g. `%{m | a: %{Map.get(m.a, :b) | c: ...}}`) because it is concise, less error-prone, works deeply without repeating the whole structure, and gracefully handles dynamic paths and missing keys via `Access`.

- id: elixir-pipe-01
  answer: |
    `|>` (the pipe) takes the result of the expression on its left and feeds it as the *first argument* to the function call on its right. The one rule: the piped value is always inserted as the first argument of the next function. (If you need it elsewhere you use `&` captures, `then/2`, or restructure.) It lets you write left-to-right data transformations instead of deeply nested calls.

- id: elixir-pipe-02
  answer: |
    `with` solves the "chained happy-path" problem where several steps each may return a `{:ok, _}` (or otherwise matchable) success value but fail otherwise — replacing nested `case` statements. Each clause uses `<-` (match) or `=` (bind) and the expression is evaluated in order; the bound variables become available to later clauses and the `do` block. As soon as every clause matches, the `do` block runs with all bindings; if all succeed it returns the do-block result. It reads as a clean linear sequence of steps.

- id: elixir-pipe-03
  answer: |
    In a `with`, if a `<-` clause's pattern does not match, the *entire unmatched value* is immediately returned from the `with` expression and the `do` block is skipped (no further clauses run). The optional `else` block then receives that non-matching value and lets you pattern-match/handle the failure (e.g. mapping `{:error, reason}` to something). Without an `else`, the raw unmatched value just propagates out.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the intermediate result is needed in more than one place, or when the data must be the *second or later* argument of the next function, or when steps are not a simple linear transform (e.g. branching, or combining several independent values). Example: `user |> Repo.insert()` is fine, but `Enum.map(list, &String.upcase/1)` can't be piped naturally because `list` is the first arg but you also supply a fun — actually that's fine; a worse case is when you need `a` and `b` together like `Enum.join(a, b)` where neither is naturally "the first." Fix: use `then/2` to wrap, use `&` captures (`&Foo.bar(&1, other)`), keep the awkward call outside the pipeline, or restructure so the threaded value lands as the first argument.

- id: elixir-error-01
  answer: |
    Use `{:ok, value}` / `{:error, reason}` tagged tuples for *expected, routine* failure modes that callers should handle (e.g. file not found, validation failure, lookup miss, already-exists) — these are part of normal control flow and you want the caller to decide. Raise exceptions for *unexpected, exceptional, or programming* errors (bugs, invariants violated, unrecoverable states) where there is no sensible local recovery and the failure should unwind. The rule of thumb: tuples for "expected errors," exceptions for "shouldn't happen / truly exceptional."

- id: elixir-error-02
  answer: |
    A trailing `!` denotes the "bang" variant that raises an exception on failure instead of returning a tagged tuple. So `File.read(path)` returns `{:ok, contents}` or `{:error, reason}`, whereas `File.read!(path)` returns the contents directly on success but raises (e.g. `File.Error`) on failure. Same convention across the stdlib: the non-bang form is for when you want to handle errors gracefully; the bang form is for when an error should be fatal / you'd rather crash than handle it.

- id: elixir-error-03
  answer: |
    `try` wraps code that may raise; `rescue` catches *exceptions* (things raised with `raise`), letting you pattern-match on the exception type; `catch` catches *thrown* values (`throw`) and exit signals (`exit`), which are different mechanisms; `after` runs regardless of whether the block succeeded, raised, or was caught — it's the "finally" clause. `after` is guaranteed to run for normal and rescued paths (it's the cleanup block), though not in cases like the whole VM terminating. The key distinction: `rescue` is for exceptions, `catch` is for throws/exits.

- id: elixir-error-04
  answer: |
    "Let it crash" means: don't wrap every operation in defensive error handling; instead allow a process to fail when something unexpected happens, and rely on a Supervisor to detect the crash and restart the process (or its siblings) in a known-good state. This is better than defensively rescuing everywhere because (a) processes are isolated so a crash is contained and can't corrupt others, (b) supervision gives clean, uniform recovery, (c) defensive code often hides bugs and accumulates complexity, and (d) you only write error handling for errors you can actually recover from. You handle the *expected* errors with tuples and let the *unexpected* ones crash.

- id: elixir-enum-01
  answer: |
    `Enum` is *eager*: each function immediately iterates the collection and produces a fully realized result (often a new list), so a chain of `Enum` calls builds intermediate collections at every step. `Stream` is *lazy*: `Stream.map/2` etc. return a composable, enumerable pipeline that does no work until it is consumed (e.g. by `Enum.to_list`, `Enum.count`, or `Enum.each`); the operations then run in a single pass. So the difference is *when* the work happens — eagerly now vs. lazily on demand.

- id: elixir-enum-02
  answer: |
    Two clear cases for `Stream` over `Enum`:
    1. Large or infinite data — e.g. `Stream.iterate/2`, `Stream.cycle/1`, or reading a huge file line-by-line, where building an intermediate list in memory would be impossible or wasteful.
    2. Composing many transformations over a big collection — using `Stream` lets you fuse the steps into one pass and avoid allocating a new collection at each stage (e.g. `stream |> Stream.map(...) |> Stream.filter(...) |> Enum.count()` only traverses once).

- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` walks a collection, applying a function `(accumulator, element) -> new_accumulator` to each element starting from an initial accumulator, and returns the final accumulator. It is the "fundamental building block" because `map`, `filter`, `sum`, `count`, `flat_map`, etc. can all be expressed as specific reduces: `map` reduces by consing transformed elements, `filter` reduces by conditionally consing, `sum` reduces by adding. Many higher-level `Enum` functions are implemented in terms of reduce.

- id: elixir-enum-04
  answer: |
    Breaking it down: `x <- list` is the *generator* — it binds `x` to each element of `list` in turn. `rem(x, 2) == 0` is a *filter* — only elements where this is true proceed. `do: {x, x * x}` is the *collect expression* — for each surviving `x`, produce the tuple `{x, x*x}` as an item. `into: %{}` sends the produced items into a map, so each tuple `{k, v}` becomes a map entry. Net effect: a map of every even number in `list` mapped to its square.

- id: elixir-proto-01
  answer: |
    A protocol is Elixir's mechanism for *ad-hoc polymorphism*: it defines a named set of functions that can be implemented differently for different data types. `defprotocol` declares the function signatures (and the type is always dispatched on the first argument). `defimpl` provides a concrete implementation for a specific type (e.g. `defimpl MyProto, for: Map`). At runtime, calling the protocol function dispatches to the implementation matching the actual type of the first argument — letting the same operation behave per-type (like `Enumerable`, `Collectable`, `String.Chars` in stdlib).

- id: elixir-proto-02
  answer: |
    A behaviour is a *module-level contract* — a set of function signatures that a module promises to implement (used for callbacks like GenServer, Plug, etc.). `@callback` declares a required function signature within the behaviour module. `@behaviour` (in the implementing module) declares that this module implements that behaviour. `@impl` (on the implementing function) marks that the function is an implementation of a behaviour callback. Together they enforce and document that a module provides the expected functions.

- id: elixir-proto-03
  answer: |
    Both look like interfaces but differ fundamentally in *what* they dispatch on. A protocol dispatches on the *data type* (runtime value) of its first argument — it's about making an operation work across many existing and user-defined types (value-based, ad-hoc polymorphism). A behaviour is about a *module* promising to implement a set of functions — it's a pluggable-module / dependency-inversion contract, with dispatch by the named module (e.g. you pass a module to be used as a callback handler). Behaviours don't care about the data type flowing through; protocols don't care which module implements them.

- id: elixir-proto-04
  answer: |
    Annotating with `@impl true` (or `@impl MyBehaviour`) documents that the function is a behaviour callback and, more importantly, lets the compiler verify it: it checks that the function actually corresponds to a `@callback` in the declared behaviour (correct name and arity), warning if you've got the signature wrong or if the behaviour doesn't define such a callback. It also protects against accidentally overriding a function when a behaviour changes. It buys compile-time safety and clearer intent.

- id: elixir-conc-01
  answer: |
    The BEAM concurrency model runs many *lightweight* processes (not OS threads) — thousands to millions — scheduled cooperatively by the BEAM VM across available schedulers/cores. Each process has its own isolated heap and stack, and there is *no shared mutable memory*: processes communicate only by copying messages through mailboxes (`send`/`receive`). This isolation matters because (a) there are no locks or race conditions on shared state, (b) a crash in one process cannot corrupt another, (c) failures are contained and supervised, and (d) the model scales predictably across cores and nodes.

- id: elixir-conc-02
  answer: |
    `Task.async/1` spawns a process that runs the given function and returns a `%Task{}` handle immediately; `Task.await/2` (default timeout 5000ms) blocks the calling process until that task finishes and returns its result. They run concurrently with the caller and each other. If the task raises, `Task.await` re-raises that same error in the caller. If the task does not finish within the timeout, `await` exits the task and raises a timeout error (e.g. `Task.Error`). So concurrency plus error/timeout propagation back to the awaiter.

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple state-holder abstraction built on top of a GenServer: it keeps a single value as its state and exposes `Agent.get`, `Agent.update`, `Agent.get_and_update`, etc., where you pass anonymous functions describing how to read/transform the state. Compared to writing a full GenServer, you don't define `handle_call`/`handle_cast`/`handle_info` callbacks or manage the state tuple yourself — the Agent handles the message protocol for you. It's far less boilerplate for "a process holding one piece of state," at the cost of less control over message handling and the serialization overhead of shipping functions to the agent.

- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is an in-memory table store that lives outside any single process; multiple processes can read/write it concurrently *without message passing* (and without copying through a mailbox). You'd use it instead of a GenServer/Agent when you need very fast, high-throughput access to a large shared dataset and want to avoid the bottleneck of serializing every operation through one process's mailbox — e.g. caches, session tables, large lookup tables. Caveats: ETS tables are not garbage-collected with a process (must be explicitly deleted or owned with `heir`/`destroy`), there's no transactional isolation beyond what you build, and writes are shared mutable state, so you lose process isolation guarantees.
```
