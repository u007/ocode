# cat-jam-free — closed-book answers for csharp (opencode-go, 2026-10-14); answerer saw only _prompts/csharp.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: csharp-null-01
  answer: |
    Nullable reference types are a compile-time feature. `#nullable enable` turns on nullable analysis/warnings for a file/project scope, `?` marks a reference as allowed to be null, and `!` suppresses a nullability warning. None of them change runtime behavior: the annotations are erased at runtime, and nullability is not enforced by the CLR.

- id: csharp-null-02
  answer: |
    Value types contain their data directly and are copied on assignment/passing; reference types store a reference to an object. Mutating a struct in a readonly field can be lost because the compiler creates defensive copies for method calls, so modifications do not persist. Mutating a struct stored in a collection also usually requires replacing the whole element because indexers return copies.

- id: csharp-null-03
  answer: |
    A record class is a reference type with synthesized value-based equality, `GetHashCode`, `==`/`!=`, `ToString`, and deconstruction; record members are often init-only. A `record struct` is a value type with similar synthesized equality and init-only positional members. A plain `class` uses identity/reference equality unless you override it and can be mutable by default.

- id: csharp-null-04
  answer: |
    `init`-only setters allow assignment during object construction/initialization but not afterward. `required` members force the caller to initialize that member in an object initializer (or constructor) for compilation. Combined, they create properties that must be set exactly once during initialization and then become read-only.

- id: csharp-pattern-01
  answer: |
    A switch expression evaluates and returns a value; a switch statement executes statements. For a switch expression, the compiler generally requires the arms to be exhaustive for the input type, otherwise it reports an error; use a discard pattern such as `_` as a general fallback.

- id: csharp-pattern-02
  answer: |
    Property patterns match an object by its named properties or fields, e.g. `{ Name: "A", Count: > 0 }`. Positional patterns match using deconstruction positions, e.g. `(0, var y)` or `Point { X: 0, Y: var y }`. Values can be bound with `var` or other subpatterns, making the bound variables available in the matching branch.

- id: csharp-pattern-03
  answer: |
    Relational patterns compare values with `<`, `>`, `<=`, `>=`. Logical patterns combine patterns with `and`, `or`, and `not`. List patterns match sequences, including their length and elements, and can use discard patterns, constants, property-like subpatterns, and slices such as `..`.

- id: csharp-pattern-04
  answer: |
    `obj is Customer c` performs the type test and declares a typed variable in one expression; the variable is only considered assigned on the true path but is scoped through the enclosing block. Plain type checking plus casting requires separate code and does not bundle scoping/definite-assignment behavior.

- id: csharp-linq-01
  answer: |
    Deferred execution means operations such as `Where`, `Select`, and `OrderBy` build a query and execute it when enumerated. Immediate execution materializes results with operators such as `ToList`, `ToArray`, `Count`, `Sum`, `First`, and `Aggregate`.

- id: csharp-linq-02
  answer: |
    `IEnumerable<T>` LINQ runs in memory using delegates. `IQueryable<T>` represents an expression tree that can be translated to a remote query, such as SQL. Mixing them can force premature client-side evaluation, losing translation and possibly loading or evaluating much more data than intended.

- id: csharp-linq-03
  answer: |
    Re-enumerating a LINQ query re-runs the whole pipeline, which can repeat expensive work, database queries, or iterator side effects and may observe changed source state. Avoid this by materializing the result once with `ToList` or `ToArray`.

- id: csharp-linq-04
  answer: |
    `First` throws if there is no match; `FirstOrDefault` returns the default value if there is none. `Single` requires exactly one match and throws otherwise; `SingleOrDefault` allows zero or one. The value-type gotcha is that `FirstOrDefault` cannot distinguish "no element" from a real default value such as `0` for `int`.

- id: csharp-async-01
  answer: |
    Use `ValueTask`/`ValueTask<T>` in hot paths when an operation frequently completes synchronously or can use pooled storage, reducing allocations. It must be consumed once: do not await it multiple times, cache it, or treat it like a normal `Task`; convert with `AsTask()` if repeated use is needed.

- id: csharp-async-02
  answer: |
    The compiler rewrites an `async` method into a state machine that splits code at `await` points and schedules continuations. Blocking with `.Result` or `.Wait()` can deadlock when the continuation must run on the current synchronization context while that same thread is blocked.

- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` tells the await to continue without capturing the current synchronization context. Library code should use it because it avoids forcing continuations back to the caller context, improving performance and reducing deadlock risk.

- id: csharp-async-04
  answer: |
    `CancellationToken` is cooperative: callers cancel a `CancellationTokenSource`, and async operations check or pass the token, throwing `OperationCanceledException` when cancelled. `IAsyncEnumerable<T>` produces an async stream, and `await foreach` consumes it lazily, allowing cancellation through the enumerator/token pairing.

- id: csharp-generics-01
  answer: |
    A `where` constraint restricts what type arguments may be used. Main kinds include `class`, `struct`, `unmanaged`, `new()`, a base class, one or more interfaces, another type parameter, `notnull`, and `allows ref struct`.

- id: csharp-generics-02
  answer: |
    `out` means covariance, allowing a type parameter to be used only in output positions and converting from more-derived to less-derived types. `in` means contravariance, allowing input-only positions and converting from less-derived to more-derived types. Variance applies to generic interfaces and delegates, not classes.

- id: csharp-generics-03
  answer: |
    Type inference infers a type argument from the compile-time types of method arguments. You must specify type arguments explicitly when there are no arguments of that type, inference is ambiguous, or the type appears only in the return type or other non-inferable context.

- id: csharp-generics-04
  answer: |
    `default(T)` produces the default value for the type: `null` for references, zero for numbers, `false` for `bool`, and zero-initialized fields for structs. Generic code needs it to return or initialize a meaningful neutral value without knowing the concrete type.

- id: csharp-delegate-01
  answer: |
    A delegate is a type representing a method signature and can reference compatible methods. `Func` delegates return a value, `Action` delegates return void, and `Predicate` delegates return `bool`; they are common built-in delegate families.

- id: csharp-delegate-02
  answer: |
    The `event` keyword restricts external access so callers can only subscribe or unsubscribe with `+=` and `-=`; they cannot invoke the delegate or replace the handler list directly. It also generates add/remove accessors for the underlying delegate.

- id: csharp-delegate-03
  answer: |
    In a `for` loop, a lambda capturing the loop variable shares that variable and sees its final value after the loop ends. Lambdas are safe when the variable is declared inside the loop body per iteration, or in modern `foreach`, where each iteration has its own variable.

- id: csharp-delegate-04
  answer: |
    A multicast delegate has an invocation list of multiple methods. Invoking it calls each target in order; for a non-void delegate, the returned value is the last method's return value. If a target throws, later targets are not invoked and the exception propagates.

- id: csharp-dispose-01
  answer: |
    `IDisposable` provides deterministic cleanup through `Dispose()`. A `using` statement scopes the resource in a block, while a `using` declaration disposes it at the end of the enclosing scope; both ensure `Dispose` is called.

- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` and `await using` are for resources that need asynchronous cleanup. Use them over `IDisposable` when disposal involves I/O or other awaitable work, especially in async methods.

- id: csharp-dispose-03
  answer: |
    A finalizer is non-deterministic cleanup run by the GC thread; `Dispose` is deterministic and caller-controlled. A type needs a finalizer only when it directly owns unmanaged resources; otherwise prefer wrapping them in managed disposable types such as `SafeHandle`.

- id: csharp-dispose-04
  answer: |
    The full pattern implements `IDisposable`, adds `protected virtual void Dispose(bool disposing)`, disposes managed members when `disposing` is true, releases unmanaged resources always, calls `GC.SuppressFinalize(this)` from `Dispose()`, and provides a finalizer that calls `Dispose(false)`.

- id: csharp-span-01
  answer: |
    Collection expressions create a collection with a literal such as `[1, 2, 3]`; the compiler infers the concrete collection type from context. The spread element `..` expands another collection into the expression.

- id: csharp-span-02
  answer: |
    `Span<T>` and `ReadOnlySpan<T>` are stack-only views into memory; `Memory<T>` is a heap-storable wrapper around memory. A `Span<T>` cannot be stored in a field or used across an `await` because it is a ref struct bound to the stack and could not safely survive suspension or heap storage.

- id: csharp-span-03
  answer: |
    `stackalloc` allocates memory on the stack, often as a `Span<T>`, avoiding heap allocation and GC pressure. You must avoid large sizes that can overflow the stack, avoid untrusted/unknown sizes, and ensure the memory is not used after the method returns.

- id: csharp-span-04
  answer: |
    An array `T[]` is a fixed-length reference object. `List<T>` is a resizable collection with extra API and possibly spare capacity. `params` collections let a method accept a variable number of arguments, often via an array or newer collection parameters, and can be called with collection expressions or spread.
