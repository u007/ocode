- id: csharp-null-01
  answer: |
    Nullable reference types are a compile-time nullability-analysis feature introduced in C# 8. They let the compiler and IDE warn about dereferencing or assigning null where a non-null value is required. They do not change the CLR representation or prevent a reference from being null at runtime.

    `?` marks a reference-type annotation as nullable. `!` is the null-forgiving operator: it suppresses the relevant compiler warning or tells the compiler to treat an expression as non-null, without performing a runtime check or conversion. `#nullable enable` enables nullable warnings for the source scope; it has no runtime behavior.

    The proposed `!!` parameter-null-check operator for C# 11 was removed before release and does not exist in any shipped C# version.

- id: csharp-null-02
  answer: |
    A value type stores its data directly, so assignment, argument passing, and return values copy the value. A reference type stores a reference to an object, so copies of the variable generally refer to the same object; mutating that object through one reference is visible through the others.

    A value type's default is a zeroed, non-null instance: `0`, `false`, or an all-zero struct, and never `null` unless the type is `Nullable<T>`. A reference type's default is `null`.

    A mutable struct held in a `readonly` field is exposed as a defensive copy. Mutating that copy silently does nothing to the field. Likewise, `foreach (var s in list) s.X = ...` and `list[i].X = ...` for a value-returning `List<T>` indexer act on a copy of the element, not the element itself. An array element is different: `array[i].X = ...` changes the actual array element.

- id: csharp-null-03
  answer: |
    `record` is shorthand for `record class`, a reference type. Its compiler-generated positional properties are `init`-only by default, and the compiler provides value-based equality, `ToString`, deconstruction for positional records, and `with` expressions. The generated positional surface is immutable by construction, although other members of a record class can still be mutable.

    `record struct` is a value type with synthesized value equality and similar record features. Its positional members are mutable by default. Declaring a `readonly record struct` makes those positional members immutable.

    A plain `class` is a reference type with ordinary fields and properties that are mutable by default. It has no compiler-generated value equality, `with` expression, or record-style deconstruction; equality is reference-based unless the class overrides equality.

- id: csharp-null-04
  answer: |
    An `init`-only property, introduced in C# 9, can be assigned during object construction, object initialization, or a `with` expression, but not later through ordinary assignment.

    A `required` member, introduced in C# 11, must be initialized when constructing the type. It may be a field or property, and it does not by itself make the member read-only. Constructors marked with `SetsRequiredMembers` can satisfy the requirement for their callers.

    They combine naturally: `required string Name { get; init; }` requires every ordinary caller to supply a name during construction, while the property still cannot be reassigned afterward. A `required` property with a normal setter remains mutable after construction.

- id: csharp-pattern-01
  answer: |
    A `switch` statement controls control flow. Its cases can contain multiple statements, use `break`, `return`, `goto`, and other flow constructs, and may be non-exhaustive or omit `default`.

    A `switch` expression computes and returns one value using arms such as `pattern => expression`. It has no fall-through behavior, and its result is target-typed. Its arms must be exhaustive. If they are not, compilation fails; add a catch-all arm such as `_ => throw new InvalidOperationException()` or a default-producing arm.

- id: csharp-pattern-02
  answer: |
    A property pattern tests or examines accessible properties and fields, and can be nested. For example, `customer is { Name: { Length: > 0 } }` checks that `Name` is a non-null string with positive length. A `var` designation, such as `Name: var name`, binds the property value; `var customer` can bind the whole matched value.

    A positional pattern matches a type's positional components by calling an accessible `Deconstruct` method. For example, `point is Point(var x, var y)` extracts and binds the two components. Its subpatterns can themselves be type, property, relational, or other patterns, so positional and property patterns can be combined.

