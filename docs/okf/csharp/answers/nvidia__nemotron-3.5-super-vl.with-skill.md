# nvidia/nemotron-3.5-super-vl — with-skill answers for csharp (openrouter, 2026-10-16); answerer saw only _prompts/csharp.md; derived/csharp.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: csharp-null-01
  answer: |
    Nullable reference types are a compile-time static-analysis feature: they let you annotate reference types as `T?` (may be null) or plain `T` (non-nullable), and the compiler warns when you dereference a possibly-null value, assign null to a non-nullable type, or ignore a null check. None of these has a runtime effect: `?`, `!`, and `#nullable enable` are erased. The `?` annotation only changes compile-time null state (and emits nullable-metadata attributes); `!` (the null-forgiving operator) tells the compiler to treat an expression as non-null and suppresses the warning; `#nullable enable` switches the file/project into annotated mode. At runtime a `string?` is still just a `string` — nothing checks nullability.
- id: csharp-null-02
  answer: |
    Value types hold their data inline; their default is a zeroed, non-null instance (`0`, `false`, an all-zero struct; only `Nullable<T>`/`T?` can be null). Reference types hold a reference to a heap object; their default is `null`. Assigning a struct copies the whole value; assigning a class copies the reference. The surprise: structs are copied on every access, so mutating a struct you obtained from somewhere does not mutate the source — e.g., `list[0].X = 5;` fails on `List<T>` because the indexer returns a copy (CS1612), and mutating a struct exposed by a `readonly` field operates on a copy (the compiler disallows mutation through it). Arrays are the exception: `arr[i].X = 5;` mutates the element in place. foreach iteration variables are also read-only copies.
- id: csharp-null-03
  answer: |
    `record` (a record class) is a reference type whose positional members are init-only auto-properties; it generates value-based equality (`IEquatable<T>`, `Equals`/`GetHashCode` based on all members), synthesized `==`/`!=`, a `with` non-destructive copy expression, and `PrintToString`. A plain `class` uses reference equality by default (`==` is identity), is fully mutable, and has no `with`. A `record struct` is a value type with value-based equality too, but its positional members are MUTABLE (`get; set;`) — only `readonly record struct` makes them init-only. So a `record struct` is not the same code generation as a record class as a value type: the mutability default differs.
- id: csharp-null-04
  answer: |
    `init`-only properties can be assigned only during object construction or object initialization; afterward they are read-only. `required` members must be supplied by every caller via object initialization, unless the constructor is marked `[SetsRequiredMembers]` (promising all required members are set). They combine naturally: `public required string Name { get; init; }` forces callers to write `new C { Name = "x" }` while preventing later mutation. Without `[SetsRequiredMembers]`, callers who omit any required member get a compile error; reflection or other construction paths that bypass initializers can still leave required members unset.
- id: csharp-pattern-01
  answer: |
    A switch expression is an expression: each arm is `pattern => expression`, it produces a value, there is no fall-through, and `_` is the discard. A switch statement is a statement with `case` labels, `break`, and possible fall-through, and it returns no value. Crucially, a switch expression's arms must be exhaustive — the compiler errors (CS8509) unless every possible input is covered or a catch-all arm (`_` or `var`) is present. A switch statement may be non-exhaustive; if nothing matches, control simply passes to the end.
- id: csharp-pattern-02
  answer: |
    Property patterns match a type and inspect named properties: `{ X: var x, Y: var y }` or `{ X: > 0 }`, binding each sub-pattern to a fresh variable (with `var`) or matching a nested pattern. Positional patterns deconstruct via a `Deconstruct` method: `Point(int X, int Y)` binds each deconstructor output to a declared variable. Both nest arbitrarily. Binding: property patterns bind by property name using the property's value; positional patterns bind by position through `out` parameters into new variables, and both bind in the enclosing scope of the `is`/switch.
- id: csharp-pattern-03
  answer: |
    Relational patterns compare a value to a constant with `<`, `<=`, `>`, `>=` (e.g., `is > 0 and <= 100`). Logical patterns combine patterns with `and`, `or`, `not` (e.g., `is not null`, `is >= 0 and <= 10`), short-circuiting. List patterns match a sequence: `[1, 2, 3]` matches exactly; `..` is a slice matching zero or more elements (`[1, ..rest]`, `[..tail]`), and `rest`/`tail` bind the matched slice via `var`.
