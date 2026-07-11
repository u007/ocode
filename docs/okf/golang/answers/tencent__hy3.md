# golang knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: go-concurrency-01
  answer: |
    A channel has an underlying buffer of fixed capacity. An unbuffered channel has
    capacity 0; a buffered channel has capacity > 0 set at creation (make(chan T, n)).

    On an unbuffered channel, a send blocks until another goroutine is ready to
    receive, and a receive blocks until another goroutine sends — the two sides
    rendezvous (synchronize). On a buffered channel, a send blocks only when the
    buffer is full, and a receive blocks only when the buffer is empty. So a send on
    a buffered channel proceeds immediately as long as there is free space; once
    full, it blocks until a receive drains it.

- id: go-concurrency-02
  answer: |
    `select` lets a goroutine wait on multiple communication operations (sends or
    receives on channels, or default). It blocks until exactly one of its cases can
    proceed, then executes that case's body. If several cases are ready
    simultaneously, one is chosen pseudo-randomly to avoid starvation.

    Adding a `default` case makes the select non-blocking: if no other case is ready,
    the default runs immediately instead of blocking. This is how you do a
    "try" / poll on a channel.

    If a select has no ready case and no default, it blocks forever (until some case
    becomes ready or the channel is closed). A select with only a default and no
    other cases is legal and just runs default each time.

- id: go-concurrency-03
  answer: |
    Closing a channel signals "no more values will be sent." Rules/conventions:
    - The sender (producer) should close the channel, not the receiver. Closing is
      meant to tell receivers the stream is done.
    - Only the sender should close; closing the same channel twice panics, and
      closing a nil channel panics. Receivers cannot and should not close it.
    - Sending on a closed channel panics. Receiving from a closed channel returns
      the remaining buffered values, then returns the zero value with the
      second "ok" boolean = false forever (no panic).
    - Receivers detect completion via the comma-ok form: v, ok := <-ch; ok==false means closed.
    Closing is used to broadcast completion to multiple consumers and to break out
    of range-over-channel loops.

- id: go-concurrency-04
  answer: |
    Before Go 1.22, the loop variable (e.g. `i` in `for i := range xs`) was a single
    variable reused across all iterations, and each goroutine captured a reference to
    that same variable. By the time the goroutines ran, the loop had typically
    finished and `i` held its final value, so every goroutine observed the same
    (last) value — the classic "all goroutines print the same number" bug.

    Go 1.22 changed loop semantics so that each iteration gets a fresh copy of the
    loop variable, effectively re-declaring it per iteration. Goroutines launched
    per iteration now capture distinct values, fixing the capture bug without needing
    the old workaround of passing the value as a function argument or copying it into
    a local variable.

- id: go-sync-01
  answer: |
    A data race is concurrent access to a shared memory location where at least one
    access is a write and the accesses are not synchronized, with no happens-before
    relationship. In Go it is undefined behavior and can produce corrupted values,
    panics, or different results across runs.

    The minimal correct way to protect a counter incremented by many goroutines is
    to guard every read-modify-write with a `sync.Mutex` (Lock/Unlock), or to use
    `sync/atomic.AddInt64` / `atomic.Int64` if the only operation is an atomic
    increment. A plain `counter++` without synchronization is a race.

