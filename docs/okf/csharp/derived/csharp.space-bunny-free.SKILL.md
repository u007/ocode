---
name: csharp-tuning-space-bunny-free
description: >
  Corrective C# knowledge for space-bunny-free, targeting the
  nullable-reference-type / value-vs-reference / record gaps this model showed
  on the closed-book csharp benchmark (record class positional members are
  init-only by default, value-type vs reference-type defaults, the `!!`
  parameter-null-check that never shipped).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `space-bunny-free` AND the repository is a C# project (a
  `*.csproj` file, or `.cs` files with `namespace`/`using` — per meta.yaml
  detection). For any other model or non-C# repo, do not load.
tuned_for: space-bunny-free
tuned_version: "alpha"
stack: csharp
source_scorecard: ../scores/space-bunny-free.md
threshold: 0.75
revalidate_when: model_version changes
---
# C# corrections for space-bunny-free

## types-nullability: record class positional members are init-only by default

- Never say a `record` / `record class` is "not automatically immutable" or
  that its positional properties are only "commonly" init-only. The compiler
  generates positional (primary-constructor) members of a `record class` as
  `init`-only. Immutability is the default, not a convention the author
  opts into.
- State the struct side explicitly and in the opposite direction: positional
  members of a `record struct` are **mutable** by default, and only a
  `readonly record struct` makes them immutable. Cover both defaults whenever
  you compare record class, record struct, and plain class.

## types-nullability: always state the default value alongside copy semantics

- When explaining value vs. reference types, do not stop at "copied vs.
  shared reference." Add the default: a value type's default is a zeroed,
  non-null instance (`0`, `false`, an all-zero struct — never `null` unless
  it is `Nullable<T>`); a reference type's default is `null`. The default
  question is asked together with the copy question and is routinely
  dropped.
- Say the defensive-copy behavior plainly, not hedged: mutating a struct held
  in a `readonly` field mutates a defensive copy and silently does nothing;
  `foreach (var s in list) s.X = ...` and `list[i].X = ...` act on a copy of
  the element, not the element itself.

## types-nullability: `!!` parameter-null-check was proposed for C# 11 and never shipped

- When explaining `#nullable enable`, `?`, `!`, and nullable reference
  types, include the version history: the `!!` parameter-null-check operator
  was slated for C# 11 and removed before release. It does not exist in any
  shipped C# version. Do not omit it, and do not describe it as available.
- Give the introducing C# version for the features you name (NRT = C# 8).
  Nullability answers in this stack are version-sensitive; "modern C#" is not
  an acceptable substitute for the version number.
