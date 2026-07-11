# csharp knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: csharp-null-01
  answer: |
    Nullable reference types (NRTs) are a compile-time-only feature that lets the compiler track nullability and emit warnings when you might dereference null or assign null to something not expected to be null. They have NO runtime effect — `string?` and `string` compile to the same IL; there is no boxing or `Nullable<>` wrapper (unlike nullable value types).
    - `?` annotation (e.g. `string?`): marks the reference type as nullable, telling the compiler null may legitimately flow there; dereferencing still requires null-checks to avoid warnings.
    - `!` operator (null-forgiving / "damn-it" operator, e.g. `x!.Length`): suppresses the nullability warning for that expression; it does nothing at runtime.
    - `#nullable enable` (and `#nullable disable`/`warn`): turns the nullable annotation+warning context on/off for a region of code; needed to opt a file/project into NRT analysis.

- id: csharp-null-02
  answer: |
    Value types (structs, enums, primitives) store their data directly — usually on the stack or inline inside another object; assignment copies the whole value. Reference types (classes, delegates, strings, arrays) store a reference to an object on the heap; assignment copies the reference, so two variables can point at the same object.
    The surprising part: a `List<T>` or a `readonly` field of a struct value type returns a COPY when you index it. So `list[0].X = 5;` fails to compile ("cannot modify the return value of ... because it is not a variable") and inside a method `var s = readonlyField; s.X = 5;` mutates only the copy — the original is unchanged. Mutations to struct fields only take effect if you operate on a real variable, not on a copy returned from a property/indexer.

- id: csharp-null-03
  answer: |
    - record class (just `record`): reference type with compiler-synthesized value-based equality (Equals/==/GetHashCode over all members), a ToString, and with-expressions (`with`) for non-destructive mutation. `==` uses value equality, not reference equality.
    - record struct: a struct (value type) with the same synthesized equality/ToString/with support; value semantics already, but the record additions make equality and printing based on members.
    - plain class: reference type; `==`/`Equals` are reference equality by default (unless overridden), and there is no `with` support or auto ToString of members.
    Mutability: records are conventionally immutable (init-only props) but can have mutable fields; a `record struct` can also be mutated like any struct. The key distinction is the synthesized equality and `with`, plus class vs struct semantics.

- id: csharp-null-04
  answer: |
    - `init` accessor: a property setter usable only during object initialization (in a constructor or object initializer). After initialization completes, the property is effectively read-only; later assignment is a compile error.
    - `required` modifier: marks a field/property that the caller MUST set when constructing the object (via object initializer or constructor); the compiler errors if it is not set.
    They combine naturally: `public required string Name { get; init; }` must be supplied by the caller and then becomes immutable. The compiler enforces both rules at construction time.

- id: csharp-pattern-01
  answer: |
    A switch expression (`expr switch { ... }`) is an EXPRESSION that evaluates to a value and uses `=>` for each arm with commas separating arms; it is designed to produce a result concisely. A switch statement is a STATEMENT with `case` labels and `break`/`return`, performing actions.
    Exhaustiveness: the switch expression REQUIRES its arms to be exhaustive at compile time — if no arm (and no `_` discard default) covers all inputs, you get a compile error "not all code paths return a value / switch expression does not handle all...". A switch statement with no `default` simply won't error at compile time and may fall through without doing anything at runtime.

