- id: csharp-null-01
  answer: |
    Nullable reference types (NRT) give you compile-time null-safety analysis: the compiler emits
    warnings when you dereference a possibly-null reference, assign null to a non-nullable parameter,
    return a possibly-null value from a non-nullable return type, etc. It does NOT change runtime
    behavior — the feature is purely a compile-time (annotation) mechanism.

    The `?` annotation (e.g. `string?`) tells the compiler "this reference may be null." It has no
    runtime effect: the compiled IL is identical whether you write it or not. It only flows through the
    compiler's nullable analysis.

    The `!` (null-forgiving) operator suppresses a specific nullable warning. For example
    `string s = maybeNull!;` tells the compiler "trust me, treat this as non-null." Also purely a
    compile-time construct with zero runtime effect.

    `#nullable enable` is a compiler directive that turns on the nullable analysis context for the code
    that follows (until `#nullable disable` or `#nullable restore`). The project-level
    `<Nullable>enable</Nullable>` MSBuild property does the same thing globally. Without it, NRT warnings
    are not emitted.

- id: csharp-null-02
  answer: |
    Value types (structs, enums, primitives like int, double, bool) are stored where they are declared:
    on the stack for locals/parameters, inline within their containing type for fields. Assigning a
    value type copies the entire value.

    Reference types (classes, interfaces, delegates, arrays, strings) are stored on the heap, and the
    variable holds a reference (pointer) to the heap object. Assigning a reference type copies the
    reference, not the object — both variables then point to the same object.

    The surprising mutation issue: when you access a struct through a collection indexer or a readonly
    field and call a mutating method on it, the compiler creates a defensive copy of the struct. The
    mutation goes to that copy, not the original stored in the collection/field, so the original remains
    unchanged. For a readonly field, you cannot reassign it or call a method that would modify it
    because the field is readonly — the compiler prevents writes. For a collection indexer like
    `list[0].Mutate()`, the indexer getter returns a copy (since the collection stores values), so you
    are mutating a throwaway copy. To actually mutate the stored struct, you must read it out, modify
    it, and write it back (e.g., `var tmp = list[0]; tmp.Mutate(); list[0] = tmp;`).

- id: csharp-null-03
  answer: |
    `record` (record class): a reference type (on the heap). It has compiler-synthesized value-based
    equality (Equals, ==, !=, GetHashCode, ToString, Deconstruct, deconstruction patterns, and `with`
    expressions for non-destructive mutation). Positional record parameters automatically become
    init-only properties. Equality compares all fields/properties by value. Supports `with` expressions:
    `var copy = original with { Property = newValue };`.

    `record struct`: a value type (on the stack, or inline). Also has synthesized value-based equality,
    ToString, and Deconstruct. It supports `with` expressions too. Because it is a value type, it is
    copied on assignment. It can be declared `readonly record struct` to get compiler-enforced
    immutability (the compiler makes fields readonly and flags mutations as errors).

    Plain `class`: a reference type with reference-identity equality by default (two different instances
    are never equal unless you override Equals/GetHashCode). Mutable by default. No `with` expressions.
    No synthesized value equality. You write your own properties, constructors, and members.

    Key distinctions:
    - Equality: record = value equality; class = identity equality (unless overridden).
    - Mutability: record properties are init-only (immutable after construction, but not readonly —
      `with` creates a copy); class is fully mutable by default.
    - With-expressions: only records support them.
    - Value type vs reference type: record is a reference type; record struct is a value type.