- id: go-sync-02
  answer: |
    `go test -race` (and `go run/build -race`) enables the race detector, which
    instruments memory accesses to detect data races at runtime and reports them with
    stacks of the conflicting goroutines. It is the standard way to find races.

    Limitations: it only finds races that actually execute during the run (so test
    coverage matters); it adds significant CPU/memory overhead (roughly 2–10x slower,
    several times more memory) so it's unsuitable for production hot paths; it cannot
    detect races in C/cgo code or logic errors that aren't memory races; and it must
    be turned on explicitly (it's off by default).

- id: go-sync-03
  answer: |
    Use `sync/atomic` when the shared state is a single machine-word-sized value that
    you only need to read/modify/write atomically (counters, flags, pointers,
    timestamps). It's lock-free and far cheaper than a mutex. Use `sync.Mutex` when
    you must protect a group of operations or larger/non-atomic data, or want
    invariants spanning multiple fields.

    Trade-off: atomics are faster and avoid goroutine blocking, but they only
    synchronize the single variable and give no mutual exclusion over compound
    critical sections; reasoning about them is subtle and misuse silently breaks
    correctness. Mutexes are heavier but simpler to reason about for complex
    invariants.

- id: go-sync-04
  answer: |
    Create a `sync.WaitGroup`, call `wg.Add(n)` once for the n goroutines you'll
    launch, launch each goroutine, and call `wg.Done()` (or `defer wg.Done()`) inside
    each goroutine when it finishes; the launcher calls `wg.Wait()` to block until all
    are done. A common variant is `Add(1)` right before each `go func()`.

    Common misuses:
    - Calling `wg.Add` inside the goroutine instead of before launching it, so `Wait`
      may return before the goroutine even increments — race and premature return.
    - Forgetting `Add` before `Wait`, or passing the WaitGroup by value (it contains
      a no-copy mutex) instead of by pointer, causing copies and a "copy locks"
      panic/race.
    - Calling `Done` more times than `Add` → panic "negative WaitGroup counter".

- id: go-errors-01
  answer: |
    `%w` in `fmt.Errorf("...: %w", err)` wraps the underlying error: it stores it as
    the wrapped cause so the result implements `Unwrap() error`, enabling
    `errors.Is`/`errors.As` to traverse the chain. The formatting is otherwise like
    `%v` (prints the error's message).

    `%v`/`%s` just interpolate the error's string representation and discard the
    type/identity relationship — you can no longer use `errors.Is`/`errors.As` to
    match the wrapped error. Use `%w` when you want to preserve the cause for
    programmatic inspection; use `%v`/`%s` when you only need the message.

- id: go-errors-02
  answer: |
    `errors.Is(err, target)` reports whether err or any error in its unwrap chain is
    equal to the target (sentinel or specific value), using `==` and the `Is` method.
    Use it to check for a specific sentinel error or a particular error value
    regardless of wrapping.

    `errors.As(err, &target)` reports whether err or any error in the chain matches
    the *type* pointed to by target, and if so assigns the matched error to target.
    Use it when you need to extract a typed error (e.g. `*os.PathError`) to read its
    fields.

    Reachable: Is for identity/value comparison, As for type extraction.

- id: go-errors-03
  answer: |
    A sentinel error is a pre-declared, comparable package-level error variable used to
    signal a specific condition callers can check (e.g. `var ErrNotFound =
    errors.New("not found")`). It's declared once at package scope and shared.

    Callers should compare against it with `errors.Is(err, ErrNotFound)` (which also
    works through wrapping) rather than raw `==`, though the older `err ==
    ErrNotFound` works for unwrapped cases. Sentinel errors are for exceptional,
    well-known conditions; avoid overusing them for rich error data.

- id: go-errors-04
  answer: |
    `error` is an interface with an underlying (type, value) pair. When a function
    returns a typed nil pointer, e.g. `var p *MyError; return p` where the return type
    is `error`, the interface's dynamic type is set to `*MyError` (non-nil) while its
    dynamic value is nil. An interface is only equal to nil when BOTH type and value
    are nil. So `err != nil` is true even though the pointer is "nil," because the
    interface carries a non-nil type. Callers then wrongly treat it as an error. Fix:
    return a true `nil` (untyped) error value, or guard with `if p != nil { return p }`.

- id: go-interfaces-01
  answer: |
    A type satisfies an interface implicitly: if a type implements all of the
    interface's method set (same names, receiver types, and signatures), it is
    assignable to that interface — no `implements` declaration needed. Satisfaction is
    structural, checked at compile time.

    "Accept interfaces, return structs" is the Go idiom: function parameters should be
    the narrowest interface the function needs (so callers can pass anything that
    satisfies it, easing testing/mocking), while return values should be concrete
    types (so callers get full functionality and aren't locked into an interface that
    may later need more methods). There are exceptions, but it's the default guidance.

- id: go-interfaces-02
  answer: |
    The empty interface `interface{}` (alias `any` since Go 1.18) has no methods, so
    every type satisfies it. It's used for values of unknown type (e.g. `fmt.Println`
    params, `map[string]any`).

    The single-result type assertion `x.(int)` panics if `x` does not hold an `int`.
    The two-result form `v, ok := x.(int)` is safe: it returns ok=false instead of
    panicking when the dynamic type doesn't match, letting you handle the mismatch
    gracefully. Prefer the comma-ok form unless you're certain of the type and want a
    panic on mismatch.

- id: go-generics-01
  answer: |
    Use generics when you need to operate uniformly over many types while preserving
    type safety and avoiding boilerplate or `interface{}` + type assertions/casts
    (e.g. containers, slices/map helpers, a function that must return the same type it
    receives). Use a plain interface parameter when behavior is naturally
    polymorphic via methods (the type only needs to satisfy an interface) and you
    don't need to preserve the concrete type across input/output — interfaces are
    simpler and avoid instantiation complexity.

    Rule of thumb: generics for type-preserving algorithms/collections; interfaces for
    behavior-based polymorphism. Prefer the simplest thing that compiles; don't reach
    for generics when an interface suffices.

- id: go-generics-02
  answer: |
    A generic (type-parameterized) function shape:

        func Map[T any, U any](s []T, f func(T) U) []U { ... }

    The bracketed `[T any, U any]` declares type parameters T and U with constraints
    (`any`). A constraint is the interface (or `~` type set) that bounds what types
    are allowed for the type parameter — it defines which operations/methods the
    function may use on values of that type. `any` allows any type; a more specific
    constraint like `constraints.Ordered` or a custom interface restricts T to types
    supporting comparison/ordering/etc.

- id: go-generics-03
  answer: |
    `comparable` is a built-in constraint satisfied only by types that support the
    `==` and `!=` operators (e.g. basic types, arrays, structs of comparable fields,
    pointers, interfaces). It's required for operations like map keys or equality
    checks on a type parameter inside generic code.

    The `~` token in a constraint means "the underlying type is X," not just the exact
    type. `~int` matches any type whose underlying type is int (including `type
    Celsius int`), whereas a bare `int` would only match the predeclared `int`. `~`
    lets you write generic code over user-defined types with a given underlying type.

- id: go-generics-04
  answer: |
    Type-argument inference is the compiler's ability to deduce the type parameters
    from the (non-type) arguments you pass, so you can often omit the explicit
    `[T, U]` list, e.g. `Map(s, f)` infers T and U from s and f. This keeps generic
    calls as readable as ordinary calls.

    You must specify type arguments explicitly when they can't be inferred from
    arguments — e.g. the type parameter appears only in the return type, or there is
    no corresponding value argument (constructors like `New[int]()`), or inference is
    ambiguous. In those cases write `Foo[int](...)`.

- id: go-context-01
  answer: |
    `context.Context` carries a cancellation signal. Calling the `cancel` function
    (returned by WithCancel/WithTimeout/etc.) closes the context's internal done
    channel (`ctx.Done()`), and `ctx.Err()` becomes non-nil. Goroutines that respect
    the context poll `<-ctx.Done()` or pass the context to blocking calls (I/O,
    `select` with a done case, `ctx.Err()` checks) and then return.

    The context itself does NOT stop a goroutine — it only provides the signal.
    It is the goroutine's own responsibility to observe the signal (via Done/Err or
    context-aware APIs) and actually stop work and return. Cancellation is cooperative.

