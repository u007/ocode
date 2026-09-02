```yaml
- id: go-concurrency-01
  answer: |
    An unbuffered channel has no storage: a send blocks until another goroutine
    is ready to receive (a rendezvous/handoff), and a receive blocks until a
    sender arrives. A buffered channel has capacity c: a send succeeds without
    blocking while the buffer has space, and blocks (the sender is parked) only
    when the buffer is full; a receive blocks only when the buffer is empty.
    So on an unbuffered channel send always waits for a receiver; on a buffered
    channel send waits only when len(ch) == cap(ch).

- id: go-concurrency-02
  answer: |
    select lets a goroutine wait on multiple channel operations at once; it
    blocks until exactly one case can proceed, and if several are ready it
    picks one uniformly at random (no priority). A default case makes the
    select non-blocking: if no other case is ready, the default runs
    immediately (useful for try-send/try-receive or polling). If a select has
    no ready case and no default, it blocks indefinitely — it will unblock
    when some case becomes ready (or a channel involved is closed, making a
    receive case proceed); if that never happens the goroutine deadlocks, and
    if the whole program is blocked the runtime panics with
    "all goroutines are asleep - deadlock!".

- id: go-concurrency-03
  answer: |
    Convention: only the sender (or a coordinator that owns all senders, e.g.
    after a WaitGroup proves no more sends will happen) closes a channel;
    receivers should never close it. Send to a closed channel panics; sending
    on a nil channel blocks forever; close of a nil or already-closed channel
    panics. Receive from a closed channel returns immediately with the zero
    value, and the two-value form `v, ok := <-ch` gives ok=false; a `for v :=
    range ch` loop terminates. Closing is used to signal "no more values are
    coming" — a broadcast that wakes all waiting receivers (e.g. to end
    workers or a range loop). Because close panics on double close and sends
    after close panic, channels with multiple senders need extra
    coordination, which is why close-by-sender is the rule.

- id: go-concurrency-04
  answer: |
    Before Go 1.22 the `for` loop's variable was a single variable reused
    (reassigned) across all iterations, and closures/goroutines capture the
    variable itself, not its value at that moment. Since goroutines usually
    execute after the loop has finished, they all observed the same variable —
    typically its final value. Go 1.22 changed the language so that each
    iteration of the loop declares a fresh instance of the loop variable, so
    each goroutine now captures its own per-iteration copy and sees its own
    value. (Pre-1.22 the workarounds were shadowing `i := i` or passing the
    value as a function argument.)

- id: go-sync-01
  answer: |
    A data race is two goroutines concurrently accessing the same memory
    location where at least one access is a write and the accesses are not
    ordered by synchronization; the result is undefined (in Go's memory model,
    a program with a race has no defined behavior for that access). Minimal
    correct protection for a shared counter: guard it with a
    `sync.Mutex` (`mu.Lock(); c++; mu.Unlock()`), or use
    `sync/atomic`: e.g. `atomic.AddInt64(&c, 1)` or an `atomic.Int64`
    whose methods do the load/add/store atomically.

- id: go-sync-02
  answer: |
    -race builds the program with instrumentation (compiler-inserted shadow
    memory around accesses) plus a runtime library that tracks
    happens-before relationships between accesses using vector clocks; when a
    conflicting unsynchronized access pair actually occurs at runtime it
    reports the race with both stacks. Limitations: it is dynamic — it only
    finds races that actually execute in that run, so it needs good test
    coverage and offers no static guarantee of race-freedom; it adds large
    overhead (roughly 5–10x memory and 2–20x CPU), so it is normally a
    test/diagnostic build rather than production; support varies by platform;
    and once a race is detected (default behavior) the program exits with a
    report rather than continuing. It also cannot prove absence of races
    anywhere else.

- id: go-sync-03
  answer: |
    Use sync/atomic when the shared state is a single word and the operation
    is one of the atomic primitives: increment/decrement, load, store,
    compare-and-swap — e.g. a counter, a flag, a once-only pointer swap. It is
    cheaper than a mutex (no goroutine parking, often a single CPU
    instruction) and avoids lock contention. Trade-offs: atomic operations are
    low-level, easy to misuse, and cannot protect multi-step invariants
    spanning several variables or more complex data structures — a Mutex (or
    channel) is required there. Atomics also make the synchronization implicit
    and scattered, so code is harder to reason about; the mutex version is
    usually clearer.

- id: go-sync-04
  answer: |
    Pattern: call `wg.Add(n)` in the parent before starting the goroutines
    (or `wg.Add(1)` immediately before each `go` statement), have each
    goroutine `defer wg.Done()`, and call `wg.Wait()` where the parent wants
    to block until all are finished. Common misuses: calling `wg.Add(1)`
    inside the goroutine body — Wait may observe the counter at zero and
    return before the Add happens (race); passing the WaitGroup by value to
    functions so Done operates on a copy and the original counter never
    reaches zero; calling Done more times than Add (panic: negative
    WaitGroup counter); and reusing/adding to a WaitGroup concurrently with
    Wait or after Wait has returned.

- id: go-errors-01
  answer: |
    `%w` wraps an error: `fmt.Errorf("doing x: %w", err)` returns an error
    whose Unwrap method yields err, so the original error chain is preserved
    and `errors.Is`/`errors.As` can traverse it. `%v` (or `%s`) merely formats
    err's text into the message — the result is an opaque error with no
    Unwrap, so identity and type information from err are lost. Rule of
    thumb: use %w when callers may need to inspect the cause; use %v if the
    cause is incidental detail.

- id: go-errors-02
  answer: |
    errors.Is(err, target) reports whether any error in err's unwrap chain
    *matches* target as a value (sentinel equality, or a custom `Is` method,
    possibly with extra matching like os.ErrNotExist semantics). errors.As(err,
    &target) walks the chain looking for the first error that *has the target's
    type* and assigns it to target, which must be a pointer to a type
    implementing error. Reach for Is when you care about a specific known
    error value; reach for As when you need a concrete error type to extract
    fields/methods from it (e.g. a *MyError with a Code field).

- id: go-errors-03
  answer: |
    A sentinel error is a predefined, exported error value that callers are
    expected to compare against, declared at package scope, typically:
    `var ErrNotFound = errors.New("not found")`. Callers should compare with
    `errors.Is(err, ErrNotFound)` — not with `==`, because == only checks the
    top-level error and misses wrapped chains; direct equality is exactly what
    errors.Is does anyway while also unwrapping. Returning wrapped context via
    `fmt.Errorf("...: %w", ErrNotFound)` keeps the sentinel matchable.

- id: go-errors-04
  answer: |
    An interface value is a (dynamic type, dynamic value) pair. Returning a
    typed nil — a `*MyError` whose pointer is nil — puts a non-nil *type*
    (*MyError) into the interface with a nil value, so the interface itself is
    not nil and `err != nil` is true at the call site. The classic bug:
    `func f() error { var e *MyError; ...; return e }` — on the success path
    e is nil but the returned error interface still holds type *MyError.
    Fixes: return an untyped nil explicitly on the success path, or have the
    function return a concrete `*MyError` and let callers convert, or build
    the pointer only in the error branch.

- id: go-interfaces-01
  answer: |
    Satisfaction is implicit: a type implements an interface by having all the
    interface's methods with matching names and signatures — no "implements"
    declaration needed, so even types from other packages can satisfy your
    interface. "Accept interfaces, return structs" means: declare function
    parameters as small, narrow interfaces so callers can pass anything
    conforming and you can easily substitute test doubles; but return concrete
    struct types so callers get the full usable API, you don't leak needless
    abstraction, and you can add methods later without breaking the contract.

- id: go-interfaces-02
  answer: |
    `any` is an alias (since Go 1.18) for `interface{}` — the empty interface
    with zero methods, which therefore every type satisfies; it's how you
    express "any value" (e.g. in containers, JSON decoding, fmt). A
    single-result type assertion `v := x.(int)` checks that x's dynamic type
    is exactly int and *panics* with a run-time error if it isn't (or if x is
    nil). The two-result form `v, ok := x.(int)` never panics: ok is false and
    v is the zero value when the assertion fails. Always use the comma-ok form
    when the dynamic type isn't guaranteed.

- id: go-generics-01
  answer: |
    Use generics when the algorithm/logic is identical for many types and you
    want full type safety while preserving the concrete type: containers and
    data structures (slice/map/set helpers), utility functions like Min/Max or
    Map/Filter, and cases where interface would force boxing, type assertions,
    or reflect. Use an interface parameter when you need behavioral
    polymorphism — dynamic dispatch on different implementations with
    different behavior — or when a small interface makes the dependency and
    test seams clearer. Generics abstract over *shapes* with one compiled
    instantiation; interfaces abstract over *behavior* at runtime. If the
    function body only calls methods declared by a small interface, an
    interface is often simpler.

- id: go-generics-02
  answer: |
    Shape:
    ```go
    func Map[T any, U any](s []T, f func(T) U) []U {
        out := make([]U, len(s))
        for i, v := range s { out[i] = f(v) }
        return out
    }
    ```
    Type parameters are declared in square brackets after the function name.
    A constraint is an interface that restricts which type arguments are
    allowed: it can require methods (like `constraints.Ordered`-style
    `cmp.Ordered`), and/or specify a set of permitted types via union syntax
    (`int | float64`) and approximation tokens (`~int` meaning any type whose
    underlying type is int). The compiler rejects calls with type arguments
    that don't satisfy the constraint.

- id: go-generics-03
  answer: |
    `comparable` is a predefined constraint permitting types that can be
    compared with == and != (all strictly comparable types: numbers, strings,
    pointers, channels, arrays/structs of comparable types). It's required for
    using a type as a map key or with == inside generic code. (Since Go 1.20,
    ordinary interface types also satisfy comparable, though comparing
    dynamic values that aren't comparable panics at runtime.) The `~` token in
    a constraint means "any type whose *underlying* type is this": `~int` is
    satisfied not only by int but by every named type defined as
    `type MyInt int`. Without `~`, the union element `int` matches only the
    exact type int.

- id: go-generics-04
  answer: |
    Type-argument inference is the compiler deriving the type arguments from
    the call's ordinary arguments and their types: given
    `func Map[T, U any](s []T, f func(T) U) []U` called as
    `Map(strs, func(s string) int { ... })`, the compiler infers T=string,
    U=int, so you write `Map(...)` instead of `Map[string, int](...)`. You
    must specify type arguments explicitly when inference can't determine
    them: when a type parameter is used only in the result type and not in any
    parameter (e.g. `func Fill[T any]() T`), when a type parameter appears
    only in a constraint that the arguments don't pin down, when arguments are
    untyped constants, or generally when inference is ambiguous — otherwise
    the compiler reports it cannot infer the type and you write them
    explicitly.

- id: go-context-01
  answer: |
    context cancellation does not and cannot stop a goroutine by force —
    there is no way to kill a goroutine in Go. Cancelling a context (via
    cancel(), timeout, or parent cancellation) closes the context's Done
    channel and makes Err return (context.Canceled/DeadlineExceeded). The
    running goroutine must cooperate: it watches `select { case <-ctx.Done():
    return ... }` or checks ctx.Err() at sensible points, and/or passes ctx to
    blocking calls that are context-aware (net/http requests, database
    queries). Responsibility for actually stopping lies with the goroutine
    (the code receiving the context); context's job is only to broadcast the
    signal.

- id: go-context-02
  answer: |
    WithCancel returns a context plus a cancel function you call manually to
    cancel early. WithTimeout(parent, d) and WithDeadline(parent, t) do the
    same but additionally arrange for automatic cancellation when the
    deadline passes — the deadline propagates to child contexts, whichever
    comes first. You must always call the returned cancel (idiomatically via
    `defer cancel()`) even on success/early return: failing to call it leaks
    the timer and keeps the child registered under its parent (and its
    resources alive) until the deadline fires — for long-lived parents and
    huge numbers of short requests this is a real memory/resource leak; go
    vet flags a lost cancel. cancel is safe to call multiple times.

- id: go-context-03
  answer: |
    WithValue attaches request-scoped key/value pairs to a context that
    travel down the call chain. It is intended only for values that span
    process/API boundaries and are genuinely per-request metadata — trace IDs,
    correlation IDs, authentication credentials, per-request loggers. It
    should NOT be used to pass optional parameters or business data to
    functions — that creates hidden, type-unsafe, invisible dependencies;
    such values belong in explicit function arguments. Keys should be a
    custom unexported type (never a plain string) to avoid collisions, and
    values should be safe for concurrent use.

- id: go-context-04
  answer: |
    Conventions: a context.Context is the first parameter of a function that
    uses, passes on, or could pass it on, named `ctx`; it is passed down
    explicitly through the call chain, never stored in a struct (barring rare
    exceptions) and not held beyond the request it belongs to. Never pass
    nil — pass context.TODO() when it's unclear which context to use
    (context.Background() at the top level / main / tests as the root);
    context.Context is immutable and safe for concurrent use; functions
    should not add parameters to take a context they ignore, and should
    propagate cancellation by honoring ctx.Done().

- id: go-slices-01
  answer: |
    A slice is a header (pointer, len, cap) pointing into a backing array.
    Multiple slices can share the same array (e.g. via s[low:high] or passing
    subslices around). If a slice still has spare capacity, append writes new
    elements into the shared array *in place* rather than reallocating — so
    those bytes are overwritten where another aliasing slice can see them
    (classic bug: append to a sub-slice clobbering the parent's elements). The
    return value is load-bearing because append may (once capacity is
    exhausted) allocate a new, larger array, copy, and return a slice header
    pointing there; if you discard the return (`append(s, x)` as a statement,
    or calling it through a non-updated variable), the original header can
    still reference the old array and you lose the appended data. Hence
    `s = append(s, x)` is the idiom.

- id: go-slices-02
  answer: |
    Nil map: reading (m[k], len(m), range, delete — delete is a no-op) is
    fine; a lookup returns the zero value. But *writing* — `m[k] = v` —
    panics at runtime ("assignment to entry in nil map"), because there is no
    hash table allocated; you must use make or a composite literal first.
    Nil slice: it is a valid, zero-length slice — len is 0, range and append
    work (append allocates), indexing within len (i.e. nothing) is fine, copy
    works, and passing it to functions like append/len is safe. So a nil
    slice behaves like an empty slice for reads, whereas a nil map
    distinguishes sharply: reads OK, writes panic.

- id: go-slices-03
  answer: |
    copy(dst, src) copies elements: min(len(dst), len(src)) elements from
    src's backing array into dst's backing array (overlapping regions are
    handled correctly). The result is that dst now contains *copied values* —
    mutating dst afterwards does not affect src. Slicing s[1:3], by contrast,
    only creates a new slice header (new pointer/len/cap) that points into
    the *same* underlying array — it is another view, not a copy: writes
    through either slice are visible through the other, and append near the
    boundary can even overwrite the parent's data. To get an independent
    slice you need copy into a fresh make'd slice (or append([]T(nil), s...)).

- id: go-slices-04
  answer: |
    Map iteration order is deliberately unspecified and randomized: Go
    intentionally varies the starting bucket and per-bucket offset per
    iteration so that no program can depend on it — order is not insertion
    order, not sorted, and can differ between two iterations of the same map
    in the same run. (If you need order, collect and sort the keys.) You
    cannot take the address of a map element because map elements are not
    addressable: the implementation may grow the map and rehash, moving
    elements to different buckets/positions in memory, so a stable pointer to
    an element would become invalid (dangling). This is also why struct
    fields inside maps can't be mutated in place (`m[k].f = v` won't
    compile) — you replace the whole value instead.

- id: go-defer-01
  answer: |
    The arguments of a deferred call — including the function value and
    receiver — are evaluated *immediately*, at the point the defer statement
    executes, even though the call itself is deferred until the surrounding
    function returns. So `defer f(x)` records the value x has right now.
    (Pointer indirections are dereferenced later, so `defer log(*p)` after
    taking &p logs the current value at return time, but `defer log(p)` logs
    the pointer.) Multiple deferred calls execute in last-in-first-out (LIFO)
    order — the most recently deferred runs first, like stacked cleanup.

- id: go-defer-02
  answer: |
    recover() re-ends a panic: when called directly inside a *deferred*
    function of a panicking goroutine, it returns the panic value and stops
    the panicking sequence, so the deferred function returns and the
    panicking function's named return values (if any) carry on; outside a
    deferred function — or when there is no panic — recover() returns nil and
    does nothing. Constraints: recover must be called directly in a function
    that was deferred (not nested deeper in another regular function call
    chain — it only works in the immediate deferred function body, or via
    `defer func(){ if r := recover(); ... }()`); it cannot catch a panic in a
    *different* goroutine — each goroutine must recover its own panics, and
    an unrecovered panic in any goroutine crashes the whole program. It
    cannot "resume" from where the panic occurred either — the panicking
    function still terminates (after deferred calls run).

- id: go-defer-03
  answer: |
    defer runs at the *function's* return, not the loop iteration's end. If
    you `defer file.Close()` inside a loop that opens files, every opened
    file stays open (each deferred Close is queued) until the enclosing
    function returns — in a long-running loop (server handling requests,
    processing millions of rows) you accumulate open descriptors and hit
    "too many open files" / fd exhaustion, and resources are held far longer
    than intended. Instead: close explicitly at the end of each iteration
    (`file.Close()` plus error check), or extract the loop body into a helper
    function where `defer file.Close()` returns at the end of a single
    iteration.

- id: go-defer-04
  answer: |
    If the function uses *named return values*, a deferred closure can read
    and assign those named result variables, since they are ordinary
    variables captured by the closure — the deferred code runs after the
    return value has been set but before control actually leaves, and its
    assignment becomes the final value returned. Common uses: implementing
    panic recovery — `defer func() { if r := recover(); r != nil {
    err = fmt.Errorf("panic: %v", r) } }()` to convert a panic into a
    returned error; and wrappers that log or instrument/annotate the error on
    the way out (e.g. adding context to err, metrics on failure). The
    function must have named results for this to be possible; with unnamed
    results the deferred function has no handle on the return value.

- id: go-testing-01
  answer: |
    A table-driven test defines a slice of anonymous struct cases — each with
    a name, inputs, and expected output — loops over them, and runs each case
    as a subtest with t.Run(tc.name, func(t *testing.T) { ... }), often with
    t.Parallel() inside. It's idiomatic because it separates test data from
    test logic (one test body, many scenarios), makes adding cases trivial,
    gives each case its own named, independently reported subtest (failures
    show exactly which case failed), enables parallel execution per case, and
    avoids repeated copy-pasted test functions.

- id: go-testing-02
  answer: |
    t.Parallel() inside a subtest marks that subtest to run concurrently with
    other parallel (sub)tests: the subtest is paused, control returns to the
    parent, and paused parallel subtests are resumed together only after the
    parent test function's sequential part finishes (the parent effectively
    waits at its end for parallel children). Historical pitfall: because
    parallel subtests resume *after* the loop that launched them has
    completed, they saw the loop variable's *final* value — all subtests
    testing the same last case — the same reuse-per-iteration issue as
    goroutines. The classic workaround was re-declaring inside the loop
    (`tc := tc`), and Go 1.22 fixed it by giving each iteration its own
    loop-variable instance.

- id: go-testing-03
  answer: |
    t.Cleanup registers a function to run after the test (or benchmark/fuzz)
    function and its subtests complete — a defer for tests, invoked even if
    the test panics or fails; it's especially useful inside helper functions
    (the helper can register the teardown right where it creates the
    resource, and can even be called in parallel tests where plain defer
    placement is awkward), e.g. removing a temp dir or stopping a server.
    t.Helper() marks the calling function as a test helper: when the helper
    fails the test (t.Fatal/t.Errorf) the reported file:line points at the
    *caller's* line in the real test rather than inside the helper, making
    failures traceable.
```
