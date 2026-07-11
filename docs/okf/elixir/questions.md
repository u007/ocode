# Elixir Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### elixir-pm-01 · pattern-matching · W3 · medium
**Q:** `=` is the match operator, not assignment — what does `{:ok, value} = fetch()` do, and what if the right side is `{:error, :notfound}`?
**A:** `=` asserts the left pattern matches the right value and binds pattern variables (`value`). It's a match, not a store (`1 = x` checks equality). A shape mismatch raises `MatchError` and binds nothing.
• = matches + binds pattern variables • not assignment (1 = x checks) • failed match raises MatchError ~ "assignment that also destructures", no MatchError

### elixir-pm-02 · pattern-matching · W3 · medium
**Q:** How do multiple function-head clauses plus guards replace conditionals? What decides which runs?
**A:** Same function defined with different arg patterns and optional `when` guards; clauses tried top-to-bottom, first matching pattern+passing guard wins. Guards allow only a restricted whitelist. No match → `FunctionClauseError`.
• first matching clause (pattern+guard) wins, top-to-bottom • guards restricted to allowed expressions • no clause → FunctionClauseError ~ multiple clauses but no ordering/guard detail

### elixir-pm-03 · pattern-matching · W2 · medium
**Q:** What does the pin `^` do, and how does `^x` differ from a bare `x` on the left of a match?
**A:** A bare variable rebinds (`x = 3` succeeds). `^x` matches against `x`'s current value instead of rebinding, so after `x = 1`, `^x = 2` raises `MatchError`.
• bare variable rebinds • ^ matches existing value, no rebind ~ "^ is a constant" without rebinding contrast

### elixir-pm-04 · pattern-matching, immutability-data · W2 · medium
**Q:** Does `%{name: n} = user` require exactly those keys? How does map matching differ from list/tuple?
**A:** Map patterns match a subset (any map with at least `:name`), ignoring extra keys; `%{}` matches every map. Tuple/list patterns match exact shape/size (`[h | t]` = head/tail of non-empty list).
• map = partial/subset match • tuple/list = exact shape (e.g. [h | t]) ~ destructuring but implies maps match all keys

### elixir-otp-01 · processes-otp, concurrency · W2 · medium
**Q:** Describe `spawn`, `send`, `receive`. What does `receive` do when no message matches?
**A:** `spawn` returns a pid; `send` delivers to the mailbox asynchronously (never blocks); `receive` pattern-matches messages out of the mailbox, leaving non-matches queued, and blocks until a match unless there's an `after` clause.
• spawn→pid, send async to mailbox • receive pattern-matches mailbox • blocks when nothing matches (until match/after) ~ says send blocks or receive drops non-matches

### elixir-otp-02 · processes-otp · W3 · medium
**Q:** GenServer — difference between `handle_call` and `handle_cast`, and what does each return?
**A:** `handle_call` is synchronous (caller blocks, default 5s) → `{:reply, reply, state}`. `handle_cast` is async fire-and-forget, no reply → `{:noreply, state}`. Both can `{:stop, ...}`.
• call sync/blocks, cast async fire-and-forget • handle_call → {:reply, reply, state} • handle_cast → {:noreply, state} ~ sync/async right but return shapes wrong

### elixir-otp-03 · processes-otp · W2 · medium
**Q:** What does `handle_info` handle, vs call/cast?
**A:** Out-of-band messages not sent via `GenServer.call/cast` — plain `send`, `{:DOWN, ...}` monitors, node events, `send_after` timers. Returns `{:noreply, state}` like cast.
• handles non-call/cast (out-of-band) mailbox messages • examples: send, :DOWN, timers; returns {:noreply, state} ~ "other messages" with no example/source distinction

### elixir-otp-04 · processes-otp · W3 · hard
**Q:** Name the three Supervisor restart strategies and what each does on a child crash. What is a child spec?
**A:** `:one_for_one` restarts only the crashed child; `:one_for_all` restarts all children; `:rest_for_one` restarts the crashed child + those started after it. A child spec describes how to start/restart a child (`:id`, start MFA, restart type).
• :one_for_one restarts only the crashed child • :one_for_all restarts all • :rest_for_one restarts crashed + later-started • child spec = how to start/restart ~ conflates one_for_all and rest_for_one

