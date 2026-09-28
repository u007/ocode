- id: go-concurrency-01
  answer: |
    An unbuffered channel has no capacity for stored values. A send on an unbuffered channel blocks until a receiver is ready to receive, and a receive blocks until a sender is ready to send. A buffered channel has a fixed capacity. A send on a buffered channel blocks only when the buffer is full; a receive blocks only when the buffer is empty. Unbuffered channels thus enforce synchronous rendezvous between sender and receiver, while buffered channels decouple them up to the buffer size.

- id: go-concurrency-02
  answer: |
    select waits until one of its channel cases (send or receive) is ready, then executes that case. If a default case is present and no channel case is ready, select executes the default immediately, making the operation non-blocking. If there is no default case and no channel case is ready, select blocks until at least one case becomes ready.

- id: go-concurrency-03
  answer: |
    The sender should close the channel, never the receiver, because the sender knows when no more values will be sent. Sending on a closed channel panics. Receiving from a closed channel returns the zero value immediately; a two-value receive (v, ok := <-ch) returns ok == false when the channel is drained and closed. Closing is used to signal to all receivers that no more values will be sent, allowing them to exit loops (e.g., range over a channel terminates when the channel is closed and drained).

- id: go-concurrency-04
  answer: |
    Before Go 1.22, a single loop variable was reused across all iterations, so goroutines that captured it by reference all saw the variable's final value after the loop completed. Go 1.22 changed the semantics so that each iteration of a for loop creates a new instance of the loop variable, so each goroutine captures its own copy. This was done via a loopvar change in the compiler and runtime.

- id: go-sync-01
  answer: |
    A data race occurs when two or more goroutines access the same memory location concurrently, at least one access is a write, and there is no synchronization (happens-before relationship) between them. The minimal correct way to protect a counter incremented by many goroutines is to use sync/atomic operations (e.g., atomic.AddInt64) or a sync.Mutex around the read-modify-write. Using a plain int with ++ from multiple goroutines without synchronization is a data race.

- id: go-sync-02
  answer: |
    The -race flag enables the Go race detector (ThreadSanitizer), which instruments memory accesses and goroutine synchronization to detect data races at runtime. Limitations: it only detects races that actually occur during the instrumented execution (it cannot catch races in untested paths); it adds significant memory and CPU overhead; it requires cgo to be enabled; it does not detect logical races or deadlocks, only actual unsynchronized concurrent accesses observed during execution.

- id: go-sync-03
  answer: |
    sync/atomic is used for simple, single-variable operations such as increments, compare-and-swap, load, and store on integers and pointers, where a full mutex would be unnecessary overhead. The trade-off is that atomic operations are harder to use correctly for complex invariants involving multiple variables or read-modify-write sequences that must be atomic as a group; a mutex is needed when the critical section involves multiple operations that must be performed together.

- id: go-sync-04
  answer: |
    Use sync.WaitGroup by calling Add(n) before launching n goroutines (or Add(1) before each), calling Done() when each goroutine finishes (often via defer), and calling Wait() to block until all goroutines have called Done(). The most common misuse is calling Add() inside the goroutine itself, which races with Wait() because Wait() may observe the counter at zero before the goroutine calls Add(). Another misuse is forgetting to call Done(), causing Wait() to block forever.

- id: go-errors-01
  answer: |
    The %w verb in fmt.Errorf wraps the error, producing a new error that carries the original error in its chain and supports errors.Is and errors.As traversal. %v formats the error using its Error() string method for display only, and %s formats the error as a string; neither wraps the error or creates a chain. %w can be used only once per Errorf call in Go versions before 1.20; Go 1.20+ supports multiple %w verbs.

- id: go-errors-02
  answer: |
    errors.Is(err, target) reports whether any error in err's chain matches target (using equality or an Is method). errors.As(err, target) finds the first error in err's chain that matches the type pointed to by target and assigns it to target. Use errors.Is when checking against a sentinel error value; use errors.As when you need to extract a typed error to inspect its fields.

- id: go-errors-03
  answer: |
    A sentinel error is a package-level error variable that represents a specific error condition, typically declared with errors.New or a var block. Callers compare against it using errors.Is (or == for direct comparison, though errors.Is is preferred). The convention is to export sentinel errors so callers can check for specific failure modes (e.g., ErrNotFound).

