# cat-jam-free — closed-book answers for golang (opencode-go, 2026-10-14); answerer saw only _prompts/golang.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: go-concurrency-01
  answer: |
    An unbuffered channel has capacity 0: a send blocks until another goroutine receives. A buffered channel has capacity N: a send blocks only when the buffer is full; a receive blocks only when it is empty.

- id: go-concurrency-02
  answer: |
    `select` waits for one of its channel operations to become ready and executes that case; when several are ready, the choice is pseudo-random. A `default` case makes it non-blocking, running `default` if no case is ready. With no ready case and no `default`, `select` blocks until one case becomes ready.

- id: go-concurrency-03
  answer: |
    Only the sender should close a channel, after no more sends will occur. Receiving from a closed channel returns buffered values then zero values with `ok == false`; sending to a closed channel panics; closing twice panics. Closing signals completion, often used with `range` over a channel.

- id: go-concurrency-04
  answer: |
    The loop variable was reused across iterations, so closures/goroutines captured the same variable and observed its final value. In Go 1.22, `for` loop variables are given per-iteration scope, so each iteration/goroutine sees its own value.

- id: go-sync-01
  answer: |
    A data race is concurrent access to the same memory location where at least one access is a write, without synchronization. The minimal protection for a shared counter is a `sync.Mutex` around the increment, or `sync/atomic` for a simple atomic counter.

- id: go-sync-02
  answer: |
    `-race` enables the race detector, which dynamically reports unsynchronized concurrent memory accesses during execution. Its limitations include runtime overhead, detection only of races exercised by the tested path, and no guarantee that absence of reports means the program is race-free.

- id: go-sync-03
  answer: |
    Use `sync/atomic` for simple single-word operations such as counters, flags, and pointers where lock-free progress matters. Use `sync.Mutex` when protecting invariants across multiple fields or arbitrary critical sections. Atomic operations are cheaper but more specialized and easier to misuse for complex state.

- id: go-sync-04
  answer: |
    Call `Add(n)` before launching goroutines, have each goroutine call `Done()` (often via `defer`), and call `Wait()` to block until the count reaches zero. A common misuse is calling `Add` inside the goroutine or after `Wait` has started, causing races, early return, or a negative counter panic.

- id: go-errors-01
  answer: |
    `%w` wraps the supplied error so the resulting error preserves the error chain for `errors.Is` and `errors.As`. `%v` or `%s` only format the error as text and do not create a wrappable/unwrappable error relationship.

- id: go-errors-02
  answer: |
    `errors.Is` checks whether an error or any wrapped error equals a sentinel. `errors.As` checks whether an error or any wrapped error can be assigned to a target type, returning it. Use `Is` for sentinel equality and `As` when you need the concrete error value or type.

- id: go-errors-03
  answer: |
    A sentinel error is a package-level `var ErrX = errors.New("...")`, conventionally named with an `Err` prefix. Callers should compare using `errors.Is(err, ErrX)` rather than direct `==`, so wrapped errors are handled.

- id: go-errors-04
  answer: |
    A nil `*MyError` has a concrete dynamic type, so when stored in an `error` interface the interface value itself is non-nil. Compare through the interface carefully; return an untyped `nil` when there is no error.

- id: go-interfaces-01
  answer: |
    A type satisfies an interface implicitly when it implements all of the interface's methods with matching signatures; no `implements` declaration is required. "Accept interfaces, return structs" means function parameters commonly use interfaces for flexibility and testability, while return values use concrete types to preserve implementation details.

- id: go-interfaces-02
  answer: |
    The empty interface (`any`) has no methods and accepts values of any type. A one-result type assertion `x.(int)` panics if the assertion is false; the two-result form `v, ok := x.(int)` reports success without panicking.

- id: go-generics-01
  answer: |
    Use generics when the same algorithm applies across multiple concrete types while preserving type safety and avoiding interface boxing. Use an interface parameter when runtime polymorphism over types implementing behavior is desired, especially for heterogeneous values.