- id: go-context-02
  answer: |
    `WithCancel(parent)` returns a context plus a manual `cancel` func you call when
    you decide to stop. `WithTimeout(parent, d)` / `WithDeadline(parent, t)` create a
    context that auto-cancels after duration d or at absolute time t (returning the
    same cancel func). All three return a cancel function.

    You must always call the returned `cancel` (ideally via `defer cancel()`) even
    when auto-cancellation will fire, because it releases resources/timers and
    detaches the child context from its parent. Failing to call cancel leaks the
    timer and the child context subtree until parent cancellation or program exit.

- id: go-context-03
  answer: |
    `context.WithValue(parent, key, val)` returns a child context carrying a
    key/value pair, retrievable via `ctx.Value(key)`. It is intended for request-scoped
    values that flow through a call chain — request IDs, trace/span IDs, auth/tenant
    info, deadlines metadata — supplied once at the boundary and read by middleware or
    deep callers.

    It should NOT be used for: passing ordinary function parameters (it hides
    dependencies and breaks static typing/testability), passing optional config that
    could be real parameters, storing mutable shared state, or large/confidential
    data. Keys should be custom unexported types (not built-in strings) to avoid
    collisions.

- id: go-context-04
  answer: |
    Conventions:
    - Pass `context.Context` as the FIRST parameter of functions/methods that do I/O,
      blocking work, or call downstream code: `func Do(ctx context.Context, ...)`.
    - Never store a context in a struct field; pass it explicitly through the call
      chain (the one exception is some legacy APIs).
    - Use `context.Background()` at the top of a program/request entry point and
      `context.TODO()` only as a temporary placeholder when the context is not yet
      known.
    - Don't pass a nil context; if a function might be called without one, accept
      Context and have callers supply Background.
    - Propagate cancellation: derive child contexts from the incoming ctx so
      cancellation flows down, and call cancel to free resources.