- id: go-errors-04
  answer: |
    This is the classic "nil interface vs nil pointer" issue. When a function returns a nil *MyError as an error interface, the interface value contains type information (*MyError) but a nil data pointer. Since the interface is non-nil (it has a type), comparing it to nil yields false. The fix is to return a literal nil error, not a nil typed pointer, or to check for the typed nil inside the function before returning.

- id: go-interfaces-01
  answer: |
    A type satisfies an interface implicitly in Go — no explicit declaration is needed. A type satisfies an interface if it implements all methods in the interface's method set. "Accept interfaces, return structs" means functions should accept interface parameters (making them flexible and testable with any implementation) but return concrete struct types (so callers know exactly what they get and can use type-specific methods without assertions).

- id: go-interfaces-02
  answer: |
    The empty interface, any (or interface{}), is satisfied by every type and can hold any value. The single-result type assertion x.(int) panics if x does not hold an int, which can crash the program. The two-result form v, ok := x.(int) does not panic — ok is false if the assertion fails, allowing safe handling. Always use the two-result form unless you are certain of the type and want a panic on mismatch.

- id: go-generics-01
  answer: |
    Use generics when you need to write a function or type that works with multiple types while preserving type safety and avoiding interface overhead (boxing, dynamic dispatch, and type assertions). Use a plain interface parameter when you need runtime polymorphism, when the set of types is small and heterogeneous with different behaviors, or when you want the simplicity of a single non-generic implementation.

- id: go-generics-02
  answer: |
    A generic function has the shape: func Name[T Constraint](args T) T { ... }, where T is the type parameter and Constraint is a constraint interface restricting which types T can be. A constraint is an interface (often embedding ~types or operators like comparable) that specifies the set of types a type parameter may be instantiated with. For example: func Max[T constraints.Ordered](a, b T) T { if a > b { return a }; return b }.

- id: go-generics-03
  answer: |
    The comparable constraint allows the use of == and != operators on values of the constrained type. It is satisfied by types that support equality comparison (not slices, maps, or functions). The ~ token in a constraint like ~int means "any type whose underlying type is int," allowing named types with int (e.g., type MyInt int) to satisfy the constraint, whereas plain int would only match the exact int type.

- id: go-generics-04
  answer: |
    Type-argument inference is when the Go compiler automatically infers the type arguments for a generic function from the types of its non-type-parameter arguments, so you can call Min(a, b) instead of Min[int](a, b). You must specify type arguments explicitly when: the function has no regular parameters to infer from (e.g., only type parameters); inference would produce an unintended type; or multiple type parameters have no inference source.

- id: go-context-01
  answer: |
    Context cancellation signals a goroutine by closing the channel returned by ctx.Done(). It is the goroutine's own responsibility to monitor ctx.Done() (or ctx.Err()) and return or clean up. Context cancellation is cooperative — the runtime does not kill or interrupt a goroutine; the goroutine must voluntarily check and respond to the cancellation signal.

- id: go-context-02
  answer: |
    context.WithTimeout and context.WithDeadline automatically cancel the context after the specified duration or at the absolute deadline, respectively. context.WithCancel creates a context that is only canceled when the returned cancel function is called. You must always call cancel to release the context's resources (parent association, timer, goroutine); failing to do so leaks the context until its parent is canceled or the deadline passes.

- id: go-context-03
  answer: |
    context.WithValue is for passing request-scoped metadata through the call chain, such as request IDs, authentication tokens, trace IDs, or locale information — data that flows alongside the request, not data the function needs to operate. It should NOT be used for passing optional function parameters (use regular arguments), passing dependencies (use parameters), or as a mechanism to avoid explicit API design.

- id: go-context-04
  answer: |
    Contexts should be passed as the first parameter of a function, conventionally named ctx context.Context. They should be threaded down the call chain through functions that need cancellation or request-scoped data. Contexts should not be stored in struct types (except in specific cases like methods that need to cancel a long-lived operation). Use context.Background() at the top level and context.TODO() as a placeholder. Do not pass a nil context.

