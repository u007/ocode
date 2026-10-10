- id: go-concurrency-01
  answer: |
    An unbuffered channel has no capacity: a send blocks until a receiver is ready to receive, and a receive blocks until a sender is ready to send. It provides synchronous handoff between goroutines. A buffered channel has a fixed capacity: a send blocks only when the buffer is full, and a receive blocks only when the buffer is empty. Sends on an unbuffered channel block until a receiver receives; sends on a buffered channel block only when the buffer is full.

- id: go-concurrency-02
  answer: |
    `select` waits until one of its channel operations can proceed, then executes that case's body. If multiple cases are ready, one is chosen uniformly at random. A `default` case makes the select non-blocking: if no channel case is ready, `default` runs immediately. If there is no `default` and no ready case, the select blocks until at least one case becomes ready.

- id: go-concurrency-03
  answer: |
    The sender should close a channel, never the receiver — closing signals "no more values will be sent." Sending on a closed channel panics. Receiving from a closed channel returns the zero value immediately (with the second return value `false` indicating the channel is closed and drained). Closing is used to signal completion to receivers, often so a `range` loop over the channel terminates. Only the sender (or a single owner) should close; closing an already-closed channel panics.

- id: go-concurrency-04
  answer: |
    Before Go 1.22, the loop variable was a single variable reused across all iterations, so all goroutines captured the same variable and saw its final value after the loop completed. Go 1.22 changed the loop semantics so each iteration gets a fresh copy of the loop variable, so each goroutine captures its own iteration's value. The fix is automatic in Go 1.22+; before that, you had to shadow the variable (e.g., `v := v`) inside the loop body.

- id: go-sync-01
  answer: |
    A data race occurs when two or more goroutines access the same memory location concurrently and at least one access is a write, without synchronization. The minimal correct way to protect a counter incremented by many goroutines is to use `sync/atomic` (e.g., `atomic.AddInt64(&counter, 1)`) or a `sync.Mutex` around the increment. `atomic` is preferred for simple counters because it is lock-free and faster.

- id: go-sync-02
  answer: |
    The `-race` flag enables Go's race detector, which instruments memory accesses and reports data races at runtime. Its limitations: it only detects races that actually occur during execution (it is not a static analysis), it adds significant overhead (2–10x slowdown, extra memory), it requires CGO to be enabled, and it may miss races that don't manifest in the exercised code paths.

- id: go-sync-03
  answer: |
    Use `sync/atomic` for simple scalar operations (increment, compare-and-swap, load/store) where you need lock-free performance. Use `sync.Mutex` when you need to protect a critical section with multiple operations or complex invariants. The trade-off: `atomic` is faster and non-blocking but limited to single-word operations; `Mutex` is more general but can cause contention and blocking.

- id: go-sync-04
  answer: |
    Use `sync.WaitGroup` by calling `wg.Add(n)` before launching goroutines, `wg.Done()` (or `wg.Add(-1)`) when each finishes, and `wg.Wait()` to block until all complete. The common misuse is calling `Add` after the goroutine has already started (or from within the goroutine itself), which can cause `Wait` to return before `Add` is called, or calling `Add` with a positive delta after `Wait` has started, which can cause a panic. Always call `Add` before launching the goroutine.

- id: go-errors-01
  answer: |
    The `%w` verb wraps the error, creating a new error that chains the original via an Unwrap method, allowing `errors.Is` and `errors.As` to traverse the chain. `%v` and `%s` format the error as a string without wrapping — they lose the error chain and identity. Use `%w` when you want to preserve the error for inspection; use `%v`/`%s` when you only need a message.

- id: go-errors-02
  answer: |
    `errors.Is(err, target)` checks whether `err` (or any error in its chain via Unwrap) equals `target` — it answers "is this error (or does it wrap) this specific error?" `errors.As(err, &target)` finds the first error in the chain that matches the target type and assigns it — it answers "is there an error of this type in the chain, and what is it?" Use `Is` for sentinel comparison; use `As` to extract typed errors.

