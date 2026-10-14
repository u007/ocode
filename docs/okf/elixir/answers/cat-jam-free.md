# cat-jam-free — closed-book answers for elixir (opencode-go, 2026-10-14); answerer saw only _prompts/elixir.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.
# elixir-pm-04 was skipped by the model in the sheet run; re-asked ALONE (single-question sheet, tool-free isolated HOME) and appended.

- id: elixir-pm-01
  answer: |
    `=` is pattern matching, not assignment. `{:ok, value} = fetch()` calls `fetch()`, then tries to match its result against the pattern `{:ok, value}`. If the result is `{:ok, data}`, `value` is bound to `data`; if the result is `{:error, :notfound}`, the patterns do not match and Elixir raises a `MatchError`.

- id: elixir-pm-02
  answer: |
    Clauses are tried top-to-bottom. The first clause whose head pattern matches the arguments and whose `when` guard evaluates to true is executed; later clauses are skipped. Guards replace `if` branches by moving conditions into the function head, e.g. `factorial(0)` first, then `factorial(n) when n > 0`.

- id: elixir-pm-03
  answer: |
    The pin operator `^` matches against the current value of an already-bound variable instead of rebinding it. `^x` requires the matched value to equal `x`; a bare `x` binds `x` to whatever matched value.

- id: elixir-otp-01
  answer: |
    `spawn` starts a new BEAM process and returns its pid. `send`/`!` asynchronously delivers a message to a process’s mailbox. `receive` waits for a message matching one of its clauses, removes it, and evaluates the corresponding body. If nothing matches, it blocks until a matching message arrives; an `after` clause can impose a timeout.

- id: elixir-otp-02
  answer: |
    `handle_call/3` handles synchronous requests made with `GenServer.call/2,3`; it must return `{:reply, reply, new_state}` (or related forms such as timeout/hibernate/stop variants). `handle_cast/2` handles asynchronous requests from `GenServer.cast/2` and returns `{:noreply, new_state}` (or a stop tuple); it has no direct caller to reply to.

- id: elixir-otp-03
  answer: |
    `handle_info/2` handles messages that arrive in the process mailbox outside the GenServer call/cast protocol, such as raw `send` messages, monitor/down notifications, timeouts, or messages from other processes. `handle_call` and `handle_cast` handle only structured GenServer client API requests.

- id: elixir-otp-04
  answer: |
    The three main strategies are `:one_for_one` (restart only the crashed child), `:one_for_all` (restart all children when one crashes), and `:rest_for_one` (restart the crashed child and children started after it). A child spec is a map/description defining how a child is started and supervised: `:id`, `:start`, `:restart`, `:shutdown`, `:type`, and `:modules`.

- id: elixir-data-01
  answer: |
    `list` is unchanged; `List.delete/2` returns a new list. Elixir data is immutable, so the function does not alter the original. Rebinding a variable simply makes that name point to a new value; the old value remains until garbage collected if no other references exist.

- id: elixir-data-02
  answer: |
    Use maps for general key-value data; keyword lists for ordered option lists with atom keys where duplicates are meaningful; structs for fixed-schema domain records with module-defined fields and compile-time guarantees. Keyword lists are lists, maps are hash-like associative structures, and structs are special maps tied to a module.

- id: elixir-data-03
  answer: |
    `%{map | key: val}` requires `key` to already exist and raises if it does not. `Map.put(map, key, val)` adds `key` if missing and updates it if present. Both return new maps; neither mutates the original.

- id: elixir-data-04
  answer: |
    `get_in/2` traverses nested immutable structures using a key path, `put_in/3` returns a new root with a nested value replaced, and `update_in/3` applies a function to a nested value and returns the updated root. They avoid manual, error-prone nested reconstruction and integrate with `Access` paths for lists/tuples/maps.

- id: elixir-pipe-01
  answer: |
    `|>` evaluates the left expression and passes it as the first argument to the function on the right. The one rule is that the piped value is inserted as the first positional argument.

- id: elixir-pipe-02
  answer: |
    `with` solves sequential “happy path” control flow and nested case statements. It chains `<-` clauses; each must match its pattern, and bindings from earlier clauses are available to later ones. If all match, the `do` block runs.

- id: elixir-pipe-03
  answer: |
    If a `<-` clause fails to match, the `with` expression short-circuits and returns that unmatched value. An `else` block receives that value and can pattern-match it to handle failures.

- id: elixir-pipe-04
  answer: |
    Piping reads badly when the value is needed in a non-first position, when a right-hand function has complex argument lists, or when branching is involved. Fix it by using direct function calls, intermediate bindings, or `case`/`with`; do not force data into the first argument awkwardly.

