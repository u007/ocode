- id: csharp-null-01
  answer: |
    Nullable reference types are a compile-time null-safety contract: they let the compiler distinguish possibly-null and non-null references, produce warnings, and perform flow analysis. For a reference type, the `?` annotation has no runtime enforcement and does not create a different runtime type, though nullable metadata may be emitted. `!` is the null-forgiving operator: it suppresses the compiler warning but performs no runtime check, so dereferencing a null value can still throw `NullReferenceException`. `#nullable enable` establishes an annotation and warning context for the code; it changes compilation diagnostics, not runtime behavior. `T?` for a value type normally represents `Nullable<T>`, which does have a runtime representation.

- id: csharp-null-02
  answer: |
    A value type stores its data inline and is copied when it is assigned or passed; changing that copy normally does not change the original. A reference type stores an object reference, so copying the reference means both variables refer to the same mutable object. Structs stored in a `readonly` field cannot normally be replaced or directly modified, and mutating operations may be rejected or use a defensive copy. A value-returning collection indexer, such as `List<T>`'s indexer, also returns a struct copy, so changing `list[index].Field` can affect only a temporary or be rejected. Write the modified struct back, or use a reference- or `ref`-returning API when appropriate.

- id: csharp-null-03
  answer: |
    `record class` is a reference type with compiler-generated, value-based equality, `with` support, and generated formatting and deconstruction behavior. `record struct` has the same kinds of generated semantics but is a value type, so it is copied and can be used without nullable reference concerns. Neither form is automatically immutable: a record can contain mutable fields or settable properties, and positional record properties are commonly `init`-only for record classes. A plain `class` has reference equality unless it explicitly overrides equality, and it does not receive the compiler-generated `with` or structural-equality behavior of a record.

- id: csharp-null-04
  answer: |
    An `init`-only property can be assigned during object construction, in an object initializer or a constructor, and in an appropriate `with` expression, but normally cannot be assigned after construction. It is a compiler-enforced initialization phase, not a separate runtime access mechanism. A `required` member is a construction-time compiler obligation: callers must initialize it through an object initializer or a constructor, although it does not have to be `init`-only. Together, a declaration such as `required string Name { get; init; }` requires the property to be supplied at construction while preventing ordinary later assignment through that property.

- id: csharp-pattern-01
  answer: |
    A `switch` statement is a control-flow statement containing cases, labels, `break` or `goto`, and a `default` section. A switch expression is an expression whose arms map an input value to result expressions; it has implicit non-fallthrough behavior and can be assigned or returned. A switch expression must be exhaustive for its input type, or it must include a discard arm such as `_`; otherwise compilation fails. The statement form does not require exhaustiveness.

- id: csharp-pattern-02
  answer: |
    A property pattern tests named readable members and can combine tests at several levels, for example `p is { Length: > 0, Name: { Length: >= 3 } }`. A positional pattern uses a positional syntax such as `p is Point(int x, int y)` and matches by invoking an accessible `Deconstruct` method, or the built-in tuple deconstruction when applicable. The variables introduced by `var`, named subpatterns, or positional parameters are pattern variables bound to the matched values and are available within the pattern's scope.

- id: csharp-pattern-03
  answer: |
    Relational patterns use the relational operators `<`, `<=`, `>`, and `>=` to compare a value with compatible operands, such as `age is >= 18 and < 65`. Logical patterns use `and`, `or`, and `not` to combine or negate other patterns, with parentheses available for grouping. List patterns match compatible sequences by position, for example `[1, 2]`, `[1, ..]`, or `[.., var last]`; a slice (`..`) represents an unspecified number of elements, and `_` discards an element.

- id: csharp-pattern-04
  answer: |
    `obj is Customer c` performs a type test and, when it succeeds, binds `c` to the same matched object without a separate cast. The compiler's flow analysis then knows that `c` is definitely assigned and has the narrowed type in the successful branch, avoiding the repeated test-and-cast pattern of `obj is Customer` followed by `(Customer)obj`. The pattern variable is scoped to the surrounding block or statement and is usable where the match is known to have succeeded; it is not a definitely assigned value on paths where the match may fail, such as the false branch of an `if` or after the `if` without further analysis.

