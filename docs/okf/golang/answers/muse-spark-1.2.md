# golang knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: go-concurrency-01
  answer: |
    Unbuffered channel = capacity 0, no internal storage. Send blocks until a receiver is ready to receive and the handoff completes; receive blocks until a sender is ready. This provides synchronization/rendezvous.
    Buffered channel = make(chan T, N) with N>0. It has a queue of capacity N. Send blocks only when the buffer is full; receive blocks only when the buffer is empty. Otherwise both proceed without a counterpart being ready. Use unbuffered when you want guaranteed handoff/signaling, buffered to decouple/rate-limit/burst-absorb and avoid goroutine leaks, but buffer size must be reasoned about.

- id: go-concurrency-02
  answer: |
    `select` waits on multiple channel operations simultaneously. It blocks until one case can proceed (send or receive), then executes that case. If multiple cases are ready, it chooses one pseudo-randomly. 
    With a `default` case it becomes non-blocking: if no other case is ready, `default` executes immediately instead of blocking.
    With no ready case and no `default`, `select` blocks indefinitely until a case becomes ready. `select{}` (no cases) blocks forever.

- id: go-concurrency-03
  answer: |
    Rules: The sender (owner) should close the channel, never the receiver, and never from multiple concurrent senders without coordination. Closing a closed channel panics. Sending on a closed channel panics. Receiving after close: buffered values are drained, then receives return zero value with `ok == false` (comma-ok form) immediately; `for v := range ch` terminates.
    Why close: to signal "no more values will be sent" so receivers can detect completion via `ok` or `range` termination. You don't need to close to free GC; only to signal. Never close a channel to signal one receiver if other senders may still send.

- id: go-concurrency-04
  answer: |
    Before Go 1.22 `for i, v := range xs { go func(){ use(v) }() }` the loop variables `i`/`v` were single variables reused per iteration. Closures captured the variable, not its value, so by the time goroutines ran they all saw the final iteration's value.
    Workaround was `v := v` shadowing inside loop or passing as arg: `go func(v T){...}(v)`.
    Go 1.22 changed `for` semantics: each iteration now creates a fresh per-iteration variable, so each closure captures a distinct variable with the value of that iteration. No fix needed for new code, but old code relying on old sharing must be updated.

- id: go-sync-01
  answer: |
    A data race is when two goroutines access the same memory location concurrently, at least one is a write, and there is no synchronization (happens-before) ordering them. In Go it's undefined behavior even if races seem benign.
    Minimal correct counter:
    ```go
    var mu sync.Mutex
    var n int
    // or var n atomic.Int64
    // per increment:
    mu.Lock(); n++; mu.Unlock()
    // or atomic.AddInt64(&n, 1) / n.Add(1) with atomic.Int64
    ```
    Use `sync.Mutex`, `sync/atomic`, or serialization via a channel. Plain `n++` from many goroutines is racy.

- id: go-sync-02
  answer: |
    `go run/test/build -race` instruments memory accesses at compile time with ThreadSanitizer-like detector. At runtime it reports data races with stacks when two unsynchronized accesses race.
    Limitations: 5-10x slowdown and higher memory use; only detects races actually executed in that run - not exhaustive/sound; needs realistic concurrent workload; can have false positives due to instrumented libs; not for production; detects only data races, not logical races, deadlocks, or race-free but incorrect synchronization.

- id: go-sync-03
  answer: |
    Use `sync/atomic` for simple, single-word shared state: counters, flags, sequence numbers, single-pointer updates where you just need one atomic Add/Load/Store/Swap/CompareAndSwap. It is lock-free, faster under low contention, non-blocking.
    Trade-off vs `sync.Mutex`: atomic is limited to primitives and single-location atomicity; it cannot protect invariants across multiple fields or multi-step operations. Mutex can protect any critical section with arbitrary invariants, easier to reason about for complex state, but has scheduling/locking overhead and risk of deadlock/contention. Prefer mutex unless contention is proven or operation is trivially atomic.