- id: csharp-pattern-02
  answer: |
    - Property patterns: `o is { Name: "x", Age: > 18 }` test an object by matching on the values of its properties/fields; you can nest and use `var` to capture: `o is { Name: var n }` binds n.
    - Positional patterns: match on deconstructed values, e.g. `point is (var x, var y)` or `(0, 0)` — requires the type to be deconstructable (have a `Deconstruct` or be a tuple/record).
    Binding: sub-patterns bind variables via `var name` (or discards `_`); the bound variable is in scope in the containing block where the match is provably true (for `is`, after the condition; for switch-expression arms, within that arm's result expression).

- id: csharp-pattern-03
  answer: |
    - Relational patterns: `<`, `<=`, `>`, `>=` compared against a constant (e.g. `age is >= 18 and < 65`).
    - Logical patterns: `and`, `or`, `not` to combine patterns (e.g. `not null`, `x is >=0 and <=10`). `or` has lower precedence than `and`.
    - List patterns: match sequences/arrays by shape, e.g. `[1, 2, _]` or with a slice `..`: `[.. var head, last]` captures a prefix/suffix. Works on arrays, strings, and any type with an indexer + Length/Count and a slice if used with `..`.

- id: csharp-pattern-04
  answer: |
    `obj is Customer c` does a type test AND, if it succeeds, assigns the typed value to `c` in a single operation — no separate cast that could throw or require an `as`+null check. With a plain type check plus cast you'd write `if (obj is Customer) { var c = (Customer)obj; ... }`, doing the check and the conversion as two steps.
    Scope of the bound variable: inside an `is` pattern, `c` is in scope only within the `if` block (or expression) where the match succeeded, and the compiler knows it is non-null/assigned there. In a switch arm or expression arm, it's scoped to that arm's body.

- id: csharp-linq-01
  answer: |
    LINQ uses deferred (lazy) execution for most query operators: methods like `Where`, `Select`, `OrderBy`, `GroupBy`, `SelectMany`, `Join` do NOT execute when called — they build an enumerable that runs only when enumerated (foreach, GetEnumerator). This means the query is re-evaluated each time you enumerate and reflects data changes.
    Immediate execution materializes results right away: `ToList`, `ToArray`, `Count`, `Sum`, `First`, `FirstOrDefault`, `Single`, `Any`, `Aggregate`, `ToDictionary`. These trigger enumeration and return a concrete value/collection.

- id: csharp-linq-02
  answer: |
    `IEnumerable<T>` LINQ (LINQ to Objects) executes in memory using compiled delegates; each operator iterates the underlying sequence. `IQueryable<T>` LINQ builds an expression tree that a provider (e.g. EF Core) translates into something else — usually SQL — and executes remotely.
    Danger of mixing: calling an `IEnumerable` operator (or `.AsEnumerable()`) on an `IQueryable` switches to client-side execution, pulling ALL rows into memory and then filtering/applying logic locally. This can silently cause huge data loads and defeats server-side querying (e.g. `query.Where(x => ExpensiveLocalMethod(x))` cannot be translated and forces the whole table into memory).

- id: csharp-linq-03
  answer: |
    Pitfall: because of deferred execution, enumerating the same LINQ query variable twice RE-RUNS the whole pipeline — re-querying the database, re-reading the file, re-computing, and re-applying side effects each time. This is wasted work and, for side-effecting or non-idempotent queries, inconsistent results.
    Avoid by materializing once: call `.ToList()` / `.ToArray()` / `.ToDictionary()` (or `.AsCached()`/`ToList` into a variable) so the query runs a single time, then iterate the cached collection.

- id: csharp-linq-04
  answer: |
    - `First`: returns the first matching element, throws `InvalidOperationException` if none.
    - `FirstOrDefault`: returns the first match, or the default value (null for refs, 0/struct default for value types) if none — never throws.
    - `Single`: returns the sole match, throws if zero OR more than one element exist.
    Value-type gotcha: `FirstOrDefault` on a non-nullable value type (e.g. `int`) returns `default(T)` = 0 when nothing matches, which is indistinguishable from a real zero value — so you can't tell "not found" from "found 0". Mitigate with `DefaultIfEmpty`+check, nullable return (`int?` via `Cast`), or by checking the source before querying. (`SingleOrDefault` has the same 0-vs-notfound ambiguity.)

- id: csharp-async-01
  answer: |
    Use `ValueTask<T>`/`ValueTask` instead of `Task`/`Task<T>` when a method is called VERY frequently on a hot path and often completes synchronously (result already available), so you avoid the allocation of a `Task` object on every call. It can wrap either a completed result (no allocation) or a real `Task`/IValueTaskSource.
    Constraints on consumption: a `ValueTask` may be awaited at most ONCE; you must not `await` it again, store it, await it concurrently, or use it with `Task.WhenAll`/`WhenAny` or multiple `await foreach`. If you need to consume it more than once, convert to `Task` via `.AsTask()`. These restrictions exist because the underlying object may be pooled/reused.

- id: csharp-async-02
  answer: |
    The compiler transforms an `async` method into a state machine: it splits the method at each `await`, captures the remainder as a continuation, and returns a `Task` that completes when the work finishes. The method runs synchronously until the first await of an incomplete operation, then yields.
    Deadlock from blocking: calling `.Result` / `.Wait()` / `.GetAwaiter().GetResult()` on an incomplete task blocks the calling thread. If that thread is a single-threaded context (UI thread, or classic ASP.NET `AspNetSynchronizationContext`) and the awaited code inside used `ConfigureAwait(true)` (the default), its continuation tries to post back onto that same blocked thread to resume — which can never run, so everyone waits forever. (ASP.NET Core's default thread-pool context avoids this, but blocking is still bad practice.)

- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` tells the awaiter not to capture the current `SynchronizationContext`/`ExecutionContext` — the continuation after the `await` will resume on a arbitrary thread-pool thread instead of being marshaled back to the original context.
    In library/infrastructure code this is recommended because the library shouldn't assume the caller needs to return to a specific context (e.g. the UI thread), avoids the classic deadlock above, and reduces context-switch overhead. (In app-level UI code you typically want the default `true` to return to the UI thread.)

- id: csharp-async-04
  answer: |
    `CancellationToken` enables cooperative cancellation: a long-running async operation periodically checks `token.IsCancellationRequested` or calls `token.ThrowIfCancellationRequested()`, which throws `OperationCanceledException`; the token is linked to a `CancellationTokenSource` that the caller cancels. Cancellation is cooperative — the operation must observe the token; it cannot be force-killed.
    `IAsyncEnumerable<T>` is an asynchronous stream: a method yields items with `await yield return` and is consumed with `await foreach`. It's for producing/consuming a sequence where each element is obtained asynchronously (e.g. streaming results from a DB or network) without buffering everything into a list.

- id: csharp-generics-01
  answer: |
    A `where` constraint on a type parameter restricts what types may be substituted for `T`, enabling you to use members/operations guaranteed by the constraint, and giving compile-time safety.
    Main kinds:
    - `where T : class` (reference type) / `where T : struct` (non-nullable value type)
    - `where T : new()` (must have a public parameterless constructor)
    - `where T : BaseType` (must derive from/inherit BaseType)
    - `where T : IInterface` (must implement the interface)
    - `where T : unmanaged`, `notnull`, `Enum`, `Delegate`, `System.Collections.Generic.IEnumerable<U>` etc.
    Multiple constraints combine with a single `where`; you can have multiple `where` clauses per type param.

- id: csharp-generics-02
  answer: |
    Generic variance lets you assign a generic type with one type argument to one with a related type argument.
    - `out` = covariant: the type parameter appears only in OUTPUT positions (return values), so you can use a more-derived type where a base is expected, e.g. `IEnumerable<out T>`, `IEnumerable<Cat>` is an `IEnumerable<Animal>`.
    - `in` = contravariant: the parameter appears only in INPUT positions, so you can use a LESS-derived type, e.g. `Action<in T>`, `Action<Animal>` can be assigned where `Action<Cat>` is expected (it just accepts more).
    Variance applies only to interface and delegate type parameters, and requires the compiler to prove the type never escapes in the wrong direction.

- id: csharp-generics-03
  answer: |
    Type inference: for a generic method call, the compiler deduces the type arguments from the types of the arguments you pass (e.g. `T Min<T>(T a, T b)` called as `Min(3, 5)` infers `int`). This works through the actual argument types/return targets.
    You must specify explicitly when inference can't determine `T`: when the method takes no arguments (e.g. `Foo<T>()`), when the arguments are unrelated/ambiguous, when you want a different type than inferred (e.g. `Sum<int>(...)` on a `long`), or to disambiguate overloads. Syntax: `Method<int>(args)`.

- id: csharp-generics-04
  answer: |
    `default(T)` yields the default value for type `T`: `null` for reference types, the zero bit-pattern for value types (0 for numbers, false for bool, all-zero struct for structs). For nullable value types it yields `null`.
    Generic code NEEDS it because within `<T>` you cannot assume `T` is a reference type (so you can't write `null`) or a value type (so you can't write `0`); `default(T)` gives a single, always-valid "empty/no value" expression for initializing fields, clearing buffers, returning "nothing", or satisfying out parameters regardless of what `T` is.

- id: csharp-delegate-01
  answer: |
    A delegate is a type-safe, object-oriented function pointer: a reference type that holds one or more method references with a specific signature, which can be invoked like a method.
    - `Func<...>`: a delegate returning a value; the last type argument is the return type (e.g. `Func<int, int>` takes int, returns int).
    - `Action<...>`: a delegate returning void (e.g. `Action<string>` takes a string, returns nothing).
    - `Predicate<T>`: legacy delegate returning `bool` (a `Func<T,bool>`); historically used for filtering. All are just convenience generic delegate types so you rarely declare custom delegate types.

- id: csharp-delegate-02
  answer: |
    A plain public delegate field lets any external code do anything: add handlers (`+=`), remove them (`-=`), REPLACE the whole list (`= null` wipes all subscribers), and even INVOKE it. The `event` keyword restricts the delegate to be an event: outside the declaring class you can only `+=` and `-=` (subscribe/unsubscribe); you cannot assign/replace it, and you cannot invoke it from outside. Inside the class it behaves like a normal delegate you can invoke/raise. This encapsulation prevents external code from clearing or firing the event and enforces the publisher/subscriber model.

- id: csharp-delegate-03
  answer: |
    The issue is variable capture. Before C# 5, the `foreach` loop variable was defined OUTSIDE the loop, so every lambda closing over it captured the SAME variable — all lambdas saw the final value. Since C# 5, `foreach` introduces a FRESH iteration variable each iteration, so closures over it are safe (each lambda sees its own value).
    For `for`/`while` loops, the loop variable is STILL a single shared variable, so a lambda capturing `i` and invoked later sees the final value — unless you copy it into a local inside the loop body (`int local = i;` and close over `local`), which each iteration makes distinct. So: be careful with `for`/loop counters; `foreach` is now safe.

- id: csharp-delegate-04
  answer: |
    A multicast delegate chains multiple methods (via `+=`); invoking it calls each subscriber in order.
    - Return values: only the LAST invoked method's return value is returned to the caller; all earlier return values are discarded. (For collecting results you must use `GetInvocationList()` and invoke each separately.)
    - Exceptions: if any handler throws, the exception propagates immediately and subsequent handlers in the chain are NOT called. Again, iterating `GetInvocationList()` lets you invoke each one individually and handle errors per-handler.

- id: csharp-dispose-01
  answer: |
    `IDisposable` defines `Dispose()` for deterministic, explicit release of resources (file handles, DB connections, unmanaged memory) that the garbage collector won't reclaim promptly. `Dispose` is called by the consumer (or `using`).
    - `using` statement: `using (var x = new C()) { ... }` — wraps in try/finally and disposes `x` at the end of the explicit block.
    - `using` declaration: `using var x = new C();` — no braces; `x` is disposed at the end of the enclosing scope (method/block). It's just syntactic sugar for the same try/finally cleanup, using scope rather than an explicit block.

- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` provides `DisposeAsync()` returning a `ValueTask`, for cleanup that itself requires awaiting (e.g. closing an HTTP connection, flushing an async stream). `await using` (and `await using var`) ensures `DisposeAsync` is awaited at scope exit, just as `using` calls `Dispose`.
    Use `IAsyncDisposable`/`await using` when the resource's teardown is inherently async; use plain `IDisposable`/`using` when cleanup is synchronous. A type can implement both, but you should pick the appropriate one to avoid blocking on async work.

- id: csharp-dispose-03
  answer: |
    - Finalizer (`~ClassName()` / `Finalize`): called non-deterministically by the GC before reclaiming the object; it runs on the finalizer thread and is a safety net for unmanaged resources. You cannot call it directly.
    - `Dispose`: deterministic, called explicitly (via `using`) by the consumer, promptly releasing resources.
    A type needs a finalizer ONLY if it directly holds unmanaged resources and needs a backstop in case the caller forgets to dispose (so the unmanaged handle is still released). If you only hold managed `IDisposable` objects, you don't need a finalizer — `Dispose` is sufficient. Finalizers also add GC overhead (objects survive an extra collection).

- id: csharp-dispose-04
  answer: |
    The full disposable pattern for a type owning both managed and unmanaged resources:
    1. Implement `IDisposable` with `Dispose()` that calls `Dispose(true)`, then `GC.SuppressFinalize(this)`.
    2. Provide `protected virtual void Dispose(bool disposing)`: free unmanaged resources ALWAYS; free managed resources ONLY when `disposing == true` (because during finalization managed objects may already be finalized).
    3. If the type holds unmanaged resources directly, implement a finalizer `~Class() => Dispose(false);` so the unmanaged cleanup still happens if `Dispose` was never called.
    4. Subclasses override `Dispose(bool)` calling `base.Dispose(disposing)`. If a subclass adds new disposable/unmanaged state, it overrides and chains to base.

- id: csharp-span-01
  answer: |
    Collection expressions (C# 12) are a concise, target-typed syntax to create collections: `[1, 2, 3]` can become an `int[]`, `List<int>`, `Span<int>`, etc., depending on the target type — no need to name the concrete type.
    The spread element `..` inlines the contents of another collection into the new one: `[.. a, 5, .. b]` produces the elements of `a`, then 5, then the elements of `b`. It works with arrays, spans, lists, and `IEnumerable` sources, and the spread must expand to a compatible element type.

- id: csharp-span-02
  answer: |
    `Span<T>` and `ReadOnlySpan<T>` are `ref struct` types that represent a contiguous region of arbitrary memory (array slice, `stackalloc`, or unmanaged pointer) with safe indexing and NO heap allocation/copy. They give array-like performance with bounds safety. `Memory<T>` is similar but is a regular struct (not ref) — it can be stored on the heap, boxed, used as a field, and used across `await`.
    You cannot store a `Span<T>` in a field or use it across an `await` because `ref struct`s can never be boxed or live on the heap (they may contain managed pointers), and async methods can hoist locals onto the heap as fields of the state machine — which would violate that rule. `Memory<T>` exists precisely for those heap/async scenarios.

- id: csharp-span-03
  answer: |
    `stackalloc` allocates a block of memory on the call stack rather than the heap, e.g. `Span<int> buffer = stackalloc int[256];`. It's very fast and needs no GC, and pairs naturally with `Span<T>`/`ReadOnlySpan<T>` (it returns a span).
    Cautions:
    - The stack is small (typically ~1 MB); allocating large buffers risks `StackOverflowException` (uncatchable).
    - The memory is only valid for the current method's execution; you must not let the span (or any reference into it) escape to a wider scope, async, or another thread — it becomes invalid (dangling) after the method returns.
    - It's not GC-tracked, so referencing objects held only on the stack via stackalloc can be unsafe if misused.

- id: csharp-span-04
  answer: |
    - Array `T[]`: fixed-size, contiguous, allocated on the heap, low-level; length is immutable after creation (you must allocate a new array to resize), and it implements `IList<T>`.
    - `List<T>`: a class wrapping a resizable array; it grows/copies automatically, offers `Add`/`Remove` and rich APIs; slightly more overhead but far more convenient for dynamic collections.
    - `params` collections (C# 13, expanding earlier `params T[]`): a parameter can be declared `params ReadOnlySpan<T>` or `params List<T>` (or array) so callers can pass either a comma-separated list of args or a single collection, with the compiler choosing efficient handling. `params ReadOnlySpan<T>` avoids allocating an array for the arguments, improving perf over the classic `params T[]`.
```
