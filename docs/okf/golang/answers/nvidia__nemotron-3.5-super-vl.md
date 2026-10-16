# nvidia/nemotron-3.5-super-vl — closed-book answers for golang (openrouter, 2026-10-16); answerer saw only _prompts/golang.md
- id: go-concurrency-01
  answer: |
    An unbuffered channel has capacity 0: a send blocks until a receiver is ready to take the value, and a receive blocks until a sender is ready — it is a synchronous rendezvous that also synchronizes the two goroutines. A buffered channel has capacity N: a send blocks only when the buffer is full, and a receive blocks only when the buffer is empty. Sends on an unbuffered channel block until a receiver exists; sends on a buffered channel block only when there is no free slot.
- id: go-concurrency-02
  answer: |
    select waits on a set of channel operations (sends/receives). If more than one case is ready, Go chooses one uniformly at random; otherwise it executes the ready case. A default case makes select non-blocking: if no other case is ready, default runs immediately instead of waiting. If no case is ready and there is no default, select blocks until some case becomes ready (an empty select{} blocks forever).
- id: go-concurrency-03
  answer: |
    Only the sender should close a channel, and never an already-closed or nil channel (both panic). Receivers must not close. After close: sending panics; receiving returns the zero value immediately with ok == false, so ranged loops terminate. Closing is used purely as a signal that no more values will be sent, e.g. to tell consumers that work is done or that they should cancel.
- id: go-concurrency-04
  answer: |
    Before Go 1.22 there was a single loop-variable storage location reused across iterations, so goroutines launched in the loop captured the shared variable (effectively its address) and all observed the final value after the loop finished. Go 1.22 changed the language so each iteration gets its own instance of the loop variables; goroutines now capture the per-iteration value, so they see the value from their own iteration.
- id: go-sync-01
  answer: |
    A data race is when two goroutines access the same memory concurrently, at least one access is a write, and there is no happens-before ordering between them — the behavior is undefined. The minimal correct protection for a counter is sync/atomic (atomic.AddInt64 on the counter); a sync.Mutex around the increment is also correct and simpler for more complex operations, but heavier.
- id: go-sync-02
  answer: |
    -race builds and runs the program with Go's race detector, which instruments memory accesses and reports data races at runtime when an actual conflicting access happens. Limitations: it only races that are exercised by the running code (needs test coverage), it adds significant CPU/memory slowdown, it cannot detect deadlocks, and it only instruments Go memory (race conditions involving Cgo memory may be missed).
- id: go-sync-03
  answer: |
    Use sync/atomic for simple, single-word operations such as counters, flags, or pointer swaps where a full lock is unnecessary; use sync.Mutex when you must protect multi-field invariants or compound check-then-act sequences. Trade-off: atomic is faster, lock-free, and has no contention, but applies only to trivial operations and is easier to misuse subtly; a mutex is more expressive and safer for complex state but costs more and can block/contend.
- id: go-sync-04
  answer: |
    Call wg.Add(n) before launching the goroutines, have each goroutine call wg.Done() when it finishes (usually via defer), and call wg.Wait() to block until the counter returns to zero. Common misuse: calling Add after goroutines have started or concurrently with Wait (a race on the counter), calling Done too many times (counter goes negative and panics), forgetting Done (Wait blocks forever), or calling Wait before Add.
- id: go-errors-01
  answer: |
    %w formats the error like %v but also wraps it: the returned error supports Unwrap, so callers can use errors.Is and errors.As to walk the chain. %v and %s only call Error() and produce a plain string with no wrapping, so the original error identity is lost.
- id: go-errors-02
  answer: |
    errors.Is reports whether err equals the target or any error in its unwrap chain — use it to match sentinel errors. errors.As finds the first error in the chain assignable to a target pointer and sets it — use it to extract a concrete error type so you can inspect its fields. Reach for Is for yes/no matching, As for type-specific inspection.
- id: go-errors-03
  answer: |
    A sentinel error is a package-level error value, conventionally declared as var ErrX = errors.New("...") (e.g., io.EOF, sql.ErrNoRows). Callers should compare against it with errors.Is(err, ErrX) rather than ==, because == breaks when the error is wrapped.
- id: go-errors-04
  answer: |
    An error interface holds a (type, value) pair. If the function returns a typed nil pointer (*MyError)(nil) as error, the interface itself is non-nil because its type part is *MyError even though the value part is nil, so err != nil is true. Callers must not return typed nils as error; return an untyped nil instead.
- id: go-interfaces-01
  answer: |
    A type satisfies a Go interface implicitly — it just needs to implement all the interface's methods; no "implements" declaration is needed. "Accept interfaces, return structs" means functions should take interface types as parameters (flexible, decoupled) but return concrete struct types, so callers get concrete values and don't need type assertions.
- id: go-interfaces-02
  answer: |
    any is an alias for interface{}, the empty interface that can hold a value of any type. A single-result type assertion x.(int) panics if x does not hold an int; the two-result form v, ok := x.(int) returns the zero value and false instead, letting you check safely.
- id: go-generics-01
  answer: |
    Use generics (type parameters) when the algorithm works over many types while preserving type safety and avoiding boxing/allocations — e.g. generic containers, slices/maps helpers. Use a plain interface parameter when you need runtime polymorphism over heterogeneous types or only need behavior, not exact type. Generics avoid interface{} type assertions and the allocation/indirection they entail.
