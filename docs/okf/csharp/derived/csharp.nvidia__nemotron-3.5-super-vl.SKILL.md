---
name: csharp-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective C# knowledge for nvidia/nemotron-3.5-super-vl, targeting gaps this
  model showed on the closed-book csharp benchmark: nullable-reference-type
  history, value vs reference type defaults, record struct mutability, and async
  idioms (ValueTask to Task conversion, async all the way, async-stream
  cancellation).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `nvidia/nemotron-3.5-super-vl` AND the repository is a C# project
  (a `*.csproj` file, or `.cs` files with `namespace`/`using` — per meta.yaml
  detection). For any other model or non-C# repo, do not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: csharp
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes
---
# C# corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
## types-nullability

- Nullable reference types are C# 8. The `!!` parameter-null-check operator was
  proposed for C# 11 and removed before release — it never shipped. Mention it
  when explaining `?`, `!`, `#nullable`; never emit `!!` in code.
- When explaining value vs reference types, always state the default: a value
  type's default is a zeroed, non-null instance (`0`, `false`, all-zero struct;
  null only for `Nullable<T>`); a reference type's default is `null`.
- **`record struct` positional members are MUTABLE** (generated `get; set;`).
  Only `readonly record struct` makes them init-only. A `record struct` is NOT
  "the same code generation as a record class but as a value type" — the
  mutability default differs. `record` / `record class` positional members ARE
  init-only.

## async

- If a `ValueTask` must be stored, awaited more than once, or fanned out
  (`Task.WhenAll`), convert it first with `.AsTask()`. Name the conversion; do
  not stop at "prefer `Task<T>` instead".
- The fix for a sync-over-async deadlock is **async all the way**: await up the
  call chain; never block with `.Result`, `.Wait()`, or
  `.GetAwaiter().GetResult()`. State the fix, not only the mechanism.
- `IAsyncEnumerable<T>` cancellation has two halves: the consumer passes a token
  with `.WithCancellation(token)`; the async-iterator producer marks its token
  parameter `[EnumeratorCancellation]`. Name both.
<!-- /kaizen:digest -->
