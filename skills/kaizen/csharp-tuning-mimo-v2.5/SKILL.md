---
name: csharp-tuning-mimo-v2.5
description: >
  Corrective C# knowledge for mimo-v2.5, targeting the nullable-reference/
  record-equality and pattern-matching gaps this model showed on the
  closed-book csharp benchmark (record class mutability, switch expression
  exhaustiveness, property-pattern syntax, `is` binding scope).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `mimo-v2.5` AND the repository is a C# project (a `*.csproj`
  file, or `.cs` files with `namespace`/`using` — per meta.yaml detection).
  For any other model or non-C# repo, do not load.
tuned_for: mimo-v2.5
tuned_version: "2.5"
stack: csharp
source_scorecard: ../scores/mimo-v2.5.md
threshold: 0.75
revalidate_when: model_version changes
---

# C# corrections for mimo-v2.5

mimo-v2.5 is strong across most of the stack (84% overall; linq,
delegates-events, generics, disposal, async, and collections-spans are all
≥ 0.78). Two areas fall below threshold: **types-nullability** (0.73) and
**pattern-matching** (0.63). The sections below target only the specific
mistakes it made there — everything else it already knows, so nothing else
is restated here.

## types-nullability: record class positional members are init-only by default

- Do not describe a `record`/`record class` as "mutable by default unless
  properties are init-only." A record's positional (primary-constructor)
  members are compiler-generated as `init`-only from the start — the
  immutability is the default, not an opt-in.
- A `record struct`, by contrast, IS mutable by default; only a
  `readonly record struct` locks its members down. State the class/struct
  default correctly in each direction — do not apply the struct's default to
  the class.

## types-nullability: value types have a non-null zeroed default, reference types default to null

- When asked about value vs. reference type behavior, always state the
  default value alongside copy semantics: a value type's default is a
  zeroed, non-null instance (`0`, `false`, an all-zero struct); a reference
  type's default is `null`. Don't stop at "copied vs. shared" — the default
  question is commonly asked together with it and is frequently dropped.

## types-nullability: `!!` parameter-null-check was proposed for C# 11 and never shipped

- When explaining `#nullable enable`/`!`/NRT, note that the `!!`
  parameter-null-check operator was proposed for C# 11, then pulled before
  release. If the model isn't sure a language feature shipped, say so rather
  than omitting version history — NRT questions in this corpus are
  version-sensitive and reward calling out what didn't ship, not just what
  did.

## pattern-matching: a non-exhaustive switch expression is a WARNING, not a compile error

- Never say a non-exhaustive switch expression fails to compile. It compiles
  with warning **CS8509** and throws `SwitchExpressionException` at runtime
  if an unmatched value is actually hit. Only a `_ =>` (or otherwise
  exhaustive) arm avoids both the warning and the runtime risk.

## pattern-matching: property pattern syntax uses `:`, not `=`

- A property pattern is written `{ PropertyName: subPattern }` — e.g.
  `p is { Age: > 18 }`. Never write it with `=` (`{ Property = subPattern }`
  is not valid property-pattern syntax and will be marked wrong).

## pattern-matching: `is not T x` inverts where the bound variable is definitely assigned

- When explaining the `is T x` declaration pattern, state where the bound
  variable is definitely assigned, including the negated form: with
  `if (obj is not Customer c) return;`, `c` is NOT assigned inside the `if`
  body (the non-matching path) but IS definitely assigned on the
  fall-through code after the guard — the inverse of the positive form. Cover
  both directions, not just the positive `is T x` case.