- id: go-slices-01
  answer: |
    A slice is a header (pointer, length, capacity) over a shared underlying array. If two slices overlap in the same underlying array (e.g., created by slicing the same array), appending to one may overwrite data visible through the other if the append fits within capacity. append's return value is load-bearing because it may point to a new underlying array if the original was full; ignoring the return value means the caller keeps the old slice header, which may not reflect the appended data.

- id: go-slices-02
  answer: |
    A nil map: reading from it returns the zero value for the value type without panicking; writing to it panics (assignment to entry in nil map). A nil slice: reading its length or cap returns 0; ranging over it iterates zero times; appending to it allocates a new backing array and returns a new slice; indexing into it (s[i]) panics with index out of range. The asymmetry is that reads on nil maps are safe but writes are not, while nil slices support append but not indexing.

- id: go-slices-03
  answer: |
    copy(dst, src) copies min(len(dst), len(src)) elements from src to dst; it does not grow dst. Slicing s[1:3] creates a new slice header that shares the same underlying array as s — the new slice's pointer points into the original array at offset 1. Modifications through either slice are visible through the other because they reference the same backing memory. To get an independent slice, you must use copy or append to a new slice with sufficient capacity.

- id: go-slices-04
  answer: |
    Map iteration order in Go is intentionally randomized by the runtime to prevent code from relying on a stable order. You cannot take the address of a map element (&m[k]) because Go maps may grow and rehash during iteration or insertion, moving elements to new memory locations; allowing pointers to map elements would create dangling pointers after rehashing. Instead, copy the value or store pointers as map values.

- id: go-defer-01
  answer: |
    The arguments to a deferred function call are evaluated at the time the defer statement is executed, not when the deferred function actually runs (at the enclosing function's return). Multiple deferred calls run in last-in-first-out (LIFO) order: the most recently deferred call executes first when the function returns or panics.

- id: go-defer-02
  answer: |
    recover stops the panicking sequence in the current goroutine and returns the value passed to panic(). Constraints: it must be called inside a deferred function (a direct call has no effect); it only works in the goroutine that is panicking — calling recover in a different goroutine does not stop the panic; if recover is called outside a deferred function or in a function that is not panicking, it returns nil. It does not work across goroutines; a panic in a child goroutine crashes the program if not recovered in that same goroutine.

- id: go-defer-03
  answer: |
    Deferring file.Close() inside a long-running loop causes all open file handles to remain open until the enclosing function returns, exhausting file descriptors and holding resources. Instead, close the file explicitly before the next iteration, or refactor the loop body into a separate function so that defer Close runs when that function returns (at each iteration). Alternatively, use a helper function that opens, processes, and closes in one scope.

- id: go-defer-04
  answer: |
    A deferred function can change a function's return value if the return value is a named return variable. The deferred function can modify the named variable before the function actually returns to its caller. This is commonly used for: converting a panic into a return value (recovering and setting an error), adjusting return values on certain code paths, ensuring cleanup happens while also modifying the result, and unlocking or releasing resources after the return value is set but before the caller receives it.

- id: go-testing-01
  answer: |
    A table-driven test defines a slice (or map) of test cases, each containing inputs and expected outputs, then iterates over them with t.Run to create subtests. It is idiomatic because: adding a new test case is trivial (append to the table); the structure is consistent and readable; it reduces code duplication; subtests provide granular failure messages; and it naturally supports t.Parallel() for each case.

- id: go-testing-02
  answer: |
    t.Parallel() marks a subtest as parallel, causing it to pause and run concurrently with other parallel tests after all non-parallel tests have completed. The historical loop-variable pitfall was that parallel subtests captured the loop variable by reference, and before Go 1.22 all subtests saw the same (final) value of the loop variable, causing incorrect test behavior. The fix is to reassign the loop variable inside the loop (e.g., tc := tc) or upgrade to Go 1.22+.

- id: go-testing-03
  answer: |
    t.Cleanup registers a function to be called after the test and all its subtests complete, regardless of whether the test passes or fails. It is used for teardown (closing files, removing temp directories, stopping servers). t.Helper marks the calling function as a test helper, so that when it calls t.Errorf or t.Fatal, the reported file and line number point to the caller of the helper, not to the helper itself, making error messages more useful.