- id: csharp-pattern-03
  answer: |
    Relational patterns compare a value with a compatible constant or constant expression using `<`, `>`, `<=`, and `>=`, as in `x is > 0 and < 10`. Constant patterns can also test equality with values such as `null` or `"active"`.

    Logical patterns combine patterns with `and`, `or`, and `not`, with normal pattern precedence and parentheses where needed.

    List patterns, introduced in C# 11, match a sequence's length and elements. `[]` matches an empty sequence, `[var first]` matches at least one element, `[1, .. var rest]` requires a first element equal to 1, and `[var first, .., var last]` requires at least two elements and binds the first and last. The `..` slice matches zero or more elements. The target generally needs a suitable `Length` or `Count` and indexer; a bare `IEnumerable<T>` is not sufficient.

- id: csharp-pattern-04
  answer: |
    `obj is Customer c` performs a runtime type test and, if it succeeds, declares and assigns the variable `c` in the same operation. The test also establishes that the value is non-null and compatible with `Customer`, so the compiler can safely use `c` and avoid a separate cast.

    A plain `obj is Customer` followed by `(Customer)obj` repeats the work and does not directly bind the checked value. A declaration pattern also does not perform arbitrary user-defined conversions.

    The pattern variable's lexical scope is its containing block, but it is definitely assigned only on paths where the pattern matched. In an `if`, it is normally usable in the true branch, not the `else` branch. It is usable on the right side of `&&` when the left side establishes the match, but not on the right side of `||` unless separately proven.

- id: csharp-linq-01
  answer: |
    Deferred execution means constructing the query does not execute its operations. The query is represented by iterators, and execution begins when the result is enumerated, such as by `foreach` or `GetEnumerator()`. Operators such as `Where`, `Select`, `OrderBy`, `Take`, and `Distinct` are generally deferred.

    Immediate execution enumerates the source during the operator call and returns a scalar or materialized result. Examples include `ToList`, `ToArray`, `ToDictionary`, `ToHashSet`, `First`, `Single`, `Any`, `Count`, and `Sum`. A deferred query therefore runs again if it is enumerated again.

- id: csharp-linq-02
  answer: |
    LINQ over `IEnumerable<T>` generally uses LINQ-to-Objects and executes against objects already in the current process. LINQ over `IQueryable<T>` carries an expression tree and a provider; a database or other provider may translate the query and execute it remotely.

    Because `IQueryable<T>` inherits from `IEnumerable<T>`, it is easy to fall back to client-side execution. Converting with `AsEnumerable`, or materializing with `ToList` before applying more operators, prevents later translation and can pull many rows, cause performance or N-plus-one problems, or produce provider errors. It can be intentional, but should be an explicit boundary rather than an accidental fallback.

- id: csharp-linq-03
  answer: |
    A deferred LINQ query variable is a recipe for producing results, not a cached result. Enumerating it more than once reruns the pipeline and may issue another database query, repeat side effects, or observe changes made to the source between enumerations.

    If a stable snapshot is wanted, materialize it once with `ToList`, `ToArray`, or `ToHashSet`, then query the materialized collection. If live results are wanted, create or enumerate a fresh query deliberately and understand that each enumeration can perform work again.

- id: csharp-linq-04
  answer: |
    `First` returns the first matching element and throws `InvalidOperationException` when the sequence is empty. `FirstOrDefault` returns the first match, or `default(T)` when there is no match. `Single` requires exactly one element and throws if there are zero or multiple elements. Each has predicate overloads.

    For a value type, `default(T)` is often indistinguishable from a legitimate value: an absent `int` produces `0`, an absent `bool` produces `false`, and an absent struct is all-zero. Use a nullable value type such as `int?` where appropriate, or use an explicit existence or `Try` result when absence must be distinguished. `SingleOrDefault` has the same default-value ambiguity.

- id: csharp-async-01
  answer: |
    `ValueTask` and `ValueTask<T>` are useful when an operation completes synchronously very often, returns a cached result, or is implemented with a pooled `IValueTaskSource`. Their struct-based representation can avoid allocations that a `Task` might require, although they are more awkward to use and do not automatically guarantee better performance.

    Treat a `ValueTask` as single-consumption. It may be awaited once or have its result obtained once with its awaiter; do not await it twice, read its result multiple times, or store and reuse it as though it were a reusable `Task`. Convert it to a `Task` once with `AsTask` when multiple consumers or long-lived storage are needed. A default, unconsumed `ValueTask` is not a valid result.