- id: csharp-linq-01
  answer: |
    Deferred execution means a LINQ operator stores a description of the query and normally does no element processing until the sequence is enumerated. Operators such as `Where`, `Select`, `OrderBy`, and `Take` are deferred when used on an `IEnumerable<T>`. Immediate or terminal operators such as `ToList`, `ToArray`, `ToDictionary`, `Count`, `Any`, `First`, and `Aggregate` enumerate the source and execute the query. Enumerating a deferred query again generally executes it again.

- id: csharp-linq-02
  answer: |
    `IEnumerable<T>` represents an in-memory sequence and LINQ-to-Objects operators generally execute ordinary C# code. `IQueryable<T>` extends `IEnumerable<T>` and also carries an expression tree and a provider, allowing a provider such as Entity Framework to translate the query into SQL or another remote query language. Mixing the two incorrectly, especially by inserting `AsEnumerable()` or materializing too early, can force a provider to download data and filter it locally, produce very large transfers, or reject operations the provider cannot translate. A deliberate `AsEnumerable()` or `ToList()` boundary should therefore mark the point where remote querying becomes client-side processing.

- id: csharp-linq-03
  answer: |
    A deferred LINQ query variable is a recipe rather than a stored result set. Enumerating it more than once reruns the operators and source enumeration, which can cause duplicate database round trips, repeat side effects, return different results after the source changes, or fail when the source is single-use or stateful. Materialize it once with `ToList`, `ToArray`, or another appropriate terminal operator when a stable snapshot is needed. If a fresh query is intended for each use, encapsulate the query construction in a method or factory instead of reusing one already-deferred sequence.

- id: csharp-linq-04
  answer: |
    `First` returns the first matching element and throws `InvalidOperationException` when there is no match. `FirstOrDefault` returns the first match or `default(T)` when there is none. `Single` requires exactly one matching element and throws if there are zero or multiple matches; `SingleOrDefault` returns the default only when there are none and still throws for multiple matches. The value-type gotcha is that `FirstOrDefault` on `int` returns `0` for an empty sequence, making an actual first value of zero indistinguishable from no result; the same issue applies to other structs, while a nullable value can represent the distinction.

- id: csharp-async-01
  answer: |
    `ValueTask` and `ValueTask<T>` are useful for hot paths or operations that are usually completed synchronously, where avoiding a `Task` allocation and supporting an efficient custom awaiter can matter. A `Task` is generally more convenient when a result may be awaited or observed by multiple consumers. A `ValueTask` instance should normally be consumed only once: do not await it multiple times, await it and then call `GetResult`, or otherwise inspect it repeatedly. If multiple consumers or repeated observation are needed, convert it with `AsTask()` once and use the resulting task.

- id: csharp-async-02
  answer: |
    The compiler transforms an `async` method into a state machine. The method begins executing synchronously until it reaches an incomplete `await`, then the state machine is suspended; later continuations resume it, and exceptions are generally captured in the returned task rather than escaping through the asynchronous call path. Calling `.Result` or `.Wait()` blocks the calling thread. If the awaited operation needs to resume the continuation on that same blocked context, such as a UI `SynchronizationContext`, the continuation cannot run and the wait can deadlock. Even without that deadlock, synchronous waiting can cause thread starvation; the normal solution is to use `await` throughout or deliberately move suitable work to a thread-pool context.

- id: csharp-async-03
  answer: |
    By default, `await` can capture the current synchronization context and resume the continuation there. `ConfigureAwait(false)` tells the awaiter that this continuation does not require the current context, so it may run on the completing thread or a thread-pool thread instead of being posted back to the captured context. This is recommended in reusable library code because the library should not assume that its caller has a UI or other special synchronization context, and it reduces context dependency and scheduling overhead. It does not make blocking safe, and UI or application code may still deliberately need to return to its context.

- id: csharp-async-04
  answer: |
    A `CancellationToken` is a cooperative cancellation signal supplied by a `CancellationTokenSource`. The source's `Cancel` method requests cancellation, while cancellable operations observe the token, often by checking `IsCancellationRequested` or calling `ThrowIfCancellationRequested`, and may register callbacks. Cancellation does not forcibly stop arbitrary code; operations that ignore the token can still complete, and a cancelled operation typically throws `OperationCanceledException` or a derived type such as `TaskCanceledException`. `IAsyncEnumerable<T>` represents an asynchronous sequence whose `GetAsyncEnumerator` provides asynchronous `MoveNextAsync` operations and a current value. `await foreach` consumes that sequence as it becomes available, can pass a cancellation token with `WithCancellation`, and is useful for streaming data without first buffering the entire sequence.