- id: go-generics-02
  answer: |
    Shape: `func Name[T Constraint](x T) T { ... }`. A constraint is a type set that limits which type arguments may be used; examples include `any`, `comparable`, `~int`, or an interface defining required methods.

- id: go-generics-03
  answer: |
    `comparable` permits types supporting `==` and `!=`, including types usable as map keys. The `~` token in `~int` means types whose underlying type is `int`, including named types with underlying type `int`.

- id: go-generics-04
  answer: |
    Type-argument inference lets the compiler infer type arguments from function arguments. You must specify them explicitly when there are no arguments from which to infer them, or when inference is ambiguous or unavailable.

- id: go-context-01
  answer: |
    Cancellation is signaled through `ctx.Done()`; it does not forcibly terminate a goroutine. The goroutine doing work must check the context and stop itself when cancelled. Responsibility to observe cancellation and exit lies with the code receiving the context.

- id: go-context-02
  answer: |
    `WithTimeout` and `WithDeadline` cancel automatically after a duration or time; `WithCancel` requires an explicit cancel call. Always call the returned cancel to release resources/timers and avoid leaks, even after the context has expired.

- id: go-context-03
  answer: |
    `context.WithValue` carries request-scoped metadata such as trace or request IDs across API boundaries. It should not be used for optional function parameters, control flow, large payloads, dependencies, or business data that should be explicit arguments.

- id: go-context-04
  answer: |
    Pass `context.Context` as the first parameter, usually named `ctx`; propagate it through calls; never store it in a struct; never pass nil; prefer `context.Background()` or `context.TODO()` as roots; call cancel to clean up derived contexts.

- id: go-slices-01
  answer: |
    `append` may reuse the backing array, so slices can alias the same underlying storage and concurrent or later writes can overwrite data seen through another slice. Its return value is load-bearing because a reallocation may return a slice with a new backing array, length, and capacity.

- id: go-slices-02
  answer: |
    Reading from a nil map returns a zero value and `false` without panicking; writing to a nil map panics. A nil slice has length zero and can be used with `append`, `len`, and `range`, but indexing it panics.

- id: go-slices-03
  answer: |
    `copy(dst, src)` copies `min(len(dst), len(src))` elements into `dst` and returns that count; it does not control capacities. Slicing `s[1:3]` creates a new header over the same backing array, so it is not an independent copy.

- id: go-slices-04
  answer: |
    Map iteration order is non-deterministic/randomized. You cannot take `&m[k]` because map values are not addressable; map growth/rehashing may relocate entries, so direct addresses would be unsafe.

- id: go-defer-01
  answer: |
    Arguments to a deferred call are evaluated immediately when the `defer` statement runs. Multiple deferred calls execute in last-in, first-out order when the function returns or panics.

- id: go-defer-02
  answer: |
    `recover` is used inside a deferred function to catch a panic in the current goroutine and return the panic value. It has no effect outside deferred execution, does not recover panics in other goroutines, and should be called directly by the deferred function.

- id: go-defer-03
  answer: |
    Deferring `file.Close()` inside a long-running loop keeps all files open until the function exits, possibly exhausting descriptors. Instead, close each file explicitly after use or isolate each iteration in a helper function where defers run promptly.

- id: go-defer-04
  answer: |
    A deferred function can modify named return values, changing the function's effective result after the return statement begins. This is commonly used for panic recovery, wrapping errors, metrics, or ensuring cleanup state is reflected in the return.

- id: go-testing-01
  answer: |
    A table-driven test defines a slice of structs containing test name, inputs, and expected outputs, then loops over them with `t.Run` subtests. It is idiomatic because it concisely scales to many cases and keeps expected behavior readable.

- id: go-testing-02
  answer: |
    `t.Parallel()` marks a subtest as eligible to run concurrently with other parallel tests. Historically, combining it with a loop range variable could capture the shared loop variable and cause tests or goroutines to use the final iteration's value.

- id: go-testing-03
  answer: |
    `t.Cleanup` registers a function to run when the test and its subtests complete, useful for teardown. `t.Helper` marks a function as a test helper so failure reports point to the caller's line rather than the helper.
