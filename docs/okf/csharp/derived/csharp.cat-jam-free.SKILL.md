---
name: csharp-tuning-cat-jam-free
description: >
  Corrective C# knowledge for cat-jam-free, targeting gaps this model showed on
  the closed-book csharp benchmark: record/record struct mutability and type
  defaults, async idioms (Task vs ValueTask, async all the way, async-stream
  cancellation), what generic constraints unlock, the full dispose pattern,
  and Span/Memory/params-collections details.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `cat-jam-free` AND the repository is a C# project (a `*.csproj`
  file, or `.cs` files with `namespace`/`using` — per meta.yaml detection).
  For any other model or non-C# repo, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: csharp
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# C# corrections for cat-jam-free

## types-nullability

- **`record struct` positional members are MUTABLE** (generated `get; set;`).
  Only `readonly record struct` makes them immutable. Never say a record
  struct's positional members are init-only.
- **`record` / `record class` positional members ARE init-only** by default —
  not "often". Immutability is the default for the class form.
- A `record class` (C# 9) also generates nondestructive mutation via `with`;
  `record struct` is C# 10.
- When explaining value vs reference types, always state the default: a value
  type's default is a zeroed, non-null instance (`0`, `false`, all-zero struct;
  null only for `Nullable<T>`); a reference type's default is `null`.
- Nullable reference types are C# 8. The `!!` parameter-null-check operator was
  proposed for C# 11 and removed before release — it never shipped. Mention it
  when explaining `?`, `!`, `#nullable`.

## async

- `Task`/`Task<T>` is a heap-allocated reference type; `ValueTask<T>` is a
  **struct** that avoids that allocation when the result is often synchronous.
  Say both halves, not just "reduces allocations".
- The fix for sync-over-async deadlock is **async all the way**: await up the
  call chain; never block with `.Result`, `.Wait()`, or
  `.GetAwaiter().GetResult()`.
- `IAsyncEnumerable<T>` cancellation: the consumer passes a token with
  `.WithCancellation(token)`; the async-iterator producer marks its token
  parameter `[EnumeratorCancellation]`. Name both.

## generics

- A `where T : ...` constraint does two things: it restricts the type argument
  AND unlocks operations on `T` — `new()` permits `new T()`, a base class or
  interface permits calling its members. Without a constraint only `object`
  members are available. Always state the unlocking side.
- To compare an arbitrary `T` against its default, use
  `EqualityComparer<T>.Default.Equals(value, default)`; `value == null` is only
  meaningful when `T` is a reference type.

## disposal

- A `using` statement compiles to `try`/`finally`, so `Dispose` runs **even when
  the block throws**. State the exception-safety guarantee explicitly.
- When a type has a finalizer, `Dispose()` calls `GC.SuppressFinalize(this)` so
  the finalizer is skipped after clean disposal (and the object is not kept for
  an extra GC cycle).
- In `Dispose(bool disposing)`, guard the body with a `_disposed` flag so
  repeated `Dispose` calls are idempotent.

## collections-spans

- `Memory<T>` is the heap-storable form; to touch its data you get a span via
  `memory.Span`.
- `List<T>` wraps a growable array: amortized O(1) `Add`, and when full it
  allocates a larger backing array and copies. Arrays are fixed length.
- **params collections are C# 13**: a `params` parameter may be `Span<T>`,
  `ReadOnlySpan<T>`, `IEnumerable<T>`, `List<T>`, etc., not only `T[]`. The
  point is avoiding the per-call array allocation (the compiler can pass a
  stack-allocated span). Name the version and the allocation benefit.