- id: go-sync-04
  answer: |
    ```go
    var wg sync.WaitGroup
    wg.Add(n) // before goroutine starts
    for i:=0; i<n; i++ {
      go func(){
        defer wg.Done()
        // work
      }()
    }
    wg.Wait()
    ```
    Common misuses: 1) Calling `Add` inside the goroutine - races with `Wait`. 2) Copying `WaitGroup` by value (must pass by pointer). 3) `Add` with negative delta or `Done` more times than `Add` -> panic. 4) Reusing `WaitGroup` before prior `Wait` returns. `Add` must happen in the goroutine that will `Wait`, before `Wait`.

- id: go-errors-01
  answer: |
    `%w` in `fmt.Errorf("... %w", err)` wraps `err` as the error chain, storing it so `errors.Is`/`errors.As` and `errors.Unwrap` can traverse it. Only one `%w` per call and `err` must be an `error`.
    `%v`/`%s` just format the error's string via `Error()` into the message and lose the chain - unwrapping will not find the underlying error. Use `%w` when you want to add context while preserving inspection; use `%v` when you intentionally want to discard the chain and only keep text.

- id: go-errors-02
  answer: |
    `errors.Is(err, target)` reports whether any error in err's chain equals `target` (using `==` or custom `Is(error)bool`). For sentinels/value equality.
    `errors.As(err, &target)` reports whether any error in the chain can be assigned to the type of `target` (interface variable pointer) and if so sets `target`. For extracting typed errors to access fields/methods.
    Reach for `Is` to check for a specific known value like `io.EOF`; reach for `As` to handle a specific error type like `*os.PathError` or custom `*MyError`.

- id: go-errors-03
  answer: |
    A sentinel error is a package-level exported `var` representing a fixed error condition: `var ErrNotFound = errors.New("not found")` or `var ErrClosed = errors.New("closed")`.
    Callers should not compare with `err == ErrNotFound` directly if wrapping is used; instead use `errors.Is(err, ErrNotFound)` which traverses wrapping. Document sentinels as part of API.

- id: go-errors-04
  answer: |
    `error` is an interface (type, value) pair. Returning `var e *MyError = nil; return e` boxes a typed nil pointer into the interface: the interface value holds dynamic type `*MyError` and nil data pointer, which is non-nil as an interface (`err != nil`). Only an untyped nil `return nil` yields a nil interface (nil type, nil value).
    Fix: `if err != nil { return err }` where `err` is actually nil, or return `nil` explicitly, or ensure function returns plain `nil` on success not a typed nil. Check `if myErr == nil` before returning as `error`.

- id: go-interfaces-01
  answer: |
    Satisfaction is implicit: a type satisfies an interface if it implements the exact method set (name, params, results) - no `implements` declaration. Requires exact signatures; value vs pointer receiver matters for method set.
    "Accept interfaces, return structs" (concrete types): function params should be interfaces requiring only the behavior needed (max flexibility, easy mocking, loose coupling); returns should be concrete structs so caller can decide what abstraction to use and you don't leak premature abstraction. Returning interfaces hides implementation and forces interface.

- id: go-interfaces-02
  answer: |
    Empty interface `interface{}` (alias `any` since Go 1.18) has no methods, so any value satisfies it.
    Type assertion `v := x.(int)` (single-result) panics if dynamic type of `x` is not `int`. Two-result form `v, ok := x.(int)` does not panic: `ok` is false and `v` is zero value if assertion fails. Always use comma-ok or `switch x.(type)` when uncertain. Excessive `any` loses compile-time safety.

- id: go-generics-01
  answer: |
    Use generics (type parameters) when logic is identical regardless of type and type-safety/performance matters: containers (slices, maps, sets, stacks), algorithms (sort, min/max, map/filter), data structures, where you want to avoid reflection or `any`+assertions.
    Use plain `interface` when you need heterogeneous collections, runtime dynamic dispatch, or behavior varies per type (different method implementations). Interfaces abstract behavior; generics abstract types while keeping static typing. If you only call methods, interface is often simpler than a constraint.

- id: go-generics-02
  answer: |
    Shape:
    ```go
    func Foo[T Constraint](x T) T { ... }
    type Slice[T any] struct { elems []T }
    func Map[S ~[]E, E any, R any](s S, f func(E)R) []R
    ```
    `T` is a type parameter. `Constraint` is an interface that restricts allowed type arguments - e.g. `any`, `comparable`, `constraints.Ordered`, or union `int|int64|float64` or `~int`. Instantiation: `Foo[int](42)` or inferred `Foo(42)`. Constraints are checked at compile time; they also determine what operations you can use on `T` (e.g. `==` needs `comparable`).

