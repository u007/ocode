# C# Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### csharp-null-01 · types-nullability · W3 · medium
**Q:** What do nullable reference types give you, and what is the runtime effect of the `?` annotation, the `!` operator, and `#nullable enable`?
**A:** NRT (C# 8) is compile-time static analysis, not runtime. `string?` may be null, `string` shouldn't be; the compiler warns on a possible null deref. `!` (null-forgiving) suppresses one warning — no runtime check, no code. `#nullable enable`/`<Nullable>` toggles the context. Annotations are erased at runtime and never change behavior. The proposed `!!` param-null-check (slated for C# 11) was removed before release and never shipped.
• NRT is compile-time static analysis (C# 8); annotations erased at runtime, add no checks • `?`=may-be-null, bare=shouldn't; `!` suppresses the warning with no runtime effect • `#nullable enable`/`<Nullable>` toggles context; `!!` param-check never shipped ~ thinks `?`/NRT adds a runtime null check or changes behavior

### csharp-null-02 · types-nullability · W3 · medium
**Q:** What is the difference between value and reference types, and what surprising thing happens when you mutate a struct held in a readonly field or a collection?
**A:** Value types (struct/enum/primitive) hold data inline and are copied on assignment/pass/return; reference types share a heap object via a reference. Mutating a struct through a member acts on a copy: a mutable struct in a `readonly` field mutates a defensive copy (silent no-op), and `foreach (var s in list) s.X=…` mutates a copy, not the element — hence prefer `readonly struct`. Default value type = zeroed instance (not null); default reference type = null.
• value copied on assignment/pass; reference shares the heap object • mutating a struct hits a copy — readonly-field/foreach-element mutation touches a defensive copy • default: value type = zeroed non-null, reference type = null ~ only says stack vs heap without copy-vs-share

### csharp-null-03 · types-nullability · W3 · medium
**Q:** Distinguish `record` (record class), `record struct`, and a plain `class`, especially around equality and mutability.
**A:** `record`/`record class` (C# 9) = reference type with compiler-generated value equality, `ToString`, and `with` copy. `record struct` (C# 10) = value type with the same value equality. A plain `class` has reference equality. Positional `record class` members are init-only (immutable); positional `record struct` members are mutable unless `readonly record struct`.
• record (class) = reference type with generated value equality + `with` (C# 9) • record struct = value type value equality (C# 10); plain class = reference equality • record class positional members init-only, record struct members mutable unless readonly ~ "record is a class with less boilerplate" with no value-equality

### csharp-null-04 · types-nullability · W2 · medium
**Q:** What do `init`-only properties and `required` members each do, and how do they combine?
**A:** `init` (C# 9) lets a property be set only during construction/object-initializer, immutable thereafter — immutable objects with initializer syntax. `required` (C# 11) forces the caller to set the member in an initializer (or a `[SetsRequiredMembers]` ctor), else a compile error. `required` + `init` = must be supplied at creation and never changes.
• init: settable only during construction/initializer, immutable after (C# 9) • required: compiler forces the member be initialized or it's a compile error (C# 11) ~ conflates the two ("init means readonly" / "required means non-null")

### csharp-pattern-01 · pattern-matching · W2 · medium
**Q:** How does a switch expression differ from a switch statement, and what happens if its arms are not exhaustive?
**A:** A switch expression (C# 8) yields a value from `pattern => result` arms (`_` = default), usable inline. Non-exhaustive arms are not a hard error: the compiler warns (CS8509) and an unmatched value throws `SwitchExpressionException` at runtime. Add a `_ =>` arm to be exhaustive.
• switch expression yields a value via `pattern => result` arms (C# 8), `_` is default • non-exhaustive → compiler warning (CS8509) + runtime SwitchExpressionException, not a compile error ~ "just nicer syntax" with no exhaustiveness behavior

### csharp-pattern-02 · pattern-matching · W2 · medium
**Q:** What are property patterns and positional patterns, and how do they bind values?
**A:** Property patterns match member values: `p is { Age: > 18, Name.Length: > 0 }` (nested form is C# 10+). Positional patterns deconstruct via `Deconstruct`/tuple: `point is (0, var y)`. Both are recursive, bind matched parts with `var`/a declaration, and combine.
• property pattern matches member values `{ Prop: pattern }` (nested since C# 10) • positional pattern deconstructs via Deconstruct/tuple `(a, b)` and can bind with var ~ knows the `is` type check but not property/positional forms

### csharp-pattern-03 · pattern-matching · W2 · medium
**Q:** Explain relational patterns, logical patterns, and list patterns.
**A:** Relational (C# 9): `<`, `>`, `<=`, `>=` against a constant (`is >= 0`). Logical (C# 9): combine with `and`, `or`, `not` (`is >= 0 and < 100`, `is not null`). List patterns (C# 11): match shape `[1, 2, _]`, and the slice pattern `..` matches any span, so `[first, .., last]` binds the ends; elements/slice can bind.
• relational `< > <= >=` (C# 9) and logical `and`/`or`/`not` (C# 9) compose patterns • list patterns `[...]` with slice `..` match/deconstruct sequences (C# 11) ~ describes only one of relational/logical/list

### csharp-pattern-04 · pattern-matching · W2 · easy
**Q:** What does the `is` declaration pattern (`obj is Customer c`) do that a plain type check plus a cast does not, and where is the bound variable in scope?
**A:** `if (obj is Customer c)` tests the type and binds `c` to the cast value in one step, replacing test-then-cast and avoiding an `InvalidCastException`/null. The binding is definitely assigned only on the matching path: `if (obj is not Customer c) return;` inverts it, so `c` is usable on the fall-through.
• `is T x` combines the type test and binding in one, avoiding a separate cast • the bound variable is scoped/definitely-assigned only on the matching path (incl. `is not` inverting it) ~ "`is` checks the type" with no mention of binding

### csharp-linq-01 · linq · W3 · medium
**Q:** What is deferred versus immediate execution in LINQ, and which operators trigger each?
**A:** `Where`/`Select`/`OrderBy`/`Take` are deferred — run nothing until enumerated. `ToList`/`ToArray`/`Count`/`First`/`Sum`/`Any` execute immediately. A deferred query re-executes every enumeration and reflects the source at enumeration time, not definition time. `ToList`/`ToArray` snapshots once.
• Where/Select/OrderBy etc. are deferred — execute on enumeration, not when defined • ToList/ToArray/Count/First/Sum force immediate execution • a deferred query re-runs each enumeration / reflects the source at enumeration time ~ knows "lazy" but not that it re-executes each enumeration

### csharp-linq-02 · linq · W3 · hard
**Q:** What is the difference between LINQ over `IEnumerable<T>` and over `IQueryable<T>`, and what is the danger of mixing them?
**A:** `IEnumerable<T>` (LINQ to Objects) runs in-process with delegates — the source is pulled into memory and filtered client-side. `IQueryable<T>` builds an expression tree a provider (EF Core) translates to SQL and runs at the source. Switching to `IEnumerable` mid-query (`AsEnumerable()`, an untranslatable method) forces client-side evaluation: all rows are fetched, then filtered in memory.
• IEnumerable = in-memory delegates (LINQ to Objects), executes client-side • IQueryable = expression tree translated by a provider (EF → SQL), executes at the source • switching to IEnumerable mid-query forces client-side evaluation (pulls all rows first) ~ knows one runs in the DB but not the expression-tree-vs-delegate reason

### csharp-linq-03 · linq · W2 · medium
**Q:** Why is enumerating the same LINQ query variable more than once a pitfall, and how do you avoid it?
**A:** A deferred sequence re-runs its whole pipeline each enumeration. Enumerating a variable twice (`.Any()` then `foreach`, `.Count()` then iterate) runs the DB/HTTP/IO work twice — or is wrong if the source is a one-shot stream. Materialize once with `ToList()`/`ToArray()`; analyzers flag "possible multiple enumeration".
• enumerating a deferred sequence twice re-runs the whole pipeline (double IO/query) • materialize once with ToList/ToArray when enumerating multiple times ~ notices it's inefficient but not the re-execution / no materialize fix

### csharp-linq-04 · linq · W2 · medium
**Q:** What is the difference between `First` and `FirstOrDefault` (and `Single`), and what value-type gotcha does `FirstOrDefault` have?
**A:** `First()`/`Single()` throw `InvalidOperationException` on empty (`Single` also on >1); `FirstOrDefault()`/`SingleOrDefault()` return `default(T)`. Use OrDefault when "no match" is valid. Gotcha: `default(T)` is null only for reference types — for a value type it's `0`/`false`, so `FirstOrDefault<int>()` returns `0` and a `== null`/"is default?" check can't tell a real 0 from "not found".
• First/Single throw on empty (Single also on >1); FirstOrDefault/SingleOrDefault return default(T) • default(T) is null only for reference types — a value-type default (0/false) can hide "not found" ~ knows OrDefault doesn't throw but misses the value-type default gotcha

### csharp-async-01 · async · W3 · hard
**Q:** When would you return `ValueTask`/`ValueTask<T>` instead of `Task<T>`, and what rules constrain how a `ValueTask` may be consumed?
**A:** `Task` is a heap-allocated reference type per operation; `ValueTask<T>` is a struct avoiding that allocation when the result is often synchronous (cache hit/hot path). Rules: await it exactly once — never twice, never concurrently, no `.Result` before completion. To store, await repeatedly, or fan out, call `.AsTask()` first. Use only on measured hot paths; default to `Task`.
• Task is heap-allocated reference type; ValueTask is a struct avoiding allocation when the result is often synchronous • a ValueTask must be consumed once — no double/concurrent await, no `.Result` before completion • to store / await repeatedly / fan out, convert with `.AsTask()` first ~ "ValueTask is a faster Task" with no consume-once rule

### csharp-async-02 · async · W3 · hard
**Q:** What does the compiler do with `async`/`await`, and how does blocking on an async call (`.Result`/`.Wait()`) cause a deadlock?
**A:** `async`/`await` (C# 5) rewrites the method into a state machine: at each `await` on an incomplete task it captures the current `SynchronizationContext`, returns to the caller, and schedules the rest as a continuation resumed on completion. Blocking with `.Result`/`.Wait()`/`.GetAwaiter().GetResult()` on a single-threaded context deadlocks — the continuation needs the context the blocked thread holds. Fix: async all the way, don't block.
• await compiles to a state machine that returns to the caller and resumes via a continuation • sync-over-async (.Result/.Wait) deadlocks by blocking the captured SynchronizationContext the continuation needs • fix: async all the way / don't block on an async call ~ "await pauses and waits" with no state-machine or deadlock mechanism

### csharp-async-03 · async · W2 · medium
**Q:** What does `ConfigureAwait(false)` do, and why is it recommended in library code?
**A:** `await task.ConfigureAwait(false)` tells the continuation not to capture/resume on the original `SynchronizationContext`, resuming on a thread-pool thread. In libraries it avoids marshaling back to the caller's context — preventing the sync-over-async deadlock and cutting overhead. Unnecessary in app code with no sync context (ASP.NET Core); don't use it in UI code that touches controls after the await.
• ConfigureAwait(false) resumes off the captured context (on a thread-pool thread) • use in library code to avoid capturing the caller's context (deadlock + overhead) ~ "add it in libraries" without saying what it does

### csharp-async-04 · async · W2 · medium
**Q:** How does `CancellationToken` cancellation work, and what are `IAsyncEnumerable<T>` and `await foreach` for?
**A:** Cancellation is cooperative: pass a `CancellationToken` in, the callee observes it (`ThrowIfCancellationRequested()`, polling, or forwarding) and throws `OperationCanceledException` when the source cancels — ignoring the token = never cancelled. `IAsyncEnumerable<T>` (C# 8) is an async stream consumed with `await foreach`, yielding items as they arrive without buffering; the producer's token uses `[EnumeratorCancellation]` and `.WithCancellation(token)`.
• CancellationToken is cooperative — the callee must observe it; cancel throws OperationCanceledException • IAsyncEnumerable<T> + await foreach streams items asynchronously (C# 8), token via WithCancellation/[EnumeratorCancellation] ~ explains only one of cancellation or async streams

### csharp-generics-01 · generics · W2 · medium
**Q:** What does a `where` constraint do on a type parameter, and what are the main kinds?
**A:** `where T : …` restricts which types substitute for `T` and unlocks the operations allowed on `T`. Kinds: `class` (reference type), `struct` (non-nullable value type), `new()` (parameterless ctor → `new T()`), a base class/interface (call its members), plus `notnull`, `unmanaged`, `where T : U`. Without a constraint you can only do what `object` allows.
• `where T:` restricts the type argument and enables the corresponding operations (call members, `new T()`, …) • names concrete kinds: class / struct / new() / base-class-or-interface ~ knows `where` exists but not that it unlocks operations on T

### csharp-generics-02 · generics · W2 · hard
**Q:** Explain generic variance: what do `out` and `in` mean, and where do they apply?
**A:** Variance applies only to generic interfaces/delegates (not classes). `out T` (covariance, output-only) makes `IEnumerable<string>` assignable to `IEnumerable<object>`. `in T` (contravariance, input-only) makes `Action<object>` assignable to `Action<string>`. No modifier = invariant (`List<T>`, `IList<T>`). Works only for reference conversions.
• out = covariance (output only): IEnumerable<string> → IEnumerable<object> • in = contravariance (input only): Action<object> → Action<string> • applies only to interfaces/delegates (reference conversions); classes/List<T> are invariant ~ gets covariance but flips or omits contravariance

### csharp-generics-03 · generics · W2 · medium
**Q:** How does type inference work for a generic method, and when must you specify the type arguments explicitly?
**A:** A generic method declares its own type params (`T Max<T>(T a, T b)`); the compiler infers the type args from the value arguments, so `Max(1, 2)` needs no `<int>`. Specify explicitly when they can't be inferred — a param only in the return type or in no argument (`Create<Customer>()`), or to disambiguate. Inference is driven by arguments, not the return-type target.
• a generic method declares `<T>`; type args are inferred from the value arguments • must specify explicitly when a param isn't determined by any argument (e.g. only in the return type) ~ shows the syntax but no inference / when-to-specify

### csharp-generics-04 · generics · W1 · easy
**Q:** What does `default(T)` produce, and why does generic code need it?
**A:** `default(T)` (or target-typed `default`, C# 7.1) = the zero value of `T`: null for reference/nullable types, a zeroed instance (`0`, `false`, all-zero struct) for value types. Generic code uses it to yield "no value" without knowing whether `T` is class or struct. `default(T) == null` is meaningful only for reference types, so compare an arbitrary `T` with `EqualityComparer<T>.Default`.
• default(T) = zero value: null for reference/nullable types, a zeroed instance for value types • lets generic code yield a default without knowing T; compare via EqualityComparer<T>.Default ~ says default(T) is always null

### csharp-delegate-01 · delegates-events, linq · W2 · easy
**Q:** What are `Func`, `Action`, and `Predicate`, and what is a delegate?
**A:** A delegate is a type-safe reference to a method you can hold and pass around. `Action`/`Action<T,…>` returns `void`; `Func<…,TResult>` returns a value (last type arg = return); `Predicate<T>` is `Func<T,bool>`. Lambdas and method groups convert to the matching delegate type, enabling LINQ operators, callbacks, and event handlers.
• Action = void-returning delegate; Func<…,TResult> = value-returning (last type arg is the return) • a delegate is a type-safe method reference passed as a value (lambdas/method groups convert to it) ~ vaguely "function pointers" without the Action-vs-Func distinction

### csharp-delegate-02 · delegates-events · W2 · medium
**Q:** What does the `event` keyword add over a plain public delegate field?
**A:** An `event` wraps a delegate but restricts outside code to `+=`/`-=` only — you can't assign it with `=` (wiping subscribers) or invoke it externally. A plain public delegate field lets any caller overwrite the invocation list or raise it, breaking encapsulation. The type raises it, typically `Handler?.Invoke(this, e)`; the compiler generates `add`/`remove` accessors.
• an event restricts outside access to += / -= — you can't assign-over or invoke it externally • a plain delegate field can be overwritten or invoked by any caller (no encapsulation) ~ "an event is just a delegate" with no access-restriction distinction

### csharp-delegate-03 · delegates-events · W3 · hard
**Q:** A loop creates a lambda each iteration that reads the loop variable. When do all the lambdas end up seeing the same final value, and when are they safe?
**A:** A lambda captures the variable itself by reference, not a value snapshot. A C-style `for` loop has one shared variable, so all lambdas close over it and read its final value ("they all print N"); fix by copying into a per-iteration local (`int copy = i;`). A `foreach` variable has been per-iteration since C# 5, so capturing it is safe — only `for` is the trap. (Unlike Go, where the loop variable changed in 1.22.)
• a lambda captures the variable by reference, not its value at capture time • a `for` loop's single shared variable makes all lambdas see the final value; fix with a per-iteration copy • a `foreach` variable is per-iteration (since C# 5), so capturing it is safe ~ "closures capture the loop var" but thinks foreach is also broken / no per-iteration-copy fix

### csharp-delegate-04 · delegates-events · W2 · medium
**Q:** What is a multicast delegate, and what happens to return values and exceptions when you invoke one?
**A:** Delegates are multicast: `+=` combines methods into one invocation list called in order (`-=` removes the last match; delegates are immutable, so combining returns a new one). For a non-void delegate only the last invoked method's return value is kept. If any handler throws, it propagates immediately and the rest don't run. Iterate `GetInvocationList()` to collect all results/run all handlers.
• multicast: += builds an invocation list invoked in order on one call • only the last return value is kept; a throwing handler stops the rest ~ knows += chains handlers but not the last-return / throw-stops-the-rest behavior

### csharp-dispose-01 · disposal · W3 · medium
**Q:** What does `IDisposable` do, and how does a `using` statement differ from a `using` declaration?
**A:** `IDisposable.Dispose()` deterministically releases resources the GC won't handle promptly (files, sockets, connections). A `using` statement calls `Dispose` at the end of the block, even on exception (a try/finally). A `using` declaration (C# 8), `using var f = …;`, disposes at the end of the enclosing scope, cutting nesting. Prefer `using` over a manual `Dispose` so it runs on exceptions.
• using calls Dispose deterministically at block end, even on exception (try/finally) • a using declaration (C# 8) `using var x = …;` disposes at the end of the enclosing scope ~ "using calls Dispose" without the exception-safety or the declaration form

### csharp-dispose-02 · disposal, async · W2 · medium
**Q:** What are `IAsyncDisposable` and `await using` for, and when do you use them over `IDisposable`?
**A:** `IAsyncDisposable.DisposeAsync()` (C# 8, .NET Core 3.0+) performs cleanup that is itself async — flushing a stream, closing a network resource — without blocking a thread. Consume it with `await using`, which awaits `DisposeAsync` at scope end. Use it when disposal does IO; a type may implement both interfaces, and `await foreach` disposes its enumerator via `DisposeAsync`.
• IAsyncDisposable.DisposeAsync enables asynchronous cleanup, consumed with `await using` (C# 8) • use it when disposal itself does IO (flush/close) so it doesn't block a thread ~ knows `await using` exists but not DisposeAsync / why it must be async

### csharp-dispose-03 · disposal · W2 · medium
**Q:** What is the difference between a finalizer and `Dispose`, and when does a type actually need a finalizer?
**A:** `Dispose` = deterministic caller-triggered cleanup (via `using`). A finalizer (`~Type()`) = a GC-run safety net for unmanaged resources if `Dispose` was missed — non-deterministic, on the finalizer thread, delaying collection by a GC cycle. Most types need no finalizer; only a class directly owning an unmanaged handle does (prefer `SafeHandle`). `Dispose` calls `GC.SuppressFinalize(this)` to skip it after clean disposal.
• Dispose = deterministic caller-driven; finalizer = non-deterministic GC safety net for unmanaged resources • only a type owning an unmanaged resource needs a finalizer (prefer SafeHandle); Dispose calls GC.SuppressFinalize ~ thinks every IDisposable needs a finalizer / that a finalizer runs deterministically

### csharp-dispose-04 · disposal · W2 · hard
**Q:** Describe the full dispose pattern for a class that owns both managed and unmanaged resources.
**A:** Public `Dispose()` calls protected virtual `Dispose(bool disposing)` then `GC.SuppressFinalize(this)`; the finalizer `~Type()` calls `Dispose(false)`. In `Dispose(bool)`: `disposing=true` frees managed + unmanaged; `false` (finalizer path) frees only unmanaged (managed refs may be collected). Guard with a `_disposed` flag (idempotent). Modern C# mostly needs only `IDisposable`/`IAsyncDisposable` + a `SafeHandle`; the full pattern is for direct unmanaged ownership.
• Dispose() → virtual Dispose(bool disposing) + GC.SuppressFinalize; the finalizer calls Dispose(false) • disposing=true frees managed+unmanaged, false frees only unmanaged; guard with a disposed flag (idempotent) ~ implements Dispose() but not the Dispose(bool)/finalizer split

### csharp-span-01 · collections-spans · W2 · medium
**Q:** What are collection expressions, and what does the spread element `..` do?
**A:** Collection expressions (C# 12) give one `[...]` syntax to create arrays, `List<T>`, `Span<T>`, and other collections: `int[] a = [1, 2, 3];`, `List<int> l = [1, 2, 3];`. The target type picks the concrete collection and the compiler an efficient construction. The spread element `..` inlines another sequence's items: `int[] both = [.. first, .. second, 99];`.
• collection expressions `[...]` (C# 12) build arrays/List/Span/etc, target-typed • the spread element `..` inlines another collection's elements ~ knows `[1,2,3]` array syntax but not target-typing or the spread element

### csharp-span-02 · collections-spans · W3 · hard
**Q:** What are `Span<T>`, `ReadOnlySpan<T>`, and `Memory<T>`, and why can't you store a `Span<T>` in a field or use it across an `await`?
**A:** `Span<T>` is a `ref struct` — a stack-only view over contiguous memory (array, `stackalloc`, unmanaged) that slices without copying/allocating; `ReadOnlySpan<T>` is the read-only form (a `string` is a `ReadOnlySpan<char>`). Being a ref struct it stays on the stack: no boxing, no class field, no lambda capture, not across `await`/`yield`. `Memory<T>` is the heap-storable counterpart; get a span via `.Span`. Use `Memory<T>` when the view must persist in a field or across `await`.
• Span/ReadOnlySpan = allocation-free slice/view over contiguous memory (a stack-only ref struct) • being a ref struct, it can't be a field, boxed, captured, or live across await/yield • Memory<T> is the heap-storable form for async/fields; obtain a Span via `.Span` ~ "Span is a fast array slice" with no ref-struct / stack-only restriction

### csharp-span-03 · collections-spans · W2 · medium
**Q:** What does `stackalloc` do, and what must you be careful about when using it?
**A:** `stackalloc` allocates a block on the stack, not the heap, avoiding a GC allocation; since C# 7.2/8 it's assigned to a `Span<T>`: `Span<int> buf = stackalloc int[16];`. It's freed when the method returns — never return or store it beyond the frame — and the size must be small/bounded, since a large or loop-driven `stackalloc` can overflow the stack. Ideal for short-lived scratch buffers on hot paths.
• stackalloc allocates on the stack (no GC allocation), used as a Span<T> • freed when the method returns — don't leak it out of the frame; keep the size bounded (stack-overflow risk) ~ "stackalloc is fast" with no lifetime or stack-overflow caveat

### csharp-span-04 · collections-spans · W2 · medium
**Q:** What is the difference between an array (`T[]`) and `List<T>`, and what are `params` collections?
**A:** An array `T[]` is fixed-length contiguous storage; `List<T>` wraps a growable array (amortized O(1) `Add`, resizes by reallocating + copying). Array for a known fixed size / interop / spans; `List<T>` when it grows. `params` historically required a `T[]` (an array allocation per call); C# 13 adds params collections, letting `params` be `Span<T>`/`ReadOnlySpan<T>`/`IEnumerable<T>`/`List<T>` etc., so the compiler can pass a stack `Span` and skip the array allocation.
• array = fixed length; List<T> = growable (amortized Add, resizes by reallocating + copying) • params collections (C# 13) let `params` be Span/ReadOnlySpan/IEnumerable/etc, not just T[] — avoiding the array allocation ~ gives the array-vs-List size difference but nothing on params collections