- id: go-errors-03
  answer: |
    A sentinel error is a package-level error variable declared with `var ErrSomething = errors.New("...")`. Callers compare against it with `errors.Is(err, ErrSomething)`. Sentinel errors provide a stable, comparable identity that callers can check regardless of wrapping or message changes.

- id: go-errors-04
  answer: |
    In Go, an interface value is nil only when both its type and value are nil. When a function returns a nil `*MyError` typed as `error`, the interface has a non-nil type (`*MyError`) and a nil value, so `err != nil` evaluates to true. This is the "nil interface vs nil concrete value" gotcha. The fix is to return a literal `nil` (untyped) instead of a typed nil pointer, or check for nil before returning.

- id: go-interfaces-01
  answer: |
    A type satisfies an interface implicitly by implementing all of its methods — no explicit declaration is needed. "Accept interfaces, return structs" means function parameters should be interfaces (to allow flexibility and testing with mocks), while return values should be concrete structs (so callers know exactly what they get and can add methods later without breaking callers).

- id: go-interfaces-02
  answer: |
    The empty interface (`any` / `interface{}`) can hold any value. A single-result type assertion like `x.(int)` panics if `x` does not hold an `int`. The two-result form `v, ok := x.(int)` returns the zero value and `false` instead of panicking, making it safe for type checking. Always use the two-result form when the type is uncertain.

- id: go-generics-01
  answer: |
    Use generics when you need to write a function or type that works with multiple types while preserving type safety and avoiding code duplication (e.g., a `Map` or `Filter` function over any slice type). Use a plain interface parameter when you only need a small set of behavior (e.g., `io.Reader`) and don't need the concrete type. Generics add complexity; prefer interfaces when they suffice.

- id: go-generics-02
  answer: |
    A generic function has type parameters in square brackets before the regular parameters: `func Name[T any](x T) T { ... }`. A constraint is an interface that restricts which types can be used for the type parameter — `any` means no restriction, `comparable` means the type supports `==` and `!=`, and custom interfaces can specify method sets or type sets.

- id: go-generics-03
  answer: |
    The `comparable` constraint allows types that support `==` and `!=` operators (integers, floats, strings, pointers, channels, structs with comparable fields, etc.). The `~` token in a constraint like `~int` means "any type whose underlying type is int" — it allows named types like `type MyInt int` to satisfy the constraint, not just the built-in `int` itself.

- id: go-generics-04
  answer: |
    Type-argument inference lets the compiler deduce type arguments from the function arguments, so you can write `Map(s, f)` instead of `Map[int, string](s, f)`. You must specify type arguments explicitly when the compiler cannot infer them — e.g., when a type parameter appears only in the return type and not in any parameter, or when the inference is ambiguous.

- id: go-context-01
  answer: |
    Context cancellation signals via a `Done()` channel that is closed when the context is cancelled. It does not forcibly stop a goroutine — the goroutine must cooperatively check `ctx.Done()` or `ctx.Err()` and return on its own. It is the goroutine's responsibility to observe cancellation and stop; the context only provides the signal.

- id: go-context-02
  answer: |
    `WithTimeout` and `WithDeadline` automatically cancel the context after a duration or at a specific time, respectively. `WithCancel` requires explicit cancellation by calling the returned `cancel` function. You must always call `cancel` to release the context's resources (timers, goroutines in the parent's children map); failing to do so causes a resource leak until the parent context is cancelled or the timer fires.

- id: go-context-03
  answer: |
    `WithValue` attaches request-scoped data (e.g., user IDs, trace IDs, auth tokens) to a context so it can be retrieved downstream. It should NOT be used for passing optional function parameters, configuration, or dependencies — those should be explicit function arguments. Values should be used sparingly, as they are untyped and can hide data flow.

