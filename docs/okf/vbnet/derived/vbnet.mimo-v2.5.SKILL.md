---
name: vbnet-tuning-mimo-v2.5
description: Corrective VB.NET guidance for the exact area mimo-v2.5 tests weak on (WithEvents/Handles vs AddHandler/RemoveHandler event wiring). Loaded only in VB.NET repos when this exact model is active.
when_to_use: The active model id is exactly mimo-v2.5 AND the repo uses VB.NET (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-VB.NET repos.
tuned_for: mimo-v2.5
tuned_version: "1"
stack: vbnet
source_scorecard: ../scores/mimo-v2.5.md
threshold: 0.75
revalidate_when: model_version changes
---

# VB.NET tuning — mimo-v2.5

> Generated from `../scores/mimo-v2.5.md` (corpus_rev 1). Covers **only** the
> tag this exact model scored below 0.75 on. It says nothing about
> syntax-basics, properties, nullability, error-handling, linq-query, oop, or
> conversions-arrays — the model already handles those well, and restating them
> would waste prompt/cache budget.

## Events: WithEvents/Handles vs AddHandler/RemoveHandler (weak: events 0.69)

- When explaining `WithEvents` + `Handles`, always state explicitly that the
  compiler wires the handler **with no explicit `AddHandler` call** — this is
  the defining contrast with the dynamic API, not an incidental detail.
- A single `Handles` clause can bind **one method to multiple events** by
  comma-separating them: `Handles btn1.Click, btn2.Click`. Mention this when
  describing `Handles` — don't imply one handler method maps to only one event.
- When asked when `AddHandler`/`RemoveHandler` is *required* rather than merely
  convenient, name the two hard constraints, not just "dynamic wiring is more
  flexible":
  - `Shared` events cannot be handled with `WithEvents`/`Handles` — they require
    `AddHandler`/`RemoveHandler`.
  - A `Structure` cannot declare a `WithEvents` field — events raised by a
    `Structure` must be handled with `AddHandler`/`RemoveHandler`.
  General reasons like "the source isn't known at compile time" are true but
  incomplete without naming these two cases where `Handles` is not just
  inconvenient but structurally unusable.
