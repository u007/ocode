- id: csharp-null-01
  answer: |
    Nullable reference types (NRTs) are a compile-time-only feature that adds static flow analysis to warn about possible null dereferences. The `?` annotation (e.g., `string?`) marks a reference type as potentially null — it has zero runtime effect; the compiled IL is identical. The `!` (null-forgiving) operator suppresses a nullable warning, asserting "this won't be null" — again no runtime effect. `#nullable enable` turns on the NRT analysis for a file or region; without it, the compiler ignores `?` annotations on reference types.

- id: csharp-null-02
  answer: |
    Value types (structs, enums, primitives) are stored inline — either on the stack or embedded inside another object — and assignment copies the entire value. Reference types (classes, arrays, strings, delegates) are stored on the heap; a variable holds a reference (pointer) to the object, and assignment copies only the reference. The surprising mutation behavior: when you access a struct through a `readonly` field or through a collection indexer (e.g., `list[0].X = 5` or `readonlyField.Mutate()`), the compiler silently works on a *copy* of the struct. The mutation is applied to the copy and then discarded, so the original struct is unchanged. This is why mutable structs are considered dangerous.

- id: csharp-null-03
  answer: |
    A `record` (record class) is a reference type with compiler-synthesized value-based equality (Equals, ==, !=, GetHashCode), a synthesized `ToString`, and a `Deconstruct` method. It is immutable by convention (init-only properties) but can have mutable properties. A `record struct` is a value type with the same synthesized equality and ToString semantics; it is copied on assignment like any struct. A plain `class` uses reference equality by default (two instances are equal only if they are the same object) and provides no synthesized equality, ToString, or deconstruction unless you write them yourself. Records also support `with` expressions for non-destructive mutation.

- id: csharp-null-04
  answer: |
    An `init`-only property can be assigned only during construction — in the constructor body or in an object initializer — and is immutable afterward. A `required` member (property or field) must be assigned by the object-initializer expression at the call site; the compiler enforces this. They combine naturally: a `required` property is typically also `init`-only, so the caller must set it at initialization and it cannot change afterward. The compiler emits an error if a required member is not set in the object initializer.

- id: csharp-pattern-01
  answer: |
    A switch expression is an expression (returns a value) with arrow syntax: `var result = x switch { 1 => "one", _ => "other" };`. It must be exhaustive — the compiler requires a discard `_` arm or coverage of all possible inputs, otherwise it emits an error. A switch statement is a statement (returns no value) with colon syntax and `case`/`break`; it does not require exhaustiveness and has no default-return semantics. Switch expressions also support property, positional, and relational patterns more cleanly.