- id: csharp-generics-01
  answer: |
    A `where` constraint specifies compile-time requirements that a type argument must satisfy, allowing the generic code to use members, conversions, or construction operations that would otherwise be unavailable. Common constraints include `class` for reference types, `struct` for non-nullable value types, `notnull` to exclude nullable type arguments, `unmanaged` for unmanaged value types, an interface or base-class requirement, and `new()` for a public parameterless constructor. Multiple constraints can be combined, as in `where T : SomeBase, IDisposable, new()`. Constraints are checked by the compiler and support the implementation of generic algorithms.

- id: csharp-generics-02
  answer: |
    Generic variance describes a safe conversion between constructed generic types. `out` marks a covariant type parameter, which may occur only in output positions, so a type producing `Derived` can be used where one producing `Base` is expected; `IEnumerable<out T>` is the standard example. `in` marks a contravariant type parameter, which may occur only in input positions, so a consumer accepting `Base` can be used where one accepting `Derived` is expected; `Action<in T>` and `IComparer<in T>` are examples. Variance declarations apply to interface and delegate type parameters, not ordinary classes or method type parameters, and the conversions are safe at runtime for reference types.

- id: csharp-generics-03
  answer: |
    For a generic method, the compiler performs type inference from the argument expressions during overload resolution. It considers exact types first, then compatible conversions and bounds, and chooses a best common type when possible while validating the method's constraints. The return type normally does not provide enough information for inference. Explicit type arguments such as `Method<int>()` are needed when the method has no informative arguments, the candidates are ambiguous or no best candidate exists, inference cannot determine a type, or the programmer wants to select a particular closed generic method. Inference also has special rules for lambdas and anonymous types, so a call that looks obvious may still need an explicit argument.

- id: csharp-generics-04
  answer: |
    `default(T)` produces the default value for the type represented by `T`: zero or the corresponding zero-like value for a value type, and `null` for a reference type. For a nullable value type it produces a value with no underlying value. Generic code needs it because a single implementation cannot safely use `null` for every type, `0` is not valid for every type, and `new T()` requires a `new()` constraint and invokes a constructor. `default(T)` does not invoke a constructor.

- id: csharp-delegate-01
  answer: |
    A delegate is a type that describes a method signature and can hold a reference to a compatible instance method, static method, method group, lambda, or anonymous method. `Func<TResult>` and its overloads represent methods that return a result; `Action` and `Action<T...>` represent methods that return `void`; and `Predicate<T>` represents a method that accepts one argument and returns `bool`. They are reference types with an invocation list, so invoking a delegate calls its target method or methods.

- id: csharp-delegate-02
  answer: |
    An `event` encapsulates a delegate-like member so that code outside its declaring type can normally subscribe with `+=` and unsubscribe with `-=` but cannot invoke, overwrite, or assign the delegate. The declaring type can still invoke a field-like event and can define custom `add` and `remove` accessors, which permits validation, bookkeeping, or thread-safe subscription. A public delegate field exposes more power: outside code could replace the entire delegate or call it directly, bypassing the intended event abstraction.

- id: csharp-delegate-03
  answer: |
    A lambda captures the variable, not a snapshot of its value. In a traditional `for` loop such as `for (int i = 0; ...)`, `i` is one variable shared by all iterations, so lambdas that run after the loop can all observe its final value. Lambdas invoked immediately, before the variable changes, see the current value. In modern C#, a `foreach` iteration variable is created per iteration and is therefore safe for deferred lambdas; an explicitly copied local such as `var copy = i` is also safe.

- id: csharp-delegate-04
  answer: |
    A multicast delegate is a delegate whose invocation list contains multiple handlers. Adding handlers with `+=` or `Delegate.Combine` appends them, and invocation normally calls them synchronously in list order. If handlers return a value, the invocation expression exposes the return value of the last handler; earlier return values are discarded. If a handler throws, normal invocation stops at that exception and later handlers are not called unless the caller handles it and invokes the remaining list separately, often through `GetInvocationList`.

- id: csharp-dispose-01
  answer: |
    `IDisposable` is the standard contract for deterministic cleanup, normally implemented by a public `Dispose` method that releases resources and should be safe to call more than once. A `using` statement scopes a resource to a block and disposes it when the block exits, including when an exception occurs. A `using` declaration introduces a resource whose lifetime extends to the end of the enclosing scope, which is often more concise for a whole method. Both forms are compiler-generated cleanup constructs based on `try`/`finally`; multiple resources are normally disposed in reverse order.

- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` defines `ValueTask DisposeAsync()` for cleanup that can be asynchronous, and `await using` is the corresponding `using` form that awaits disposal when the resource leaves scope. It is appropriate for resources such as asynchronous streams, network clients, channels, or storage handles whose release may need asynchronous I/O. A resource with only synchronous cleanup should generally implement `IDisposable`; an async cleanup path should not be exposed merely to use the interface if it has no asynchronous work. `await using` can also fall back to synchronous `IDisposable` disposal for ordinary resources.

- id: csharp-dispose-03
  answer: |
    A finalizer is invoked nondeterministically by the garbage collector after an object becomes unreachable, subject to GC timing and process lifetime, and it must not depend on other managed objects still being available. `Dispose` is an explicit, deterministic cleanup operation and can safely release both managed and unmanaged resources. A type normally needs a finalizer only when it directly owns unmanaged memory and has no safer mechanism such as a correctly used `SafeHandle`; even then, the finalizer is a safety net and the type should provide `Dispose`. Managed-only types generally should not have finalizers because they delay collection and do not provide a needed guarantee.

- id: csharp-dispose-04
  answer: |
    The standard pattern keeps a guarded unmanaged handle or native pointer, exposes `Dispose`, and routes it through a protected virtual `Dispose(bool disposing)`. The public method calls `Dispose(true)` and then `GC.SuppressFinalize(this)`; the finalizer calls `Dispose(false)` and never accesses managed resources. In `Dispose(bool)`, the managed fields are disposed only when `disposing` is true, while the unmanaged resource is released in either case, the state is marked disposed, and derived classes call the base implementation. A finalizer such as `~Type()` should be avoided and release is delegated through the same guard when a `SafeHandle` is used. The cleanup should be idempotent, should not normally throw from a finalizer, and asynchronous resources may need a corresponding carefully guarded `IAsyncDisposable` implementation.

- id: csharp-span-01
  answer: |
    A collection expression is target-typed collection-initializer syntax introduced in modern C#, such as `[1, 2, 3]`. The target type determines whether the result is an array, `List<T>`, span, or another supported collection-builder type, so the expression does not necessarily imply one particular runtime type. The spread element `..` inserts the elements of another collection into the result, for example `[1, ..values, 4]`. It can be used to avoid an intermediate collection and can produce stack-backed spans for suitable inputs, while still preserving the target type's construction semantics.

- id: csharp-span-02
  answer: |
    `Span<T>` is a mutable, length-delimited view over contiguous memory; it can view an array, `stackalloc` storage, or suitably pinned memory. `ReadOnlySpan<T>` is the corresponding read-only view, while `Memory<T>` is an ordinary struct that can retain a memory reference and be stored, boxed, or carried across an `await` as long as the underlying memory remains valid. `Span<T>` and `ReadOnlySpan<T>` are `ref struct` types, so their values cannot be fields of ordinary classes or structs, cannot be boxed or captured, and cannot normally cross an `await` boundary. These restrictions prevent a stack-relative or tightly scoped view from being hidden in a heap object, closure, or async state machine; newer C# ref-struct async features may allow limited local use but still do not permit carrying the value across the boundary.

- id: csharp-span-03
  answer: |
    `stackalloc` reserves a fixed-size region of unmanaged memory on the current thread's stack, commonly exposed as a `Span<T>` or pointer. It is fast and is not tracked or reclaimed by the garbage collector, but the memory is valid only for the lifetime of the stack frame or a permitted synchronous scope. The requested size must be bounded and appropriate for the stack; large or attacker-controlled sizes can cause a stack overflow. The value must not escape through a returned span, field, closure, or asynchronous continuation, and stackalloc memory should not be assumed to be zeroed, so it must be initialized before being read.

- id: csharp-span-04
  answer: |
    An array `T[]` has a fixed length set when it is created, supports indexing and enumeration, and cannot be resized in place, although its elements can be changed. `List<T>` has a mutable size with `Add`, `Remove`, and capacity management; it is normally backed by a contiguous array that may be replaced and copied as it grows. Both are reference types, but retrieving a value-type element still copies that value, and a `List<T>` indexer does not expose an assignable struct variable. The traditional `params T[]` modifier lets a method receive zero or more arguments and the compiler normally bundles them into an array; modern `params` collection and span forms can avoid that separate array allocation. `params` modifies a parameter and is not itself a collection type.