- id: csharp-pattern-04
  answer: |
    `obj is Customer c` performs the type test and binds `c` to a definitely-assigned variable in one step (unlike `is` plus a cast, which tests twice and never binds). If the test succeeds, `c` holds the converted reference; if it fails, `c` is assigned its default (null for a reference type) in the enclosing scope. The variable is in scope in the enclosing statement scope — usable on the right-hand side of a short-circuiting `&&` and in code following the `is` expression — which a standalone cast cannot do.
- id: csharp-linq-01
  answer: |
    Deferred execution means the query is not run when you build it; it runs when you enumerate (foreach, await foreach, ToList, etc.). Most operators (`Where`, `Select`, `Take`, `Skip`, `OrderBy`, `Join`, `Distinct`, `Reverse`) are deferred and return lazy `IEnumerable<T>` wrappers. Immediate execution runs at the call site: `ToList`, `ToArray`, `ToDictionary`, `ToLookup`, `Count`, `LongCount`, `First`, `FirstOrDefault`, `Single`, `Max`, `Min`, `Sum`, `Average`, `ElementAt`. Materializing operators force enumeration of the source.
- id: csharp-linq-02
  answer: |
    `IEnumerable<T>` (LINQ-to-Objects) takes `Func<...>` delegates and executes in memory after loading data. `IQueryable<T>` builds expression trees (`Expression<Func<...>>`) that a provider (e.g., EF Core) translates — to SQL — and executes remotely; it stays deferred until enumeration. Mixing them is dangerous: once any operator forces `IEnumerable<T>` (e.g., `AsEnumerable()` or a `Func`-based operator over a query), the rest runs client-side, potentially loading entire tables into memory; `IQueryable` operators after that no longer translate. Providers may also silently evaluate parts client-side, causing N+1 queries or huge result sets.
- id: csharp-linq-03
  answer: |
    Because deferred queries re-run the whole pipeline on every enumeration, enumerating the same query variable twice re-executes side effects, can return different results (random sources, changing databases), and for `IQueryable` re-runs the remote query twice. Avoid by materializing once: `var list = query.ToList();` then enumerate `list` as many times as needed. If the source is already a collection, prefer calling operators directly on it.
- id: csharp-linq-04
  answer: |
    `First` throws if the sequence is empty; `FirstOrDefault` returns `default(T)` if empty instead of throwing; `Single` throws if empty or if more than one element; `SingleOrDefault` throws only if more than one. Value-type gotcha: `FirstOrDefault` on a sequence of `int` (or any non-nullable value type) returns `0` when empty, indistinguishable from a real first element of `0`, so you cannot tell "not found" from "found zero" — check emptiness separately (`Any`) or use `First`/try-catch, or keep the element type nullable (`int?`) so `default` is `null`.
- id: csharp-async-01
  answer: |
    Return `ValueTask<T>` when the operation usually completes synchronously (cached result, already-completed task) to avoid allocating a `Task<T>`. Consumption rules: a `ValueTask` is a one-shot struct — await it at most once, do not store it across awaits, do not pass it to `Task.WhenAll`, and do not read it after it has been consumed. If you need multiple awaits, storage, or fan-out, convert first with `.AsTask()` (which may allocate a backing `Task`) and operate on that. Never `await` the same `ValueTask` twice.
- id: csharp-async-02
  answer: |
    The compiler rewrites an `async` method into a state machine (`MoveNext`, `AsyncMethodBuilder`, fields for locals/awaiters). `await` captures the current `SynchronizationContext`/`TaskScheduler` and resumes the continuation on it; until then the method returns control to its caller. Blocking with `.Result`, `.Wait()`, or `GetAwaiter().GetResult()` parks a thread while the continuation needs that same context to run — on a UI thread or a constrained thread pool this deadlocks: the thread waits, the continuation is queued to the context the thread owns, and neither proceeds. Fix: async all the way — `await` up the call chain instead of blocking; never mix `.Result` with async code.
- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` tells the awaiter not to capture/restore the current `SynchronizationContext` or `TaskScheduler`; the continuation resumes on a thread-pool thread wherever convenient. Library code should use it because it has no UI context to return to: it avoids deadlocks (blocking callers), removes context hops, and improves throughput. The tradeoff: after `ConfigureAwait(false)`, the code cannot rely on ambient context (e.g., `HttpContext`, UI thread affinity).
- id: csharp-async-04
  answer: |
    Cancellation is cooperative: a `CancellationTokenSource` owns the cancellation request; consumers receive a `CancellationToken`, check `IsCancellationRequested`, call `token.ThrowIfCancellationRequested()` (throws `OperationCanceledException`), or pass the token to async APIs so they can observe it. `IAsyncEnumerable<T>` is an asynchronously pulled stream; `await foreach` consumes it element-by-element without blocking. Cancellation has two halves: the consumer attaches a token with `.WithCancellation(token)`, and the async-iterator producer receives the token via a parameter marked `[EnumeratorCancellation]` so the iterator honors cancellation between yields.
- id: csharp-generics-01
  answer: |
    A `where` constraint restricts which types can substitute a type parameter and unlocks members/methods the compiler can assume exist. Main kinds: `where T : struct` (non-nullable value type), `where T : class` / `where T : notnull` (reference or non-nullable), `where T : unmanaged`, `where T : new()` (parameterless ctor), `where T : <base class>`, `where T : <interface>`, `where T : U` (another type parameter); they can be combined where compatible.
- id: csharp-generics-02
  answer: |
    Variance lets generic interface/delegate conversions follow subtyping. `out T` (covariant): `T` appears only in output positions (return values, read-only properties); `IEnumerable<out string>` is assignable to `IEnumerable<object>`. `in T` (contravariant): `T` appears only in input positions (method parameters); `IComparer<in object>` is assignable to `IComparer<string>`. `out`/`in` apply only to interfaces and delegates (not classes), and the compiler enforces that `out` types never appear in input positions and `in` types never in output positions, keeping it type-safe.
- id: csharp-generics-03
  answer: |
    Type arguments are inferred from the call-site arguments (and, in some cases, from how the result is used via target typing in newer C#). Inference fails — and you must write them explicitly, e.g., `M<int>(...)` — when no argument carries that type parameter, inference is ambiguous, you pass `default`, the method is used as a method group without a target type, or constraints can't be satisfied by inference alone. Explicit args are also required when calling a generic method through a delegate expression or reflection.
- id: csharp-generics-04
  answer: |
    `default(T)` produces the zero value for any `T`: `0`/false/zeroed struct for value types (only `Nullable<T>` can be null via `default`), and `null` for reference types. Generic code needs it because, without constraints, it cannot know whether `T` is a value or reference type, cannot assign `null` to an unconstrained `T`, cannot call a parameterless constructor without `new()`, and has no other universal "empty" value. It is the only way to initialize an unconstrained type parameter.
- id: csharp-delegate-01
  answer: |
    A delegate is a type-safe, multicast function pointer: a reference type holding a method pointer plus an optional target instance, invoked through `Invoke` or dynamically. `Func<T, TResult>` encapsulates a method that returns a value; `Action<T>` encapsulates a void method; `Predicate<T>` is a legacy `Func<T, bool>`. Delegates support `+=`/`-=` composition (multicast) and are the backbone of events, callbacks, and LINQ predicates.
- id: csharp-delegate-02
  answer: |
    The `event` keyword wraps a delegate field with encapsulation: external code can subscribe (`+=`) and unsubscribe (`-=`) but cannot invoke the event, cannot assign it (`=`) or set it to null, and cannot see the invocation list. Field-like events also provide a thread-safe add/remove pair and a default implementation. This prevents external code from resetting other handlers or raising the event; only the containing class may raise it.
- id: csharp-delegate-03
  answer: |
    Closures capture the variable, not its value. In a `for` loop there is a single loop variable shared by all iterations, so lambdas created in each iteration all see the same final value after the loop — unsafe unless you copy the variable into a local inside the loop. In a `foreach` loop (C# 5 and later), the iteration variable is logically per-iteration, so each lambda captures its own copy — safe. (Before C# 5, `foreach` shared the variable too.) Safety therefore depends on loop kind and language version: `for` = shared/unsafe unless you introduce a local copy; modern `foreach` = per-iteration/safe.
- id: csharp-delegate-04
  answer: |
    A multicast delegate (built with `Delegate.Combine`/`+=`) holds an invocation list and calls each target in order when invoked. For non-void delegates, the result is the return value of the LAST target only; earlier return values are discarded. If any target throws, the exception propagates and the remaining targets are NOT invoked. To invoke every handler regardless of failures, enumerate `GetInvocationList()` and call each delegate individually inside its own try/catch.
- id: csharp-dispose-01
  answer: |
    `IDisposable` exposes `Dispose()` for deterministic release of resources (file handles, sockets, memory) instead of waiting for the GC. A `using` statement (`using (var x = ...) { }`) scopes disposal to the brace block; a `using` declaration (`using var x = ...;`, C# 8) scopes disposal to the end of the enclosing scope (method or block). Both wrap the code in try/finally so `Dispose` runs even on exception; the difference is lifetime — statement ends at the closing brace, declaration ends at scope end, and multiple declarations dispose in reverse declaration order.
- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` exposes `DisposeAsync()` returning a `ValueTask` for non-blocking resource release (flush, close a socket/stream asynchronously). `await using` (C# 8) scopes disposal like `using` but awaits `DisposeAsync`. Use `IAsyncDisposable`/`await using` when cleanup itself is async or when the type implements it (many stream-like types implement both `IDisposable` and `IAsyncDisposable`; prefer `DisposeAsync`). If only `IDisposable` exists, use `using`.
- id: csharp-dispose-03
  answer: |
    `Dispose` is deterministic and caller-controlled; a finalizer (`~Class()`) runs non-deterministically on the GC finalizer thread at some point after the object becomes unreachable. A type needs a finalizer only as a backstop when it directly owns unmanaged resources (raw handles), in case callers forget `Dispose`. Purely managed types never need one; finalizers add cost (extra GC generation, delayed reclamation, thread contention) — prefer `SafeHandle` + `Dispose` over a custom finalizer.
- id: csharp-dispose-04
  answer: |
    Full pattern: implement `IDisposable.Dispose()` calling `Dispose(true)` then `GC.SuppressFinalize(this)`; provide `protected virtual void Dispose(bool disposing)` with a `_disposed` guard: when `disposing` is true, release managed resources (dispose owned `IDisposable`s, null references); release unmanaged resources (CloseHandle, Marshal.FreeHGlobal) regardless of `disposing`; set the flag and prevent reuse. Add a finalizer `~Class()` calling `Dispose(false)` (unmanaged only). Subclasses override their own `Dispose(bool)` and call `base.Dispose(bool)` last. Disposed objects should throw `ObjectDisposedException` on use.
- id: csharp-span-01
  answer: |
    Collection expressions (C# 12) let you write `[1, 2, 3]` to create any collection type: arrays, `List<T>`, `Span<T>`, or any type with a collection initializer/`Add` or a suitable extension. The spread element `..` inlines another collection's elements: `[0, ..rest, 4]` or `[..other]`. It works wherever a collection expression is expected and avoids explicit `Concat`/`AddRange`. (`..` as a range operator is different: `x..y` in slicing.)
- id: csharp-span-02
  answer: |
    `Span<T>` and `ReadOnlySpan<T>` are ref structs: contiguous, low-overhead views of memory (stack, array interior, or unmanaged) that may live only on the stack. `Memory<T>` is a regular struct heap-safe counterpart usable across awaits and storable in fields. You cannot store a `Span<T>` in a field, box it, implement an interface with it, or use it across an `await` — the compiler forbids it because async state machines hoist captured locals to the heap, and a ref struct cannot. Use `Memory<T>` (or `ReadOnlyMemory<T>`) when you need to span await boundaries or fields.
- id: csharp-span-03
  answer: |
    `stackalloc T[n]` allocates `n` elements of `T` on the current stack and returns a `Span<T>`; it is extremely fast and is reclaimed automatically when the method returns — no GC pressure. Cautions: it can overflow the stack (default ~1 MB; keep allocations small, a few hundred bytes to ~1 KB, or use `ArrayPool<T>` for larger), it cannot cross `await`/`yield` or be captured by lambdas/local functions (ref-struct rules), you must not return the span from the allocating method, and uninitialized stack memory must be written before reads (no zero-fill guarantee in all contexts).
- id: csharp-span-04
  answer: |
    An array (`T[]`) is fixed-size, covariant (`string[]` converts to `object[]`, but element writes can throw `ArrayTypeMismatchException`), zero-overhead, and directly span-able. `List<T>` is a growable wrapper around an internal array with `Add`/`Insert`/`Remove`, O(1) amortized append but O(n) insert/remove, no covariance, and small per-element indirection cost for value types (stored inline in the backing array). `params` collections (C# 12) let `params` target any collection-expression type (`params Span<int>`, `params List<int>`, not just `params T[]`), so call sites can use `M([1, 2, 3])` with collection expressions.
