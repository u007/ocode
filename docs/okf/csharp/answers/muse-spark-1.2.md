# csharp knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: csharp-null-01
  answer: |
    Nullable Reference Types (NRTs, C# 8+) give you compile-time static flow analysis for null. When enabled, reference types are non-nullable by default; the compiler warns when you may dereference null or assign null to a non-nullable location.

    Runtime effect: **none** - it is entirely compile-time/erasure. No codegen difference, no runtime check is inserted.
    - `string?` (`?` annotation) = marks the type as nullable (may be null). At runtime it's still just `string`; the `?` is encoded only as nullable attributes (`[Nullable]`, `NullableContextAttribute`) for the compiler to read.
    - `x!` (null-forgiving / dammit operator) = suppresses a warning that `x` may be null, telling the compiler "trust me, not null here". Emits no IL - just hides the warning, still null at runtime if you were wrong -> NullReferenceException.
    - `#nullable enable` (also `<Nullable>enable</Nullable>` / `Nullable` context) = enables the warnings/annotations in that lexical scope/file/project. `disable` turns analysis off, `restore` restores project default. Again, only affects diagnostics, not runtime behavior.

- id: csharp-null-02
  answer: |
    Value types (`struct`, `enum`, primitives) have copy-by-value semantics, reference types (`class`, `interface`, `delegate`, `record class`) have copy-by-reference semantics. Conceptually values live inline/stack and are copied on assignment/pass; references live on heap with GC and assignment copies the reference. Assignment/equality defaults differ too (value types compare content via ValueType.Equals if not overridden).

    Surprise with mutation: structs are copied when accessed through a readonly field, property, `in` parameter, or `foreach` iteration variable. Mutating that copy mutates the temporary, not the original.
    Example: `readonly struct Point`; `class C { public readonly Point p; }` then `c.p.X = 5` or `c.p.Mutate()` compiles (if not readonly struct) but operates on a defensive copy returned by the readonly field accessor, so the original is unchanged and compiler may warn/error (CS1612). Similarly `foreach(var s in listOfStructs) s.Mutate()` or `myList[0].Mutate()` (when List<T> indexer returns a copy) does nothing observable. Fix: make structs readonly/immutable, reassign whole value: `c.p = c.p with {X=5}` or `list[i] = newVal`.

- id: csharp-null-03
  answer: |
    `class`: reference type, reference equality by default (unless overridden), mutable by convention. No synthesized members.

    `record` (= `record class`): reference type with value-based equality. Compiler synthesizes: primary constructor, `Equals`/`GetHashCode` (structural equality over declared properties/positional parameters), `ToString`, `IEquatable<T>`, `EqualityContract` for inheritance, and `with` (non-destructive mutation via clone). Properties are `init`-only if positional. Still a reference type (null-able, heap, reference identity via ReferenceEquals still available).

    `record struct` (C#10): value type (struct) with same synthesized value semantics, but as a struct it is copy-by-value, cannot be null, no inheritance (but can implement interfaces). `record struct` defaults to mutable struct with value equality; `readonly record struct` makes it immutable/readonly like recommended structs. Equality is value equality in all record cases vs reference equality for plain class.

    In short: use `class` for identity/mutable entities, `record`/`record struct` for data/value aggregates where structural equality and `with` are desired.

- id: csharp-null-04
  answer: |
    `init`-only properties: can be set only during object initialization - in constructor or object initializer / `with` initializer. After construction they are readonly. Allows immutable objects with initializer syntax: `set;` -> `init;` . Compiler enforces not settable afterwards.

    `required` members (C#11): forces caller to initialize the member via object initializer or constructor. `public required string Name {get; init;}` means `new Foo()` without setting `Name` is an error (CS9035). Enforced by compiler via `[RequiredMember]` + `[SetsRequiredMembers]` on constructors.

    Together: `required init` gives mandatory immutable initialization without writing a constructor: `public required string Name {get; init;}` must be supplied at creation but cannot be changed after. Constructors that set them must be marked `[SetsRequiredMembers]` or assign all required members. This replaces many constructor overloads for required data while preserving immutability.

- id: csharp-pattern-01
  answer: |
    `switch` statement: imperative statement with `case` labels, `break`/`goto`, fall-through not allowed, can be non-exhaustive, defaults to doing nothing.

    `switch` expression (C#8): expression-form `x switch { pat => expr, ... }` produces a value. Arms are `pattern => expression` comma-separated, body is expression, not statement. More concise, requires exhaustiveness.

    Exhaustiveness: switch expression must be exhaustive - compiler requires every possible input be handled or it errors (CS8509) if no discard `_`/`var _` arm and the input type isn't covered. Switch statement has no such requirement - unmatched input just skips the switch (or hits `default` if present). With switch expression you typically add `_ => throw ...` or `_ => defaultValue`.

- id: csharp-pattern-02
  answer: |
    Property pattern: matches object by testing properties, e.g. `p is { FirstName: "A", Age: >18 }` or in switch `Person { Age: >21, Name: { Length: >3 } }`. Can be nested, tests equality/pattern per property. Does not bind unless you use a nested declaration pattern.

    Positional pattern: matches via `Deconstruct` (or positional record). `point is (0,0)` is shorthand for calling `Deconstruct(out var x, out var y)` and matching those values. For records `p is Person("Bob", var age)` extracts positional parameters.

    Binding: use declaration `var` or type inside: `if (o is Person { Age: var a } )` binds `a`; `if (o is Customer c)` binds `c`; `if (p is (var x, var y))` binds `x`,`y`. Property pattern with `var` binds the property value, positional pattern binds the deconstructed positions.

- id: csharp-pattern-03
  answer: |
    Relational patterns: compare with `<`, `<=`, `>`, `>=` relative to constant, e.g. `x is >0 and <10`, `switch` arm `> 100 => "big"`. Works with IComparable types.

    Logical patterns: combinators `and`, `or`, `not` to combine patterns. `x is not null and >=0`, `is >0 and <10 or 42`. `and` has higher precedence than `or`; `not` negates.

    List patterns (C#11): match sequences against element patterns with `[]`. e.g. `arr is [1,2,3]` exact, `arr is [1, .., 3]` slice pattern `..` matches zero+ elements, `arr is [var first, .. var rest]` captures. Also `list is [>0, .., <0]` and `..` can be used once with binding. Works on any type with Count/Indexer or IEnumerable pattern.

- id: csharp-pattern-04
  answer: |
    `obj is Customer c` (declaration/type pattern with C#7) does in one atomic operation: null-check, type test (`is`), and cast+assignment to a new variable `c` of that type. Plain `if (obj is Customer) { var c = (Customer)obj; }` does double type check and risks race/extra cast, and `as`+null check doesn't work for value types and is two steps. Pattern is safer, clearer, works with pattern combinators.

    Scope: `c` is in scope after the `is` expression where the pattern is known to be true. In `if (obj is Customer c) { // c in scope here }` and with `&&`/`||` flow: `if (obj is Customer c && c.Name=="A")` c is in scope after `&&`. For `if (!(obj is Customer c))` or `if (obj is not Customer c)` scope rules apply to the else/false branch appropriately. Outside the if, c remains in scope in the enclosing block after C#8? Actually after `if`, it falls out of scope unless using `is` as expression; but with `if/else` chains it is definite-assignment based - available where compiler can prove true. General rule: variable is in scope after the `is` in the enclosing scope but only definitely assigned on true path.

- id: csharp-linq-01
  answer: |
    Deferred execution: query not executed until enumerated. Building `Where`/`Select`/`OrderBy`/`GroupBy`/`Join` etc. returns an `IEnumerable<T>`/`IQueryable<T>` that re-executes pipeline on each enumeration. Immediate execution: executes at call time and returns scalar/collection.

    Deferred (streaming): `Where`, `Select`, `SelectMany`, `Take`, `Skip`, `OrderBy`/`ThenBy`, `GroupBy`, `Join`, `Distinct`, `Concat`, `Zip`.
    Immediate: `ToList()`, `ToArray()`, `ToDictionary()`, `Count()`, `Any()`, `First()`, `Single()`, `Max()`/`Min()`/`Sum()`/`Average()`, `ForEach` (List), `ToLookup()`. `Count()`/`Any()` with predicate also immediate.

    Implication: deferred allows composition/optimization but side-effects and DB calls happen late and repeatedly; immediate materializes and captures snapshot.

- id: csharp-linq-02
  answer: |
    `IEnumerable<T>`: LINQ-to-Objects, delegates (`Func<T,bool>`). Operators execute in-memory via iteration in CLR. Pull-based enumeration.

    `IQueryable<T>`: LINQ-to-provider, expression trees (`Expression<Func<T,bool>>`). Query is an AST that the provider (EF Core, etc.) translates - e.g. to SQL - and executes remotely.

    Danger of mixing: passing `IQueryable` where `IEnumerable` extension is chosen (e.g. calling `.AsEnumerable()` too early or calling an `IEnumerable` overload by using a `Func` instead of `Expression`) causes client-side evaluation: entire table may be pulled into memory then filtered, losing indexes/predicates, huge performance and correctness difference. Opposite: using unsupported .NET method inside `IQueryable` expression throws at runtime (NotSupportedException) because provider can't translate it. Also mixing can cause N+1 or subtle change from server null semantics to C# null semantics.

- id: csharp-linq-03
  answer: |
    Because LINQ queries are deferred, enumerating the same query variable twice re-executes the whole pipeline twice. If the source is non-repeatable (DB query, `File.ReadLines`, random, `DateTime.Now`, network) you get different results, double cost, double DB roundtrip, or exception (disposing). Even on in-memory list, you recompute filters/sorts.

    Example: `var q = src.Where(x=>Expensive(x)); if(q.Any()) foreach(var x in q)...` executes Expensive twice.

    Avoid: materialize once when you need multiple passes: `var list = q.ToList();` / `ToArray()` then reuse `list`. Or be intentional that you want re-evaluation. Analyzer CA1851 warns on multiple enumeration.

- id: csharp-linq-04
  answer: |
    `First()` returns first element, throws `InvalidOperationException` if sequence empty. `FirstOrDefault()` returns first or `default(T)` if empty (no throw). `Single()` expects exactly one element - throws if zero or more than one; `SingleOrDefault()` allows zero but throws if >1.

    Value-type gotcha: for `T` struct, `default(T)` is not null but zero-initialized value (e.g. `0` for int, `default(Point)`). So `FirstOrDefault()` on empty `IEnumerable<int>` returns `0`, indistinguishable from a real `0` element. You cannot tell empty vs contains-default. Solutions: use `FirstOrDefault` with nullable `int?`, use `TryGet` pattern, check `Any()` first, or use `DefaultIfEmpty`, or in .NET 6+ use `FirstOrDefault(defaultValue)` overload or `TryGetNonEnumeratedCount`.

- id: csharp-async-01
  answer: |
    Return `ValueTask`/`ValueTask<T>` when the operation is often synchronous/completes synchronously and you want to avoid allocating a `Task` object per call (hot path, high throughput, e.g. cache hit). `ValueTask` is a struct that can wrap either a `T` value synchronously or a `Task<T>` asynchronously.

    Consumption rules (important): `ValueTask` may only be awaited/consumed once. Do not `await` twice, do not call `.Result` after awaiting, do not concurrent-await. You must not block on it synchronously except via `AsTask()`. If you need to store/await multiple times or pass around, call `.AsTask()` to get a `Task`. Also don't ignore the returned ValueTask - await it. Newer `ValueTask` has `IValueTaskSource` pooling so double-await can corrupt pooling.

- id: csharp-async-02
  answer: |
    Compiler rewrites `async` method into a state-machine struct/class implementing `IAsyncStateMachine`, with `MoveNext` handling suspension points. Each `await` checks if awaitable is completed; if not, captures current `SynchronizationContext`/`TaskScheduler`, registers continuation, returns to caller; when awaited operation completes, state machine resumes at next state.

    Deadlock via `.Result`/`.Wait()`/`GetAwaiter().GetResult()`: blocking the thread that holds the context needed to resume. Classic UI/ASP.NET (classic) case: UI thread calls `asyncMethod().Result`. Async method awaits with captured context (trying to post back to UI thread). UI thread is blocked waiting for result, awaited continuation waits for UI thread to be free -> circular wait -> deadlock. Fix: use `await` all way, or `ConfigureAwait(false)` to not capture context, or avoid blocking ( `await` instead).

- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` says "don't capture and marshal back to the current SynchronizationContext/TaskScheduler when resuming; continue on any thread-pool thread." Without it, `await` posts continuation to the captured context (UI thread, ASP.NET request context).

    Recommended in library code because libraries don't know/care about caller's context, and capturing it adds overhead and deadlock risk if caller blocks. Using `false` improves performance and avoids deadlocks when library is blocked on by app code. App/UI code that needs to return to UI thread should NOT use `false` (needs context for UI updates).

- id: csharp-async-04
  answer: |
    `CancellationToken` is cooperative cancellation. Source `CancellationTokenSource` signals via `Cancel()`; token's `IsCancellationRequested` becomes true, `ThrowIfCancellationRequested()` throws `OperationCanceledException`. Operations periodically check token or register callbacks, pass token to async APIs (HttpClient, EF). Token can be linked, have timeout, and be passed to `Task.Run`.

    `IAsyncEnumerable<T>` (C#8) is async pull stream: producer yields `yield return` with `await` delays; consumer enumerates with `await foreach (var x in stream.WithCancellation(token))`. Each `MoveNextAsync()` returns `ValueTask<bool>`. Useful for streaming DB rows, paginated APIs, infinite streams without buffering whole list. Supports `[EnumeratorCancellation]` to propagate token, `await using` for async disposal of enumerator.

- id: csharp-generics-01
  answer: |
    `where` constrains what type arguments can be used for a generic parameter, enabling use of operations and providing better compile errors.

    Main kinds:
    - `where T : class` must be reference type (nullable `class?` allowed)
    - `where T : struct` must be non-nullable value type
    - `where T : notnull` must be non-null
    - `where T : unmanaged` must be unmanaged struct
    - `where T : new()` must have public parameterless constructor (must be last)
    - `where T : BaseClass` must inherit/implement (`where T : IComparable<T>`)
    - `where T : U` type parameter constraint (naked constraint)
    - `where T : default` for overridable in C# rarely
    Can combine: `where T : class, IEntity, new()`

- id: csharp-generics-02
  answer: |
    Variance applies to generic interfaces and delegates, describing whether `Generic<Derived>` can be used as `Generic<Base>` (covariance) or vice versa (contravariance).

    `out T` = covariance: `T` only appears in output positions (return types). Allows `IEnumerable<Derived>` -> `IEnumerable<Base>`. Safe because producer only returns Ts.

    `in T` = contravariance: `T` only appears in input positions (parameter types). Allows `IComparer<Base>` -> `IComparer<Derived>` or `Action<Base>` -> `Action<Derived>`? Actually `Action<in T>` contravariant so `Action<Base>` usable as `Action<Derived>`? Example `Action<object>` can handle string. Safe because consumer only accepts Ts.

    Only valid on interfaces/delegates, not classes/structs. `in`/`out` must be correctly placed; `IEnumerable<out T>`, `IComparer<in T>`, `Func<out TResult, in T>`.

- id: csharp-generics-03
  answer: |
    Compiler infers type arguments from argument types (and sometimes return). For `void Foo<T>(T x)` calling `Foo(42)` infers `T=int`. If multiple parameters, inference uses best common type; if conflicting, fails. Inference also considers generic constraints to filter.

    Must specify explicitly when: inference impossible (no args e.g. `Create<T>()` or `T` only in return), ambiguous, want different type than inferred (e.g. `Foo<object>(str)`), or compiler cannot infer due to lambda/overload needing target type: `Foo(x=>x.Length)` can't infer without explicit `<string>`. Also when using `default` or `null` literal without type context. Syntax `Method<T1,T2>(args)`.

- id: csharp-generics-04
  answer: |
    `default(T)` (or `default` in newer) produces the default value of `T`: `null` for reference types and nullable value types, zero-bit pattern for non-nullable value types (`0` for numeric, `false` for bool, zeroed struct).

    Generic code needs it because `T` is unknown - you can't write `null` (fails if T is struct) nor `0` or `new T()`. `default(T)` is the only type-safe zero/null sentinel that works for all `T`. Used for initializing, returning "not found" (with caveat for value types), `EqualityComparer<T>.Default.Equals(x, default(T))`, clearing arrays, etc. With `where T: struct` or `class` you could know, but without constraint you need it. `default` literal does same via target typing.

- id: csharp-delegate-01
  answer: |
    A delegate is a type-safe function-pointer / object that holds a method (or list) with specific signature, can be invoked like a method, supports closures, and is basis for events/lambdas.

    BCL generic delegates:
    - `Action` / `Action<T1..T16>` : void-returning delegate `void Action<T>(T arg)`
    - `Func<TResult>` / `Func<T1..,TResult>` : returns value, last type param is return, up to 16 params `TResult Func<T,TResult>(T arg)`
    - `Predicate<T>` : specialized `bool Predicate<T>(T obj)` historically `Func<T,bool>` equivalent, returns true/false.

    So `Action` for side-effects, `Func` for transformations, `Predicate` for tests.

- id: csharp-delegate-02
  answer: |
    `event` adds encapsulation over a delegate field following observer pattern. From outside the declaring type you can only `+=` (subscribe) and `-=` (unsubscribe); you cannot invoke, assign (`=`), clear, or read the invocation list directly. Inside the declaring class you can invoke it (null check). Plain `public Action OnClick` would allow anyone to `obj.OnClick = null` wiping subscribers or to `obj.OnClick()` invoking it, breaking encapsulation. `event` hides the underlying delegate field and exposes add/remove accessors (like property vs field), which can also be custom with `add`/`remove` (e.g. thread-safe). In interfaces, `event` forces implementor to provide event semantics.

- id: csharp-delegate-03
  answer: |
    Classic closure capture pitfall. Loop variable is captured by reference (the variable, not its value).

    - C# 5+ `foreach` variable is fresh per iteration -> safe: each lambda captures distinct variable, sees its iteration value.
    - `for (int i=0; i<n; i++)` the single `i` variable is captured; all lambdas share same `i` and see final value after loop (`n`) when invoked later. Unsafe.

    Fix: copy to local inside loop: `for(...) { var copy=i; lambdas.Add(()=>copy); }` . Since C#5 foreach was fixed to be per iteration; before C#5 foreach also had sharing bug. Same for `using`/`while`.

- id: csharp-delegate-04
  answer: |
    A multicast delegate is a delegate that holds an invocation list of multiple methods (combined via `+=`/`Delegate.Combine`). Invoking it calls each target sequentially in order added.

    Return values: only the return value of the *last* delegate in the list is returned; earlier returns are discarded (unless you manually iterate `GetInvocationList()` and collect).

    Exceptions: if any handler throws, invocation stops immediately and exception propagates; later handlers are NOT called. To ensure all run and observe all exceptions, iterate `GetInvocationList()` and invoke each with try/catch, optionally aggregate into `AggregateException`. `void` multicast events use this behavior; that's why event handlers should not throw.

- id: csharp-dispose-01
  answer: |
    `IDisposable` (`void Dispose()`) provides deterministic cleanup of resources (unmanaged handles, file locks, subscriptions) - releasing promptly instead of waiting for GC. Call `Dispose()` when done.

    `using` statement: `using (var r = new Res()) { ... }` or `using var r = new Res();` ensures `Dispose()` called even if exception, via try/finally.

    Difference: `using` statement has explicit block scope: `using (var x = ...) { /* x disposed at } */`. `using` declaration (C#8) is `using var x = ...;` with no braces - disposed at end of enclosing block (method/block) when variable goes out of scope. Declaration is syntactic sugar for less nesting but lifetime is larger; statement gives tighter control and can dispose earlier, especially important in loops.

- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` (`ValueTask DisposeAsync()`) and `await using` are for resources whose cleanup is asynchronous (needs await) - e.g. async flush, network close, `IAsyncEnumerable` enumerator.

    `await using var r = new AsyncRes();` or `await using (var r=...)` will `await r.DisposeAsync()` at scope exit, correctly asynchronously.

    Use over `IDisposable` when Dispose may do I/O or must not block (asynchronous file streams, DB connections, async locks). If type implements both, `await using` prefers async; plain `using` calls sync Dispose (may block). Only use `await using` in async method/context.

- id: csharp-dispose-03
  answer: |
    `Dispose` (IDisposable) = deterministic, explicit, called by user via `Dispose()`/`using`, runs promptly on same thread, can access managed objects, should suppress finalizer.

    Finalizer (`~MyClass(){}` / `Finalize`) = non-deterministic, called by GC on finalizer thread if object becomes unreachable and wasn't disposed, runs later (maybe never if process exits). Cannot safely access other managed objects (they may be finalized), must only free unmanaged resources, cannot throw.

    Need finalizer only when type *directly* holds unmanaged resource (IntPtr/handle, native allocation) and wants safety net if consumer forgets Dispose. Pure managed wrappers should NOT have finalizer - they just cascade Dispose to owned disposables. In modern code, use `SafeHandle` instead of writing your own finalizer. If you have finalizer, implement full dispose pattern with `Dispose(bool)`.

- id: csharp-dispose-04
  answer: |
    Full pattern (classic, rarely needed now):
    ```csharp
    class MyResource : IDisposable {
      private IntPtr _handle; private FileStream _managed; private bool _disposed;
      public void Dispose() { Dispose(true); GC.SuppressFinalize(this); }
      protected virtual void Dispose(bool disposing) {
        if(_disposed) return;
        if(disposing) { _managed?.Dispose(); } // free managed only when disposing true
        // free unmanaged always
        CloseHandle(_handle); _handle = IntPtr.Zero;
        _disposed = true;
      }
      ~MyResource() { Dispose(false); } // finalizer
    }
    ```
    Steps: public Dispose() calls Dispose(true) + SuppressFinalize; protected virtual Dispose(bool disposing) branches; finalizer calls Dispose(false); guard double-dispose; derive overrides call base. Now prefer `SafeHandle` for unmanaged, so often finalizer not needed and simple `Dispose()` suffices.

- id: csharp-span-01
  answer: |
    Collection expressions (C#12) unified syntax for creating collections: `[1,2,3]` can target `T[]`, `List<T>`, `Span<T>`, `IEnumerable<T>`, etc. `[]` is empty. Compiler uses target type's creation (array, collection initializer, `Create`).

    Spread element `..` (e.g. `[..a, 1, ..b]`) spreads/inlines another collection into the new collection, like `Concat`. `..a` enumerates `a` and inserts elements. `..` alone can be empty collection. Example: `int[] c = [..a, ..b];` `List<int> l = [..span];` `.AddRange` alternative.

- id: csharp-span-02
  answer: |
    `Span<T>` = ref struct contiguous memory view over array/stack/native, without owning; mutable, no allocation, can slice (`span[1..3]`). `ReadOnlySpan<T>` = readonly view (works for `string`). `Memory<T>`/`ReadOnlyMemory<T>` = heap-storable counterpart (struct but not ref struct) that can be stored in fields and across awaits.

    Can't store `Span<T>` in field or across `await` because it's `ref struct`: it may contain interior/ stack reference that must not outlive the stack frame and cannot be heap-allocated or moved by GC. Compiler enforces: no boxing, no field of class, no capture in lambda/async/iterator, no `await` in method with Span live across await (would need to hoist to heap). Use `Memory<T>` for async/fields then `.Span` to get Span synchronously when needed.

- id: csharp-span-03
  answer: |
    `stackalloc` allocates memory on the current stack frame instead of heap: `Span<int> s = stackalloc int[256];` Very fast, no GC, automatically freed when method returns (no explicit free).

    Must be careful: stack is limited (~1MB per thread by default), so don't allocate large or unbounded sizes -> stack overflow corrupts process. Don't return the Span/memory outside method (use-after-return). Only use in safe/checked context when size is small/constant. In loops, repeated stackalloc per iteration can grow stack. For large buffers, fall back to `ArrayPool<T>.Shared.Rent` or heap. In older C# requires `unsafe` fix; now `Span<T> stackalloc` is safe in safe context.

- id: csharp-span-04
  answer: |
    `T[]` array: fixed size, contiguous, allocated on heap, covariance (dangerous), `Length`, stackalloc possible, can be `Span<T>`.

    `List<T>` generic dynamic array: resizable, wrapper over `T[]` with `Count`/`Capacity`, `Add`/`Remove`, richer API. Overhead but flexible. Both implement `IList<T>`.

    `params` collections: `void Foo(params T[] args)` / `params IEnumerable<T>` / `params Span<T>` / `params List<T>` (C#13 params collections) allows calling `Foo(1,2,3)` as variadic sugar; compiler creates array/collection. `params` must be last parameter, only one. `params` with `Span<T>` avoids allocation when combined with collection expressions.
```
