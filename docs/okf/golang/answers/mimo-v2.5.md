- id: go-concurrency-01
  answer: |
    An unbuffered channel has zero capacity; a send blocks until another goroutine is ready to receive, and vice versa. A buffered channel has capacity N; a send blocks only when the buffer is full. So: unbuffered = synchronous handoff, buffered = asynchronous up to N items.

- id: go-concurrency-02
  answer: |
    `select` blocks until one of its cases can proceed, then executes that case. If multiple are ready, one is chosen at random. A `default` case makes `select` non-blocking — if no case is ready, the default executes immediately. With no cases and no default, the `select` statement blocks forever.

- id: go-concurrency-03
  answer: |
    Only the sender (or a coordinator) should close a channel. After close: sends panic, receives return the zero value (and a second bool is false if provided). Closing is used to signal "no more data" to receivers.

- id: go-concurrency-04
  answer: |
    The loop variable was shared by all iterations — all goroutines captured the same variable, so they saw the final value. In Go 1.22, each iteration of a `for` loop now gets its own copy of the loop variable, so goroutines see the correct per-iteration value.

- id: go-sync-01
  answer: |
    A data race is concurrent unsynchronized access to the same memory location where at least one is a write. The minimal correct protection for a counter is `sync.Mutex` (Lock/Unlock around reads/writes), or `sync/atomic` for simple integer operations.

- id: go-sync-02
  answer: |
    `-race` enables the race detector, which instruments memory accesses at compile time and reports unsynchronized concurrent access at runtime. Limitations: it only detects races that actually execute during the run, adds ~2–10× overhead, uses extra memory, and cannot detect races in pure channel-based synchronization or races that never occur during a particular execution path.

- id: go-sync-03
  answer: |
    Use `sync/atomic` for simple, low-contention integer or pointer operations where avoiding lock overhead matters. Use `Mutex` for protecting critical sections with complex logic, multiple variables, or higher contention. Trade-off: atomic is faster but limited to simple operations; Mutex is more flexible but has overhead.

- id: go-sync-04
  answer: |
    Call `wg.Add(n)` before launching goroutines, `wg.Done()` in each goroutine (via defer), and `wg.Wait()` in the caller. Common misuse: calling `wg.Add` inside the goroutine (race with Wait), calling Add after Wait, or forgetting Done (causes panic/leak).

- id: go-errors-01
  answer: |
    `%w` wraps the error, preserving its type for `errors.Is`/`errors.As` unwrapping. `%v`/`s` just call `.Error()` — they produce a string representation but don't preserve the error chain.

- id: go-errors-02
  answer: |
    `errors.Is(err, target)` checks if err or any wrapped error equals target (using `==` or `Is` method). `errors.As(err, &target)` checks if err or any wrapped error can be assigned to target (via type assertion). Use Is for sentinel errors, As for typed errors.

- id: go-errors-03
  answer: |
    A sentinel error is a pre-declared package-level error value (e.g., `var ErrNotFound = errors.New("not found")`). Callers compare against it with `errors.Is(err, ErrNotFound)`.

- id: go-errors-04
  answer: |
    Because a nil `*MyError` pointer is not a nil `error` interface. The interface holds (type, value) — when the concrete type is `*MyError`, the interface's type field is non-nil, so the interface itself is non-nil even though the value is nil.

- id: go-interfaces-01
  answer: |
    A type satisfies an interface by implementing all its methods — no explicit declaration needed (implicit satisfaction). "Accept interfaces, return structs" means: accept interface parameters for flexibility and testability, but return concrete types for usability.

- id: go-interfaces-02
  answer: |
    `any` is the empty interface (no methods) — every type satisfies it. `x.(int)` panics if x is not an int. The two-result form `v, ok := x.(int)` returns the value and a bool, avoiding the panic.

- id: go-generics-01
  answer: |
    Use generics when you need type safety across multiple concrete types with the same algorithm (e.g., a max function). Use interfaces when you need to work with any type that satisfies a behavioral contract (methods). Generics give compile-time type safety; interfaces give runtime polymorphism.

- id: go-generics-02
  answer: |
    ```go
    func Max[T constraints.Ordered](a, b T) T { ... }
    ```
    A constraint is an interface that specifies which type arguments are allowed (e.g., `constraints.Ordered` means types with `<`, `>` operators).

