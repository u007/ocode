# cat-jam-free — WITH-SKILL closed-book answers for csharp (opencode-go, 2026-10-14)
# Delivery: derived/csharp.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/csharp.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: csharp-null-01
  answer: |
    Nullable reference types add compile-time null-state tracking and warnings for possible null dereferences; they do not change runtime types. `?` on a reference type is a compile-time-only annotation with no runtime effect, `!` only suppresses a nullable warning with no runtime check, and `#nullable enable` turns the analysis context on for the following code (warnings/errors depending on configuration).
- id: csharp-null-02
  answer: |
    Value types store data directly and are copied by value; reference types store a reference to heap data. Defaults are zeroed, non-null instances for value types (null only via Nullable<T>) and null for reference types. Mutating a struct held in a readonly field or obtained from a collection often operates on a temporary copy, so the change can be lost; arrays allow in-place element mutation.
- id: csharp-null-03
  answer: |
    `record`/`record class` is a reference type with compiler-generated value equality, `with` expressions, and init-only positional members by default. `record struct` is a value type with value equality but mutable `get; set;` positional members unless marked `readonly`. A plain `class` uses identity equality by default and has none of those generated features unless added.
- id: csharp-null-04
  answer: |
    `init`-only setters can be assigned during construction/object initialization but not afterward. `required` members must be set by an object initializer, or by a constructor annotated to set required members. Together, a `required init` member must be supplied at construction and then becomes immutable.
- id: csharp-pattern-01
  answer: |
    A switch expression produces a value and uses `pattern => result` arms; a switch statement executes statements and can fall through empty cases. A switch expression must be exhaustive: if no arm matches at runtime it throws, and missing coverage is normally a compile-time error unless covered by a discard/default pattern.
- id: csharp-pattern-02
  answer: |
    Property patterns match and bind nested property values, e.g. `{ Name: var n }`. Positional patterns deconstruct a type by position, e.g. `(int X, int Y)`, often via a `Deconstruct` method. Both can nest patterns and bind variables.
- id: csharp-pattern-03
  answer: |
    Relational patterns compare a value with a constant using `<`, `>`, `<=`, or `>=`. Logical patterns combine patterns with `and`, `or`, and `not`. List patterns match sequence elements, including wildcards, slice patterns such as `..`, and length checks.
- id: csharp-pattern-04
  answer: |
    `obj is Customer c` combines the type test and safe cast into one operation, avoiding an invalid-cast exception and a separate cast, and binds `c` only when the test succeeds. The variable is in scope for the remainder of the enclosing block but is definitely assigned only on the successful-match path.
- id: csharp-linq-01
  answer: |
    Deferred execution stores the query recipe and runs it when enumerated; operators returning `IEnumerable<T>`, such as `Where`, `Select`, `OrderBy`, `GroupBy`, and `Join`, are deferred. Immediate execution runs during the call for operators that materialize or aggregate, such as `ToList`, `ToArray`, `ToDictionary`, `Count`, `Sum`, `First`, `Single`, and `Aggregate`, as well as `foreach`.
- id: csharp-linq-02
  answer: |
    `IEnumerable<T>` LINQ executes compiled delegates in memory. `IQueryable<T>` builds an expression tree that a provider translates, for example to SQL. Mixing them can force client-side evaluation, transfer too much data, or fail provider translation.
- id: csharp-linq-03
  answer: |
    Enumerating the same query twice reruns the whole pipeline, repeating work, side effects, and possibly seeing changed source data. Materialize the result once with `ToList` or `ToArray`, then work with that collection.
- id: csharp-linq-04
  answer: |
    `First` returns the first item and throws if empty; `FirstOrDefault` returns `default(T)` if empty. `Single` requires exactly one item and throws otherwise; `SingleOrDefault` allows zero or one. For value types, `FirstOrDefault` returns a normal default such as 0, which is indistinguishable from a stored 0.
- id: csharp-async-01
  answer: |
    Return `ValueTask`/`ValueTask<T>` when a method often completes synchronously and you want to avoid allocating a `Task`. A `ValueTask` must be consumed once; do not await it multiple times or inspect it repeatedly. Use `AsTask()` if multiple awaits or broader usage are needed.
- id: csharp-async-02
  answer: |
    The compiler lowers `async` methods into a state machine returning `Task` or `Task<T>`. Blocking with `.Result` or `.Wait()` can deadlock when the continuation is posted back to a captured synchronization context, because the blocked thread cannot run it. The fix is to await all the way up the call chain.
- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` prevents the await from capturing the current synchronization context, so continuations run on the thread pool instead of being marshaled back. Library code uses it to avoid context-dependent deadlocks and reduce overhead; UI code usually omits it when updating the UI.
- id: csharp-async-04
  answer: |
    `CancellationToken` supports cooperative cancellation: callers signal it, and operations check or pass it, throwing `OperationCanceledException` when canceled. `IAsyncEnumerable<T>` is an async stream consumed with `await foreach`; the consumer passes a token with `.WithCancellation(token)`, and the producer parameter marked `[EnumeratorCancellation]` receives it.
- id: csharp-generics-01
  answer: |
    A `where` clause both restricts which types can be used for `T` and unlocks operations on `T`, such as `new()` enabling `new T()` and base class/interface constraints enabling their members. Without constraints, only `object` members are available. Main kinds include `class`, `struct`, `notnull`, `unmanaged`, `new()`, and base-type or interface constraints.
- id: csharp-generics-02
  answer: |
    Variance applies to generic interfaces and delegates with reference types. `out` makes a type parameter covariant, usable in output positions, such as `IEnumerable<out T>`. `in` makes it contravariant, usable in input positions, such as `IComparable<in T>`. Classes remain invariant.
- id: csharp-generics-03
  answer: |
    Type arguments are inferred from the method call's arguments. Specify them explicitly when there are no arguments, when inference cannot determine `T`, when inference is ambiguous, or when you need a type different from the inferred one.
- id: csharp-generics-04
  answer: |
    `default(T)` produces the type's default: `null` for reference types and a zeroed value for value types (0, false, or an all-zero struct). Generic code needs it because `T` may be either reference or value, so `null` or `new T()` is not generally valid.
- id: csharp-delegate-01
  answer: |
    A delegate is a type-safe reference to a method, including its target object. `Func<...>` represents delegates returning a value, `Action<...>` represents delegates returning void, and `Predicate<T>` represents a delegate returning bool.
- id: csharp-delegate-02
  answer: |
    `event` restricts invocation to the declaring type, allows external code only to subscribe with `+=` and unsubscribe with `-=`, and provides add/remove accessor semantics. It prevents external code from invoking or replacing the underlying multicast list directly.
- id: csharp-delegate-03
  answer: |
    A `for` loop has one loop variable, so lambdas capturing it may all see its final value. A `foreach` loop has a per-iteration variable since C# 5, so capturing it is safe; capturing a variable declared inside the loop body is also safe.
- id: csharp-delegate-04
  answer: |
    A multicast delegate invokes each handler in its invocation list in order. It returns the last handler's return value and discards earlier ones. If one handler throws, invocation stops and the exception propagates.
- id: csharp-dispose-01
  answer: |
    `IDisposable.Dispose` releases resources deterministically. A `using` statement wraps its scope in try/finally and disposes at block end; a `using` declaration disposes the variable at the end of its enclosing scope without a nested block.
- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` supports asynchronous cleanup through `DisposeAsync`, and `await using` awaits it. Use it when disposal performs I/O or other awaitable work, or for types implementing it; otherwise synchronous `IDisposable` and `using` are sufficient.
- id: csharp-dispose-03
  answer: |
    A finalizer is runtime-invoked, non-deterministic cleanup intended for unmanaged resources when `Dispose` was not called; `Dispose` is deterministic cleanup. Implement a finalizer only when a type directly owns unmanaged resources not wrapped by safe handles; prefer `SafeHandle` and avoid finalizers otherwise. `Dispose` calls `GC.SuppressFinalize(this)`.
- id: csharp-dispose-04
  answer: |
    Implement `IDisposable`, with `Dispose()` calling `Dispose(true)` and then `GC.SuppressFinalize(this)`. In `protected virtual void Dispose(bool disposing)`, guard with a `_disposed` flag, dispose managed resources only when `disposing` is true, and always release unmanaged resources. A finalizer should call `Dispose(false)`.
- id: csharp-span-01
  answer: |
    Collection expressions (C# 12) use syntax such as `[a, b, c]` to create arrays, lists, spans, and other collections in a target-typed way. The spread element `..expr` inserts all elements from another collection.
- id: csharp-span-02
  answer: |
    `Span<T>` and `ReadOnlySpan<T>` are ref structs representing contiguous memory views; `ReadOnlySpan<T>` is read-only. `Memory<T>` is a normal struct that can be stored on the heap. `Span<T>` cannot be a field, boxed, or used across `await` because ref structs must remain stack-local.
- id: csharp-span-03
  answer: |
    `stackalloc` allocates memory in the current stack frame, often as a `Span<T>`, avoiding heap allocation. Be careful with large sizes, recursion, and lifetime; use only amounts known to fit and avoid risking stack overflow.
- id: csharp-span-04
  answer: |
    An array `T[]` has fixed length, while `List<T>` wraps a resizable array with dynamic `Count` and `Capacity` and amortized O(1) `Add`. Params collections in C# 13 allow a `params` parameter to be `Span<T>`, `ReadOnlySpan<T>`, `IEnumerable<T>`, `List<T>`, and others, letting the compiler avoid per-call array allocation.