### elixir-data-01 · immutability-data · W2 · easy
**Q:** After `List.delete(list, 2)`, what happens to `list`? What does rebinding do?
**A:** Nothing changes — it returns a new `[1, 3]`; data is never mutated in place. Rebinding just repoints the name at a new value; old references still see old data.
• original unchanged, function returns new value • rebinding repoints name, not mutation ~ "it's immutable" without rebinding-vs-mutation

### elixir-data-02 · immutability-data · W2 · medium
**Q:** Compare maps, keyword lists, and structs. When each?
**A:** Map = general key/value, any keys, unique, unordered, fast lookup. Keyword list = ordered list of `{atom, value}`, allows duplicates, used for options/DSLs. Struct = map with fixed compile-time fields + `__struct__` tag (`defstruct`).
• map: general, any keys, unordered • keyword list: ordered {atom,val}, dup keys, options • struct: fixed compile-time fields + __struct__ ~ two of three correct

### elixir-data-03 · immutability-data · W2 · medium
**Q:** `%{map | key: val}` vs `Map.put(map, key, val)` when the key doesn't exist?
**A:** `%{m | k: v}` only updates existing keys — missing key raises `KeyError` (unknown struct field = compile error). `Map.put` inserts or overwrites. Both return a new map.
• %{m | k: v} updates existing only, KeyError if missing • Map.put inserts/overwrites • both return new map ~ says they're equivalent

### elixir-data-04 · immutability-data · W2 · medium
**Q:** How do `put_in`/`update_in`/`get_in` help with nested immutable data, and why better than manual nesting?
**A:** They read/rewrite a value deep by path, returning a new structure updated only along that path (structural sharing for the rest). `update_in` takes a function; `get_in` reads. Beats hand-rebuilding every intermediate map.
• put_in/update_in return new nested structure at a path • update_in takes fn; get_in reads (avoids manual re-nesting) ~ "updates nested maps" with no new-structure point

### elixir-pipe-01 · pipe-with · W2 · easy
**Q:** What does `|>` do, and the one rule about how the value is passed?
**A:** `a |> f(b)` becomes `f(a, b)` — the left value is inserted as the FIRST argument. Flattens nested calls into a top-to-bottom pipeline; hence core APIs take data first.
• x |> f(args) → f(x, args), value is first arg • flattens nested calls into a pipeline ~ "chains functions" but wrong on first-arg insertion

### elixir-pipe-02 · pipe-with, error-handling · W3 · medium
**Q:** What problem does `with` solve, and how does it chain `{:ok, _}` steps?
**A:** Keeps the happy path flat vs nested `case`. Each `<-` clause proceeds while its pattern matches; the first non-matching clause short-circuits and returns that value (unless `else` handles it).
• with chains via <-, flat happy path • proceeds while each matches • first non-match short-circuits, returns value ~ "pipe for ok tuples" without short-circuit

### elixir-pipe-03 · pipe-with, error-handling · W2 · medium
**Q:** In `with`, what happens to a value that fails a `<-` clause, and what's `else` for?
**A:** The non-matching value becomes the `with` result — returned as-is when there's no `else` (so `{:error, _}` flows out). `else` matches those failures to transform them; don't over-stuff it.
• failed <- value returned as with result (as-is without else) • else matches failures to transform ~ implies unmatched values raise

### elixir-pipe-04 · pipe-with · W1 · medium
**Q:** A case where piping is the wrong tool, and the fix?
**A:** Don't pipe a single call, or when the value isn't the first argument. Use `then(x, fn v -> g(a, v) end)` or a named variable to place the value in a non-first position.
• don't pipe single call / when value isn't first arg • use then/2 or a named variable ~ "sometimes less clear" with no fix

### elixir-error-01 · error-handling · W3 · medium
**Q:** When use `{:ok,_}`/`{:error,_}` tuples vs raising?
**A:** Tagged tuples for expected, recoverable outcomes the caller decides about (handled with `case`/`with`). Raise for truly exceptional/unexpected conditions or programmer errors the caller can't sensibly handle.
• tuples for expected/recoverable failures (case/with) • raise for exceptional/programmer errors ~ picks one without the expected-vs-exceptional line