- id: go-generics-03
  answer: |
    `comparable` permits types that support `==` and `!=` (booleans, numerics, strings, pointers, channels, arrays of comparable, structs with comparable fields; not slices, maps, funcs). Required for using `T` as map key or comparing.
    `~` in constraint like `~int` means underlying type: matches any type whose underlying type is `int` (including `type MyInt int`), not just exactly `int`. Without `~`, `type MyInt int` would not satisfy `int`.

- id: go-generics-04
  answer: |
    Type-argument inference lets the compiler infer `T` from ordinary arguments so you can omit explicit `[T]`: `func Min[T constraints.Ordered](a,b T) T` called as `Min(1,2)` infers `T=int`. Must specify explicitly when inference is ambiguous or impossible: no value arguments to infer from (e.g. `func New[T any]() T`), return-type-only inference not supported, partial inference not allowed, or you want a specific type different from inferred (e.g. `Parse[int]("42")`, `Make[MyInt](...)`). If inference fails, compiler asks to supply type arguments.

- id: go-context-01
  answer: |
    `context` does not preemptively kill goroutines. Cancellation closes `ctx.Done()` channel. Code that started the goroutine cancels via `cancel()` or timeout; the goroutine must cooperatively listen: `select { case <-ctx.Done(): return ctx.Err() ; default: }` or inside loops/selects. It is wholly the goroutine's responsibility to observe `Done`, stop work, release resources and return. If it ignores `Done`, it leaks/keeps running.

- id: go-context-02
  answer: |
    `WithCancel(parent)` returns ctx that cancels only when `cancel()` is called. `WithTimeout(parent, d)`/`WithDeadline(parent, t)` cancel automatically when deadline/timeout expires AND when `cancel()` is called.
    You must always call the returned `cancel` func (usually `defer cancel()`) even for Timeout/Deadline to release timer and goroutine that watches the deadline and to release parent reference, otherwise contexts/timers leak until deadline expires.

- id: go-context-03
  answer: |
    `WithValue(parent, key, val)` carries request-scoped data along a call chain - e.g. request ID, trace, authenticated user, deadline metadata - for handlers/middleware to retrieve via `ctx.Value(key)`.
    Should NOT be used for: optional function parameters, configuration, mutable state, passing data that could be explicit args, or as a bag for dependencies. Keys should be unexported types to avoid collisions. Values should be immutable and relevant to the request lifetime.

- id: go-context-04
  answer: |
    Conventions: `context.Context` is the first parameter of a function (`func Do(ctx context.Context, ...)`), named `ctx`. Never store `Context` in a struct; pass explicitly down the call chain. Don't pass nil - use `context.Background()` (main/init) or `context.TODO()` when unsure. Functions that respect cancellation should select on `ctx.Done()` and return `ctx.Err()`. Only use context values for request-scoped data, not control flow. Derive children with WithCancel/Timeout/Value and defer cancel.

- id: go-slices-01
  answer: |
    A slice is header {ptr, len, cap} over a shared array. `s2 := s1[2:5]` or `s2 := s1` share the same array. `append(s, x)` if `len < cap` writes into the next slot of that array and returns a longer slice; code holding another slice over same array sees the overwritten element even though it didn't append. If `len==cap`, append allocates new array and copies. Therefore `s = append(s, x)` return value is load-bearing: it holds new len and possibly new ptr/cap - ignoring it loses the growth/update.

- id: go-slices-02
  answer: |
    Nil map (`var m map[K]V`): read `v := m[k]` returns zero value, `v, ok := m[k]` with ok=false, no panic; iterating does nothing. Write `m[k]=v`, `delete(m,k)` on nil? delete is safe on nil, but assignment panics: `assignment to entry in nil map`.
    Nil slice (`var s []T`): len=0 cap=0 ptr=nil. Read `s[i]` panics (bounds). `append(s, ...)` works - allocates new array. Comparing len, ranging, slicing with valid bounds works like empty slice. `make(map)` and `make([]T,0)` init to usable empty; maps need init before writes, slices don't.

