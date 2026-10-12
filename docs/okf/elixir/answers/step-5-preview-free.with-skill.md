# step-5-preview-free — WITH-SKILL validation answers for elixir (opencode-go, 2026-10-12); closed-book,
# derived/elixir.step-5-preview-free.SKILL.md body prepended to _prompts/elixir.md as active guidance.
- id: elixir-pm-01
  answer: |
    `=` is not assignment but the MATCH operator: the right-hand value is tested
    against the left-hand pattern. In `{:ok, value} = fetch()` the LHS is a
    tuple pattern whose first element is the literal atom `:ok` and whose second
    element is an unbound variable `value` (a pattern placeholder).

    If `fetch()` returns `{:ok, 42}`, the match succeeds: `:ok` matches the
    literal `:ok`, and `value` binds to `42`. Either the whole match succeeds
    with all bindings made, or it raises — there is no partial binding.

    If `fetch()` returns `{:error, :notfound}`, the shapes don't line up:
    element 0 of the RHS is `:error`, which does not match the literal `:ok`.
    The match fails and Elixir raises `** (MatchError)` ("no match of right hand
    side value"). This is the default behaviour — if you don't want the crash
    you must write a `case`/multi-clause function that handles `:error`
    explicitly, or guard with a prior check. Name the error: `MatchError`, not
    an error return.
- id: elixir-pm-02
  answer: |
    Multi-clause functions replace `case`/`if` by moving the branching into the
    function head. Elixir tries clauses TOP TO BOTTOM; the first clause whose
    pattern matches AND whose guard (if any) evaluates to true runs:

    ```elixir
    def grade(n) when n >= 90, do: :a
    def grade(n) when n >= 80, do: :b
    def grade(n) when is_integer(n), do: :f
    ```

    So order matters — a broad early clause shadows later ones.

    Three things the guidance makes precise:

    1. **A guard is a whitelist, not just "side-effect free".** `when` accepts
       only guard-safe expressions: comparisons, boolean/arithmetic operators,
       type checks (`is_integer/1`, `is_binary/1`, …), `in` with a literal list
       or range, and a few Kernel functions (`length/1`, `map_size/1`,
       `tuple_size/1`, `elem/2`, `hd/1`, `tl/1`, `rem/2`, `div/2`, `abs/1`,
       `byte_size/1`). Your own functions are NOT allowed even if pure —
       purity is irrelevant; whitelist membership is. Calling `my_check(x)` in
       `when` is a compile error. Reusable guards are built with
       `defguard`/`defguardp`.
    2. **An error inside a guard makes that guard FAIL** (the clause is
       skipped); it does not crash the call.
    3. **If no clause's pattern+guard passes, the call raises
       `FunctionClauseError`** ("no function clause matching in …"). There is no
       silent `nil`. Add a catch-all (`def grade(_)`) if you want a default, or
       let it crash deliberately — but name the error. Note also: a clause
       placed AFTER a catch-all is unreachable and the compiler emits a warning
       ("this clause cannot match because a previous clause always matches").
- id: elixir-pm-03
  answer: |
    `^` (pin) means: "match against the CURRENT VALUE already bound to this
    variable, don't rebind it". A pinned variable must already be bound.

    The difference from a bare `x` on the left of `=`:

    - Bare `x` (unbound) binds; bare `x` (already bound) REBINDS — Elixir
      permits rebinding an existing name, so `x = 1; x = 2` succeeds.
    - `^x` pins `x`: it matches only if the incoming value equals the existing
      binding. `x = 1; ^x = 2` raises `MatchError`.

    ```elixir
    {a, b} = {1, 2}
    {^a, b} = {1, 3}   # ok: a is 1, b REBINDS to 3
    {^a, b} = {9, 3}   # ** (MatchError)
    ```

    Pinning is also the way to match against a value stored in a variable when
    the variable appears as a MAP KEY: `%{^key => value} = map` matches the key
    equal to the current `key` value.
- id: elixir-pm-04
  answer: |
    Map patterns match a SUBSET of keys. `%{name: n} = user` succeeds as long
    as `user` is a map that contains the `:name` key — extra keys are fine —
    and `n` binds to the value at `:name`. It raises `MatchError` only when the
    key is absent or the RHS is not a map.

    Lists and tuples match the FULL structure: a tuple's size must match
    element-for-element (`{a, b} = {1, 2, 3}` fails), and list patterns match
    specific head/tail shape (`[a, b] = [1]` fails; `[h | t] = []` fails).
    Shortcuts like `[:ok, "v" | rest]` are structural, not key-subset.

    Don't confuse this with the UPDATE syntax `%{map | key: val}` — that is a
    different operation: it requires `key` to already exist and raises
    `KeyError` at RUNTIME if not (there is no compile-time key check for plain
    maps; structs are the exception).
- id: elixir-otp-01
  answer: |
    Raw primitives (below any OTP framework):

    - `spawn(fun)` starts a NEW lightweight process running `fun` and returns
      its PID. Processes are cheap (not OS threads) and isolated: each has its
      own heap, mailbox, and GC.
    - `send(pid, msg)` puts `msg` in `pid`'s mailbox (async, returns the msg).
    - `receive do pattern -> ... end` waits for the process's own mailbox and
      consumes the FIRST message, in mailbox (arrival) order, that matches any
      clause, extracting bindings.

    `receive` is SELECTIVE: messages that don't match any clause are skipped
    over and REMAIN in the mailbox — they are not discarded and not an error.

    If NO message in the mailbox matches, the process BLOCKS (suspends) until
    one arrives. It does not time out, error, or return nil by default. If you
    need a bound, add an `after` clause (`receive do ... after 5_000 -> :timeout end`).
- id: elixir-otp-02
  answer: |
    Both are request message handlers; the difference is SYNCHRONY and what the
    client gets back.

    - `handle_call(msg, from, state)` — synchronous RPC. The client is blocked
      (with timeout) in `GenServer.call/3` and expects a REQUEST-REPLY.
      Returns usually `{:reply, reply, new_state}`. Alternatives:
      `{:noreply, state}` (reply later via `GenServer.reply(from, value)`),
      `{:stop, reason, reply, new_state}` (stops the server).
    - `handle_cast(msg, state)` — asynchronous, fire-and-forget. The client
      `GenServer.cast/2` returns immediately; there is no reply to send. It
      must return `{:noreply, new_state}` (or `{:stop, reason, new_state}`).

    So the main contract difference: `handle_call` owes the caller a reply and
    may name that reply or defer it; `handle_cast` owes the caller nothing.
- id: elixir-otp-03
  answer: |
    `handle_call/3` and `handle_cast/2` only receive messages sent through the
    GenServer API (`GenServer.call/3` and `GenServer.cast/2`).

    `handle_info/2` is the catch-all for everything ELSE that lands in the
    process mailbox:

    - raw `send(pid, msg)` from any code that knows the PID,
    - lifecycle/system messages — `{:DOWN, ref, :process, pid, reason}`
      (monitor notifications), `{:EXIT, ...}` exit signals from linked/trapped
      exits, `{:nodedown, ...}`,
    - `:timeout` / ticker messages,
    - stale replies arriving without a matching call.

    Return shapes mirror the others: `{:noreply, new_state}` most commonly,
    `{:stop, reason, new_state}` to terminate. In short: call/cast are the
    public typed API; handle_info handles the untyped world, which is exactly
    why it's where monitor and cleanup messages are processed.
- id: elixir-otp-04
  answer: |
    The three restart strategies (what happens when ONE child crashes):

    - `:one_for_one` (default) — restart only the crashed child; siblings are
      untouched.
    - `:one_for_all` — terminate ALL children, then restart all of them; used
      when children depend on each other's state.
    - `:rest_for_one` — terminate the crashed child plus every child started
      AFTER it, then restart that subset.

    A child spec is the per-child START/RESTART specification the supervisor
    uses to build its children. As a map (or legacy tuple) it typically carries:
    an `:id`, the start procedure (`:start` — an MFA like `{MyWorker, :start_link, [arg]}`),
    the `:restart` strategy — `:permanent` (always restarted), `:transient`
    (restarted only on abnormal exit), `:temporary` (never restarted) — the
    `:shutdown` behaviour/timeout (`:brutal_kill`, an integer ms, or
    `:infinity`), and the `:type` (`:worker` vs `:supervisor`). The supervisor
    honors the `max_restarts`/`max_seconds` intensity limit, after which the
    supervisor itself shuts down.
- id: elixir-data-01
  answer: |
    `list` is UNCHANGED — it is still `[1, 2, 3]`. `List.delete(list, 2)`
    returns a NEW list `[1, 3]`; if you care about the result you must rebind
    (`list = List.delete(list, 2)`). Nothing in the BEAM mutates the old list;
    it lives as long as any other term still references it.

    Rebinding a variable does NOT overwrite memory. Variables are immutable
    LABELS (pointers) to values; rebinding attaches the label to a different
    value. The old value stays intact for anyone else holding it:

    ```elixir
    a = [1, 2, 3]
    b = a                       # b points at the same value
    a = [1, 3]                  # a now labels a new list
    b                           # => [1, 2, 3]  (untouched)
    ```

    This is what "immutable data" means in practice: sharing is safe (no
    defensive copying), and once a value is unreferenced it becomes garbage
    for the process's GC to reclaim.
- id: elixir-data-02
  answer: |
    - **Maps** — the general-purpose key/value store: any key type, no fixed
      shape, unordered. The default container for keyed data (`%{id: 1, name: "x"}`),
      with O(log n) access. Use for ad-hoc, dynamically shaped data.
    - **Keyword lists** — lists of 2-tuples, conventionally `{atom, value}`
      (`[name: "x", timeout: 5]` is sugar for `[{:name, "x"}, {:timeout, 5}]`).
      Two distinguishing properties vs maps: ORDER is preserved and DUPLICATE
      keys are allowed. That's why they're the idiom for FUNCTION OPTIONS
      (`File.read(path, warn: true)`), config-ish key-value data, and
      DSL-style ordered arguments. Lookup is O(n) and you can't pattern-match
      them by key. Since Elixir 1.13+ 1.13 keyword lists are as fast as maps
      for small sizes but still no key-based pattern.
    - **Structs** — compile-time maps with a FIXED set of keys, a `__struct__`
      key, and per-field defaults:
      ```elixir
      defmodule User do
        defstruct [:name, role: :guest]
      end
      %User{name: "Ann"}   # %User{name: "Ann", role: :guest}
      ```
      Use structs for DOMAIN ENTITIES where the shape is known and should stay
      fixed — `%User{}` in a struct pattern only matches Users, and
      `%User{u | role: :admin}` fails TO COMPILE if the key isn't a field
      (typos are caught early). Maps for flexible data; keyword lists for
      ordered options; structs for known-shape entities.
- id: elixir-data-03
  answer: |
    They differ exactly on the missing-key case:

    - `%{map | key: val}` UPDATES ONLY — `key` must already exist. If it
      doesn't, it raises `KeyError` AT RUNTIME, whether the key is a literal
      or a variable:
      ```elixir
      m = %{a: 1}
      %{m | a: 2}   # => %{a: 2}
      %{m | b: 2}   # ** (KeyError) key :b not found in: %{a: 1}
      ```
      There is NO compile-time key check for plain maps — the compiler can't
      know what keys a map will hold at runtime. The compile-time check exists
      ONLY for structs (`%User{u | nmae: x}` fails to compile because the
      struct's fields are known).
    - `Map.put(map, key, val)` inserts OR overwrites and never raises:
      `Map.put(m, :b, 2) # => %{a: 1, b: 2}`.

    Both return a NEW map; neither mutates the input.
- id: elixir-data-04
  answer: |
    They navigate nested immutable structures via a PATH instead of forcing you
    to rewrite every level by hand. `get_in/2` is the safe read; `put_in/2,3`
    and `update_in/2,3` rebuild the intermediate maps for you.

    Exact failure behaviour (the part that's commonly wrong):

    - `get_in(data, path)` returns `nil` when ANY step of the path is missing
      — it never raises on a nil intermediate.
    - `put_in(data, path, v)` and `update_in(data, path, fun)` do NOT tolerate
      a missing intermediate: when a step resolves to `nil` they raise
      `ArgumentError` ("could not put/update key … on a nil value"). Only the
      LAST key may be absent — `put_in` adds it, and `update_in` passes `nil`
      into your function. Never say `update_in` "returns the current value"
      when the path is missing.
    - To CREATE intermediates on the fly, use `Access.key/2` with a default:
      `put_in(data, [Access.key(:user, %{}), Access.key(:address, %{}), :city], "KL")`.

    ```elixir
    data = %{user: %{name: "Ann"}}
    get_in(data, [:user, :city])                     # nil
    put_in(data, [:user, :city], "KL")               # %{user: %{name: "Ann", city: "KL"}}
    update_in(data, [:user, :name], &String.upcase/1) # %{user: %{name: "ANN"}}
    put_in(%{}, [:user, :city], "KL")                # ** (ArgumentError)
    put_in(%{}, [Access.key(:user, %{}), :city], "KL") # %{user: %{city: "KL"}}
    ```

    Better than manual nesting because you name only ONE path rather than
    re-stating every level from the root:
    `%{data | user: %{data.user | address: %{data.user.address | city: "KL"}}}`
    — verbose, repeated, and grows multiplicatively with depth and change.
    Two more notes: there is no `fetch_in`; the macro forms take a dotted path
    (`put_in(data.user.city, "KL")`) which is STRICT field access (raises
    KeyError on a missing key), while the function forms take a path list and
    use `Access`. Bindings inside update syntax
    (`%{user = data.user | ...}`) are not valid Elixir.
- id: elixir-pipe-01
  answer: |
    `x |> f(args)` rewrites to `f(x, args)` — it evaluates the right-hand call
    with the LEFT-HAND value passed in as the FIRST argument. It's pure
    syntactic sugar that removes nesting and makes data flow read left to
    right:

    ```elixir
    "a,b,c" |> String.split(",") |> Enum.map(&String.upcase/1)
    # == Enum.map(String.split("a,b,c", ","), &String.upcase/1)
    ```

    THE ONE RULE: the piped value always lands in the FIRST positional
    parameter of the next call. So chaining works only when each next function
    takes the previous result as its first argument. To break the rule you
    need `then/2` or capture syntax (`value |> fun.(other)`).
- id: elixir-pipe-02
  answer: |
    A chain of tagged-tuple steps written with nested `case`s telescopes into
    a right-drifting pyramid:

    ```elixir
    case fetch_user(id) do
      {:ok, user} ->
        case fetch_account(user) do
          {:ok, acct} -> get_balance(acct)
          err -> err
        end
      err -> err
    end
    ```

    `with` solves the same thing FLAT: each `pattern <- expr` clause must match
    or the whole `with` short-circuits and returns the NON-MATCHING value.
    `{:ok, x}` on the left unwraps; `{:error, r}` on the right aborts:

    ```elixir
    with {:ok, user} <- fetch_user(id),
         {:ok, acct} <- fetch_account(user),
         {:ok, bal}  <- get_balance(acct) do
      {:ok, bal}
    end
    ```

    If any step returns `{:error, reason}`, that value IS the result of the
    whole expression — returned as data, not raised. All three matches pass ->
    the `do` block runs. `<-` accepts any pattern (`{:ok, x}`, bare variables,
    `%User{} = struct`), and the last clause's value can also be the result by
    mixing in a final plain `= expr` binding.
- id: elixir-pipe-03
  answer: |
    If a value fails to match a `<-` clause, the whole `with` SHORT-CIRCUITS
    and RETURNS that non-matching value VERBATIM as the expression's value.
    It's not an error and not nil — e.g. `{:error, :notfound}` comes straight
    back out of the `with`.

    The optional `else` block is for MAPPING those mismatch results into
    something more useful — grouping, normalising, or matching several failure
    shapes:

    ```elixir
    with {:ok, user} <- fetch_user(id) do
      {:ok, render(user)}
    else
      {:error, :notfound} -> {:error, 404, "no such user"}
      {:error, :timeout}  -> {:error, 503, "upstream slow"}
      other -> other
    end
    ```

    Two precise consequences: WITHOUT `else` every mismatch passes through
    unchanged (so you must match the shape you returned); WITH `else` the
    unmatched value is matched against the else clauses IN ORDER, and if none
    matches, the `with` raises `WithClauseError`. So `else` always needs a
    catch-all or it trades a pass-through for a crash.
- id: elixir-pipe-04
  answer: |
    Two cases where piping reads badly and how to fix them:

    1. **The piped value isn't the first argument of the next call.** Piping
       silently puts the value in position 1, which can be flatly WRONG:

       ```elixir
       "hello" |> File.write!("report.txt")
       # File.write!(path, contents): "hello" becomes the PATH -> File.write!("hello", "report.txt")
       ```

       Fix: don't pipe — write `File.write!("report.txt", "hello")` directly,
       or use `then/2` (`"hello" |> then(&File.write!("report.txt", &1))`).

    2. **You need the original value again later in the chain.** A pipe passes
       along a single evolving value, so once you've piped it into a derived
       value, the original is gone:

       ```elixir
       user |> fetch_address() |> format()
       # now you need user.postal_code too — can't pipe it back out
       ```

       Fix: keep named intermediates (`addr = fetch_address(user)`; loop/
       `Enum.reduce`; or pipe only the branch that flows linearly and compute
       the rest around it).

    A related reading problem: very long single-expression chains hide which
    step failed in the runtime error and make stepping/printing awkward —
    bind intermediate results instead of chasing stacktraces through a
    10-stage pipe.
- id: elixir-error-01
  answer: |
    Use tagged tuples for EXPECTED, RECOVERABLE failures — outcomes that are
    part of the function's contract and that a caller is meant to handle:
    validation failures, `{:error, :not_found}`, network unavailability,
    parse failures, "no such record". They are explicit in the return type,
    cost nothing, force the caller to confront the error (the compiler warns
    on unused match results), and never unwind the stack:

    ```elixir
    def parse(s), do: if valid?(s), do: {:ok, transform(s)}, else: {:error, :bad_format}
    ```

    Use `raise` for PROGRAMMER ERRORS and broken invariants — states that mean
    the code is wrong, not that the user passed bad data:
    `raise ArgumentError, "expected positive, got #{n}"`, internal assertion
    failures, calling an API with an impossible state. Raising unwinds the
    process (a crash under a supervisor is a legitimate recovery).

    The line: if the caller can and should DECIDE what to do (domain outcome),
    return `{:error, reason}`; if there is no sane recovery in place (bug,
    invariant violation, contract misuse), raise. Common follow-on: raise
    inside libraries for misuse, tuples at boundaries where callers retry or
    present the error.
- id: elixir-error-02
  answer: |
    The trailing `!` is a NAMING CONVENTION (not syntax) meaning: the FAILING
    variant RAISES instead of returning a tuple. The bang function is the
    "strict/expect-success" sibling of the non-bang function:

    ```elixir
    File.read("a.txt")    # => {:ok, contents} | {:error, :enoent}
    File.read!("a.txt")   # => contents     | ** (File.Error) could not read ...
    ```

    `!` = "this raises on failure, use it when you're confident it will succeed
    (or prefer a crash)". Non-bang returns the failure as data. So
    `File.read!/1` NEVER returns `:error` — it either succeeds or raises.
    The pairing is per-function: `String.to_integer` / `String.to_integer!`,
    `Map.fetch` / `Map.fetch!`. It's convention only — nothing is enforced at
    compile time, and you should follow it in your own modules: `MyApp.parse/1`
    returning `{:ok, _}` / `{:error, _}` alongside `MyApp.parse!/1` that raises.
- id: elixir-error-03
  answer: |
    `try/rescue/after` (plus `else` and `catch`) wraps code that may fail:

    ```elixir
    try do
      do_risky()
    rescue
      e in RuntimeError -> {:recovered, e.message}
      ArgumentError -> :bad_arg
    after
      cleanup()
    end
    ```

    - **`rescue`** catches EXCEPTIONS — structs raised via `raise/2`, built-in
      error types (`RuntimeError`, `ArgumentError`, `Enum.EmptyError`, and
      `error => e` on the error kind). It can also filter by type
      (`rescue %RuntimeError{}`) and re-raise with `reraise/2,3`.
    - **`catch`** catches the NON-error kinds: `:throw` values, `:exit`
      signals, and (legacy) error terms: `catch :throw, value`, `catch :exit,
      reason`, `catch :error, e`. `rescue` handles raise-based exceptions;
      `catch` is for the throw/exit escape hatches and BEAM interop.
    - **`after`** ALWAYS runs, no matter how the `try` ends — normal return,
      rescued exception, re-raised, untrapped exit — it's the `finally`. Use
      it for guaranteed cleanup: closing files/sockets, deleting temp state,
      releasing resources. Note it runs even when a `rescue` clause itself
      re-raises, and even when the process gets an exit signal.
    - **`else`** (bonus) matches the SUCCESS value of the `try` body against
      patterns, like a `case` over the result.
- id: elixir-error-04
  answer: |
    "Let it crash" is the OTP belief that processes should be small, isolated
    units that are allowed to FAIL FAST; recovery is the SUPERVISOR's job, not
    every function's job.

    Why crashing beats defending everywhere:

    1. **Isolation.** BEAM processes share NO memory — only messages. A crash
      takes down one process's heap and nothing else; no locks, no races, no
       half-mutated shared state to repair defensively. Rescuing every error
       with `try/rescue` doesn't buy you correctness; it buys you LIMPING
       processes in an undefined state.
    2. **Independent restart groups.** A supervisor restarts the crashed child
       (per its child spec and restart strategy) with clean, freshly
       initialised state, or shuts down a subtree and lets higher levels
       decide. You get known-good restart semantics instead of
       application-specific, untested cleanup paths.
    3. **Failure surface.** Crash logs point at the root cause immediately;
       swallowed errors hide bugs for months. Failing fast makes the error
       diagnosis the common case.
    4. **Simplicity of code.** Without defensive branches everywhere, happy
      paths stay linear; complex recovery is expressed once, at the
      supervision-tree level, where it can be tested.

    When to still rescue: on the BOUNDARY (a GenServer `init`, a request entry
    point) to translate an unexpected crash into a controlled error, and when
    the caller of a pure function can actually turn the error into a better
    result than a restart would. The rest of the time: let the supervisor
    restart the process.
- id: elixir-enum-01
  answer: |
    The difference is WHEN THE WORK RUNS (eagerness vs laziness):

    - **`Enum` is EAGER.** Each operation (`map`, `filter`, `reduce`,
      `sum`…) immediately computes its entire result from the input and
      returns a materialised collection. `Enum.map(list, &f/1)` is done the
      moment it returns. Enum works on any `Enumerable` but runs end to end
      every time.
    - **`Stream` is LAZY.** `Stream.map(list, &f/1)` builds a DESCRIPTION of
      the step, not the result. Work happens only when a consumer asks for
      elements (Enum functions like `Enum.to_list`, `Enum.take`, `Enum.sum`,
      `run`, etc.), and can stop part-way (an intermediate stream step is NOT
      evaluated for elements never consumed):

    ```elixir
    1..100_000_000 |> Stream.map(&(&1 * 3)) |> Stream.filter(&rem(&1, 2) == 0) |> Enum.take(3)
    # => [6, 12, 18]   (only 6 elements were ever transformed)
    ```

    Extra Stream capability: it can be infinite (`Stream.iterate/2`,
    `Stream.cycle/1`), which Enum cannot.
- id: elixir-enum-02
  answer: |
    Two clear wins for `Stream`:

    1. **A narrowing / selective pipeline over a big (or external) source.**
       When the first stage filters aggressively and later stages are
       expensive, Stream only pays for the survivors: `File.stream!("huge.csv")
       |> Stream.map(&parse_row/1) |> Stream.filter(&keep?/1) |> Enum.take(50)`.
       With Enum, every one of a million rows would be parsed and filtered up
       front before the first 50 results were needed; the Stream version stops
       reading after enough matches and processes each row once.

    2. **Infinite / unbounded producers, and partial consumption.**
       You cannot hand an infinite range to Enum (`Enum.map(1..infinity)` —
       never returns). With Stream: `Stream.iterate(0, &(&1 + 2)) |> Stream.map(&(&1 * &1)) |> Stream.take(5) |> Enum.to_list()` works because only the elements actually pulled are produced. Same shape for polling an API, reading from a socket, or a lazily generated sequence — plus it saves memory because collected results never build the intermediate lists an all-Enum pipeline would allocate.
- id: elixir-enum-03
  answer: |
    `Enum.reduce(enumerable, acc, fun)` FOLDS the collection into a single
    value: it calls `fun.(element, acc)` for every element in order,
    re-threading the returned value as the next `acc`, and returns the final
    accumulator. `acc` is your seed value.

    ```elixir
    Enum.reduce([1, 2, 3], 0, fn x, acc -> acc + x end)  # => 6
    ```

    It's considered the fundamental building block because map, filter, sum,
    join, max, group_by, count… can each be expressed as ONE reduce — the other
    Enum functions are (conceptually) pre-written reduces:

    - map: `Enum.reduce(l, [], fn x, acc -> [f(x) | acc] end) |> Enum.reverse()`
    - filter: `Enum.reduce(l, [], fn x, acc -> if p(x), do: [x | acc], else: acc end)`
    - sum: `Enum.reduce(l, 0, &+/2)`
    - length: `Enum.reduce(l, 0, fn _, n -> n + 1 end)`

    That's why reduce deserves the "fundamental" label: any single-pass
    traversal with a stateful accumulator is a reduce; anything else is a
    special case of it. (An exception worth knowing: reduce is sequential —
    it can't express concurrent `map`/|> work, which is what Task
    `async_stream` is for.)
- id: elixir-enum-04
  answer: |
    It's a comprehension with four pieces:

    - `for x <- list` — the GENERATOR: bind `x` to each element of `list` in
      turn.
    - `rem(x, 2) == 0` — a FILTER (any boolean expression; no `x` needed): keep
      only the values for which it's truthy.
    - `into: %{}` — the COLLECT target: instead of the default list, results go
      into a map. This requires the result of the body (`do` part) to be a
      2-tuple `{key, value}`, and `%{}` must be Collectable.
    - `do: {x, x * x}` — the BODY: the expression evaluated for each surviving
      element.

    So this comprehension takes the even numbers of `list` and produces
    `%{x => x * x}`, e.g. `for x <- [1, 2, 3, 4], rem(x, 2) == 0, into: %{}, do: {x, x * x}` gives `%{2 => 4, 4 => 16}`.

    Extras worth knowing: multiple generators are CARTESIAN-dependent
    (`for x <- a, y <- b` = a outer, b inner), `x <- list` iterates in order,
    you can bind values with `pattern <- expr` filters,
    `:uniq => true` dedupes, and by default into a list the body produces one
    result per iteration.
- id: elixir-proto-01
  answer: |
    A protocol is runtime POLYMORPHISM ON DATA — a way to make one function
    name work across many different TYPES, where the implementation is chosen
    based on the FIRST ARGUMENT's type, and each implementation is swapped in
    without changing the caller.

    ```elixir
    defprotocol Size do
      def size(data)          # declared, no default body
    end

    defimpl Size, for: BitString do
      def size(b), do: byte_size(b)
    end

    defimpl Size, for: Map do
      def size(m), do: map_size(m)
    end

    Size.size("abc")   # => 3        dispatched to the BitString impl
    Size.size(%{a: 1}) # => 1        dispatched to the Map impl
    ```

    - `defprotocol` declares the function signatures (and may provide a default
      via `Protocol`).
    - `defimpl` provides a concrete implementation `for:` a specific type —
      built-in types AND any module/struct. Dispatch is dynamic at runtime:
      call sites don't know or care which impl runs.

    Key property: protocols are OPEN/EXTENSIBLE — implementations can be added
    for ANY type (including ones the protocol author never thought of), and
    existing datatypes never need modification. Examples in the Elixir stdlib:
    `String.Chars`, `Inspect`, `Enumerable`, `Collectable`, `Jason.Encoder`.
    (Protocol consolidation exists to keep dispatch fast.)
- id: elixir-proto-02
  answer: |
    A behaviour is a COMPILE-TIME CONTRACT: a list of function signatures a
    module promises to provide, used for module-level polymorphism (the same
    role an interface plays in OO languages). Behaviours are how libraries
    define pluggable modules — GenServer is just a behaviour.

    - `@callback` — declares the contract: `@callback init(term) :: {:ok, state} | {:error, reason}`.
      It specifies name, arguments, and return type. It lives in the module
      that DEFINES the behaviour.
    - `@behaviour` — a module opts IN with `@behaviour MyWorkerBehaviour`. The
      compiler then WARNS if any `@callback` is missing (mismatched arity or
      absent). `@optional_callbacks` declares callbacks that may be omitted
      without a warning.
    - `@impl` — annotates the implementing function:
      `@impl true` (or `@impl MyBehaviour` when several behaviours apply).
      It tells the compiler "this definition is meant to satisfy a callback",
      so typos and signature drift are caught — if your function doesn't
      match any declared `@callback`, the compiler emits an error. It also
      documents intent and disambiguates when multiple behaviours declare
      same-named callbacks.

    In one line: `@callback` declares, `@behaviour` binds the contract, `@impl`
    validates and marks the fulfilment.
- id: elixir-proto-03
  answer: |
    Fundamentally:

    - A **protocol** is POLYMORPHISM OVER TYPES — dispatch happens at RUNTIME
      on the data itself. You dispatch to implementations per TYPE, and you can
      extend it to new types AFTER the fact (including types the protocol
      author never saw). `Size.size("abc")` picks its implementation by value
      type, so the same call site handles BitString, Map, MyStruct
      interchangeably. It answers: "how is ONE function implemented across many
      data types?"
    - A **behaviour** is a MODULE-LEVEL CONTRACT — dispatch happens by
      REFERENCE: the caller chooses which MODULE to call, and the compiler
      validates that module implements the required functions. It answers:
      "what functions must a module provide to be a GenServer / plug-in /
      adapter?"

    So protocols are OPEN and value-dispatched; behaviours are STRUCTURED and
    module-dispatched. Consequences: a protocol implementation can be attached
    to ANY type (even ones you don't own); a behaviour requires an explicit
    `@behaviour` opt-in and yields compile-time checks on the module's
    surface. Elixir protocols never use `@behaviour`, and GenServer-style
    callbacks never use dispatch-on-type — different problems, despite both
    looking like "interfaces".
- id: elixir-proto-04
  answer: |
    `@impl true` tells the compiler the annotated function is meant to
    implement a `@callback` of the declared `@behaviour`, and it BUYS:

    1. **Compile-time validation of the contract.** A typo (`def handel_call/3`),
       wrong arity, wrong argument names, or wrong return shape now produces a
       compile error ("function does not match the callback") instead of a
       silent no-op you'd only discover at runtime when your GenServer
       mysteriously doesn't respond.
    2. **Protection against drift.** If the library later changes the callback's
       signature, your module fails to compile at upgrade time instead of
       crashing in production.
    3. **Documentation and reviewability.** Readers see at a glance which
       functions are deliberate interface implementations vs internal helpers.
    4. **Disambiguation.** With several behaviours active (`@behaviour GenServer`
       plus `@behaviour MyBehaviour`), `@impl MyBehaviour` says exactly which
       callback a function fulfils, resolving collisions in callback
       validation.

    There is no runtime cost — `@impl` is purely a compile-time marker; it
    doesn't change dispatch or behaviour, only what the compiler checks and
    what humans see.
- id: elixir-conc-01
  answer: |
    BEAM concurrency is built on LIGHTWEIGHT PROCESSES — not OS threads or
    green threads you manage yourself. Each process is a small, independently
    GC'd runtime object with its own heap (no shared heap), its own MAILBOX,
    its own state, and its own scheduler slot. You can have millions of them;
    the VM's schedulers (one per core, by default) multiplex them across cores
    with preemption.

    There is NO shared mutable memory because processes communicate ONLY by
    MESSAGE PASSING: `send(pid, msg)` copies `msg` from the sender's heap into
    the receiver's mailbox, and the receiver picks messages out with
    `receive`. One process can never reach into another process's heap, so one
    process's data is physically inaccessible (not merely "should not be
    touched") to the others.

    Why it matters:

    1. **No data races.** No locks, no mutexes, no atomics, no lost-update
      problems — you cannot have two processes mutate the same memory because
      no memory is shared. Correctness comes from isolation by construction.
    2. **Failure isolation.** A crash unwinds one process; no other process's
      memory is corrupted, so the BEAM's supervisor model can contain and
      restart failures cleanly. (Compare shared-memory concurrency, where one
      bad write can poison everyone.)
    3. **No stop-the-world GC.** Each process collects its own garbage, so the
      whole VM never freezes on any single process's state.
    4. **Simplicity of concurrency.** Concurrency is "a bunch of processes
      exchanging immutable messages" — one communication primitive, easy to
      reason about and test.
- id: elixir-conc-02
  answer: |
    `Task.async(fun)` SPAWNS a linked process (a task) that runs `fun` and
    returns a `%Task{}` struct immediately — the CALLER keeps going while the
    task runs in parallel. `Task.await(task, timeout \\ 5000)` BLOCKS the
    caller until the task finishes, then returns the task's result:
    `{:ok, value}`. That's the concurrency mechanism: build many tasks with
    async, work (or block on) them, then await.

    ```elixir
    t1 = Task.async(fn -> work1() end)
    t2 = Task.async(fn -> work2() end)
    Task.await(t1)  # blocks until work1 finishes, returns {:ok, res1}
    Task.await(t2)
    ```

    Failure and timeout behaviour (both are LINKED-process events):

    - **If the task raises:** the task process dies with that exception, and
      because it's LINKED to the caller, the exit propagates — the caller
      EXITS with the task's exit reason (you don't get `{:error, e}`; you get
      a crash, unless the caller is trapping exits). Caveat: any part of
      `Task.async` you never `await` AND forget also crashes the caller, so
      always pair awaits with your asyncs or use `Task.yield/2` for a
      non-crashing peek.
    - **On timeout (default 5000 ms):** `Task.await` EXITS the caller with a
      `{:timeout, task}` error — it doesn't return an error tuple. Use
      `Task.yield(task, timeout)` instead if you want to keep running when
      results don't arrive and manage the task's shutdown yourself.
    - **Abnormal task shutdown:** also propagates to the caller, for the same
      link reason.
- id: elixir-conc-03
  answer: |
    An `Agent` is a process whose JOB IS TO HOLD SHARED STATE — a thin,
    purpose-built wrapper around a GenServer for exactly one thing: state
    storage. You start it with an initial state and interact with
    `Agent.update(agent, fun, timeout)` (turn the current state into a new
    one) and `Agent.get(agent, fun, timeout)` (read/derive from it):

    ```elixir
    {:ok, agent} = Agent.start_link(fn -> %{hits: 0} end)
    Agent.update(agent, fn s -> %{s | hits: s.hits + 1} end)
    Agent.get(agent, fn s -> s.hits end)   # => 1
    ```

    How it differs from a full GenServer:

    - An Agent IS a GenServer underneath (same process model, same links,
      same supervision), but its message surface is fixed and read/write-only.
      There is no `handle_call`/`handle_cast`/`handle_info` you can inspect or
      extend.
    - Use an Agent when the shared data is SIMPLE and needs CUSTOM LOGIC
      (it's essentially "a shared mutable variable"): counters, caches,
      connection pools, small accumulators.
    - Use a GenServer when you need anything else: custom message handling with
      non-trivial replies, async work inside init/handlers, spawning tasks,
      periodic ticks, interactions with other processes, or just a state
      machine. A GenServer is the general framework; an Agent is the
      pre-baked shortcut when "state + read/write ops" is the whole job.
- id: elixir-conc-04
  answer: |
    ETS (Erlang Term Storage) is the BEAM's in-memory TERM STORAGE: named
    tables of Elixir terms, living OUTSIDE any process's heap (owned by the
    BEAM itself), accessed directly with Erlang functions (`:ets.insert`,
    `lookup`, `lookup_element`, `delete`, `update_element`, `select/match`, …).
    Tables are created with `:ets.new/2`, are keyed for constant-time lookups,
    and have configurable access rights (`:public` — any process reads/writes;
    `:protected` — owner writes, all read; `:private`).

    Use ETS instead of a process when:

    1. **Many processes need to READ or WRITE the same data.** A GenServer
      serialises every request through one mailbox: it's a single coordinator
      acting as a throughput bottleneck and, under load, a queue. ETS tables
      are CONCURRENT — many processes access them in parallel (constant-time
      key lookups; per-key write atomicity).
    2. **The data needs no complex per-message logic** — e.g. lookup tables,
      hot caches (`:ets` with `read_concurrency`, write_concurrency), counters
      (`:ets.update_counter` is atomic), process registries, config. If reads
      and writes dominate and coordination is trivial, ETS beats a process.

    Prefer a process (GenServer/Agent) when: ownership semantics, ordered
    sequences of messages, back-pressure/mailbox flow control, supervision
      restart semantics, or access patterns driven by events rather than
    key lookups.

    Caveats that shape the trade: ETS COPIES terms in/out (it's safe — no
    shared mutation), a table dies with its owning process unless a heir is
    set, it's memory-only (no persistence like DETS/Mnesia), and reads don't
    block, but concurrent writes to the SAME key are serialised (that's the
    atomic unit). Rule of thumb: ETS for fast shared KEY DATA, processes for
    coordination.
