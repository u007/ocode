- id: go-concurrency-01
  answer: |
    An unbuffered channel has no capacity: a send and receive must rendezvous, so a send blocks until a receiver is ready. A buffered channel has a fixed capacity: a send completes immediately if space is available and blocks only when the buffer is full. A receive from an empty buffered channel blocks.

- id: go-concurrency-02
  answer: |
    `select` waits until at least one channel or communication case is ready, then executes exactly one ready case; if several are ready, it chooses pseudo-randomly. A `default` case makes the select nonblocking and is chosen only when no other case is ready. Without `default`, a select with no ready case blocks the goroutine until one becomes ready.

- id: go-concurrency-03
  answer: |
    The component that owns a channel's sending lifecycle should close it after all sends are complete; receivers generally should not close it unless ownership explicitly says otherwise. Receiving from a closed channel continues to return buffered values until they are drained, then returns the element's zero value and `ok == false`. Sending on a closed channel panics, so all sends must be ordered before or prevented after closure. Closing communicates that no more values will be sent, releases blocked receivers, and terminates `range` loops.

- id: go-concurrency-04
  answer: |
    Historically, a loop variable declared by a `for` or `range` statement was one shared, reused variable, so closures and goroutines could all observe its final value. Starting with Go 1.22, each iteration receives its own loop-variable instance when the module's language version is Go 1.22 or later. The change also covers conventional three-clause loops, not only `range` loops.

- id: go-sync-01
  answer: |
    A data race occurs when goroutines access the same memory location concurrently, at least one access writes, and the accesses are not ordered by Go's happens-before rules. The result is then undefined; code that appears to work is not made correct. For a shared counter, the minimal protection is an atomic increment, such as `var n atomic.Int64; n.Add(1)`. A `sync.Mutex` around every access is also correct.

- id: go-sync-02
  answer: |
    The `-race` flag enables Go's dynamic race detector, which instruments memory accesses and reports data races observed during the run, typically with relevant goroutine stack traces. It can alter timing and has a performance cost. It only covers paths and executions actually exercised, so a clean run does not prove the program is race-free, and nondeterministic bugs may require multiple runs. It is not a detector for logic errors, deadlocks, or every possible interleaving.

- id: go-sync-03
  answer: |
    Use `sync/atomic` for a simple shared value—such as a counter, flag, pointer, or value updated and read independently—when lock-free-style operations and memory ordering are sufficient. It usually has lower overhead for simple operations. The trade-off is that atomics support only specific operations and types; compound updates, multi-field invariants, and arbitrary critical sections are clearer and safer with a mutex. Atomic is not automatically faster under heavy contention.

- id: go-sync-04
  answer: |
    Call `wg.Add(1)` before starting each goroutine, arrange `defer wg.Done()` inside it, and call `wg.Wait()` only after all tasks have been added:

    ```go
    var wg sync.WaitGroup
    for _, item := range items {
        wg.Add(1)
        go func() {
            defer wg.Done()
            process(item)
        }()
    }
    wg.Wait()
    ```

    A common misuse is putting `Add` inside the launched goroutine. `Wait` may then observe a zero counter and return before any task is counted, allowing unsynchronized use of supposedly completed work. Calls with a positive delta must occur before `Wait` starts when the counter can be zero. Extra `Done` calls, or copying and reusing a `WaitGroup` incorrectly, can panic.

- id: go-errors-01
  answer: |
    `%w` formats an error like `%v` and also wraps it, making the operand available through `errors.Unwrap`, `errors.Is`, and `errors.As`. `%v` and `%s` produce formatted text but do not establish an error chain. Modern Go also permits multiple `%w` operands, producing an error with multiple unwrap paths.

- id: go-errors-02
  answer: |
    `errors.Is` checks whether an error, or an error in its chain, matches a particular target error; it is appropriate for sentinel errors and other matching semantics. `errors.As` searches the chain for an error assignable to a specified concrete or interface type and assigns it to a target. For example, use `errors.As(err, &target)` when recovering a `*MyError` and its fields.

- id: go-errors-03
  answer: |
    A sentinel error is a package-level error value used as a stable classification or condition, commonly declared as `var ErrNotFound = errors.New("not found")`. Callers should use `errors.Is(err, ErrNotFound)`, not a direct `==` comparison, because the function may return an error that wraps the sentinel.

