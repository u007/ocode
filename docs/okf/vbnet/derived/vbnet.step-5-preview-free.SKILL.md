---
name: vbnet-tuning-step-5-preview-free
description: Corrective VB.NET guidance for the one area step-5-preview-free tests weak on (LINQ query syntax: the mandatory `Group By ... Into` clause and the `Aggregate ... Into` form). Loaded only in VB.NET repos when this exact model is active.
when_to_use: The active model's provider-stripped canonical id is exactly step-5-preview-free AND the repo uses VB.NET (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-VB.NET repos.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: vbnet
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# VB.NET tuning — step-5-preview-free

## LINQ query syntax: Group By / Aggregate

- `Into` is mandatory in a VB `Group By` clause. There is no form that omits
  it, not even when only the key and the default group are needed. Minimum:
  `From p In people Group By p.City Into Group`.
- Aggregates and their aliases go in that same `Into` clause, never in a later
  `Select`: `Group By p.City Into Group, Total = Count(), Avg = Average(p.Age)`.
  The group itself may be aliased (`Into g = Group`). After the clause the key
  is reachable by its member name (`City`) and the group by `Group` or its
  alias; a trailing `Select` is optional.
- `Aggregate` is a query starter, like `From`, and the clause that follows its
  range is `Into`, never `In`:
  `Dim total = Aggregate n In nums Where n > 0 Into Sum(n)`.
  One aggregate yields a scalar; several (`Into Sum(n), Max(n), Count()`) yield
  one anonymous object, not a sequence.
- Do not open a query with `From x In xs` and then add `Aggregate x In xs ...`:
  the range variable cannot be redeclared and the source is not re-named.
  Either start with `Aggregate o In orders Where o.Paid Into Sum(o.Amount)`,
  or use `Aggregate` as a later clause over a nested sequence:
  `From c In customers Aggregate o In c.Orders Into Total = Sum(o.Amount)`.
- The VB `Aggregate` clause is not `Enumerable.Aggregate` (the custom fold).
  It applies the built-in aggregate functions only: `Sum`, `Count`,
  `LongCount`, `Average`, `Min`, `Max`, `Any`, `All`. `Any`/`All` are two of
  those functions, not custom accumulators. A custom fold is the method call
  `.Aggregate(seed, Function(acc, x) ...)`.