- id: elixir-error-01
  answer: |
    Use `{:ok, value}` / `{:error, reason}` for expected, domain-level outcomes that callers should inspect and handle. Raise exceptions for truly exceptional conditions, broken invariants, or unrecoverable programmer errors. Tagged tuples support normal control flow; exceptions unwind the stack.

- id: elixir-error-02
  answer: |
    A trailing `!` conventionally means the function raises an exception on failure instead of returning an error tuple. For example, `File.read/1` returns `{:ok, content}` or `{:error, reason}`, while `File.read!/1` returns the content or raises.

- id: elixir-error-03
  answer: |
    `try/rescue/after` runs code; `rescue` catches exceptions, while `catch` catches thrown values and exits. `after` is guaranteed to run after the `try` body whether it completes normally or fails, unless the process/VM is abruptly terminated.

- id: elixir-error-04
  answer: |
    “Let it crash” means let a process fail and be restarted by a supervisor rather than adding defensive rescue everywhere. Processes are isolated, so a crash stays localized and recovery starts from a clean state; this keeps code simpler and avoids operating on potentially corrupt state.

- id: elixir-enum-01
  answer: |
    `Enum` is eager: transformations are applied immediately and materialize concrete collections. `Stream` is lazy: it describes a pipeline that only evaluates elements when consumed, such as by an `Enum` terminal function or comprehension.

- id: elixir-enum-02
  answer: |
    Use `Stream` when processing very large or conceptually infinite sequences where holding all results in memory is undesirable, and when composing several transformations while avoiding intermediate collections. It is also appropriate for streaming input such as files or sockets.

- id: elixir-enum-03
  answer: |
    `Enum.reduce/3` folds a collection into an accumulator by repeatedly applying a reducer function: `reducer(element, accumulator) -> new_accumulator`. It is fundamental because `map`, `filter`, `sum`, and many others can be defined as specialized reductions.

- id: elixir-enum-04
  answer: |
    In `for x <- list, rem(x, 2) == 0, into: %{}, do: {x, x * x}`, `x <- list` is a generator, `rem(x, 2) == 0` is a filter, `into: %{}` collects results into a map, and `do: {x, x * x}` produces each key-value pair. The comprehension builds a map of even numbers to their squares.

- id: elixir-proto-01
  answer: |
    A protocol defines polymorphic behavior by data type. `defprotocol` declares the function interface; `defimpl` implements that protocol for a specific type. Call sites invoke protocol functions, and runtime dispatch selects the implementation based on the first argument’s type.

- id: elixir-proto-02
  answer: |
    A behaviour is a module defining a callback contract. `@callback` declares an expected function signature, `@behaviour` declares that a module implements that contract, and `@impl` marks a function as implementing a callback. The compiler can then detect missing or mismatched callbacks.

- id: elixir-proto-03
  answer: |
    Protocols dispatch based on the type of the data argument, providing open polymorphism. Behaviours define a module-level callback contract; dispatch is by module identity and explicit implementation. Thus protocols are type-driven, while behaviours are interface-driven modules.

- id: elixir-proto-04
  answer: |
    `@impl true` or `@impl MyBehaviour` documents that the function implements a behaviour callback and lets the compiler verify that it actually matches a declared callback. It catches typos, incorrect arities, and accidental non-implementations.

- id: elixir-conc-01
  answer: |
    BEAM processes are lightweight, scheduled preemptively, and have isolated mutable state; they communicate only by message passing. There is no shared mutable memory, which eliminates data races, keeps failures localized, supports supervision trees, and makes distribution easier.

- id: elixir-conc-02
  answer: |
    `Task.async/1` spawns a process to run a function and returns a task handle. `Task.await/2` blocks until completion or timeout. If the task raises, the linked task process exits and the failure propagates to the caller unless exits are trapped. On timeout, `await` raises a timeout failure.

- id: elixir-conc-03
  answer: |
    An `Agent` is a simple stateful process wrapping state access functions like `get` and `update`. Unlike a full `GenServer`, it requires no custom callbacks, message handling, or reply logic, making it useful for simple shared state while still being a separate supervised process.

- id: elixir-conc-04
  answer: |
    ETS is an in-memory table system accessible across processes with concurrency controls. Use it for shared caches, large read-heavy datasets, counters, or when GenServer/Agent message passing becomes a bottleneck. It still has an owning process and access options, but avoids serializing all reads through one process.
- id: elixir-pm-04
  answer: |
    No. Map pattern matching is a subset/partial match: `%{name: n} = user` succeeds if `user` contains the key `:name` and binds `n` to its value; extra keys are ignored. This differs from tuple matching, where the tuple must have the exact arity and pattern positions/type must line up, and from list matching, where fixed-width lists require the same length (unless a rest capture such as `[h | t]` is used).