- id: go-errors-04
  answer: |
    An interface value contains both a dynamic type and a value of that type. Assigning a typed nil such as `(*MyError)(nil)` to `error` gives the interface the non-nil dynamic type `*MyError`, so the interface itself compares unequal to nil. The function should return an untyped `nil` on success, or otherwise arrange not to box a nil pointer into a non-nil error interface.

- id: go-interfaces-01
  answer: |
    A type satisfies an interface implicitly if its method set contains all methods required by that interface; no explicit declaration is needed. “Accept interfaces, return structs” means functions should accept the smallest interface that describes what they need, while constructors and APIs normally return concrete types, preserving access to their full method set and avoiding unnecessary interface coupling. Consumers often define their own narrow interfaces.

- id: go-interfaces-02
  answer: |
    `any` is an alias for the empty interface, which has no required methods, so every type implements it. An unchecked assertion such as `x.(int)` panics if the interface's dynamic value is not exactly an `int`. The safe form, `v, ok := x.(int)`, returns the zero value and `false` when the assertion fails, allowing the caller to handle another type or report an error.

- id: go-generics-01
  answer: |
    Use generics when operations or data structures handle a homogeneous collection or relation between input and output types while preserving compile-time type information, such as converting `[]T` to `[]U`. They avoid type assertions and can improve reuse and type safety. Use a plain interface parameter when values are heterogeneous, when only a small behavioral method set is required, or when the relationship between input and output types does not need to be represented.

- id: go-generics-02
  answer: |
    A generic function has type parameters before its value parameters, for example:

    ```go
    func Map[T, U any](values []T, fn func(T) U) []U
    ```

    A constraint defines the permitted type set and the operations or methods available for those type parameters. `any` permits every type while providing no useful type-specific operations; other constraints can require methods or permit a union of types.

- id: go-generics-03
  answer: |
    The predeclared `comparable` constraint permits type arguments to be used with `==` and `!=`. Slice, map, and function types cannot be type arguments because they are not comparable. Since Go 1.20, ordinary interface types may also satisfy it, although comparing interface values whose dynamic values are non-comparable can panic at runtime.

    The `~` token means “the underlying type is” and includes defined types as well as the unnamed type. Thus `~int` permits `int` and any locally defined type whose underlying type is `int`.

- id: go-generics-04
  answer: |
    Type-argument inference lets the compiler derive a function's type parameters from its arguments and, in applicable cases, the surrounding assignment or return context. For example, calling a generic `Map` with a `[]string` and a `func(string) int` infers both type arguments. Explicit arguments are needed when no parameter or surrounding context supplies enough information, such as `Make[int]()`, or when the caller wants to select a particular instantiation.

- id: go-context-01
  answer: |
    Cancellation closes the context's `Done` channel and makes `Err()` return `context.Canceled` or a deadline-related error. It does not forcibly kill or preempt an arbitrary goroutine. The operation using the context is responsible for observing `Done()`, checking `Err()`, or using APIs that accept the context, and then stopping work and returning promptly. Cancellation signals that work should stop; the operation decides how to stop safely.

- id: go-context-02
  answer: |
    `WithTimeout` and `WithDeadline` derive a context that is automatically canceled when its deadline arrives. `WithCancel` derives a manually cancelable context with no time limit. All return a `CancelFunc`; the derived context remains usable until the parent is canceled or `cancel` is called. The cancel function must always be called, usually with `defer cancel()`, to release timers and other resources and to tell descendant work to stop when an operation finishes early.

- id: go-context-03
  answer: |
    `context.WithValue` attaches a value to a context so request-scoped data can pass through call boundaries that do not otherwise accept it, such as trace or request identifiers. Keys should normally use an unexported custom type to avoid collisions. It should not be used for optional function parameters, replacing a required argument, or as a general-purpose bag of unrelated application state.

- id: go-context-04
  answer: |
    Conventionally, pass `ctx context.Context` as the first parameter of functions that perform cancellable work, and pass it downward to dependencies rather than storing it in ordinary structs. Do not pass a nil context; use `context.Background()` or `context.TODO()` only when no context is available. Derive deadlines or cancellation with `context.WithTimeout`, `WithDeadline`, or `WithCancel`, always call the cancel function, and use typed unexported keys for context values. Document which context controls an operation when there may be several sources of cancellation.