- id: csharp-async-02
  answer: |
    The compiler transforms an `async` method into a state machine. It runs synchronously until an incomplete `await` is reached, then stores the method's state and registers a continuation. The returned task receives the eventual result, cancellation state, or exception. `await` itself does not create a worker thread; it allows the calling thread to return while the continuation runs later.

    `.Result` and `.Wait()` block the calling thread. In a UI application or another host with a single-threaded synchronization context, the async continuation may need to return to that same context. If the context thread is blocked waiting for the task, the continuation cannot run and a deadlock occurs. Avoid synchronous waiting on async work and use `await` throughout the call chain.

- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` tells the awaiter not to capture the current `SynchronizationContext` for the continuation. The continuation may therefore run on a different thread, often the thread pool, rather than being posted back to the original context.

    It does not make the operation run in the background, change cancellation, or force a particular thread. Library code commonly uses it to avoid synchronization-context dependencies and reduce the chance of UI or legacy-host deadlocks. UI and application code normally leaves the default capture enabled so it can return to the UI thread, and any UI updates must still be marshaled appropriately.

- id: csharp-async-04
  answer: |
    A `CancellationToken` is a cooperative cancellation signal. A `CancellationTokenSource.Cancel()` requests cancellation and invokes registered callbacks; it does not forcibly terminate a thread or operation. Code passes the token through layers and checks it or registers callbacks, commonly using `IsCancellationRequested` or `ThrowIfCancellationRequested`. A cancelled operation may throw `OperationCanceledException` or produce a cancelled task. Linked sources can combine tokens, and a timeout can be represented with one.

    `IAsyncEnumerable<T>` represents a stream whose elements are produced asynchronously. `await foreach` obtains an async enumerator, repeatedly awaits `MoveNextAsync`, reads `Current`, and asynchronously disposes the enumerator. `WithCancellation(token)` supplies a token to the enumeration, and `[EnumeratorCancellation]` can connect a method's token parameter to the iterator. This is useful for incremental I/O, paging, and large or unbounded data without first loading everything.

- id: csharp-generics-01
  answer: |
    A `where` constraint restricts the types that may be supplied for a type parameter and lets the compiler assume those guarantees. For example, `where T : IDisposable` allows generic code to call `Dispose`, while `where T : new()` allows `new T()`.

    The main constraints include `class` for reference types, `struct` for value types, `notnull`, `unmanaged`, a base class, one or more interfaces, `new()` for a public parameterless constructor, and special constraints such as `Enum` or `Delegate`. Constraints can be combined, for example `where T : class, IDisposable, new()`. They are checked primarily at compile time and improve type safety and available operations.

- id: csharp-generics-02
  answer: |
    Generic variance describes safe conversions between constructed reference types. `out` declares covariance: the type parameter may appear only in output positions, so `IEnumerable<Derived>` can be used as `IEnumerable<Base>`. `in` declares contravariance: the type parameter may appear only in input positions, so an `IComparer<Base>` can be used where an `IComparer<Derived>` is expected.

    `Action<in T>` and `Func<in T, out TResult>` are common examples. Value types remain invariant, and variance does not apply to generic method type parameters. A covariant parameter cannot be used in an input position, and a contravariant parameter cannot be used in an output position.

- id: csharp-generics-03
  answer: |
    For a generic method, the compiler infers its type arguments from the supplied method arguments, using conversions, parameter types, lambda information, and applicable constraints. For example, `M(42)` can infer `int` for `M<T>(T value)`. `var` asks the compiler to infer the type of a variable or expression; it does not explicitly provide type arguments to the method.

    Explicit arguments such as `M<string>(null)` are required when no argument provides information about `T`, when inference fails or is ambiguous, when overload resolution needs disambiguation, or when a particular base/interface type must be selected. A return type alone generally does not supply type arguments for an ordinary generic method invocation.

- id: csharp-generics-04
  answer: |
    `default(T)` produces the default value for `T`: a zeroed value type such as `0`, `false`, or an all-zero struct; `null` for a reference type; and `null` for `Nullable<T>`. The `default` literal is a target-typed shorthand introduced in C# 7.1.

    Generic code needs it because `new T()` is illegal for an unconstrained type parameter unless a `new()` constraint is present. `default(T)` can safely initialize a generic variable without knowing whether `T` is a value or reference type. It does not invoke a user-defined constructor and is not necessarily a suitable “not found” sentinel for a value type.

- id: csharp-delegate-01
  answer: |
    A delegate is a strongly typed reference to a method, lambda, or method group. It encapsulates the signature, and usually the target object, so the method can be invoked later. Custom delegate types can be declared with syntax such as `delegate int Transform(int value);`.

    `Func<TResult>` returns a result, with overloads such as `Func<T, TResult>` for input arguments. `Action` returns `void`, with overloads for zero or more inputs. `Predicate<T>` has the signature `bool Predicate(T value)` and is intended for tests or filters. Method groups and compatible lambdas can be converted to these delegate types.

- id: csharp-delegate-02
  answer: |
    An `event` restricts how a delegate member can be used. Code outside the declaring type can generally subscribe with `+=` and unsubscribe with `-=`, but cannot invoke the event, read its delegate value like a field, or replace the delegate arbitrarily. The declaring type raises the event by invoking it.

    A plain public delegate field exposes invocation and assignment to outside code, which lets callers replace or invoke the callback directly. An event may also have custom `add` and `remove` accessors, and those accessors can have different accessibility.

- id: csharp-delegate-03
  answer: |
    The loop variable declared in a traditional `for` initializer is one shared captured local. If lambdas created during the loop are invoked after it finishes, they all see the final value. If they are invoked during the same iteration, they see the value current at that moment.

    A local copy inside the body is safe, for example `int copy = i; handlers.Add(() => copy);`. A `foreach` iteration variable is also safe for delayed invocation: since C# 5 it has per-iteration capture semantics, so each lambda sees its own item.

- id: csharp-delegate-04
  answer: |
    A multicast delegate is a delegate with an invocation list. Adding handlers with `+=` combines them, and invocation calls the handlers in list order; `-=` removes a matching handler. With an `Action`, every handler is called. With a value-returning delegate, the return value is the value returned by the last handler, while earlier return values are discarded.

    If a handler throws synchronously, invocation stops at that handler, later handlers are not called, and the exception propagates to the invoker. Side effects from earlier handlers remain. This describes synchronous invocation; a multicast `Func<Task>` does not automatically await or aggregate the tasks returned by earlier handlers.

- id: csharp-dispose-01
  answer: |
    `IDisposable` defines `void Dispose()`, a convention for releasing resources deterministically. A well-designed `Dispose` method is generally safe to call more than once.

    A `using` statement scopes the resource to a block and compiles to a `try`/`finally` that calls `Dispose` at the end, even if the body throws. A `using` declaration, such as `using var resource = ...;`, keeps the resource in scope until the end of the enclosing block and disposes it there, typically in reverse declaration order. A declaration is not necessarily disposed immediately before the next statement; use a block if that timing is required.

- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` defines `ValueTask DisposeAsync()` for cleanup that requires asynchronous work, such as closing a network resource, flushing a remote service, or releasing an asynchronous stream. An `await using` statement or declaration awaits `DisposeAsync` during cleanup instead of synchronously blocking.

    Use `IAsyncDisposable` when the release operation itself is asynchronous; use `IDisposable` when release is synchronous or cheap. A type may implement both, and callers choose the appropriate operation. `await foreach` also asynchronously disposes its async enumerator when iteration ends, including after `break` or an exception.

