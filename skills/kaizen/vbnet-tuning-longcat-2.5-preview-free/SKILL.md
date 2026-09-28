---
name: vbnet-tuning-longcat-2.5-preview-free
description: Corrective VB.NET guidance for the exact area longcat-2.5-preview-free tests weak on (WithEvents/Handles vs AddHandler/RemoveHandler event wiring). Loaded only in VB.NET repos when this exact model is active.
when_to_use: The active model id is exactly longcat-2.5-preview-free AND the repo uses VB.NET (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-VB.NET repos.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: vbnet
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.75
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