- id: go-slices-01
  answer: |
    Slicing does not copy elements, so two slices can refer to the same backing array. If a slice has spare capacity, `append` may write into that array rather than allocate, overwriting elements visible through another slice. `append` is load-bearing because it returns the updated slice header, including any newly allocated backing array. Code must use the returned value; the original slice header is not modified.

- id: go-slices-02
  answer: |
    Reading from a nil map is valid: a lookup returns the value type's zero value and `false` in the two-result form, `delete` has no effect, `len` is zero, and iteration performs no iterations. Assigning a new key to a nil map panics because no backing map exists. A nil slice has zero length and capacity, is safe to range over, and can receive values through `append`; indexing a nil or otherwise out-of-bounds slice panics.

- id: go-slices-03
  answer: |
    For slices, `copy(dst, src)` copies elements from `src` into the overlapping region beginning at `dst[0]`, up to `min(len(dst), len(src))`, and returns the number copied. It does not clone the backing array, and any extra capacity in `dst` remains unrelated. Slicing such as `s[1:3]` merely changes a slice header's starting pointer, length, and capacity; its elements still occupy the original backing array, so mutations are shared. Use an explicit copy when independent storage is required.

- id: go-slices-04
  answer: |
    Go deliberately does not define a useful map iteration order; entries may appear in a different order on every iteration. A map element is not an addressable variable, so `&m[k]` is illegal because the addressable location produced by indexing is not available. To mutate an element, use a pointer value stored in the map, copy the value out and back, or store mutable values in another structure such as a slice.

- id: go-defer-01
  answer: |
    The deferred function value and its arguments are evaluated immediately when the `defer` statement executes; the call itself occurs later. Multiple deferred calls run in last-in, first-out order. A deferred closure can still reference variables directly, and those variable references are evaluated when the closure eventually runs.

- id: go-defer-02
  answer: |
    `recover` stops the current panic and returns the value supplied to `panic`. It returns a value only when called directly by a deferred function while that same goroutine is unwinding from a panic; otherwise it returns nil. Calling it indirectly through another helper does not qualify. It cannot recover a panic occurring in a different goroutine, and an unrecovered panic in any goroutine normally terminates the program.

- id: go-defer-03
  answer: |
    The defer runs only when the surrounding function returns, so a loop that iterates many times can keep every file, lock, or other resource open until the entire function ends, potentially exhausting descriptors or retaining locks and memory. Put the loop body in a small helper function, call `defer Close()` inside that helper, and invoke the helper for each iteration. Alternatively, explicitly close each resource on every exit path while handling its error.

- id: go-defer-04
  answer: |
    A deferred closure can assign to a named result variable after the return value has been established but before the function actually returns:

    ```go
    func f() (err error) {
        defer func() {
            if r := recover(); r != nil {
                err = fmt.Errorf("recovered: %v", r)
            }
        }()
        // work
        return nil
    }
    ```

    This is commonly used to convert panics into errors and to record timing, logging, or cleanup information. It requires named results and careful handling of a nil `recover()` result.

- id: go-testing-01
  answer: |
    A table-driven test represents inputs and expected results as a slice of cases, then loops over the cases and runs each through a subtest such as `t.Run`. It is idiomatic because it reduces repeated test boilerplate, makes related cases easy to compare and extend, and produces focused names and failure locations for each scenario. Each subtest can receive independent setup, and parallel execution can be selected where safe.

- id: go-testing-02
  answer: |
    Calling `t.Parallel()` pauses the current subtest and tells `testing` to run it in parallel with other eligible parallel tests, subject to the `-parallel` limit. It does not run in parallel with a sequential test. A common historical pitfall occurs when a subtest closure captures a `range` loop variable: before Go 1.22, all closures could observe the reused variable's final value, especially because `t.Parallel()` delays their execution. Go 1.22 fixes this for Go 1.22 language versions; older code used `tc := tc`.

- id: go-testing-03
  answer: |
    `t.Cleanup` registers a cleanup function that runs when the test and all of its subtests have completed; multiple cleanups run in last-registered, first-executed order. It is useful for releasing temporary resources even when a test fails. `t.Helper` marks the calling function as a test helper, causing reported failures and relevant locations to be attributed to the caller rather than to an internal line in the helper.