- id: csharp-dispose-03
  answer: |
    `Dispose` is an explicit, deterministic cleanup operation, usually invoked through `using` or `await using`. It can release both managed and unmanaged resources and can be called while the object is still otherwise reachable.

    A finalizer, written as `~TypeName()`, is invoked nondeterministically by the garbage collector after the object becomes unreachable. It is not guaranteed to run at a particular time or at process exit, and it adds overhead. It should be a last-resort safety net for directly owned unmanaged resources when deterministic disposal cannot be guaranteed, not for managed-only resources. Prefer a `SafeHandle` when appropriate; its own cleanup handles the underlying unmanaged resource. A finalizer does not replace `Dispose`.

- id: csharp-dispose-04
  answer: |
    The full pattern is to implement `IDisposable`, centralize cleanup in an idempotent and preferably thread-safe `protected virtual void Dispose(bool disposing)`, and provide a public `Dispose()` that calls `Dispose(true)` and then `GC.SuppressFinalize(this)` after deterministic cleanup.

    A finalizer, if needed, calls `Dispose(false)`. In the `disposing == true` path, release managed resources and clear their references. Release unmanaged resources in both paths, but never access managed objects or throw from the finalizer. Make each release safe against repeated calls and ensure exceptions cannot cause double-release or leaks.

    Derived classes override `Dispose(bool)`, release their own resources in the appropriate path, and call the base implementation. A sealed class can use a non-virtual helper. For difficult unmanaged ownership, wrap the resource in `SafeHandle`; for asynchronous cleanup, provide an appropriate `IAsyncDisposable` implementation as well.