- id: go-slices-01
  answer: |
    A slice is a header (pointer, len, cap) over a shared backing array. Two slices
    derived from the same array (via slicing or before a reallocation) alias the same
    underlying memory. `append` only reuses the backing array (writing into its free
    capacity) until len == cap; if another slice still references that array, the
    append writes into positions the other slice also sees, silently overwriting its
    data. Once capacity is exceeded, append allocates a new array and the original
    slice is unaffected — so behavior depends on cap, making it subtle.

    Because append may or may not allocate, its return value is load-bearing: you MUST
    reassign `s = append(s, x)`. Ignoring the return value drops the new length/cap
    (and any new backing array), so subsequent operations use the stale header and you
    "lose" appended elements.

- id: go-slices-02
  answer: |
    A nil map is a declared-but-uninitialized map (`var m map[K]V` or `m := map[K]V(nil)`,
    not via make). Reading from a nil map returns the zero value and is safe (no panic);
    writing to a nil map panics with "assignment to entry in nil map."

    A nil slice (`var s []T`, nil because its pointer is nil) is safe for both reading
    (len/cap are 0, range does nothing, indexing panics as with any out-of-range) and
    `append` — `append` on a nil slice simply allocates and works. So the key
    difference: nil maps panic on write but not read; nil slices are fully usable with
    append and safe to read (empty). To use a map you must `make` or literal-init it
    before writing.

- id: go-slices-03
  answer: |
    `copy(dst, src)` copies up to `min(len(dst), len(src))` elements from src to dst,
    starting at index 0 of each, into the existing backing arrays; it returns the
    number of elements copied. It does NOT allocate and does NOT change dst's length —
    only the first len(dst) slots can be filled, so leftover dst elements are
    untouched and excess src elements are dropped.

    Slicing `s[1:3]` does not create an independent slice: it produces a new header
    pointing into the SAME backing array (with adjusted pointer/len/cap). Mutating
    elements through either slice affects the shared array, so the two slices are
    aliases over the same memory, not copies. Use copy or `append([]T(nil), s...)`
    to get an independent backing array.

- id: go-slices-04
  answer: |
    Map iteration order in Go is deliberately randomized: each `for k, v := range m`
    starts at a pseudo-random bucket and order, specifically so programs don't come to
    depend on a stable order. Code must not assume any particular order; sort keys
    explicitly if you need deterministic output.

    You cannot take the address of a map element (`&m[k]`) because map entries can be
    relocated when the map grows/rehashes; a pointer would be invalidated, so the
    language disallows it (compile error). Instead take a pointer after copying into a
    variable, or address a slice element.