- id: csharp-null-04
  answer: |
    `init`-only properties: the setter (accessor) can only be invoked during object construction —
    from the constructor body or from an object initializer (`new Foo { X = 1 }`). After construction,
    the property is effectively read-only. This is enforced at compile time. An `init` accessor is
    like a `set` accessor but restricted to the initialization phase.

    `required` members (C# 11): marks a property or field as mandatory in object initializers. The
    compiler enforces that every `required` member is assigned in the object initializer expression.
    If you forget, you get a compile error. This applies to properties and fields of classes, structs,
    and records.

    How they combine: `required` tells the compiler "this member must be set during initialization,"
    and `init` tells the compiler "this can only be set during initialization." Together:
    `required int Foo { get; init; }` means the property MUST be set in the object initializer and
    CANNOT be changed afterward. The `required` keyword is essentially the enforcement of a
    compile-time contract that the member is initialized, while `init` enforces the immutability
    after construction.

- id: csharp-pattern-01
  answer: |
    Switch statement: uses `case value: ... break;` syntax, does not return a value, does not require
    exhaustiveness (falls through to code after the switch if no case matches), supports fall-through
    via `goto case`, and each case body is a statement block. It is a statement, not an expression.

    Switch expression: uses `value => result` arrow syntax, always returns a value (all arms must
    produce a value of the same type), must be exhaustive (the compiler enforces that every possible
    input is covered), and is an expression — it can be assigned, passed as an argument, or returned.
    It supports patterns and can use a discard `_` as the default arm.

    If the arms of a switch expression are not exhaustive, the compiler emits error CS8509: "The switch
    expression does not handle all possible values of its input type." You must add more arms, use a
    discard pattern `_`, or add a `default` arm. For non-exhaustive switch *statements*, there is no
    such error — control simply falls through.

- id: csharp-pattern-02
  answer: |
    Property patterns match against properties (or fields) of the input object. Syntax:
    `customer is { Address.City: "London" }` — this matches if `customer.Address.City` equals
    "London." You can nest property patterns deeply: `{ Address: { City: "London", Zip: "..." } }`.
    The matched property values can also be bound with `var`: `{ Age: var age }` binds the Age value
    to `age`. Property patterns are useful when you don't know the exact type but know a property path
    and value to check.

    Positional patterns match against the Deconstruct method of a type. If a type has a
    `Deconstruct(out T1, out T2, ...)` method (or the compiler-generated one for positional record
    parameters), you can write `point is (var x, var y)` which calls `Deconstruct` and binds the outputs
    to `x` and `y`. For a record `Point(int X, int Y)`, `p is (var x, var y)` decomposes it and binds
    the values. The number of bound variables must match the number of Deconstruct parameters.

    Both patterns bind variables that are in scope in the switch arm body (or in the enclosing block
    if used in an `if` with an `is` pattern).

- id: csharp-pattern-03
  answer: |
    Relational patterns: match a value against a relational operator. Syntax: `x is > 5`, `x is < 10`,
    `x is >= 0`, `x is <= 100`, `x is == 5`, `x is != 0`. They work with any type that supports the
    corresponding operator.

    Logical patterns: combine patterns with `and`, `or`, and `not`.
    - `and`: both patterns must match. `x is > 0 and < 100` (both true).
    - `or`: either pattern matches. `x is < 0 or > 100`.
    - `not`: negates a pattern. `x is not null`, `x is not (> 5)`.
    You can combine them: `x is > 0 and not < 10 or == -1`.

    List patterns (C# 11): match on sequences (arrays, lists, spans, any type with a Count/Length
    property and an indexer). Syntax uses square brackets:
    - `[first, .., last]` — matches a list with at least 2 elements, binds the first to `first` and
      the last to `last`.
    - `[var x, var y, .. rest]` — matches a list with at least 2 elements, binds the first two to x
      and y, and the remaining elements (possibly zero) to `rest`.
    - `[]` — matches an empty list.
    - `[.. var all]` — matches any list and binds all elements to `all`.
    The `..` is the slice pattern — it matches zero or more elements.

- id: csharp-pattern-04
  answer: |
    `obj is Customer c` performs a type test AND a cast in a single operation. If `obj` is non-null and
    is of type `Customer` (or a derived type), the pattern matches and `c` is a non-null `Customer`
    variable bound to the cast value. If `obj` is null or not a Customer, the pattern does not match
    and `c` is not assigned (it would be in a "not definitely assigned" state or simply not in scope
    for use as a valid Customer).

    A plain type check + cast is two separate operations: `obj is Customer` (returns bool) followed by
    `(Customer)obj` (a cast that could throw InvalidCastException if the first check was somehow
    invalidated, e.g., by a race or if the object's type is a proxy that lies). More importantly, the
    declaration pattern guarantees the variable is valid within the scope where the match succeeded.

    Scope of the bound variable: the variable `c` is in scope in the enclosing block where the pattern
    match is true. For `if (obj is Customer c) { /* c in scope here */ }`, `c` is in scope in the
    if-body. It is also in scope after `&&` chains: `if (obj is Customer c && c.Age > 18) { ... }`
    — `c` is in scope in the right operand and the body. In a switch expression arm, the bound
    variable is in scope in the arm's body expression.

- id: csharp-linq-01
  answer: |
    Deferred (lazy) execution: the query is defined but not executed until you actually enumerate it
    (foreach, for-loop, pass to a method that enumerates). Operators that use deferred execution:
    Where, Select, SelectMany, OrderBy, OrderByDescending, ThenBy, GroupBy, GroupJoin, Join, Skip,
    SkipWhile, Take, TakeWhile, Distinct, Union, Intersect, Except, Concat, Reverse, OfType, Cast,
    AsQueryable, AsEnumerable. These return `IEnumerable<T>` or `IQueryable<T>` and build an expression
    tree or iterator chain that is only evaluated on enumeration.

    Immediate (eager) execution: the query is executed immediately and the results are materialized.
    Operators that trigger immediate execution: ToList, ToArray, ToDictionary, ToLookup, Count, LongCount,
    First, FirstOrDefault, Last, LastOrDefault, Single, SingleOrDefault, Any, All, Contains, Aggregate,
    Sum, Min, Max, Average, ElementAt, ElementAtOrDefault, ForEach (not a LINQ operator but forces
    enumeration).

    Deferred allows you to compose queries incrementally (add more operators before execution) and
    provides lazy evaluation (only computes what is needed). Immediate forces materialization and is
    necessary when you need the results in a concrete form or want to snapshot the data.

- id: csharp-linq-02
  answer: |
    LINQ over `IEnumerable<T>` (LINQ-to-Objects): operates on in-memory collections. The query
    delegates (Func<T, bool>, Func<T, TResult>) are compiled to IL and execute directly against the
    in-memory sequence. All LINQ operators are available and work with any IEnumerable<T>.

    LINQ over `IQueryable<T>`: the query is built as an expression tree (Expression<Func<T, bool>>).
    The LINQ provider (e.g., Entity Framework, NHibernate) translates this expression tree into a
    native query language (SQL, etc.) and executes it on the data source. Not all operators are
    supported by all providers — unsupported operators throw NotSupportedException at runtime when the
    query is executed.

    The danger of mixing: if you start with IQueryable (building a SQL query) and then switch to
    IEnumerable (e.g., by calling AsEnumerable() or by using a method that takes IEnumerable<T>), all
    subsequent operators execute in-memory instead of being translated to the database query. This can
    cause massive performance problems — for example, if you filter a table with Where (translated to
    SQL), then call AsEnumerable() and do more filtering in-memory, the database returns ALL rows and
    you filter in memory. The query is no longer executed as a single SQL statement.

- id: csharp-linq-03
  answer: |
    When you define a LINQ query (deferred execution) and enumerate the same query variable more than
    once, the query is re-executed against the source each time. For example:

        var query = dbContext.Users.Where(u => u.IsActive); // deferred
        Console.WriteLine(query.Count()); // executes SQL query #1
        foreach (var u in query) { ... }    // executes SQL query #2

    Each enumeration re-runs the query. If the source is a database, that means multiple round-trips.
    If the source is an expensive computation or has side effects, the cost is multiplied. If the
    source changes between enumerations, the results may be inconsistent.

    To avoid this: materialize the query results into a concrete collection (ToList(), ToArray(),
    ToDictionary()) after the query definition, then enumerate the materialized collection:

        var results = dbContext.Users.Where(u => u.IsActive).ToList(); // executes once
        Console.WriteLine(results.Count);
        foreach (var u in results) { ... } // uses in-memory list

- id: csharp-linq-04
  answer: |
    `First`: returns the first element of the sequence. Throws `InvalidOperationException` if the
    sequence is empty.

    `FirstOrDefault`: returns the first element, or `default(T)` if the sequence is empty (does not
    throw). For reference types, default is null; for value types, default is 0, false, etc.

    `Single`: returns the only element of the sequence. Throws `InvalidOperationException` if the
    sequence is empty OR contains more than one element.

    `SingleOrDefault`: returns the only element, or `default(T)` if empty. Throws
    `InvalidOperationException` if more than one element.

    The value-type gotcha for `FirstOrDefault`: when `T` is a value type (e.g., `int`), `FirstOrDefault`
    returns `default(int)` which is `0` when no element is found. But `0` is also a valid element value
    — you cannot distinguish "no element found" from "the first element is 0." For example:

        var numbers = new List<int> { 0, 1, 2 };
        var first = numbers.Where(x => x > 10).FirstOrDefault(); // returns 0
        // Is this "no match" or "the value 0"?

    To handle this, use `int?` (nullable value type): `FirstOrDefault` returns `null` when no element
    is found, or the value wrapped in `int?`. Or use `First` and catch the exception, or check `.Any()`
    before calling `First`.

- id: csharp-async-01
  answer: |
    Return `ValueTask<T>` (or `ValueTask`) instead of `Task<T>` when:
    - The operation is very likely to complete synchronously (e.g., a cache hit, a fast-path check,
      reading from an in-memory buffer) and you want to avoid the allocation of a Task<T> object.
    - The method is called frequently and the synchronous completion path is common, so the GC pressure
      from Task allocations matters.
    - You are writing library code (e.g., System.IO.Pipelines, System.Threading.Channels) where
      high-throughput and low-allocation are critical.

    Rules for consuming a ValueTask:
    1. A ValueTask can be consumed (awaited) at most ONCE. You cannot await it twice, call AsTask()
       twice, or use it with ConfigureAwait multiple times.
    2. After the first await/consumption, the ValueTask should be discarded.
    3. You cannot call AsTask() and then also await the ValueTask directly.
    4. If the ValueTask wraps an IValueTaskSource (used for pooling/reuse in high-performance
       scenarios), it absolutely must not be consumed more than once — doing so causes undefined
       behavior (the underlying source may have been returned to the pool).
    5. If you need to consume it multiple times, call AsTask() once and reuse the resulting Task<T>.

    `ValueTask` (non-generic) is used for async methods that return void (no result), avoiding the
    Task allocation for synchronous completions.

- id: csharp-async-02
  answer: |
    The compiler transforms an `async` method into a state machine struct that implements
    IAsyncMethodBuilder (AsyncTaskMethodBuilder for Task, AsyncValueTaskMethodBuilder for ValueTask).
    The method runs synchronously until it hits the first `await` on an incomplete (not yet completed)
    task. At that point, it returns a Task to the caller. When the awaited task completes, the
    continuation (rest of the method) is scheduled to run on the captured SynchronizationContext (or
    TaskScheduler.Current if no context). This means the method may complete on a different thread than
    it started on.

    Deadlock with `.Result` / `.Wait()`: Consider this on a UI thread (or ASP.NET Classic request
    context) that has a SynchronizationContext:

        var result = GetDataAsync().Result; // blocks the UI thread

    The async method starts, hits an await, and captures the UI SynchronizationContext. When the
    awaited task completes, the continuation tries to post back to the UI SynchronizationContext to
    resume execution. But the UI thread is blocked on `.Result`, waiting for the task to complete. The
    continuation can never run because the thread that would execute it is blocked. Classic deadlock.

    In ASP.NET Core, there is no SynchronizationContext, so the continuation runs on the thread pool
    and this specific deadlock does not occur. However, blocking on async code is still bad practice —
    it causes thread pool starvation under load (threads are blocked instead of being available to
    serve other requests).

- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` tells the awaiter not to capture and marshal continuations back to the
    original SynchronizationContext or TaskScheduler. Instead, after the awaited task completes, the
    continuation runs on the thread pool (any available thread). Without `ConfigureAwait(false)`, the
    await captures the current SynchronizationContext and posts the continuation back to it, which
    marshals the continuation to the original context (e.g., the UI message loop, or the ASP.NET
    request context).

    Why it is recommended in library code:
    1. A library should not force its caller's context. The library doesn't know whether it is being
       called from a UI thread, an ASP.NET request, or a background thread. Capturing and marshaling
       back to the caller's context is the caller's decision, not the library's.
    2. It prevents deadlocks. If the library's consumer blocks on the library's async method
       (`.Result`/`.Wait()`), and the library uses `ConfigureAwait(false)`, the continuation runs on
       the thread pool instead of trying to post back to the blocked context thread — no deadlock.
    3. It improves performance slightly by avoiding the overhead of context marshaling.
    4. It is a signal to the reader: "this continuation does not need the original context."

    You do NOT need `ConfigureAwait(false)` in application-level code (UI event handlers, ASP.NET
    controller actions) because you typically DO want to return to the request/UI context to update
    UI or access HttpContext.

- id: csharp-async-04
  answer: |
    CancellationToken: a lightweight struct that signals cooperative cancellation. You create a
    CancellationTokenSource (CTS), get its Token, and pass the token to async methods. The async
    method periodically checks `token.ThrowIfCancellationRequested()` — if the CTS has been cancelled
    (via `Cancel()`), this throws `OperationCanceledException` (or TaskCanceledException). The token
    can also be registered with a callback (`token.Register(() => ...)`) for cleanup. Tokens can be
    linked (CreateLinkedTokenSource) so multiple sources can trigger the same token. Cancellation is
    cooperative — the method must check the token; it cannot be forcibly aborted.

    IAsyncEnumerable<T>: an async version of IEnumerable<T>. It represents a sequence of elements
    that are produced asynchronously. The compiler generates an async iterator state machine that
    returns IAsyncEnumerator<T> with a MoveNextAsync() method. You produce elements with
    `await foreach` or by using `yield return` inside an `async` method that returns
    IAsyncEnumerable<T>.

    `await foreach`: the async counterpart of foreach. It calls MoveNextAsync() on the async
    enumerator and awaits each element. At the end of the loop, it calls DisposeAsync() on the
    enumerator. Example:

        await foreach (var item in GetItemsAsync())
        {
            Console.WriteLine(item);
        }

    Use IAsyncEnumerable<T> for streaming scenarios: reading from a database cursor, processing a
    stream of events, reading chunks from a network stream, or any scenario where elements are produced
    asynchronously and you want to process them one at a time without buffering the entire sequence.

- id: csharp-generics-01
  answer: |
    A `where` constraint on a type parameter restricts what types can be used as arguments for that
    parameter. It tells the compiler "T must satisfy this condition," which allows you to call methods
    or access members on T that the constraint guarantees.

    Main kinds of constraints:
    - `where T : struct` — T must be a non-nullable value type (int, double, custom struct). Allows
      default(T), boxing, etc.
    - `where T : class` — T must be a reference type (class, interface, delegate, array). Allows
      null assignment and reference semantics.
    - `where T : notnull` — T must be a non-nullable type (value type or non-nullable reference
      type). Introduced in C# 8.
    - `where T : unmanaged` — T must be an unmanaged type: not a reference type and contains no
      reference-type fields recursively. Allows unsafe pointer operations.
    - `where T : new()` — T must have a public parameterless constructor. Allows `new T()` inside
      the generic code. Must be listed last.
    - `where T : BaseClass` — T must be or derive from BaseClass. Allows calling BaseClass members.
    - `where T : IInterface` — T must implement IInterface. Allows calling interface members.
    - Multiple constraints can be combined, but `new()` must be last, and you can only specify one
      class constraint (either `class`, `struct`, or a specific base class).

- id: csharp-generics-02
  answer: |
    Generic variance describes how type arguments in generic interfaces and delegates relate to each
    other when the type arguments have an inheritance relationship. It is supported only for
    reference-type type parameters.

    `out` (covariant): the type parameter can only appear in output positions (return types, get-only
    properties). It allows a more-derived type to be used where a less-derived type is expected.
    Example: `IEnumerable<Derived>` can be assigned to `IEnumerable<Base>` because `IEnumerable<out T>`
    is covariant. You can read T from the collection but not add it (no input positions). Variance is
    declared as `interface IEnumerable<out T>`.

    `in` (contravariant): the type parameter can only appear in input positions (method parameters,
    set-only properties). It allows a less-derived type to be used where a more-derived type is
    expected. Example: `IComparer<Base>` can be assigned to `IComparer<Derived>` because
    `IComparer<in T>` is contravariant. You can pass T to the comparer but not get it out. Variance is
    declared as `interface IComparer<in T>`.

    Where they apply: generic interfaces and delegates only (not classes, not structs). The variance
    annotation (`out`/`in`) is only valid if the type parameter appears exclusively in output (for out)
    or input (for in) positions. Variance is checked at compile time and only works for reference types.

- id: csharp-generics-03
  answer: |
    Type inference: the compiler determines the type arguments for a generic method from the types of
    the actual arguments passed to the method. For example:

        T Identity<T>(T value) => value;
        var result = Identity(42); // compiler infers T = int

    The inference works by matching each parameter type (which may contain T) against the corresponding
    argument type. If a parameter is `T` and the argument is `42`, T is inferred as `int`. If there are
    multiple parameters using T, all must agree. The compiler uses a multi-phase algorithm: first it
    fixes type parameters from explicit arguments, then makes lower-bound and upper-bound inferences,
    and finally fixes any remaining parameters.

    You must specify type arguments explicitly when:
    1. The type parameter does not appear in any method parameter (e.g., `T Create<T>()` — no parameter
       to infer from).
    2. The type parameter appears only in the return type, not in parameters.
    3. Overload resolution is ambiguous and specifying the type argument resolves the ambiguity.
    4. You want to force a specific type that differs from what would be inferred (e.g., `Identity<object>(42)`
       to treat the argument as object).
    5. The method has no parameters at all (`void Foo<T>()`).
    6. You are calling a generic method on a generic type and need to specify both the type and method
       type arguments.

    C# does not support partial type inference — you either provide all type arguments or none.

- id: csharp-generics-04
  answer: |
    `default(T)` produces the default value for the type T:
    - For reference types (classes, interfaces, delegates, arrays): `null`.
    - For nullable value types (e.g., `int?`): `null`.
    - For non-nullable value types: the zero-initialized value — `0` for numeric types, `false` for bool,
      `'\0'` for char, and a struct with all fields set to their default for custom structs.
    - For `void` (used in some generic contexts): no value (compile-time only).

    In C# 7.1+, you can write just `default` (without the type) and the compiler infers the type from
    context.

    Generic code needs `default(T)` because T could be either a reference type or a value type, and you
    cannot use `null` (doesn't work for value types) or `0` (doesn't work for reference types). For
    example:

        T[] array = new T[10];
        T first = array[0]; // for a class T, this is null; for int T, this is 0

        void Reset<T>(ref T value)
        {
            value = default(T); // works for any T
        }

        bool IsDefault<T>(T value)
        {
            return EqualityComparer<T>.Default.Equals(value, default(T));
        }

    You need it when you want to return "nothing" / "zero" from a generic method, initialize a variable
    to its zero value, or compare against the default without knowing whether T is a reference or value
    type.

- id: csharp-delegate-01
  answer: |
    A delegate is a type-safe function pointer — a reference to a method with a specific signature
    (parameter types and return type). A delegate instance holds a reference to a method and optionally
    a target object (the instance on which to call the method for instance methods). Delegates are
    multicast by default (can reference multiple methods).

    `Func<T1, T2, ..., TResult>`: a generic delegate representing a method that takes T1...Tn
    parameters and returns TResult. The last type parameter is always the return type. For example:
    `Func<int, int, bool>` is a delegate for a method `(int, int) => bool`. `Func<TResult>` takes no
    parameters and returns TResult. There are overloads for 0 through 16 type parameters (Func<T>
    through Func<T1..T15, TResult>).

    `Action<T1, ..., Tn>`: a generic delegate representing a method that takes T1...Tn parameters and
    returns void. For example: `Action<string>` is a delegate for `void(string)`. Overloads for 0
    through 16 type parameters.

    `Predicate<T>`: a delegate that takes one parameter of type T and returns bool. Defined as
    `delegate bool Predicate<T>(T obj)`. It is essentially `Func<T, bool>` but has a distinct type.
    Commonly used with List<T>.FindAll, List<T>.Exists, etc.

    You can assign a method group, a lambda expression, an anonymous method, or another delegate to
    a delegate variable. Delegates enable callbacks, event handling, and functional programming
    patterns.

- id: csharp-delegate-02
  answer: |
    The `event` keyword restricts the accessibility of a delegate (or group of delegates) to enforce
    encapsulation. Without `event`, a public delegate field can be:
    1. Subscribed to (`+=`) and unsubscribed (`-=`) by external code — same as with event.
    2. **Invoked (called) by external code** — anyone can trigger the delegate.
    3. **Overwritten (`=`)** — anyone can replace the entire delegate, wiping out existing subscribers.

    With `event`, external code can ONLY:
    1. Subscribe (`+=`)
    2. Unsubscribe (`-=`)

    The event can only be invoked from within the declaring class (or struct). This is the key
    encapsulation: external code can raise their hand to listen but cannot fire the event themselves.
    This follows the observer pattern — the publisher controls when the event fires.

    Example:
        public event EventHandler<MyArgs> SomethingHappened; // external: can += / -= only
        public EventHandler<MyArgs> SomethingHappened;       // external: can = , += , -= , and invoke

    Events also appear differently in metadata (as event accessors) and can be declared in interfaces.

- id: csharp-delegate-03
  answer: |
    In a `for` loop, the loop variable is a single variable that is mutated each iteration. A lambda
    that captures this variable captures the variable itself (by reference), not its value at the time
    the lambda is created. All lambdas created in the loop share the same captured variable. When the
    loop finishes, the variable holds its final value, and all lambdas see that final value when
    invoked later:

        var actions = new List<Action>();
        for (int i = 0; i < 3; i++)
        {
            actions.Add(() => Console.WriteLine(i)); // captures the variable i
        }
        foreach (var a in actions) a(); // prints 3, 3, 3 (the final value of i)

    In a `foreach` loop (C# 5.0 and later), the loop variable is logically a new variable each iteration.
    The compiler generates a fresh variable per iteration, so each lambda captures a distinct variable.
    Lambdas are safe:

        var actions = new List<Action>();
        foreach (var i in new[] { 0, 1, 2 })
        {
            actions.Add(() => Console.WriteLine(i)); // captures a per-iteration copy
        }
        foreach (var a in actions) a(); // prints 0, 1, 2

    To make `for` loop lambdas safe, create a local copy inside the loop body:

        for (int i = 0; i < 3; i++)
        {
            int copy = i; // fresh variable each iteration
            actions.Add(() => Console.WriteLine(copy));
        }
        // prints 0, 1, 2

- id: csharp-delegate-04
  answer: |
    A multicast delegate is a delegate whose invocation list contains more than one method. You create
    one by combining delegates with `+=` (or `-=` to remove). For example:

        Action greet = () => Console.WriteLine("Hello");
        greet += () => Console.WriteLine("World");
        greet(); // calls both, in order: Hello then World

    When a multicast delegate is invoked:
    - All methods in the invocation list are called in the order they were added.
    - If the delegate has a non-void return type (e.g., Func<T>), the return value of the LAST method
      in the invocation list is returned. Return values from earlier methods are discarded.
    - If any method in the invocation list throws an exception, the invocation stops immediately.
      Remaining methods in the list are NOT called. The exception propagates to the caller of the
      delegate.

    To get all return values or handle exceptions per-method, retrieve the invocation list via
    `GetInvocationList()` and invoke each delegate individually:

        foreach (Func<int> f in myDelegate.GetInvocationList())
        {
            try { Console.WriteLine(f()); }
            catch (Exception ex) { /* handle */ }
        }

- id: csharp-dispose-01
  answer: |
    `IDisposable` is an interface with a single method: `void Dispose()`. It provides a standard,
    deterministic way to release unmanaged resources (file handles, database connections, network
    sockets, GDI objects, etc.) and to release managed resources that wrap unmanaged resources. When
    you are done with an object that implements IDisposable, you call Dispose() to release resources
    immediately rather than waiting for the garbage collector.

    `using` statement (classic form):
        using (var resource = new StreamReader("file.txt"))
        {
            // use resource
        } // Dispose() called here automatically, even if an exception is thrown

    This compiles to a try/finally block where Dispose() is in the finally. The variable is scoped to
    the using block.

    `using` declaration (C# 8+ form):
        using var resource = new StreamReader("file.txt");
        // use resource
        // Dispose() called at the end of the enclosing scope (method, block, etc.)

    This does NOT create a nested block. Dispose() is called at the end of the enclosing scope (e.g.,
    the end of the method). It compiles to a try/finally where the finally is at the scope boundary.
    The variable is in scope from declaration to the end of the enclosing block.

    Key difference: the using statement limits the variable's scope to its block and disposes at the
    block's end. The using declaration keeps the variable in scope until the enclosing block's end and
    disposes there. Both guarantee Dispose() is called even on exception.

- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` is an interface with a single method: `ValueTask DisposeAsync()`. It is the
    asynchronous counterpart of IDisposable. Some resources need to perform asynchronous work during
    cleanup (flushing a buffer to disk asynchronously, closing a database connection that needs async
    I/O, sending a final message over a network stream). Calling Dispose() (synchronous) for such
    resources can cause thread blocking, deadlocks, or performance issues.

    `await using` is the async counterpart of `using`. It calls `DisposeAsync()` instead of `Dispose()`:

        await using var connection = await GetConnectionAsync();
        // use connection
        // DisposeAsync() called at the end of the scope, and the returned ValueTask is awaited

    `await using var x = ...;` (declaration form) calls DisposeAsync() at the end of the enclosing
    scope. `await using (var x = ...;) { ... }` (statement form) calls it at the block's end.

    When to use over IDisposable:
    - When DisposeAsync() is available and the disposal involves I/O or other async operations.
    - When you are in an async context and want to avoid blocking a thread during disposal.
    - When the resource's DisposeAsync() does meaningful async work (e.g., flushing async streams).
    - Prefer await using when both IDisposable and IAsyncDisposable are implemented — DisposeAsync()
      is the more complete cleanup path.

    IAsyncDisposable was introduced in C# 8 alongside IAsyncEnumerable.

- id: csharp-dispose-03
  answer: |
    A finalizer (destructor, declared as `~ClassName()`) is a method that the garbage collector calls
    during object finalization, on a dedicated finalizer thread. It is non-deterministic — you don't
    know when (or if) it will run. It runs only when the GC collects the object and the object has not
    been properly disposed. The finalizer is a safety net for when Dispose() was not called.

    `Dispose` (via IDisposable) is called deterministically by user code (via `using` or an explicit
    call). It runs on the caller's thread at a predictable time. It is the primary, deterministic
    cleanup mechanism.

    A type needs a finalizer ONLY if it directly owns unmanaged resources — raw OS handles (IntPtr),
    unsafe pointers, or other resources not wrapped by any SafeHandle or IDisposable type. If you use
    a SafeHandle (e.g., SafeFileHandle, SafeWaitHandle) or another IDisposable wrapper for your
    unmanaged resource, that wrapper's own finalizer will clean up the resource. You do NOT need to
    write your own finalizer.

    Best practice: if your type directly holds an unmanaged resource (rare in modern C# — usually you
    use SafeHandle), you implement both the finalizer and IDisposable. The finalizer calls
    Dispose(false) as a fallback. If you only use managed resources (other IDisposable objects), you
    implement IDisposable but NOT a finalizer. Adding an unnecessary finalizer hurts performance
    (objects with finalizers survive an extra GC generation and require two collections).

- id: csharp-dispose-04
  answer: |
    The full dispose pattern for a class owning both managed and unmanaged resources:

        public class MyResource : IDisposable
        {
            private bool _disposed = false;

            // Finalizer (only if you directly own unmanaged resources)
            ~MyResource()
            {
                Dispose(false);
            }

            // Public Dispose
            public void Dispose()
            {
                Dispose(true);
                GC.SuppressFinalize(this); // prevent finalizer from running
            }

            // Protected virtual Dispose(bool)
            protected virtual void Dispose(bool disposing)
            {
                if (_disposed)
                    return;

                if (disposing)
                {
                    // Dispose managed resources
                    // Call Dispose() on any managed IDisposable fields you own
                    // e.g., _stream?.Dispose();
                }

                // Release unmanaged resources
                // Close handles, free unmanaged memory, etc.
                // e.g., CloseHandle(_rawHandle); _rawHandle = IntPtr.Zero;

                _disposed = true;
            }
        }

    Key elements:
    1. `Dispose(bool disposing)`: the core logic. `disposing` is true when called from Dispose()
       (safe to touch managed objects — they are still alive). It is false when called from the
       finalizer (managed objects may already be finalized — do NOT touch them; only clean up
       unmanaged resources).
    2. `_disposed` flag: prevents double-dispose and use-after-dispose.
    3. `GC.SuppressFinalize(this)`: tells the GC not to call the finalizer because Dispose() already
       cleaned up — improves performance (no need for a second collection pass).
    4. `protected virtual`: allows derived classes to override and add their own cleanup.
    5. Finalizer: only if the class directly owns unmanaged resources. It calls Dispose(false) as a
       safety net. If the class uses SafeHandle for all unmanaged resources, omit the finalizer
       entirely — SafeHandle's own finalizer handles cleanup.

- id: csharp-span-01
  answer: |
    Collection expressions (C# 12): a unified syntax `[...]` for creating collections. The compiler
    infers the target type from context and calls the appropriate constructor or Add method:

        int[] a = [1, 2, 3];           // array
        List<int> l = [1, 2, 3];       // List<int>
        Span<int> s = [1, 2, 3];       // Span<int> (stackalloc'd)
        IEnumerable<int> e = [1, 2, 3]; // array backing

    The spread element `..`: expands another collection into the expression. All elements of the
    spread collection are inserted at that position:

        var list = new List<int> { 4, 5 };
        var combined = [1, 2, ..list, 6, 7]; // [1, 2, 4, 5, 6, 7]

    You can spread any enumerable type (arrays, lists, spans, anything with GetEnumerator). The spread
    element appears inside `[...]`. Multiple spread elements are allowed:
    `[..first, ..second, ..third]`.

    Collection expressions work with any type that has an appropriate constructor (taking a
    collection/span) or implements IEnumerable with an Add method. For custom types, you can define a
    constructor taking Span<T> or use the `[CollectionBuilder]` attribute.

- id: csharp-span-02
  answer: |
    `Span<T>`: a ref struct that represents a contiguous, arbitrarily-length region of memory. It can
    point to stack memory, heap memory (arrays, strings), or unmanaged memory. It provides safe,
    bounds-checked access with performance comparable to raw pointers. It is a ref struct, meaning it
    can only live on the stack.

    `ReadOnlySpan<T>`: the read-only version of Span<T>. Same memory representation, but the elements
    cannot be mutated through it. Created from arrays, strings, or via `span.AsReadOnly()` or implicit
    conversion from Span<T>.

    `Memory<T>`: a regular (non-ref) struct that represents a contiguous region of memory. Unlike
    Span<T>, it can be stored in fields, used across await, and boxed. It holds a reference to the
    underlying memory (array, string, or MemoryManager) plus offset and length. It provides the same
    safe indexing as Span<T>. Use it when you need to store a memory region in a field or use it in
    async state machines.

    Why Span<T> cannot be stored in a field or used across await:
    - Span<T> is a ref struct. The C# compiler enforces that ref structs can only exist on the stack.
    - Fields live on the heap (inside a class). Allowing a Span<T> in a field would mean it could
      point to stack memory that has been popped (dangling pointer) or to heap memory that the GC
      moves (Span<T> cannot be tracked by the GC because it's a ref struct with no heap representation).
    - `await` captures the local variable state into the state machine object, which lives on the heap.
      Storing a Span<T> there would violate the stack-only guarantee.
    - `yield return` has the same issue (iterator state machines are on the heap).
    - Span<T> also cannot be a generic type argument, cannot be boxed, and cannot be used in LINQ.

    `Memory<T>` is the workaround: it is a regular struct that wraps a reference (object + offset + length),
    so it can be stored on the heap safely. The GC tracks the underlying object reference.

- id: csharp-span-03
  answer: |
    `stackalloc` allocates a block of memory on the current thread's stack (instead of the heap). It is
    used in performance-critical code to avoid GC pressure for small, short-lived buffers:

        Span<int> buffer = stackalloc int[128]; // modern C# (Span<T> form)
        // older: unsafe int* buffer = stackalloc int[128];

    The memory is automatically reclaimed when the method returns (the stack frame is popped). No GC
    involvement, no heap allocation.

    What to be careful about:
    1. **Stack size**: the default stack is ~1 MB. A large stackalloc can overflow the stack and throw
       StackOverflowException, which is unrecoverable (terminates the process). Don't allocate large
       buffers on the stack.
    2. **Stack frame lifetime**: the memory is only valid while the stack frame is alive. You cannot
       return a Span<T> created by stackalloc from the method, store it in a field, or let it escape the
       method in any way.
    3. **Ref struct constraint**: the Span<T> from stackalloc is still a ref struct and follows all ref
       struct rules (no fields, no async, no boxing).
    4. **Size limits**: while there's no hard language limit, practical limits are much smaller than
       the stack. Typically a few KB is safe; tens of KB is risky.
    5. **Use Span<T>, not pointers**: in safe code, use the Span<T> form (no `unsafe` keyword needed).
       The compiler still emits the stackalloc IL but the Span provides bounds checking.
    6. **Don't use in async methods if the span must outlive an await**: the span itself can be used in
       async methods (it lives on the stack for the duration of the method execution), but you still
       cannot pass it across an await boundary.

- id: csharp-span-04
  answer: |
    Array (`T[]`): a fixed-size, contiguous block of memory allocated on the heap. Once created, the size
    cannot change. Access is bounds-checked (unless in unsafe context or using Span). Arrays implement
    IList<T>, ICollection<T>, IEnumerable<T>. Performance: excellent for random access (O(1) indexing),
    cache-friendly. Memory overhead: small (just the array header + length).

    `List<T>`: a dynamic-size collection backed by an internal array. It grows automatically as elements
    are added (doubling strategy: capacity doubles when full). Provides Add, Remove, Insert, Contains,
    IndexOf, etc. Performance: amortized O(1) for Add, O(1) for indexing, O(n) for search/remove in the
    middle. Memory overhead: extra capacity (the internal array is usually larger than Count), plus the
    List<T> object itself. Use List<T> when you need a mutable, growable collection.

    `params` collections: the `params` keyword on a method parameter allows the caller to pass a
    variable number of arguments. The parameter is declared as an array, but the compiler allows
    individual arguments:

        void PrintAll(params string[] items) { ... }
        PrintAll("a", "b", "c"); // compiler creates new[] { "a", "b", "c" }
        PrintAll(new[] { "x", "y" }); // can also pass an array directly

    The `params` parameter must be the last parameter in the method signature. Only one `params`
    parameter per method. The caller can pass zero arguments (an empty array is created). When the
    compiler sees individual arguments, it allocates an array — so there's a small allocation cost if
    you call it frequently with many arguments. In C# 12+, `params` also works with Span<T> and
    collection expressions (params ReadOnlySpan<string>, params collection expressions) to avoid the
    array allocation.