### elixir-error-02 · error-handling · W2 · easy
**Q:** The convention behind trailing `!` — `File.read!` vs `File.read`?
**A:** Non-bang returns `{:ok, v}`/`{:error, r}` for you to handle; bang returns the raw value on success and RAISES on failure. Use bang when you'd rather crash than thread an error.
• non-bang returns tuple, bang returns value or raises • pick bang to crash rather than handle ~ "! can error" without value-vs-raises

### elixir-error-03 · error-handling · W2 · medium
**Q:** Explain `try/rescue/after`; how does `rescue` differ from `catch`? When is `after` guaranteed?
**A:** `rescue` handles raised exceptions (by type); `catch` handles `throw`/`:exit` (a separate mechanism); `after` always runs for cleanup (success or failure). Explicit `try` is rare — prefer tuples or let-it-crash.
• rescue = raised exceptions, catch = throw/exit (different mechanism) • after always runs for cleanup ~ conflates rescue and catch

### elixir-error-04 · error-handling, processes-otp · W3 · medium
**Q:** Explain "let it crash." Why crash over defensively rescuing?
**A:** Isolated, supervised processes crash on unexpected state and get restarted from a clean initial state — self-healing, no corrupt intermediate state, crash isolated to one process. Code the happy path; still use tuples for expected errors.
• crashed process restarted by supervisor from clean state • isolation: one crash doesn't kill the system • avoid defensive rescue; tuples for expected ~ "let it crash" with no supervisor/isolation reason

### elixir-enum-01 · enum-stream · W3 · medium
**Q:** Core difference between `Enum` and `Stream` regarding when work happens?
**A:** `Enum` is eager — computes immediately, returns a concrete result, builds an intermediate list per chained step. `Stream` is lazy — composes the computation and runs only when a terminal `Enum` consumes it, passing elements one at a time with no intermediate lists.
• Enum eager: computes now, concrete result • Stream lazy: composes, runs at terminal Enum • Enum builds intermediate lists, Stream avoids them ~ "Stream is lazy" without intermediate-list/deferred detail

### elixir-enum-02 · enum-stream · W2 · medium
**Q:** Two situations where `Stream` clearly beats `Enum`?
**A:** (1) Large/infinite sources — line-by-line file streaming, `Stream.iterate`/`cycle` — to bound memory. (2) Multi-step pipelines over big data to avoid an intermediate list per step / early termination with `take`. Small or single-pass → plain `Enum`.
• large/infinite sources to bound memory • multi-step big pipelines avoid intermediate lists / early termination ~ one case, or "faster" with no memory/laziness reason

### elixir-enum-03 · enum-stream · W2 · medium
**Q:** What does `Enum.reduce/3` do, and why is it the building block behind map/filter/sum?
**A:** Folds a collection into one accumulated value: threads an updated accumulator through every element from an initial value. Fundamental because map (accumulate list), sum (accumulate number), filter (conditional accumulate) are all special cases of reduce.
• reduce folds to one value via (element, acc) -> acc • threads accumulator through all elements from initial • map/filter/sum are special cases ~ "it sums/combines" without accumulator generality

### elixir-enum-04 · enum-stream · W2 · medium
**Q:** Explain the parts of `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`.
**A:** `x <- list` is a generator (also pattern-matches, skipping non-matches; multiple generators nest). Bare boolean expressions are filters that drop elements. `into:` sets the collectable (default list; here a map); `do:` builds each element.
• x <- list generator (+ pattern filter), multiple nest • bare booleans are filters • into: sets result type (default list), do: builds each ~ "a loop that maps" without generator/filter/into

### elixir-proto-01 · protocols-behaviours · W2 · medium
**Q:** What is a protocol, and how do `defprotocol`/`defimpl` give polymorphism?
**A:** A protocol declares functions dispatched at runtime on the first argument's data type; `defimpl ..., for: List` gives per-type implementations. Open/extensible — new types (incl. third-party structs) can implement it without changing it.
• protocol dispatched at runtime on first arg's data type • defimpl = per-type impl, new types can implement (open) ~ "an interface" without runtime data-type dispatch