- id: go-generics-03
  answer: |
    `comparable` allows types that support `==` and `!=` (maps require this). `~int` means "any type whose underlying type is int" — it includes both `int` and named types like `type MyInt int`.

- id: go-generics-04
  answer: |
    Type-argument inference lets Go infer type arguments from function arguments (e.g., `Max(1, 2)` infers `int`). You must specify explicitly when inference is ambiguous or when there's no argument from which to infer (e.g., `func New[T any]() *T`).

- id: go-context-01
  answer: |
    When a context is cancelled (via its `Done()` channel), goroutines should check `ctx.Done()` in a select and return. It's the goroutine's responsibility to listen for cancellation; the parent cancels the context.

- id: go-context-02
  answer: |
    `WithTimeout`/`WithDeadline` set a time limit; `WithCancel` only cancels on explicit call. You must always call `cancel` to release resources (timer goroutines, channel references), even if the context already expired — otherwise you leak memory.

- id: go-context-03
  answer: |
    `WithValue` attaches request-scoped key-value pairs (e.g., request ID, auth token). It should NOT be used for function parameters or optional arguments — it's for values that flow through the call stack in a request context, not for passing data between functions.

- id: go-context-04
  answer: |
    Pass `ctx` as the first parameter of every function. Don't store it in structs. Don't pass nil — use `context.Background()` or `context.TODO()` as a placeholder. Cancel contexts in a LIFO order with defer.

- id: go-slices-01
  answer: |
    If two slices share the same underlying array (from slicing), appending to one can overwrite elements visible to the other if the append exceeds the original capacity and reallocates. `append`'s return value is load-bearing because the backing array may have changed — ignoring it means you may be writing to a stale/old array.

- id: go-slices-02
  answer: |
    Reading from a nil map returns the zero value. Writing to a nil map panics. A nil slice is valid to read from (returns zero values, length 0) and append to — it behaves like an empty slice that hasn't been allocated yet.

- id: go-slices-03
  answer: |
    `copy(dst, src)` copies `min(len(dst), len(src))` elements from src to dst. Slicing (`s[1:3]`) shares the underlying array — modifying one affects the other. To get an independent slice, use `copy` into a new slice.

- id: go-slices-04
  answer: |
    Map iteration order is intentionally randomized (not guaranteed). You can't take the address of a map element (`&m[k]`) because the map may resize/rehash internally, invalidating pointers — Go prohibits it to prevent dangling pointers.

- id: go-defer-01
  answer: |
    Arguments to a deferred call are evaluated immediately when the defer statement is reached. Multiple deferred calls execute in LIFO (stack) order — last deferred, first executed.

- id: go-defer-02
  answer: |
    `recover` catches panics. It can only be called directly inside a deferred function (not in a nested function). It only works on the goroutine that panicked — panics in other goroutines are not recoverable and will crash the program.

- id: go-defer-03
  answer: |
    Each loop iteration creates a new deferred Close, so all closes stack up and execute at function return — you may hold hundreds of open file descriptors. Instead, open and close inside the loop body, or use an anonymous function with defer inside the loop.

- id: go-defer-04
  answer: |
    A deferred function can modify named return values by using a pointer or the named return value directly (e.g., `func f() (err error) { defer func() { if r := recover(); r != nil { err = ... } }(); ... }`). This is commonly used for panic recovery, resource cleanup that sets error status, or logging errors.

- id: go-testing-01
  answer: |
    A table-driven test defines a slice of test cases (structs with input/expected) and iterates over them. It's idiomatic because it's readable, easy to add new cases, produces clear failure output (test name), and avoids duplicating test logic.

- id: go-testing-02
  answer: |
    `t.Parallel()` marks a subtest as safe to run concurrently with other parallel subtests. Historically, loop variables captured in closures shared the same variable — all parallel subtests saw the final value. Go 1.22 fixed this by scoping variables per iteration.

- id: go-testing-03
  answer: |
    `t.Cleanup` registers a function to run after the test finishes (like defer, but survives subtests). `t.Helper` marks a function as a test helper — when it fails, the error is attributed to the caller's line, not the helper's line.