- id: go-generics-02
  answer: |
    Shape: func Name[T Constraint](params ...) T { ... } — type parameters are declared in brackets after the function name and used in the signature/body. A constraint is an interface that type arguments must satisfy (implement); any type argument used at the call site must implement all methods in the constraint.
- id: go-generics-03
  answer: |
    comparable is a predeclared constraint meaning the type supports == and != (and can be used as a map key or compared in generic code). The ~ token means "underlying type": ~int matches any type whose underlying type is int, including defined types like type MyInt int, not just int itself.
- id: go-generics-04
  answer: |
    Type-argument inference lets the compiler deduce type parameters from the function arguments at the call site, so you usually omit the brackets. You must specify type arguments explicitly when the compiler cannot infer them — e.g. calling a generic function whose type params appear only in the return value, assigning the function to a variable first, or when inference is ambiguous.
- id: go-context-01
  answer: |
    Cancellation is signaled by closing ctx.Done(); the goroutine must observe this (by selecting on ctx.Done() or polling ctx.Err()) and stop itself. The context does not forcibly kill the goroutine — it is the running code's responsibility to check the signal and return/clean up promptly.
- id: go-context-02
  answer: |
    WithCancel creates a cancelable context cancelled manually via the returned cancel; WithTimeout/WithDeadline cancel automatically after a duration or absolute time (they still return a cancel you must call when done early). You must always call cancel so the context and its children can be garbage-collected and resources (timers) released — otherwise the context leaks until its deadline.
- id: go-context-03
  answer: |
    WithValue attaches request-scoped data to a context so it travels across API and process boundaries — typical uses are trace IDs, user IDs, auth tokens. It should NOT be used for optional function arguments, core business data, or to replace explicit parameters; values should be immutable and only used where passing them explicitly is impractical.
- id: go-context-04
  answer: |
    Pass context.Context as the first parameter, named ctx, explicitly to every function that needs it (down the call chain). Never store it in a struct, never pass nil (use context.TODO() or context.Background() when unsure), don't store the cancel function in a struct, and always cancel contexts you create when you're done with them.
- id: go-slices-01
  answer: |
    A slice is a header (pointer, length, capacity) over a shared backing array, so two slices can point to the same array; appending within spare capacity writes into that shared array and can silently overwrite data another slice sees. append may also reallocate, so the result can have a different backing array — you must always use the returned slice, never the original header.
- id: go-slices-02
  answer: |
    Reading from a nil map returns the zero value (and ok == false in the two-index form); writing to a nil map panics. A nil slice has len/cap 0: reading an index panics only if out of range, len() is safe, and append to a nil slice works (it allocates). So nil-map write panics where nil-slice operations are generally safe unless indexed.
- id: go-slices-03
  answer: |
    copy(dst, src) copies element values from src into dst and returns the number copied, which is min(len(dst), len(src)). Slicing s[1:3] does not copy: it builds a new slice header over the same backing array, so changes through either slice are visible to the other (bounded by capacities).
- id: go-slices-04
  answer: |
    Go map iteration order is intentionally randomized and not guaranteed to be stable across runs. You cannot take &m[k] because a map element's address is not stable — the runtime may move entries during map growth — so taking the address would be unsafe.
- id: go-defer-01
  answer: |
    Deferred call arguments are evaluated when the defer statement executes, not when the enclosing function returns. Multiple defers run in LIFO order — the last defer registered runs first — when the function returns.
- id: go-defer-02
  answer: |
    recover() regains control of a panicking goroutine, returns the panic value (or nil if not panicking), and stops the panic sequence. It only works if called directly from a deferred function in the same goroutine that is panicking; it covers only that goroutine — each goroutine recovers from its own panics.
- id: go-defer-03
  answer: |
    defers run when the function returns, not at each loop iteration, so defer file.Close() inside a long loop leaves files open until the function exits, exhausting file descriptors. Close explicitly after each iteration (checking the error), or extract the iteration body into its own function so its defers run per call.
- id: go-defer-04
  answer: |
    If the function uses named return values, a deferred closure can read and modify them because they are in scope — e.g. changing a named error return based on later logic. This is commonly used for logging/metrics on return, wrapping errors, or recovering from panics and converting them into returned errors.
- id: go-testing-01
  answer: |
    A table-driven test defines a slice of case structs (input, expected, maybe name) and loops over them, typically calling t.Run(name, ...) per case. It is idiomatic because adding cases is just data, failures are isolated per subtest, cases can run in parallel, and the test stays readable and maintainable.
- id: go-testing-02
  answer: |
    t.Parallel() marks a subtest to run in parallel with other parallel subtests, pausing until parallel peers complete. Historically, calling it inside a loop captured the loop variable by reference (one shared variable), so all subtests saw the final value — the same loop-variable pitfall fixed in Go 1.22, where each iteration gets its own variable.
- id: go-testing-03
  answer: |
    t.Cleanup registers a function to run after the test (and its subtests) finish, even if the test failed — used for teardown such as closing files or resetting state. t.Helper marks the calling function as a test helper so stack traces and failure reports point at the caller, not the helper itself.
