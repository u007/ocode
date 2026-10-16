# nvidia/nemotron-3.5-super-vl — closed-book answers for csharp (openrouter, 2026-10-16); answerer saw only _prompts/csharp.md
- id: csharp-null-01
  answer: |
    Nullable reference types (NRTs) are a compile-time static-analysis feature (C# 8): `string?` may hold null while plain `string` is treated as non-nullable, and the compiler emits warnings/errors on likely null dereferences, assigning a nullable to a non-nullable, etc. The `?` annotation itself has no runtime effect — it is annotation/metadata only and the CLR does not enforce it. The `!` null-forgiving operator likewise has no runtime effect; it only suppresses the compiler's nullability warnings for that expression. `#nullable enable` turns on the nullable annotation context for a file/project: reference types become non-nullable by default, annotations are recorded, and nullability warnings are enabled. (The four contexts are disable, enable, warnings-only, and annotations-only.)
- id: csharp-null-02
  answer: |
    Value types (structs, enums, primitives) hold their data inline — a variable of struct type contains the value itself, and assignment copies the whole value. Reference types (class, delegate, string) are heap objects; variables hold references and assignment copies the reference. The surprising mutation cases: (1) a mutable struct held in a `readonly` field — because the field cannot be reassigned, the compiler copies the struct before member access (treating it like `in`), so a mutating call appears to succeed but the mutation is silently discarded on the copy. (2) Collection element access: a `List<T>` indexer returns a copy (it is a property, not a storage location), so `list[i].Field = x` either fails to compile ("not a variable") or mutates a temporary that is never written back, leaving the stored element unchanged. Arrays are the exception: `arr[i]` is a variable, so mutations stick.
- id: csharp-null-03
  answer: |
    `record class` (C# 9) is a reference type whose compiler synthesizes value-based equality (`IEquatable<T>`, `Equals`, `GetHashCode`, `==`/`!=` keyed on `EqualityContract`), a `with` non-destructive copy expression, deconstruction, and `ToString` via `PrintMembers`; positional records get init-only primary-constructor properties. `record struct` (C# 10) is the same code generation but as a value type — field-by-field value equality without reflection, `with` support, still stack/inline allocated (not a `ref struct`). A plain `class` uses reference equality by default (`==`/`Equals` compare identity), is typically mutable, and has no synthesized `with`, deconstructor, or `PrintMembers`. Use records for data-centric, preferably immutable types where value equality matters; classes for identity-oriented modeling.
- id: csharp-null-04
  answer: |
    `init`-only properties (C# 9) expose an `init` accessor instead of `set`: assignable during object initialization (object initializer or constructor), read-only afterwards. `required` members (C# 11) are properties/fields the compiler forces every construction site to supply via an object initializer; an ordinary constructor does not satisfy the requirement unless marked `[SetsRequiredMembers]`, and a required member without a setter must be initialized in such an attributed constructor. They combine naturally: `required` + `init` means the member must be supplied during initialization and can never change afterward (e.g. `public required string Name { get; init; }`).
- id: csharp-pattern-01
  answer: |
    A switch expression (C# 8) is an expression that yields a value: arms are `pattern => expression`, evaluated top to bottom with first match winning and no fall-through. A switch statement is a statement with `case` blocks where fall-through is possible (`break`/`goto` required). Exhaustiveness matters for expressions: if no arm matches the input at runtime, it throws `SwitchExpressionException`, and the compiler warns when arms provably don't cover all inputs (e.g. no discard `_`). A non-exhaustive switch statement simply does nothing when no case matches.
- id: csharp-pattern-02
  answer: |
    Property patterns match an object's properties/fields by name, each with a nested sub-pattern: `{ Age: > 30, Address: { City: "London" } }`; bindings use `var`/`is var` (e.g. `{ Age: var age }`). Positional patterns match via deconstruction and bind by position: `p is Point(var x, var y)` invokes the deconstructor and assigns each slot to a fresh variable. Property patterns bind by member name; positional patterns bind by constructor/deconstructor order.
- id: csharp-pattern-03
  answer: |
    Relational patterns compare a value against a constant with `<`, `>`, `<=`, `>=` (e.g. `is >= 0 and <= 100`). Logical patterns combine patterns with `and`, `or`, `not` (e.g. `is not null`, `is > 0 and < 10`, `is "red" or "blue"`). List patterns (C# 11) match sequences element by element: `[1, 2, 3]`, `[1, .. rest]` (the `..` slice matches zero or more elements and binds the remainder), `[..]`, `[1, .., 2]`, binding individual elements with `var`.
- id: csharp-pattern-04
  answer: |
    `obj is Customer c` performs the type test and, on success, assigns `obj` to a fresh strongly-typed variable `c` in one step, with flow analysis: `c` is definitely assigned and treated as non-null `Customer` within the enclosing scope (typically the matching branch/nearest enclosing statement block); on the failed path `c` is unassigned. A plain check-plus-cast (`if (obj is Customer) { var c = (Customer)obj; }`) repeats the test and requires an explicit cast. The bound variable lives in the enclosing scope of the `is` expression — usable inside the matching branch, not after a failed test outside it.
- id: csharp-linq-01
  answer: |
    Deferred execution means the query only builds a lazy pipeline and does no work until the result is enumerated (`foreach`, `await foreach`, `.ToList()`, etc.); immediate execution runs the moment the operator is called. Deferred operators: `Where`, `Select`, `SelectMany`, `OrderBy`, `ThenBy`, `Take`, `Skip`, `Reverse`, `Cast`, `OfType`, `Zip`, `Distinct`, `Union`, etc. Immediate operators: `Count`, `LongCount`, `Sum`, `Min`, `Max`, `Average`, `First`, `FirstOrDefault`, `Single`, `SingleOrDefault`, `ToList`, `ToArray`, `ToDictionary`, `ToLookup`, `Contains`, `ElementAt`, `ElementAtOrDefault`, `Aggregate`. A consequence: deferred queries capture variables by reference and re-run on every enumeration.
- id: csharp-linq-02
  answer: |
    `IEnumerable<T>` is LINQ to Objects: operators take `Func<T,...>` delegates and run in memory. `IQueryable<T>` is LINQ to a provider (EF Core, LINQ to SQL): operators take `Expression<Func<T,...>>`, building an expression tree the provider translates (e.g. to SQL). The danger of mixing: supplying a plain `Func` where `Expression<Func>` is expected compiles the tree to a delegate, so the predicate runs in memory after all rows are pulled down — silent client evaluation, massive data transfer, or runtime translation failures. Calling `AsEnumerable()` too early forces the rest of the pipeline client-side as well; overload resolution can also silently bind to `Enumerable` instead of `Queryable`.
- id: csharp-linq-03
  answer: |
    Because deferred queries re-execute on every enumeration, enumerating the same query variable twice re-runs side effects (closures, random numbers, database calls) and can return inconsistent results. Avoid it by materializing once (`.ToList()`/`.ToArray()`) and reusing the collection, or by deliberately re-querying.
- id: csharp-linq-04
  answer: |
    `First` throws `InvalidOperationException` on empty input; `FirstOrDefault` returns `default(T)` instead (null for reference types, zero-value for value types). `Single`/`SingleOrDefault` require exactly one element: `Single` throws on zero or many, `SingleOrDefault` throws on many but returns default on zero. Value-type gotcha: `FirstOrDefault` on `IEnumerable<int>` returns `0` when empty, so you cannot distinguish "found 0" from "not found" without checking `Any()` first or using `First` with try/catch.
- id: csharp-async-01
  answer: |
    Return `ValueTask`/`ValueTask<T>` when completion is often synchronous (cached or already-completed result) to avoid allocating a `Task<T>` object — it is a struct wrapping either a result or an inner `Task`. Constraints: a `ValueTask` should be awaited exactly once; multiple awaits of the same instance are incorrect (the second may observe a consumed/disposed state). Do not pass it to APIs that await more than once (`Task.WhenAll`, repeated `.Result`), do not block on it repeatedly, and prefer `Task<T>` if callers might store or re-await it.
- id: csharp-async-02
  answer: |
    The compiler rewrites an `async` method into a state machine (`<MethodName>d__N`), hoisting locals and turning each `await` into a suspend/resume point: it gets the awaiter, checks `IsCompleted`, registers the remainder as a continuation on an incomplete task, and returns to the caller; completion resumes the state machine. Blocking with `.Result`/`.Wait()` on a thread with a synchronization context (UI, legacy ASP.NET) deadlocks: the continuation is posted back to that same context to resume, but the context thread is blocked waiting for the task — circular wait.
- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` tells the awaiter not to marshal the continuation back to the captured `SynchronizationContext`/`TaskScheduler`; the rest of the method runs on the thread pool/default executor. Recommended in library code because libraries do not need the caller's UI context: it avoids deadlocks when consumers block, removes context-marshaling overhead, and stops the library imposing its caller's ambient context.
- id: csharp-async-04
  answer: |
    Cancellation is cooperative: `CancellationTokenSource.Cancel()` flips `IsCancellationRequested`; consumers poll it, pass the token to APIs (`WithCancellation`, `Task.Delay(..., token)`, `Task.Run(..., token)`), or register callbacks (`token.Register`). Honoring APIs throw `OperationCanceledException` so the task ends `Canceled`. `IAsyncEnumerable<T>` is an asynchronously pulled stream of elements; `await foreach` enumerates it, awaiting each `MoveNextAsync`, and pairs with cancellation via `WithCancellation`.
- id: csharp-generics-01
  answer: |
    `where` clauses constrain a type parameter so the compiler knows which members may be used on `T`, checked at the call site. Main kinds: `where T : <base class>`, `where T : <interface>`, `where T : struct` (non-nullable value type), `where T : class` / `notnull` / `any` (reference-type flavor), `where T : new()` (parameterless constructor), `where T : U` (naked constraint binding one parameter to another), `where T : unmanaged` (C# 7.3). These let generic code call methods, compare, construct, or take addresses safely.
- id: csharp-generics-02
  answer: |
    Variance permits safer reference conversions between generic types. `out T` (covariant): T appears only in output positions (return values, get-only properties) — `IEnumerable<out T>` means `IEnumerable<string>` converts to `IEnumerable<object>`. `in T` (contravariant): T appears only in input positions (parameters) — `Action<in T>` means `Action<object>` converts to `Action<string>`. Applies to generic interfaces and delegates only (not classes), reference types only, with the compiler enforcing in/out position safety at the declaration site.
- id: csharp-generics-03
  answer: |
    For generic methods the compiler infers type arguments from the static types of the call-site arguments (`Do("hi")` infers `T = string`), unifying across multiple arguments and honoring constraints; the return type is generally not used for inference. Specify type arguments explicitly when inference is impossible or ambiguous: the parameter appears only in the return type (`Cast<T>(object)`), no argument carries it, multiple conflicting candidates exist, or you want to pin a particular type/constraint.
- id: csharp-generics-04
  answer: |
    `default(T)` produces the default value for whatever `T` is: `null` for reference types, zero-initialized for value types (0, false, zeroed struct fields). Generic code needs it because it cannot write `null` (illegal for non-nullable structs) or `0` (illegal for reference types) uniformly — `default(T)` works unconditionally for any type parameter, e.g. to initialize scratch variables or clear slots.
- id: csharp-delegate-01
  answer: |
    A delegate is a type-safe, reference-type function pointer: an immutable invocation list of methods with a compatible signature; `+=`/`Delegate.Combine` and `-=`/`Delegate.Remove` combine or remove targets. `Func<T..., TResult>` encapsulates methods returning a value; `Action<T...>` encapsulates void methods; `Predicate<T>` is `Func<T, bool>`. All are multicast-capable (`System.Delegate`/`MulticastDelegate`).
- id: csharp-delegate-02
  answer: |
    `event` wraps a delegate field so external code can only subscribe or unsubscribe (`+=`/`-=`); only the declaring class may invoke it or reset it (`= null`). This stops clients from clobbering other handlers or raising the event for everyone, and field-like events auto-generate thread-safe `add`/`remove` accessors.
- id: csharp-delegate-03
  answer: |
    Lambdas capture the variable, not its value. With a `for` loop there is a single shared loop variable across all iterations, so every lambda observes the final value after the loop. For `foreach`, modern C# (C# 5+) gives each iteration its own variable, so lambdas capture the per-iteration value and are safe. Either way, copying the iteration value into a local inside the body (`var copy = item;`) makes capture safe.
- id: csharp-delegate-04
  answer: |
    A multicast delegate holds multiple targets in its invocation list (`Delegate.Combine`). Invoking it runs targets in order; the observable return value is only the last target's (earlier results are discarded); if any target throws, the remaining targets are not invoked and the exception propagates to the caller. Use `GetInvocationList()` to invoke each target individually and handle exceptions per target.
- id: csharp-dispose-01
  answer: |
    `IDisposable.Dispose()` provides deterministic release of resources (file handles, sockets, database connections, etc.) instead of waiting for the GC. The `using { }` statement scopes disposal to a block (compiles to try/finally). The `using var x = ...;` declaration (C# 8) scopes disposal to the end of the enclosing scope (method or block), disposing multiple declarations in reverse order with less nesting.
- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable.DisposeAsync()` returns a `ValueTask` for releasing resources asynchronously (flush/close I/O without blocking threads). `await using x = ...;` (C# 8) awaits `DisposeAsync` at scope exit. Use it when the type implements `IAsyncDisposable` and disposal would block on async I/O (e.g. `SqlConnection`, streams used asynchronously); prefer `await using`/`DisposeAsync` in async code, falling back to `IDisposable` only when the type lacks the async API.
- id: csharp-dispose-03
  answer: |
    `Dispose` is deterministic, caller-driven cleanup; a finalizer (`~T()`) runs nondeterministically on the GC finalizer thread some time after the object becomes unreachable. A type needs a finalizer only when it directly owns unmanaged resources, as a backstop if `Dispose` is forgotten — paired with `GC.SuppressFinalize(this)` in `Dispose` so the backstop is unnecessary once disposed. Purely managed resources never need a finalizer.
- id: csharp-dispose-04
  answer: |
    Full pattern: (1) `public void Dispose() { Dispose(true); GC.SuppressFinalize(this); }`; (2) `protected virtual void Dispose(bool disposing)` — if `disposing`, release managed `IDisposable` fields; always release unmanaged resources (close handles), guard against running twice with a `_disposed` flag, and throw `ObjectDisposedException` on use after disposal; (3) `~ClassName() { Dispose(false); }` — the finalizer releases unmanaged resources without touching managed objects. Managed resources are freed only when `disposing == true`; unmanaged resources in both paths.
- id: csharp-span-01
  answer: |
    Collection expressions (C# 12) let you write `[1, 2, 3]` to initialize any collection type, target-typed to `T[]`, `List<int>`, `Span<int>`, `IEnumerable<T>`, etc., instead of `new[]` or initializer braces. The spread element `..` splices another sequence into a collection expression: `[..otherList, 4, 5]` copies `otherList`'s elements into the new collection.
- id: csharp-span-02
  answer: |
    `Span<T>` is a `ref struct` view over arbitrary contiguous memory (arrays, stack, unmanaged): stack-only, cannot be boxed, cannot be a field/array element/generic argument in most cases, and cannot cross `await` (async state machines are heap objects that cannot hold ref structs). `ReadOnlySpan<T>` is its read-only counterpart. `Memory<T>` is the heap-safe equivalent: not a ref struct, can be a field, usable across `await`, backed by arrays/strings or pooled memory. Use `Span` for synchronous scratch work within a method; `Memory` when the view must outlive it.
- id: csharp-span-03
  answer: |
    `stackalloc` allocates a buffer on the call stack (allowed directly in `Span<T>` expressions without `unsafe` since C# 7.2). Care: the stack is tiny (~1 MB), so oversized allocations overflow fatally; the buffer dies with the stack frame, so never return or store it beyond the method; it is not GC-tracked and needs no pinning, but bounds safety is your responsibility.
- id: csharp-span-04
  answer: |
    `T[]` is a fixed-size heap object, covariant (`string[]` converts to `object[]`), with immutable length after creation. `List<T>` is a mutable growable wrapper around an array (`Count` vs `Capacity`, `Add`/`Remove`, etc.), heap-allocated per instance and not covariant. `params` collections (C# 13): `params` parameters now accept any collection type, not just `T[]` — e.g. `params Span<T>`, `params List<T>`, `params IEnumerable<T>` — so call sites can use collection expressions (`M(1, 2, 3)`). Only one `params` parameter per method, and it must be last.