- id: go-slices-03
  answer: |
    `copy(dst, src)` copies `min(len(dst), len(src))` elements element-wise (shallow) from src into dst, returns count, handles overlapping correctly. It does not change slice headers' len.
    Slicing `a := s[1:3]` does not allocate/copy; it creates a new slice header pointing into the same underlying array (ptr = &s[1], len=2, cap = cap(s)-1). Modifying `a[i]` mutates `s[1+i]`. Independent copy needs `copy` or `append([]T(nil), s[1:3]...)` or `slices.Clone`.

- id: go-slices-04
  answer: |
    Go map iteration order is intentionally randomized per iteration; spec guarantees no defined order and a different random start per `for k,v := range m`. Never depend on order.
    You cannot take address of map element `&m[k]` because map is a hash table that may rehash/grow and move entries, which would invalidate pointer. Also map values are not addressable (unlike slices/arrays). Workaround: store pointers in map `map[K]*V` or copy value to variable.

- id: go-defer-01
  answer: |
    Deferred call's function value and arguments are evaluated immediately when `defer` statement executes, not when it runs. E.g. `defer f(a())` calls `a()` now and saves result for later `f`.
    Multiple defers run LIFO (last deferred, first executed) after surrounding function sets return values but before it returns to caller. Deferred closures capturing variables obey closure semantics (evaluated at run time).

- id: go-defer-02
  answer: |
    `recover()` regains control after `panic`. It only works when called directly inside a deferred function executed in the same goroutine that panicked. It returns the panic value, or nil if no panic or not recovering. After recover, panic is considered handled and program continues; otherwise unwinds. Constraints: must be `defer func(){ recover() }()` pattern, not in a nested helper unless that helper is the deferred func; only recovers panics in its own goroutine, not other goroutines; recover outside defer always returns nil.

- id: go-defer-03
  answer: |
    `defer file.Close()` inside a loop registers defers that don't run until the enclosing function returns, not per iteration. In a long loop that opens many files, you hold all files/descriptors open until end, exhausting FDs/memory.
    Fix: close explicitly at end of iteration `file.Close()` or wrap iteration body in a function/closure where defer runs per call:
    ```go
    for _, p := range paths {
      func(){ f, _ := os.Open(p); defer f.Close(); ... }()
    }
    ```

- id: go-defer-04
  answer: |
    If function has named return values `func foo() (n int, err error)`, deferred funcs run after the `return` statement assigns values but before caller sees them, and can modify named results.
    Common use: wrapping error, setting error on panic, completing named return:
    ```go
    func Do() (err error) {
      defer func(){ if err != nil { err = fmt.Errorf("do: %w", err) } }()
      ...
    }
    ```
    With unnamed returns, defer cannot affect them (it can only affect via closure over variable if assigned).

- id: go-testing-01
  answer: |
    Table-driven test defines a slice/map of struct cases `{name, input, want, wantErr}` and loops over them, running each as `t.Run(name, func(t *testing.T){...})`.
    Idiomatic because it reduces boilerplate, makes adding cases trivial, documents expectations compactly, gives uniform failure reporting, and encourages covering edge cases. Standard go tooling/reporting works well with it.

- id: go-testing-02
  answer: |
    `t.Parallel()` marks a test (or subtest) as able to run in parallel with other parallel tests; execution pauses until parent returns, then runs concurrently. Increases speed and exposes race assumptions.
    Historic pitfall: loop variable capture: `for _, tc := range cases { t.Run(tc.name, func(t *testing.T){ t.Parallel(); use(tc) })}` all subtests captured same `tc` variable (pre Go 1.22) and could race/see last case. Fix `tc := tc` inside loop. Must also not share mutable state across parallel tests.

- id: go-testing-03
  answer: |
    `t.Helper()` marks caller as helper function; failures inside it report file:line of caller, not helper itself, cleaning stack.
    `t.Cleanup(func())` registers a function to run after the test (and its subtests) finish, regardless of pass/fail, in LIFO order, after the test's deferred returns, even for parallel tests. For resource teardown (temp dirs, stubs). Prefer over `defer` inside tests because `Cleanup` runs after subtests finish and works with `t.Parallel`.
```
