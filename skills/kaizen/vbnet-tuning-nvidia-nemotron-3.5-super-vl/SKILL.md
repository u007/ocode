---
name: vbnet-tuning-nvidia-nemotron-3.5-super-vl
description: Corrective VB.NET guidance for the one area nvidia/nemotron-3.5-super-vl tests weak on (the VB inheritance model — per-member `Implements` clauses, what `NotOverridable` applies to, and `MustInherit` interface obligations). Loaded only in VB.NET repos when this exact model is active.
when_to_use: The active model's provider-stripped canonical id is exactly nvidia/nemotron-3.5-super-vl AND the repo uses VB.NET (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-VB.NET repos.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: vbnet
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes
---
# VB.NET tuning — nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
## Inheritance model: Implements, NotOverridable, MustInherit

- `Implements IFoo` on the class line is only half of implementing an interface.
  VB binds every member explicitly: each implementing member ends with its own
  `Implements IFoo.Member` clause, e.g.
  `Public Sub Save() Implements IRepository.Save`. The method name need not match
  the interface member, one member may implement several interface members
  (`Implements IA.Run, IB.Run`), and a member without the clause does not
  implement anything even if its signature matches.
- A `MustInherit` class that says `Implements IFoo` is not exempt from any
  member. It must still provide an `Implements IFoo.Member` clause for every
  interface member; the member itself may be `MustOverride` so that derived
  classes supply the body.
- `NotOverridable` is not a default and cannot be written on an ordinary member.
  It is legal only together with `Overrides`
  (`Public NotOverridable Overrides Sub Run()`) and means "this override is
  final: no further subclass may override it". Members are already
  non-overridable unless marked `Overridable`, `MustOverride` or `Overrides`.
- To forbid inheriting from a class, use `NotInheritable` on the class;
  `NotOverridable` never applies to a class.
- `Overridable` marks a base member virtual; `Overrides` replaces it;
  `MustOverride` is an abstract member whose containing class must be
  `MustInherit`; `MustInherit` makes the class non-instantiable. Keep the five
  keywords to those roles.
<!-- /kaizen:digest -->
