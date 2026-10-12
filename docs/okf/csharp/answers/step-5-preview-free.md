# step-5-preview-free — closed-book answers for csharp (opencode-go, 2026-10-12); answerer saw only _prompts/csharp.md
Answers below are from my own knowledge only — no web search, no repo/answer-key access. I've flagged the two spots where my recall is not fully certain (stackalloc zeroing; record-struct `==` synthesis is stated from memory).

- id: csharp-null-01
  answer: |
    Nullable reference types (C# 8) are a purely **static analysis** feature: the compiler warns
    where a possibly-null value may be dereferenced, tracking null state through your code and
    through annotated library signatures (via compiler-generated attributes like
    NullableAttribute, NotNullWhen, MaybeNull).

    - `string?` on a reference type is an *annotation* meaning "may hold null". It has **zero
      runtime effect**: `string?` and `string` are the same runtime type, no checks are inserted,
      reference types were always nullable at runtime. It only changes compiler warnings.
    - `!` (null-forgiving operator) tells the analyzer "stop warning about this expression; I
      assert it isn't null". Again **no runtime check** — if you're wrong you get a normal
      NullReferenceException.
    - `#nullable enable` is a preprocessor directive switching on the nullable context
      (warnings + annotations) for a file or region; `#nullable disable` turns it off and
      `#nullable restore` reverts to the project setting. The same can be set project-wide with
      `<Nullable>enable</Nullable>`.

- id: csharp-null-02
  answer: |
    **Value types** (structs, enums, primitive numerics, `record struct`, tuples) are stored
    inline — on the stack or embedded inside the containing object — and assignment copies the
    entire value. Their default is a zeroed value (0, '\0', false, all-fields-zero) and their
    default equality is field-by-field.

    **Reference types** (class, interface, delegate, string? no — string is a class, object,
    arrays, `record`) are heap objects accessed through references; the default is `null`
    (unless it's `notnull`-constrained generic), and default equality is identity-based.

    The surprise: a struct is *copied* every time it's read through something that isn't a
    variable. When a struct is in a `readonly` field (or an `in` parameter, or a readonly
    struct property), reading it to call a mutating method makes a **defensive copy** — your
    mutation lands on the copy and the stored struct is unchanged (and direct assignment like
    `field.X = 5` is a compile error). Same with collections: `list[0].Mutate()` on a
    `List<int>`-of-struct silently mutates a temporary copy (the change is lost), and
    `list[0].X = 5` won't compile ("cannot modify the return value"). Arrays are the exception:
    `arr[0].X = 5` compiles because an array element access is a variable, not a copy.

- id: csharp-null-03
  answer: |
    `record` (record class): a **reference type** with **value-based equality** — the compiler
    synthesizes `Equals(object)`, a strongly-typed `Equals`, `GetHashCode`, `ToString`,
    `Deconstruct` (for positional records), and `==` / `!=` operators; inheritance keeps equality
    correct via a synthesized `EqualityContract`. It supports `with` expressions for
    non-destructive mutation. Two distinct instances with the same field values are `==` equal,
    which is impossible for a plain class.

    `record struct` (C# 10): a **value type** with value-based equality and synthesized
    `ToString`/`Deconstruct`/`IEquatable<T>` — but (from my recall) it does **not** synthesize
    `==` / `!=` operators unless you write them. `readonly record struct` makes it immutable.
    Copying follows value semantics; `with` works too.

    plain `class`: reference equality by default (two instances with identical data are never
    `==`), mutable by default, nothing synthesized.

    Choose `record` for immutable data objects with value semantics that you compare/copy
    wholesale; `record struct` for small value types needing equality; plain `class` when you
    actually need reference identity and mutation.

- id: csharp-null-04
  answer: |
    `init`-only property: the property's setter is replaced by an `init` accessor, which may
    only run during **object initialization** — inside the declaring type's constructor, or in
    an object initializer (`new P { Name = "x" }`). After construction the property is
    effectively read-only, so it gives you write-once-at-init semantics with nicer creation
    syntax than a constructor parameter.

    `required` member (C# 11): the compiler **requires every caller** to initialize the member
    when constructing the object — via object initializer or a constructor — otherwise you get a
    compile error. It guarantees the object is never constructed with that member missing.

    Combined (`public required string Name { get; init; }`) you get a property that **must be
    supplied at creation time** and then can never change — enforced immutability that the
    compiler can't let you route around. The `SetsRequiredMembers` attribute lets a
    constructor opt out of enforcement for call sites it guarantees initialize the members.

- id: csharp-pattern-01
  answer: |
    A **switch expression** is an *expression*: it evaluates its arms top-to-bottom, picks the
    first whose pattern matches, and yields a **value** you can assign or return
    (`var s = x switch { 1 => "one", _ => "many" };`). Arms are `pattern => result`; there is no
    fall-through and no `case`/`break`; you can add `when` guards.

    A **switch statement** is a *statement*: `case` labels with bodies ending in `break`,
    `return`, or `throw`; it produces no value, can declare variables, and (unlike the
    expression) permits fall-through only from an empty case label to the next one.

    If a switch **expression** is not exhaustive, the compiler emits warning **CS8509** ("does
    not handle all possible values"); if a value reaches it at runtime with no matching arm, it
    throws `SwitchExpressionException`. So you either add a `_` catch-all, handle every enum
    member, or rely on the runtime check — always handle `null` explicitly for nullable inputs.

- id: csharp-pattern-02
  answer: |
    **Property patterns** match an object against its properties' patterns:
    `if (p is { X: 0, Y: 0 })` — nested recursively, so
    `{ Address: { City: "NY", Zip: "10001" } }` tests down the graph. Constants are compared
    (`{ Status: OrderStatus.Shipped }`), and a nested declaration pattern **binds** the matched
    value to a new variable: `{ Name: string n, Age: > 18 }` both tests and captures `n`.

    **Positional patterns** match through `Deconstruct`: positional records, tuples, and any
    type with a `Deconstruct` method: `point is (0, 0)`, `order is ("VIP", var total) and
    total > 1000`. Each element of the positional pattern is a full sub-pattern applied to the
    corresponding deconstructed value, so they combine: `obj is (0 or 1, > 0 and < 100, var
    rest)`.

    Binding is via declaration patterns inside either form — the variables come into existence
    (definitely assigned) when the overall match succeeds; a constant type pattern just tests.

- id: csharp-pattern-03
  answer: |
    **Relational patterns** (C# 9) compare the input to a constant with `<`, `>`, `<=`, `>=`:
    `grade is >= 90 and < 100`, `c is 'a'`, `d is <= DateTime.Now`.

    **Logical patterns** (C# 9) combine patterns with `and`, `or`, `not`:
    `s is not null`, `day is DayOfWeek.Saturday or DayOfWeek.Sunday`,
    `n is > 0 and < 100`. Bindings inside a combined pattern become definitely assigned only
    where that sub-pattern was tested — e.g. after `x is int i and i > 5`, `i` is assigned.

    **List patterns** (C# 11) match sequences element-wise:
    `n is [1, 2, 3]`, `n is [> 0, .., > 0]`, `n is [var first, .., var last]`, `n is []`
    (empty). The *slice pattern* `..` matches zero-or-more elements at its position, `_`
    discards, and `var` patterns capture. The total element count implied by the non-slice
    elements must match. It applies to arrays, spans, immutable arrays, `List<T>`, and other
    types with an indexer plus `Length`/`Count`.

- id: csharp-pattern-04
  answer: |
    `obj is Customer c` is the **declaration pattern**: it performs one runtime type check and
    conversion that, on success, *both* validates the type and assigns a fresh strongly-typed
    variable `c` of type `Customer`.

    Compared with plain `obj is Customer` plus `(Customer)obj` cast: no second cast expression
    to get wrong, no double evaluation (a naive `obj is Customer && ((Customer)obj).IsActive`
    can re-check a value that changed between — a real race for properties/volatile state), and
    flow analysis lets you use `c` immediately without an uninitialized intermediate. It's also
    the only form that composes with other patterns (`is int i and i > 5`, `is { Name: not
    null }`).

    Scope of `c`: it is in scope after the `is` expression wherever definite-assignment analysis
    proves the pattern matched — inside the `if` body, in the right operand of `&&`/`||`, in a
    `while` body, and in the corresponding arm of a `switch` (scoped to that arm only). After
    the enclosing `if`, it is in scope but *not* definitely assigned, so using it there is a
    compile error.

- id: csharp-linq-01
  answer: |
    **Deferred (lazy)**: defining the query runs nothing; the whole pipeline executes — and the
    predicates/lambdas are invoked — *each time the result is enumerated*, and it reads the
    source's state **at enumeration time**, not at definition time. Deferred operators include
    `Where`, `Select`, `SelectMany`, `Take`/`Skip`/`TakeWhile`/`SkipWhile`, `Distinct`,
    `OrderBy`/`ThenBy`, `GroupBy`, `Join`/`GroupJoin`, `Zip`, `Concat`, `Cast`/`OfType`,
    `TakeLast`, `Chunk`, `AsEnumerable`.

    **Immediate (eager)**: forces enumeration right away and returns a concrete result or
    collection: `ToList`, `ToArray`, `ToDictionary`, `ToLookup`, `Count`, `LongCount`, `Any`,
    `All`, `Contains`, `SequenceEqual`, `First`/`FirstOrDefault`/`Single`/`SingleOrDefault`/
    `Last`/`LastOrDefault`, `Min`/`Max`/`Sum`/`Average`/`Aggregate`.

    Practical consequence: closures capture by reference, so a deferred query can observe later
    mutations of captured variables and the source — a classic source of "why did it change?"
    bugs. Materialize with `ToList`/`ToArray` when you intend a snapshot.

- id: csharp-linq-02
  answer: |
    `IEnumerable<T>` is a sequence of *materialized objects in memory*; its extension methods
    (`Enumerable`) take compiled **delegates** (`Func<T,bool>`), and execution happens in-process
    as you iterate.

    `IQueryable<T>` represents a query as an **expression tree** (`Expression<Func<T,bool>>`);
    its extension methods (`Queryable`) build up the tree and hand it to the provider's
    `IQueryProvider` (EF Core, LINQ to SQL) to translate to SQL/another language and execute
    remotely. Everything up to a `ToList`/`First`/etc. stays a description.

    Danger of mixing: passing an `IQueryable` to an `Enumerable` method — often accidentally via
    `AsEnumerable()`, or just because the lambdas are delegates so `Enumerable.Where`
    overload-resolves — silently stops translation and executes the rest of the chain **in
    memory**, potentially fetching entire tables (the old EF "client evaluation" trap) or
    producing N+1 behavior. The reverse mistake — a lambda body that can't be translated to
    SQL — throws at runtime (or is evaluated client-side unpredictably). Keep the chain
    `IQueryable` until the final materialization, and keep mid-chain lambdas free of
    non-translatable logic.

- id: csharp-linq-03
  answer: |
    Because of deferred execution, a query variable holds a *recipe*, not results. Every
    `foreach` (or `ToList`, `Count`, ...) over the same variable **re-executes the entire
    pipeline** — re-running filters/projections, re-reading the source, and re-invoking any
    side-effecting lambdas. If the source data changed between enumerations you get *different
    results from the same variable*, and any captured closure variables are re-read at their
    current values, so results can depend on ordering in surprising ways. It also doubles the
    cost of expensive queries (multiple DB round-trips / duplicate computations).

    Avoid it by materializing when you want a stable snapshot: `var list = query.ToList();`
    and reuse `list`. Or, if you keep the query lazy, guarantee it is side-effect-free and the
    source is immutable for the lifetime of the variable.

- id: csharp-linq-04
  answer: |
    `First()`: returns the first element; **throws** `InvalidOperationException` ("Sequence
    contains no elements") if the source is empty. `FirstOrDefault()`: returns the first element
    or `default` if empty — no exception. Both have predicate overloads.

    `Single()`: returns the *only* element; throws if the source is empty **or** contains more
    than one element (it must scan for a second item). `SingleOrDefault()`: returns `default`
    if empty, still throws if more than one.

    The value-type gotcha: `FirstOrDefault` on an empty `IEnumerable<int>` returns `0` (and
    `bool` → `false`, a struct → zeroed) — which is **indistinguishable from a sequence that
    genuinely contains 0**. Reference types escape this via `null`, so absence becomes
    unrepresentable for value types. Workarounds: use `First`/`Single` and catch when absence is
    genuinely exceptional, use `FirstOrDefault` on `IEnumerable<int?>` so absence is `null`,
    `DefaultIfEmpty(sentinel)`, or guard with `Any()`. Note `First` is cheap (stops after one);
    `Single` costs more because it verifies uniqueness.

- id: csharp-async-01
  answer: |
    Return `ValueTask<T>` instead of `Task<T>` when the method is on a **hot path that often
    completes synchronously** — a cached value, a buffered/awaitable-not-needed operation, or
    an `IValueTaskSource`-backed pooled operation (sockets, pipelines). It's a struct, so a
    synchronous completion allocates **nothing**, unlike `Task<T>`.

    Consumption rules: you may await it **at most once**. Don't call `AsTask()` and *then* await
    the original (or await twice), don't block on it with `.Result`/`.GetAwaiter().GetResult()`,
    don't store it and await it much later, and don't mix consumption modes. `AsTask()` is an
    escape hatch for interop, usable once. If in doubt about these rules, use `Task<T>` — the
    default answer.

- id: csharp-async-02
  answer: |
    The compiler rewrites an `async` method into a **state machine**: a generated type
    implementing `IAsyncStateMachine` whose `MoveNext()` contains your body, split at each
    `await` into segments; local state is lifted into fields and the "current segment" is the
    state, so a resumed method resumes *mid-method*. The builder
    (`AsyncTaskMethodBuilder` / `AsyncValueTaskMethodBuilder`) produces the returned `Task`;
    return values and exceptions are captured and put on it; an already-completed awaited
    operation continues **synchronously** on the same thread.

    The `.Result`/`.Wait()` deadlock: an async method by default captures the current
    `SynchronizationContext` (UI thread, or classic ASP.NET's per-request single-threaded
    context) and schedules its continuation back onto it. If you block a thread on `.Result()`
    inside that context, the continuation — which needs that very same context/thread — can
    never run, so the task never completes, and the blocked thread waits forever. Fixes: `await`
    all the way up, `ConfigureAwait(false)` in library code that doesn't need the caller's
    context (ASP.NET Core has no SynchronizationContext, so this is mostly a UI/legacy-ASP.NET
    problem; thread-pool starvation is a related non-deadlock version).

- id: csharp-async-03
  answer: |
    `ConfigureAwait(false)` makes the continuation after the `await` **not capture and restore**
    the caller's `SynchronizationContext` or `TaskScheduler`; the continuation runs on whatever
    thread finished the await — typically a thread-pool thread.

    It's recommended in **library/infrastructure code** because: (1) a library has no business
    forcing work back onto the caller's context — it doesn't know what that context is and
    doesn't need it; (2) capturing and posting back has real overhead; (3) deadlock avoidance
    for callers who block on your library (their single-threaded context isn't hijacked by your
    continuation — although blocking is still a bug there). In application code that must touch
    UI controls or request state after the await, keep the default (`ConfigureAwait(true)`).
    ASP.NET Core apps have no `SynchronizationContext`, so it's a no-op performance-wise, but
    it remains the correct habit for libraries.

- id: csharp-async-04
  answer: |
    `CancellationToken` is cooperative cancellation. A `CancellationTokenSource.Cancel()`
    flips the token; the cancelling code propagates the token *down* to every layer. The
    callee cooperates by polling `token.ThrowIfCancellationRequested()` (which throws
    `OperationCanceledException`, putting the Task into the **Canceled** state, distinct from
    Faulted), by passing the token to APIs that accept one (HTTP, DB, sockets, `Task.Delay`,
    `Task.Run`), or via `token.Register(callback)` for operations that can't poll. Linked tokens
    (`CreateLinkedTokenSource`) combine sources. Operations that never check the token can't be
    cancelled.

    `IAsyncEnumerable<T>` + `await foreach`: an **async stream** (C# 8). The producer is an async
    iterator — an `async` method that can `await` between `yield return`s — and the consumer
    pulls **one element at a time** (`await foreach`, awaiting `MoveNextAsync`). This lets you
    stream and process data (DB cursor rows, network packets, JSON fragments) without buffering
    the whole set, and supports cancellation via `WithCancellation(token)` or an
    `[EnumeratorCancellation]` parameter.

- id: csharp-generics-01
  answer: |
    A `where` clause **constrains the type arguments** a generic can be instantiated with, and
    therefore what the compiler lets the body do (member access, `new`, operators, `default`
    semantics).

    Main kinds: `where T : class` (reference type; enables `default(T) == null`, ref-equality
    idioms), `where T : struct` (non-nullable value type), `where T : notnull` (either, but not
    a nullable ref or value), `where T : unmanaged` (unmanaged struct — pointer usage legal),
    `where T : new()` (public parameterless constructor — required to write `new T()`),
    `where T : BaseClass` (inheritance — grants access to base members), `where T : IOne, ITwo`
    (interface(s) — grants their members), `where T : U` (naked type parameter), and
    `where T : allows ref struct` (C# 13). Rules: at most one base class; `new()` goes last;
    constraints must be repeated when overriding/implementing; type parameters' constraints can
    only be satisfied transitively.

- id: csharp-generics-02
  answer: |
    `out T` = **covariant**, `in T` = **contravariant**, declared on the type parameter of a
    generic **interface or delegate** (C# 4).

    `out`: T may appear only in **output positions** (return types, get-only property types) —
    then `IEnumerable<string>` is implicitly convertible to `IEnumerable<object>`, so a
    producer of strings is a producer of objects.

    `in`: T may appear only in **input positions** (method parameters) — then
    `IEqualityComparer<object>` is assignable to `IEqualityComparer<string>`: a comparer for any
    object can compare strings.

    Where they apply: interfaces and delegates only — not classes, not structs, not generic
    methods. The compiler enforces position rules (an `out` param can't show up in an input
    position, and vice versa), which is why `IList<T>` can't be variant (it uses T both ways).
    And variance conversions are reference conversions only — `IEnumerable<int>` does **not**
    become `IEnumerable<object>` implicitly (no boxing identity conversion for value types);
    use `Cast<object>()`.

- id: csharp-generics-03
  answer: |
    Type inference works from the method's **arguments**: the compiler infers each type argument
    from the types of the corresponding arguments — `M.GenericMax(3, 7)` infers `T = int`
    because the parameter's type *is* `T`. It propagates through nested calls, method-group
    arguments, and (C# 10+) lambda "natural types" (`x => x` passed to `M<T>(Func<T,T>)` can
    infer). The return-type/assignment context is **not** used to infer type arguments.

    You must specify type arguments explicitly when inference can't produce them:

    - T doesn't appear in any parameter list — e.g. `T Get<T>(int id)` (only the return type).
    - Inference is ambiguous/contradictory — e.g. two arguments would imply two different types
      for one T.
    - No natural type exists for an argument (a lambda with no natural type pre-C# 10, or
      `null` literals), or you deliberately want a wider/different type than the argument's
      natural type (`F<decimal>(1, 2)` forces decimal arithmetic where int inference would lose
      precision).

    Inference is all-or-nothing per method: either every type argument is inferred or you name
    them all (there's no partial specification).

- id: csharp-generics-04
  answer: |
    `default(T)` produces the **default value** of T: `null` for reference types (when T is
    unconstrained and instantiated to a reference type), and a **zero-initialized value** for
    value types — `0`, `false`, `'\0'`, or a struct with every field zeroed (a nullable
    `Nullable<S>` becomes null).

    Generic code needs it because generic code is *generified* — the JIT doesn't know statically
    whether T is a class or a struct, so unconstrained generic code cannot legally write `null`
    (compile error without `where T : class`) or the literal `0`. `default(T)` is the
    type-parameterized way to express "no real value": `TryGet`-style out parameters, sentinel
    initializations, `EqualityComparer<T>.Default` / `Comparer<T>` "lowest value" patterns,
    conditional defaults. Since C# 7.1 the **`default` literal** infers the type (`T x =
    default;`), and `x is default` is a pattern.

- id: csharp-delegate-01
  answer: |
    A **delegate** is a type that represents method(s) with a specific signature — safely typed
    function pointers. A delegate instance holds a reference to the method plus its target (for
    instance methods), supports multicast invocation lists (combine with `+`/`+=`, remove with
    `-`/`-=`), late binding, and closures (lambdas/method groups compile to delegates).

    - `Func<TResult, ...>` — returns a value: `Func<int, bool>` takes an `int`, returns `bool`;
      `Func<T>` has no inputs. Up to 16 inputs.
    - `Action<...>` — returns `void`: `Action`, `Action<T, T>`, up to 16 inputs.
    - `Predicate<T>` — legacy convention: takes a `T`, returns `bool`; still used by
      `List<T>.FindAll`/`RemoveAll`/`TrueForAll` and `Array.Exists`. Functionally identical to
      `Func<T, bool>` but kept for compatibility.

    (These generic delegates are themselves declared with variance — `Action<in T>` /
    `Func<in T, out TResult>` — so `Action<object>` is assignable to `Action<string>`.)

- id: csharp-delegate-02
  answer: |
    `event` restricts a delegate field to subscription: outside the declaring type, code can
    only call `+=` and `-=` (add/remove handlers) — it **cannot invoke the delegate** and
    **cannot assign to or clear it** with `=`, so no other class can raise your event, clear
    all subscribers, or set it to null. Internally the compiler synthesizes a private backing
    delegate field with `add`/`remove` accessors (which you can write yourself, like
    properties) — this enables custom storage, thread-safe storage, and explicit interface
    implementation.

    Raising convention: a `protected virtual void OnSomething(Args e)` method that snapshots
    the delegate and invokes it with the null check (`?.Invoke(this, e)`); the event type is
    typically `EventHandler` or `EventHandler<TEventArgs>` and raises pass `(sender, args)`.

- id: csharp-delegate-03
  answer: |
    Classic **closure capture**: a lambda captures the *variable*, not its value at creation
    time.

    - `foreach` was the historical trap — before **C# 5.0** there was a single loop variable
      shared by every iteration, so every lambda created in the loop saw the **final** value.
      C# 5.0 made `foreach` allocate a fresh per-iteration variable, so lambdas in `foreach`
      are safe today.
    - `for` loops were **never fixed**: the loop variable (`for (int i = 0; ...)`) is a single
      variable mutated each iteration, so all lambdas created inside a `for` body see the same
      final value of `i` — the classic "all 10 handlers print 10" bug.

    Workaround that always works: copy into a per-iteration local inside the body —
    `var copy = i; actions.Add(() => copy);` — because each loop body invocation declares a new
    `copy`. (Also remember the captured variable's lifetime is extended, and the same trap hits
    async continuations awaiting inside loops.)

- id: csharp-delegate-04
  answer: |
    A **multicast delegate** is a delegate whose invocation list contains more than one method
    (built with `+`/`+=` or passed as multiple). Invoking it calls **every entry in the list,
    in order**.

    - **Return values**: only the return value of the **last** method invoked is propagated;
      every other return value is discarded. (For non-uniform signatures you can't even
      multicast.)
    - **Exceptions**: if one target throws, the remaining targets are **not** invoked — the
      exception propagates to the invoker and iteration over the list stops there. To isolate
      subscribers, enumerate `GetInvocationList()` and try/catch each invoke individually
      (this is why event handlers with shared state or re-entrancy need care).
    - Removal (`-`) removes the most recently added matching entry; removing the last target
      returns null. Equality compares the target and method, not the invocation list.

- id: csharp-dispose-01
  answer: |
    `IDisposable` exposes `Dispose()` — the contract for **deterministic cleanup** of resources
    (unmanaged handles, OS resources, and owned `IDisposable` members like streams) at the point
    the consumer is done, instead of waiting for the GC/finalizer to reclaim memory.

    `using` **statement**: `using (var x = ...) { body }` — the compiler wraps the body in
    try/finally and calls `x.Dispose()` when the block exits, normally or via exception; `x` is
    scoped to the braces, and you can catch inside or nest multiple.

    `using` **declaration** (C# 8): `using var x = ...;` — no braces, disposable is released
    when the **enclosing scope** (rest of the method/block) exits, including on exception;
    multiple declarations dispose in reverse declaration order. It reduces nesting, but the
    resource lives until method end, and you can't place a catch between `new` and dispose.

- id: csharp-dispose-02
  answer: |
    `IAsyncDisposable` adds `ValueTask DisposeAsync()` for types whose cleanup does
    **asynchronous I/O** — flushing and closing a network/file stream, releasing a pooled
    connection, draining an async enumerator. `await using` (statement or declaration, C# 8)
    awaits `DisposeAsync()` at scope exit — the async equivalent of `using`, since blocking a
    thread in `Dispose()` to flush would be wasteful.

    Use it over `IDisposable` when the cleanup has real async operations. A type may implement
    **both**: `await using` picks `DisposeAsync` (well-written implementations fall back to
    Dispose internally), and a plain `using` uses `Dispose`. Implement `DisposeAsync` to call
    `Dispose(true)` under the hood and `GC.SuppressFinalize(this)`.

- id: csharp-dispose-03
  answer: |
    A **finalizer** (`~ClassName()`) is invoked by the **GC**, nondeterministically, on the
    finalizer thread before reclaiming memory — a safety net for releasing unmanaged resources
    if the consumer forgot `Dispose`. `Dispose` is deterministic, synchronous, and
    caller-driven — it can release both unmanaged resources and managed `IDisposable` members.

    A type needs a finalizer only when it **directly owns unmanaged resources** (a raw
    `IntPtr`/native handle) *and* consumers might not dispose — and even then, modern guidance
    says wrap the handle in a `SafeHandle`/`CriticalHandle` subclass (which has its own
    finalizer) and skip the finalizer entirely. A type that merely holds `IDisposable`
    fields must **not** get a finalizer just because its members do — finalization doesn't
    cascade into members, and finalizable objects pay a real cost (extra GC cycles, delayed
    reclamation, resurrection risk). Always `GC.SuppressFinalize(this)` after a successful
    `Dispose`.

- id: csharp-dispose-04
  answer: |
    Classic full pattern:

    ```csharp
    class Foo : IDisposable {
      private IntPtr _handle;
      private Stream _stream;               // managed disposable
      ~Foo() => Dispose(false);
      public void Dispose() {
        Dispose(true);
        GC.SuppressFinalize(this);          // finalizer no longer needed
      }
      protected virtual void Dispose(bool disposing) {
        if (disposing) {
          _stream?.Dispose();               // release OTHER managed objects
          // unsubscribe events, large fields = null
        }
        // either way: release unmanaged resources
        if (_handle != IntPtr.Zero) { CloseHandle(_handle); _handle = IntPtr.Zero; }
      }
    }
    ```

    Rules: make `Dispose(bool)` idempotent; after finalization completes call
    `Dispose(false)` from `~Foo()` and never touch other managed objects there (they may
    already be collected); objects can be finalized in any order; call `base.Dispose(disposing)`
    in a subclass's override; throw `ObjectDisposedException` from members after dispose when
    meaningful. **Modern (.NET Core+)**: if the only unmanaged resource is wrapped in a
    `SafeHandle`, drop the finalizer and the `Dispose(bool)` split entirely — just implement
    `IDisposable` and dispose the safe handle plus managed members; keep the full pattern only
    for raw unmanaged handles.

- id: csharp-span-01
  answer: |
    **Collection expressions** (C# 12) are a single target-typed literal syntax `[ ... ]` for
    building collections: `int[] xs = [1, 2, 3];`, `List<string> names = ["a", "b"];`,
    `Span<char> c = ['h', 'i'];` — the compiler chooses how to build it for the target type
    (array, `List<T>` builder, stackalloc for spans). No left-hand type means `int[]`-style
    array; nesting works (`int[][] m = [[1,2],[3,4]];`) and `[]` creates an empty target.

    The **spread element** `..` splices another enumerable's contents inline:
    `var merged = [.. defaults, .. overrides, extra];` — no nested array, the elements are
    inserted at that position, and `..` works with arrays, `List<T>`, spans, other collection
    expressions, and ranges (`[.. xs[1..^1], xs[0]]`). The compiler emits efficient copies
    (indexer/Count based) when it can, builder-based fallback otherwise.

- id: csharp-span-02
  answer: |
    `Span<T>` / `ReadOnlySpan<T>` are **ref structs** — a type-safe, contiguous *view* over
    arbitrary memory (an array slice, `stackalloc`, native memory, or `string`), with cheap
    slicing (a view, no copy) and bounds checks. `ReadOnlySpan<T>` is the read-only variant.
    `Memory<T>` / `ReadOnlyMemory<T>` are the **heap-storable counterparts**: a plain struct
    over (object, offset, length) or managed data — it can live in a class field, be boxed,
    captured by lambdas, and passed across `await`. You call `.Span` to get a fast synchronous
    view when you're actually processing data.

    Why `Span<T>` can't be a field or cross `await`: it contains a **managed byref** to the
    data, and `ref struct`s are stack-only — byrefs can't be heap-allocated and can't outlive
    the frame that produced them. Async state machines and lambda closures are heap objects, so
    the compiler forbids capturing a `Span<T>` in an async method/lambda (CS4013 / CS8207 style
    errors). `Memory<T>` has no byref, which is exactly why it's the escape hatch for async and
    long-lived storage.

- id: csharp-span-03
  answer: |
    `stackalloc` allocates memory from the **current stack frame** instead of the GC heap:
    `Span<int> buf = stackalloc int[1024];` (no `unsafe` needed since C# 7.3), or
    `int* p = stackalloc int[n]` in unsafe code. No heap allocation or GC pressure, freed
    automatically when the method returns — the fastest buffer type, popular in parsers and
    serializers.

    Cautions:

    - The stack is small (roughly 1 MB default, less on threads you configure); overrunning
      throws an **uncatchable StackOverflowException** that kills the process — keep buffers
      small and bounded, don't size from untrusted input.
    - The memory dies at method exit — the resulting Span must not escape (the compiler
      enforces this via ref struct rules).
    - Repeated `stackalloc` in a loop can accumulate within the frame; avoid
      variable-length allocations in hot loops.
    - Zeroing: don't *assume* it (older docs treated it as uninitialized; verify per runtime if
      your code depends on zeroed memory — explicitly fill when in doubt).

- id: csharp-span-04
  answer: |
    **`T[]` (array)**: fixed length once created, contiguous, zero-based, elements are
    individually addressable variables. Can't grow — `Array.Resize` allocates a new array and
    copies. Historically **covariant** (`string[]` implicit-ref-converts to `object[]`) with a
    runtime type check on writes that throws `ArrayTypeMismatchException`. Static helpers
    (`Array.Sort`, `IndexOf`, `Reverse`); value types are stored inline; `Rank` for
    multidimensional arrays.

    **`List<T>`**: a **dynamic array** — internal array-plus-count that grows automatically
    (doubling), `Add` is amortized O(1) with occasional reallocation+copy; `Count` is a
    property (vs `Length` on arrays), `Capacity` reports/sets the backing size, `TrimExcess`
    reclaims space; `Insert`/`RemoveAt` are O(n) because of shifting. Generic, so no boxing and
    **no** covariance. Slow-ish indexer compared to a raw array's due to range checks.

    **`params` collections**: a `params` parameter lets callers pass zero or more individual
    arguments; the compiler packs them into the collection. Classically `params T[]` — which
    allocates an array unless you already have one (pass it directly, but watch the
    pass-null ambiguity). Since C# 13, `params` generalizes to **other collection types**
    (`Span<T>`, `ReadOnlySpan<T>`, `List<T>`, etc.), so perf-sensitive APIs can avoid the
    array allocation — with collection expressions / spread this is the modern way to get
    array-free variadic calls.