- id: go-defer-01
  answer: |
    The arguments to a deferred call are evaluated at the moment the `defer` statement
    is executed (not when the deferred function later runs). So `defer fmt.Println(x)`
    captures the value of x at defer time. The function body, however, runs later, at
    function return.

    Multiple deferred calls run in LIFO order — last deferred, first executed —
    like a stack. This makes defer useful for layered cleanup (e.g. lock/unlock,
    open/close) that unwinds in reverse order of acquisition.

- id: go-defer-02
  answer: |
    `recover()` stops a panic and returns the value passed to `panic`, but only when
    called directly inside a deferred function. If called outside a deferred function
    (or from a non-deferred context), it returns nil and does nothing.

    Constraints:
    - It must be invoked from within a `defer`ed function; that deferred function must
      be on the call stack of the goroutine that panicked.
    - It only recovers the panic in the CURRENT goroutine — a panic in one goroutine
      cannot be recovered by another goroutine's defer. Each goroutine must recover
      its own panics.
    - It returns nil if there is no panic in progress.

- id: go-defer-03
  answer: |
    Writing `defer file.Close()` inside a long-running loop defers each close until
    the surrounding function returns, not at loop iteration end. This leaks file
    descriptors / resources across potentially many iterations — you can exhaust the
    FD limit or hold locks/handles open — and the defers pile up until the function
    exits (which may be much later or never in a long-running loop).

    Instead, handle cleanup per iteration explicitly: open and immediately `defer`
    close within a scoped helper function (so it runs at that call's return), or
    call `file.Close()` directly (with error handling) at the end of each iteration,
    or wrap the body in a function so defer's LIFO scope is the iteration.

- id: go-defer-04
  answer: |
    A deferred function can read and modify named return values, because named
    results are allocated at function entry and the deferred call runs after the
    function body but before the actual return, with access to those variables. So a
    defer can transform the result, e.g. `defer func(){ if r := recover(); r != nil {
    err = fmt.Errorf("...") } }()` to convert a panic into a returned error.

    This is commonly used to: set/adjust a named `err` return (e.g. on panic),
    implement "try/finally"-style cleanup, or to log/annotate return values.
    Caveat: it only works with NAMED return variables; anonymous returns cannot be
    changed by defer (it changes the local copy that's then returned as-is, but you
    can't mutate an unnamed result).

- id: go-testing-01
  answer: |
    A table-driven test defines a slice of test cases (structs with inputs and
    expected outputs, often a name), then loops over them calling the same test logic
    for each case. Example: `tests := []struct{ in int; want bool }{...}; for _, tt :=
    range tests { got := f(tt.in); if got != tt.want { t.Errorf(...) } }`.

    It's idiomatic because it avoids duplicating boilerplate across cases, makes adding
    cases trivial, keeps expectations beside inputs, and pairs naturally with
    `t.Run(tt.name, ...)` subtests for isolated, parallelizable, named failures.
    (In Go 1.22+ the loop-variable capture pitfall no longer applies.)

- id: go-testing-02
  answer: |
    `t.Parallel()` marks a (sub)test to run concurrently with other parallel tests;
    the test runner pauses the parent until all parallel children call t.Parallel,
    then runs them together. Calling it in a subtest `t.Run("name", func(t
    *testing.T){ t.Parallel(); ... })` lets many subtests execute in parallel.

    Historical loop-variable pitfall: before Go 1.22, when launching parallel subtests
    inside a `for` loop, each closure captured the same loop variable, so every
    subtest ran with the final iteration's value. The workaround was copying the loop
    var into a local or passing it as a parameter; Go 1.22's per-iteration loop
    variables removed the bug.

- id: go-testing-03
  answer: |
    `t.Cleanup(f)` registers a function `f` to be called when the test (or subtest)
    finishes — like a deferred cleanup tied to the test lifecycle, run after the test
    body regardless of pass/fail and in LIFO order if multiple are registered. Useful
    for tearing down resources (temp dirs, servers) without manual defer chains.

    `t.Helper()` marks the calling function as a test helper, so that when it reports
    a failure via `t.Error`/`t.Fatal`/etc., the file:line reported points to the
    CALLER's location rather than the helper's internals — making failures readable
    when you wrap assertions in helper functions.
```