### elixir-proto-02 · protocols-behaviours · W2 · medium
**Q:** What is a behaviour, and what do `@callback`, `@behaviour`, `@impl` do?
**A:** A behaviour is a module contract. `@callback name(args) :: ret` declares required functions with specs; `@behaviour X` adopts it (compiler warns on missing callbacks); `@impl X`/`@impl true` marks an implementing function (warns if it's not a callback). GenServer/Supervisor are behaviours.
• behaviour = module contract, @callback declares required fns • @behaviour adopts (warns on missing), @impl marks a callback impl ~ "like an interface" but roles unclear

### elixir-proto-03 · protocols-behaviours · W3 · hard
**Q:** Protocols and behaviours both look like interfaces — the fundamental difference?
**A:** Protocol = runtime polymorphism dispatched on a value's DATA TYPE (one name, many per-type impls). Behaviour = compile-time contract on a MODULE, dispatched by naming the module explicitly (swappable adapters). Data-type polymorphism vs swappable-module API.
• protocol dispatches at runtime on data type • behaviour = compile-time module contract, explicit-module dispatch • data-type-vs-module is the axis ~ "both are interfaces" with only one side

### elixir-proto-04 · protocols-behaviours · W1 · medium
**Q:** Why annotate an implementation with `@impl true` / `@impl MyBehaviour`?
**A:** The compiler verifies the function actually matches a declared `@callback` (warns on typo'd name/arity, or inconsistent use across callbacks). It also documents that the function satisfies a behaviour; `@impl MyBehaviour` records which one.
• compiler verifies it matches a declared @callback (warns on mismatch) • documents intent / must be consistent ~ "it's documentation" without compile-time verification

### elixir-conc-01 · concurrency · W3 · medium
**Q:** Describe the BEAM concurrency model. Why no shared mutable memory, and why does it matter?
**A:** Lightweight VM-scheduled processes, each with its own heap/stack, share nothing, and communicate only by copying messages into mailboxes. No shared mutable state → no data races/locks, crashes can't corrupt another's memory, independent GC — enabling massive concurrency and fault isolation.
• lightweight isolated processes, own heap, share nothing • communicate only by copying messages • consequence: no races/locks + crash isolation ~ "processes send messages" without isolation consequence

### elixir-conc-02 · concurrency · W2 · medium
**Q:** How do `Task.async`/`Task.await` run work concurrently, and what if the task raises or times out?
**A:** `Task.async` spawns a linked, monitored process returning a `%Task{}`; `Task.await(task, timeout)` blocks for its result — so you can fan out several and await in parallel. Linked failure propagates to the caller; `await` exits on timeout (default 5000ms). Use `Task.Supervisor` for unlinked/supervised work.
• async spawns concurrent (linked) process→Task; await blocks for result • can run several in parallel • linked failure propagates; await timeout (5s) ~ "runs in background" without await/linking/timeout

### elixir-conc-03 · concurrency, processes-otp · W1 · medium
**Q:** What is an `Agent`, and how does it differ from a full `GenServer`?
**A:** A thin process wrapper holding state: `Agent.start_link(fn -> init end)` then `Agent.get`/`Agent.update` with functions. Use for simple shared, concurrent-safe state; reach for GenServer when you need custom call/cast/info handling or lifecycle logic.
• Agent = process holding state via get/update fns • simple shared state vs GenServer for custom message handling ~ "stores state" without GenServer contrast

### elixir-conc-04 · concurrency · W2 · medium
**Q:** What is ETS, and when use it instead of a process to hold shared data?
**A:** In-memory key/value table owned by a process but read/written directly by others, no message-passing through the owner. Scales for high-concurrency, read-heavy shared data (caches, counters) where a single GenServer bottlenecks. Caveats: outside the immutable world, dies with owner, concurrent writes need care.
• ETS = in-memory table read/written directly by many processes • use for high-concurrency/read-heavy where a GenServer bottlenecks ~ "a fast store" without the bottleneck reason
