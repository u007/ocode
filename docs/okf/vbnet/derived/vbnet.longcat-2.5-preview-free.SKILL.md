---
name: vbnet-tuning-longcat-2.5-preview-free
description: Corrective VB.NET guidance for the exact area longcat-2.5-preview-free tests weak on (WithEvents/Handles vs AddHandler/RemoveHandler event wiring). Loaded only in VB.NET repos when this exact model is active.
when_to_use: The active model id is exactly longcat-2.5-preview-free AND the repo uses VB.NET (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-VB.NET repos.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: vbnet
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# VB.NET tuning — longcat-2.5-preview-free

## Events: WithEvents/Handles vs AddHandler/RemoveHandler

- `WithEvents` is valid only on a class- or module-level field. It cannot be
  used on a local variable, and a `Structure` cannot declare a `WithEvents`
  field.
- `WithEvents` + `Handles` means the compiler wires the handler with **no
  explicit `AddHandler` call**. Say so directly, because that is the main
  difference from the dynamic API. Assigning a new object to the
  `WithEvents` field rewires the handler automatically.
- One `Handles` clause can bind one method to several events if you
  separate them with commas: `Handles btn1.Click, btn2.Click`.
- When asked when `AddHandler`/`RemoveHandler` is *required*, name the two
  cases where `Handles` cannot be used at all, not only "dynamic wiring":
  - `Shared` events. They cannot be handled through `WithEvents`/`Handles`.
  - Events raised by a `Structure`. A `Structure` cannot hold a
    `WithEvents` field.

## LINQ query syntax: Group By / Aggregate

- Aggregate functions in a VB `Group By` belong in its `Into` clause, not in
  a later `Select`: `From p In people Group By p.City Into Group, Total = Count(), Avg = Average(p.Age)`.
  `Into Group` binds the grouped sequence; each aggregate can be aliased
  (`Name = Sum(...)`).
- `Aggregate` is a query keyword that starts a query and folds it to a single
  scalar, not a sequence: `Dim total = Aggregate n In nums Where n > 0 Into Sum(n)`.
  `From` starts a query that yields a sequence (`IEnumerable(Of T)`).
- A VB query begins with `From` or `Aggregate`. It may end with any clause,
  and `Select` is optional.

## OOP: Implements and inheritance modifiers

- `Inherits` takes one base class only. `Implements` lists any number of
  interfaces on the type.
- Each implementing member must also carry its own clause binding it to the
  interface member: `Public Sub Save() Implements IRepo.Save`. Listing the
  interface on the class is not enough. The clause lets the member have a
  different name and lets one member implement several interface members.
- Members are non-virtual by default. `Overridable` makes a member virtual,
  and `Overrides` replaces an `Overridable` (or `MustOverride`) base member.
- `MustOverride` is an abstract member with no body, and its class must be
  `MustInherit`. `MustInherit` classes cannot be instantiated.
- `NotOverridable` is the sealing modifier and is valid only together with
  `Overrides` (`Public NotOverridable Overrides Sub M()`). It is not the
  default: an `Overrides` member is itself overridable in further subclasses
  unless marked `NotOverridable`.