- id: csharp-pattern-02
  answer: |
    A property pattern matches on properties or fields of the input: `{ Name: "Alice", Age: > 18 }` checks that `Name` equals "Alice" and `Age` is greater than 18. A positional pattern matches on the result of a `Deconstruct` method (or a tuple's positional elements): `point is (0, 0)` deconstructs `point` into its X and Y components. Both bind values — you can write `{ Name: var name }` or `(var x, var y)` to capture the matched values into new variables for use in the arm body.

- id: csharp-pattern-03
  answer: |
    Relational patterns compare against a constant using `<`, `>`, `<=`, `>=` (e.g., `n is > 0 and < 100`). Logical patterns combine patterns with `and`, `or`, and `not` (e.g., `c is >= 'a' and <= 'z' or >= 'A' and <= 'Z'`). List patterns (C# 11) match on the length and elements of a collection or span: `[1, 2, ..]` matches a list starting with 1 and 2; `[_, .., last]` binds the last element; `[]` matches an empty collection. List patterns work on anything countable with an indexer and Length/Count.

- id: csharp-pattern-04
  answer: |
    `obj is Customer c` performs a type check and, if successful, casts and assigns the result to `c` in a single operation — it is equivalent to `obj is Customer && ((Customer)obj) != null` but without the double cast. The bound variable `c` is in scope in the enclosing block where the `is` expression appears true: in an `if` condition, `c` is available inside the `if` body; in a pattern within a `switch` expression arm, it is available in that arm's body. The variable is definitely assigned only when the pattern matches.

- id: csharp-linq-01
  answer: |
    Deferred execution means the query is not executed when it is defined — only when it is enumerated (e.g., in a `foreach`, or when calling a terminal operator). Operators like `Where`, `Select`, `OrderBy`, `GroupBy`, `Skip`, `Take` are deferred; they build up an expression tree or iterator. Immediate execution forces the query to run right away: `Count`, `First`, `Single`, `ToList`, `ToArray`, `ToDictionary`, `Aggregate`, and `foreach` enumeration all trigger immediate execution. Deferred queries re-execute on each enumeration; immediate queries snapshot the data.

- id: csharp-linq-02
  answer: |
    LINQ over `IEnumerable<T>` is LINQ to Objects — it runs in-memory using delegates and iterators. LINQ over `IQueryable<T>` builds an expression tree that a provider (e.g., Entity Framework) translates into a remote query (SQL). The danger of mixing them: if you accidentally switch from `IQueryable` to `IEnumerable` mid-query (e.g., by calling an extension method that takes `IEnumerable<T>`), the remaining operators execute in-memory, potentially pulling the entire table into memory before filtering — a severe performance and scalability problem.

- id: csharp-linq-03
  answer: |
    A LINQ query variable stores the query definition, not the results. Enumerating it more than once re-executes the entire query each time — hitting the database, re-reading the file, or re-running the computation. This is wasteful and can produce inconsistent results if the underlying data changes between enumerations. To avoid it, materialize the results once with `ToList()` or `ToArray()` and then enumerate the materialized collection as many times as needed.

- id: csharp-linq-04
  answer: |
    `First` returns the first element and throws `InvalidOperationException` if the sequence is empty. `FirstOrDefault` returns the first element or `default(T)` if empty. `Single` returns the only element and throws if the sequence is empty *or* has more than one element; `SingleOrDefault` returns the only element or `default(T)` if empty, but throws if there is more than one. The value-type gotcha: `FirstOrDefault` on a value type (e.g., `int`) returns `0` when the sequence is empty — not `null` — so you cannot distinguish "no element" from "element that is zero" without checking `Any()` or using `Nullable<T>`.

- id: csharp-async-01
  answer: |
    Return `ValueTask`/`ValueTask<T>` instead of `Task<T>` when the operation is expected to complete synchronously on the hot path (e.g., a cache hit), to avoid the heap allocation of a `Task` object. Rules: a `ValueTask` may be awaited only once; it must not be converted to `Task` (via `.AsTask()`) more than once; it must not be used with `GetAwaiter().GetResult()` for synchronous blocking; and it should not be stored for later use. If the operation might complete asynchronously or needs to be consumed multiple times, use `Task<T>`.

- id: csharp-async-02
  answer: |
    The compiler transforms an `async` method into a state machine struct that implements `IAsyncStateMachine`. Each `await` is a suspension point: the state machine captures the current state, registers a continuation, and returns to the caller. When the awaited task completes, the continuation resumes the state machine on the captured `SynchronizationContext` (or `TaskScheduler`). The deadlock: if you block on an async call with `.Result` or `.Wait()` on a thread that owns a `SynchronizationContext` (UI thread, ASP.NET Classic request context), the blocked thread cannot process the continuation that is trying to post back to that same context — the continuation waits for the thread, the thread waits for the result. Classic deadlock.

- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` tells the awaiter not to capture and post the continuation back to the original `SynchronizationContext` or `TaskScheduler`; the continuation runs on the thread pool instead. This avoids the deadlock described above and reduces unnecessary context marshaling overhead. It is recommended in library code because libraries should not care about the caller's synchronization context — the caller can decide whether to resume on their context. In application-level code (UI event handlers, ASP.NET request handlers), you typically want the default `ConfigureAwait(true)` to resume on the original context.

- id: csharp-async-04
  answer: |
    `CancellationToken` enables cooperative cancellation: a token source can signal cancellation, and the token is passed into async methods that periodically check `token.ThrowIfCancellationCancellationRequested()` or pass it to async APIs that support it. The operation can observe the token and throw `OperationCanceledException` (or `TaskCanceledException`) to unwind. `IAsyncEnumerable<T>` represents an async stream — a sequence that is produced asynchronously. `await foreach` consumes it, calling `GetAsyncEnumeratorAsync` and `MoveAsyncNextAsync` in a loop, allowing you to process items as they arrive without blocking.

- id: csharp-generics-01
  answer: |
    A `where` constraint on a type parameter restricts what types can be used as arguments, enabling the compiler to know what operations are valid on `T`. Main kinds: `where T : class` (must be a reference type), `where T : struct` (must be a non-nullable value type), `where T : notnull` (must be non-null), `where T : unmanaged` (must be an unmanaged type), `where T : new()` (must have a parameterless constructor), `where T : BaseClass` (must derive from a base class), `where T : IFoo` (must implement an interface). Multiple constraints can be combined.

- id: csharp-generics-02
  answer: |
    Generic variance allows a generic type to be used with a more derived or less derived type than originally specified. `out` (covariance) on a type parameter means the type is used only in output positions (return values) — `IEnumerable<out T>` lets you assign `IEnumerable<string>` to `IEnumerable<object>`. `in` (contravariance) means the type is used only in input positions (parameters) — `IComparer<in T>` lets you assign `IComparer<object>` to `IComparer<string>`. Variance applies only to interfaces and delegates, not to classes, and only to reference types.

- id: csharp-generics-03
  answer: |
    The compiler infers generic method type arguments from the types of the actual arguments passed in. For `void Foo<T>(T item)`, calling `Foo(42)` infers `T = int`. You must specify type arguments explicitly when: the type parameter does not appear in any method parameter (e.g., `T Create<T>()`), when the arguments are not sufficient to infer all type parameters, when you want to force a specific type rather than the inferred one, or when the method is called without arguments. Explicit specification looks like `Foo<string>("hello")`.

- id: csharp-generics-04
  answer: |
    `default(T)` produces `null` for reference types and nullable value types, and the zero-initialized value (`0`, `false`, struct with all fields zero) for non-nullable value types. In C# 7.1+, `default` (without the type parameter) can be used when the type is inferable. Generic code needs `default(T)` because you cannot write `null` (T might be a value type) or `0` (T might not be numeric) — `default(T)` is the only universal "empty" value that works for any T.

- id: csharp-delegate-01
  answer: |
    A delegate is a type-safe function pointer — a reference to a method with a specific signature. `Func<T1, T2, ..., TResult>` represents a method that takes parameters and returns a value (up to 16 parameters). `Action<T1, T2, ...>` represents a void-returning method. `Predicate<T>` is a delegate that takes one parameter and returns `bool` (used by methods like `List<T>.FindAll`). All are defined as delegate types under the hood; `Func` and `Action` are generic and cover most common signatures.

- id: csharp-delegate-02
  answer: |
    The `event` keyword adds encapsulation over a plain public delegate field. With a plain delegate field, any external code can invoke the delegate (`field()`) or overwrite it (`field = handler`). With an `event`, only the containing class can invoke the delegate; external code can only subscribe (`+=`) or unsubscribe (`-=`). This prevents external classes from raising the event or clearing all subscribers.

- id: csharp-delegate-03
  answer: |
    In C# 5 and later, the `foreach` loop variable is logically re-created on each iteration, so lambdas capturing it capture a per-iteration copy — they are safe and each sees its own value. In a `for` loop, the loop variable is a single variable shared across all iterations; lambdas created inside the loop that capture it will all see the final value after the loop completes. To make `for` loops safe, create a local copy inside the loop body (`var local = i;`) and capture that.

- id: csharp-delegate-04
  answer: |
    A multicast delegate is a delegate instance that has multiple methods in its invocation list (combined with `+=`). When invoked, all methods are called in the order they were added. The return value is the return value of the *last* method invoked — earlier return values are discarded. If any method throws an exception, the exception propagates immediately and the remaining methods in the invocation list are not called. To get all return values or handle exceptions per-method, you must call `GetInvocationList()` and invoke each delegate individually.

- id: csharp-dispose-01
  answer: |
    `IDisposable` defines a single method `Dispose()` that releases unmanaged resources (file handles, database connections, etc.) deterministically. A `using` statement (`using (var x = new Foo()) { ... }`) is syntactic sugar for a `try/finally` that calls `Dispose()` in the `finally` block — the scope is the enclosing block. A `using` declaration (`using var x = new Foo();`) calls `Dispose()` at the end of the *enclosing scope* (method, etc.) — the scope is the entire enclosing block, not a nested block. Both guarantee disposal even if an exception is thrown.

- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` defines `DisposeAsync()` which returns a `ValueTask` — it is for cleanup that involves asynchronous operations (e.g., flushing a stream asynchronously, closing a connection with async I/O). `await using` calls `DisposeAsync()` instead of `Dispose()`. Use it over `IDisposable` when the resource's cleanup is inherently async; blocking on async cleanup with `.Result` can cause deadlocks. A type can implement both `IDisposable` and `IAsyncDisposable`; `await using` prefers `DisposeAsync` if both are available.

- id: csharp-dispose-03
  answer: |
    A finalizer (destructor, `~ClassName()`) is called non-deterministically by the garbage collector when the object is collected — you cannot control when or if it runs. `Dispose()` is called deterministically by the developer (or `using`) to clean up resources immediately. A type needs a finalizer only if it *directly* holds unmanaged resources (raw handles, pointers) that would leak if `Dispose` is never called. If a type only holds managed `IDisposable` objects, it does not need a finalizer — those objects have their own finalizers or disposal.

- id: csharp-dispose-04
  answer: |
    The full dispose pattern: (1) Implement `IDisposable`. (2) Add a protected virtual `void Dispose(bool disposing)` method. When `disposing` is `true` (called from `Dispose()`), release both managed and unmanaged resources. When `false` (called from the finalizer), release only unmanaged resources — managed objects are already being finalized. (3) `Dispose()` calls `Dispose(true)`, then `GC.SuppressFinalize(this)` to prevent the finalizer from running. (4) Add a finalizer that calls `Dispose(false)`. (5) If the class is sealed, the pattern can be simplified (no virtual method, no finalizer if no unmanaged resources).

- id: csharp-span-01
  answer: |
    Collection expressions (C# 12) provide a concise syntax to create collections: `[1, 2, 3]` can target `int[]`, `List<int>`, `Span<int>`, `IEnumerable<int>`, and other collection types — the compiler picks the appropriate construction pattern. The spread element `..` includes all elements from another collection expression: `[1, 2, .. otherList, 5]` spreads the contents of `otherList` into the new collection. Spread works with any enumerable and can appear anywhere in the expression.

- id: csharp-span-02
    `Span<T>` is a ref struct that provides a type-safe, bounds-checked view over contiguous memory — it can point to stack, heap (arrays, strings), or native memory. `ReadOnlySpan<T>` is the read-only version. `Memory<T>` is a regular struct (not ref struct) that represents a contiguous memory region and can be stored in fields, boxed, and used across `await` boundaries. `Span<T>` cannot be stored in a field (except in another `ref struct`), cannot be boxed, cannot be used across an `await` (because it might point to stack memory that won't exist after the async continuation), and cannot be used in generic type parameters — because it is a `ref struct` and the compiler enforces stack-only lifetime.

- id: csharp-span-03
  answer: |
    `stackalloc` allocates a block of memory on the stack instead of the heap. It must be used in an `unsafe` context (or assigned to a `Span<T>`/`ReadOnlySpan<T>` in safe code). You must be careful about: (1) stack size — the default thread stack is 1 MB, so large allocations can cause `StackOverflowException` which cannot be caught; (2) the memory is only valid in the current stack frame — you cannot return a `Span<T>` created with `stackalloc` from the method; (3) in safe code with `Span<T>`, the compiler enforces that the span does not escape the method.

- id: csharp-span-04
  answer: |
    An array (`T[]`) is a fixed-size, contiguous block of memory — its length is set at creation and cannot change. `List<T>` is a dynamic collection that wraps an internal array and automatically resizes (doubles capacity) when needed; it provides `Add`, `Remove`, `Insert`, and O(1) amortized append. `params` on a parameter (`void Foo(params int[] args)`) allows the caller to pass a variable number of arguments — either as individual values (`Foo(1, 2, 3)`) or as an array (`Foo(new[] { 1, 2, 3 })`). The compiler converts the individual arguments into an array at the call site.
