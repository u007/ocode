---
name: csharp-tuning-longcat-2.5-preview-free
description: >
  Corrective C# knowledge for longcat-2.5-preview-free, targeting its
  nullability/value-vs-reference-type gaps and its async gaps (ValueTask
  consumption rules, async-all-the-way, CancellationToken and
  IAsyncEnumerable API names).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `longcat-2.5-preview-free` AND the repository is a C# project (a
  `*.csproj` file, or `.cs` files with `namespace`/`using` — per meta.yaml
  detection). For any other model or non-C# repo, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: csharp
source_scorecard: ../scores/longcat-2.5-preview-free.rerun-2026-10-05.md
threshold: 0.85
revalidate_when: model_version changes
---
# C# corrections for longcat-2.5-preview-free

## types-nullability: state defaults, copy semantics and record mutability exactly

- Nullable reference types are compile-time analysis only (C# 8); annotations
  are erased at runtime and add no null checks. The `!!` parameter-null-check
  operator was proposed for C# 11 but removed before release and never shipped;
  use `ArgumentNullException.ThrowIfNull(x)`.
- Always state defaults: a value type's default is a zeroed, non-null instance
  (`default(int)` is 0; null only for `Nullable<T>`); a reference type's
  default is `null`.
- Structs are copied on assignment, argument passing and return; classes share
  one heap object. Do not reduce this to "stack vs heap" (a struct field inside
  a class lives on the heap).
- Calling a mutating member on a struct held in a `readonly` field mutates a
  defensive copy and silently does nothing to the field. The same applies to a
  `foreach` iteration variable: it is a per-iteration copy. Prefer
  `readonly struct`.
- `record` / `record class`: reference type; positional members are init-only
  (immutable by default); compiler generates value equality (`Equals`,
  `GetHashCode`, `==`, `!=`), `ToString`, `with` and, for positional records,
  `Deconstruct`.
- `record struct` (C# 10): value type with generated value equality; positional
  members are mutable (`get; set;`) unless declared `readonly record struct`,
  which makes them init-only. Never say a record struct is immutable by default.
- A plain `class` has reference equality unless `Equals`/`==` are overridden.

## async: ValueTask rules, blocking deadlocks, cancellation and async streams

- `ValueTask<T>` is a struct that avoids a `Task` allocation when the result
  is often already available synchronously (cache hit, hot path); default to
  `Task<T>` otherwise.
- A `ValueTask` must be awaited or consumed exactly once: no awaiting it twice,
  no concurrent awaits, no `.Result`/`GetAwaiter().GetResult()` before it has
  completed.
- To store a `ValueTask`, await it more than once, or fan out with
  `Task.WhenAll`/`Task.WhenAny`, call `.AsTask()` once and use only the
  resulting `Task` afterwards. Converting with `.AsTask()` is the sanctioned
  way to get those capabilities; do not list it as a forbidden operation.
- `await` compiles the method into a state machine; at an incomplete await it
  returns to the caller and registers a continuation.
- Blocking on async work with `.Result`, `.Wait()` or `.GetAwaiter().GetResult()`
  deadlocks on a single-threaded `SynchronizationContext` (UI, classic ASP.NET):
  the blocked thread owns the context the continuation needs. ASP.NET Core has
  no such context.
- Always give the fix: make the call chain async all the way up and `await`
  instead of blocking; `ConfigureAwait(false)` in library code is a mitigation,
  not the primary fix.
- Cancellation is cooperative: the callee must observe the token, via
  `token.ThrowIfCancellationRequested()`, polling `IsCancellationRequested`, or
  forwarding the token to downstream APIs. Cancelling the
  `CancellationTokenSource` makes observers throw `OperationCanceledException`
  (`TaskCanceledException` derives from it). Code that ignores the token is
  never cancelled.
- `IAsyncEnumerable<T>` (C# 8) is an asynchronous stream consumed with
  `await foreach`; items arrive as produced without buffering the whole
  sequence.
- Exact API names: `IAsyncEnumerable<T>.GetAsyncEnumerator(CancellationToken)`,
  `IAsyncEnumerator<T>.MoveNextAsync()` (returns `ValueTask<bool>`), `Current`,
  `DisposeAsync()`. There is no `GetAsyncEnumeratorAsync` or `MoveAsyncNextAsync`.
- Pass a token to an async stream with `.WithCancellation(token)` on the
  consumer; in an `async IAsyncEnumerable<T>` iterator method mark the token
  parameter `[EnumeratorCancellation] CancellationToken ct = default`.