- id: csharp-span-01
  answer: |
    Collection expressions are a C# 12 target-typed syntax for constructing collections. The target type determines whether `[1, 2, 3]` becomes an array, a `List<T>`, a span, an interface implementation, or another supported collection type. The compiler chooses the appropriate construction and element conversions.

    A spread element uses `..` to insert the elements of a compatible existing array, span, or collection into the target at that position. For example, `int[] result = [1, ..middle, 4];`. It is a spread inside a collection expression, not the range operator. Spreads can be especially useful for combining spans without unnecessary array allocation.

- id: csharp-span-02
  answer: |
    `Span<T>` is a view over a contiguous block of existing memory and permits indexed access and mutation without copying the data. `ReadOnlySpan<T>` is the corresponding read-only view. `Memory<T>` is a non-ref-struct memory representation that can reference managed or unmanaged memory, can be stored in fields, and can be used across `await`; `ReadOnlyMemory<T>` is its read-only counterpart.

    `Span<T>` and `ReadOnlySpan<T>` are `ref struct` types. The compiler forbids putting them in ordinary class or struct fields, boxing them, capturing them in lambdas or iterators, or allowing them to live across an `await`. The runtime cannot safely track their combination of references and pointers, and stack-backed instances would otherwise outlive their storage. Their underlying memory must also remain valid while the view is used.

- id: csharp-span-03
  answer: |
    `stackalloc` reserves a contiguous block of unmanaged memory in the current method's stack frame. In safe code it commonly produces a `Span<T>`, avoiding a GC allocation.

    The memory's lifetime is limited to the method, so it must not be returned, stored beyond the method, or used after an `await`; stack-backed storage can disappear when the frame unwinds. The element type must be unmanaged, the size must be nonnegative, and the memory is not guaranteed to be initialized, so initialize it before reading. Large or unbounded sizes can overflow the stack, and repeated or dynamic allocations can cause excessive stack growth; use pooled or heap storage for those cases.

- id: csharp-span-04
  answer: |
    `T[]` is a fixed-size, contiguous array whose length is exposed as `Length`; it cannot be resized and has no `Add` or `Remove` operation. `List<T>` is a variable-size collection with `Count`, `Add`, `Insert`, `Remove`, and automatic backing-array growth. Both are reference types, so both variables default to `null`.

    The traditional `params` modifier marks the final parameter and allows callers to pass zero or more individual arguments. The compiler packs those arguments into an array; passing an existing array directly avoids another pack, and zero arguments produce an empty array. It is a compile-time calling convenience, not runtime variable-argument machinery.

    Since C# 13, `params` can also be used with `ReadOnlySpan<T>` and `IEnumerable<T>` parameters, called params collections. These forms enable allocation-saving span arguments and collection-expression-friendly calls.
