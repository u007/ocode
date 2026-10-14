---
name: vbnet-tuning-cat-jam-free
description: Corrective VB.NET guidance for the one area cat-jam-free tests weak on (WithEvents/Handles event wiring and when AddHandler/RemoveHandler is required). Loaded only in VB.NET repos when this exact model is active.
when_to_use: The active model's provider-stripped canonical id is exactly cat-jam-free AND the repo uses VB.NET (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-VB.NET repos.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: vbnet
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# VB.NET tuning — cat-jam-free

## Events: WithEvents/Handles vs AddHandler/RemoveHandler

- When explaining `WithEvents` + `Handles`, state outright that the compiler
  wires the handler with **no explicit `AddHandler` call**. That is the defining
  contrast with the dynamic API, so say it, not only "the compiler generates
  the wiring".
- One `Handles` clause can bind a single method to **several events**, comma
  separated: `Private Sub OnClick(sender As Object, e As EventArgs) Handles btnOk.Click, btnCancel.Click`.
- Assigning a new object to a `WithEvents` field rewires the `Handles`
  methods to the new instance automatically. A `WithEvents` field must be a
  class-level field: never a local variable, and never in a `Structure`.
- When asked when `AddHandler`/`RemoveHandler` is *required* rather than just
  more flexible, name the cases where `Handles` cannot be used at all:
  `Shared` events, events on a `Structure`, and sources that are not held in a
  `WithEvents` field (locals, collection items, objects created at runtime).
  Also needed to attach a lambda or to unsubscribe (`RemoveHandler`) before the
  subscriber's lifetime ends.
- `AddHandler source.Event, AddressOf Handler`. `AddressOf` produces the
  delegate. Pair every `AddHandler` on a long-lived source with a
  `RemoveHandler` to avoid leaks.