- id: go-context-04
  answer: |
    Conventions: pass `context.Context` as the first parameter of a function (named `ctx`); do not store it in struct types; do not pass `nil` — use `context.TODO()` if unsure; propagate the incoming context to downstream calls; do not add it to optional parameters. Contexts flow down the call tree, not up.

- id: go-slices-01
  answer: |
    Slices share an underlying array. Appending to a slice with spare capacity writes into the shared array, which can overwrite data visible through another slice pointing to the same array. `append` may or may not allocate a new array depending on capacity, so you must always use the return value (`s = append(s, x)`) — ignoring it means you may lose the appended element or silently corrupt another slice's data.

- id: go-slices-02
  answer: |
    Reading from a nil map returns the zero value (no panic). Writing to a nil map panics with "assignment to entry in nil map." A nil slice, by contrast, can be read (returns zero value), appended to (allocates a new array), and ranged over (zero iterations) — all without panicking. The asymmetry is that maps require initialization before writes, while slices do not.

- id: go-slices-03
  answer: |
    `copy(dst, src)` copies `min(len(dst), len(src))` elements from `src` to `dst` and returns the number copied. Slicing (`s[1:3]`) creates a new slice header pointing to the same underlying array — it does not copy data. Modifying elements through the sub-slice affects the original and vice versa. To get an independent slice, use `append([]T(nil), s...)` or `slices.Clone`.

- id: go-slices-04
  answer: |
    Go map iteration order is intentionally randomized to prevent developers from depending on it. You cannot take the address of a map element (`&m[k]`) because map entries may move during rehashing as the map grows, making pointers to them invalid. Instead, store pointers as values (`m[k] = &v`) or copy the value out.

- id: go-defer-01
  answer: |
    Arguments to a deferred call are evaluated immediately when the `defer` statement is executed, not when the deferred function actually runs. Multiple deferred calls run in LIFO (last-in, first-out) order — the most recently deferred runs first.

- id: go-defer-02
  answer: |
    `recover` stops a panic and returns the value passed to `panic`, allowing the function to return normally. It only works when called directly inside a deferred function (not in a nested function). It only covers the goroutine in which the panic occurred — a panic in another goroutine cannot be recovered. If `recover` is called when no panic is in progress, it returns `nil`.

- id: go-defer-03
  answer: |
    `defer file.Close()` inside a long-running loop defers the close until the function returns, not the end of each iteration. This means all opened files remain open simultaneously, potentially exhausting file descriptors. Instead, close the file explicitly at the end of each iteration, or extract the loop body into a helper function so `defer` runs per iteration.

- id: go-defer-04
  answer: |
    A deferred function can change a function's return value if the return value is named (a named return parameter). The deferred function can assign to the named return variable, and the modified value is what the caller receives. This is commonly used for cleanup that must happen before returning, or for recovering from panics and returning a fallback error.

- id: go-testing-01
  answer: |
    A table-driven test defines a slice of test cases (each with inputs and expected outputs) and iterates over them in a loop, running each as a subtest. It is idiomatic because it makes adding new cases trivial, keeps test logic DRY, provides clear failure messages per case, and ensures consistent coverage across inputs.

- id: go-testing-02
  answer: |
    `t.Parallel()` marks a subtest to run in parallel with other parallel subtests — it pauses until all non-parallel subtests complete, then runs concurrently. The historical loop-variable pitfall: before Go 1.22, parallel subtests captured the loop variable, so all subtests saw the final value. Go 1.22's per-iteration loop variable fixed this, but before that you needed `tc := tc` inside the loop.

- id: go-testing-03
  answer: |
    `t.Cleanup` registers a function to run after the test (and its subtests) complete — used for teardown like closing files, removing temp directories, or stopping servers. `t.Helper` marks the calling function as a test helper, so failure messages and line numbers point to the actual test call site rather than inside the helper function, improving debuggability.
