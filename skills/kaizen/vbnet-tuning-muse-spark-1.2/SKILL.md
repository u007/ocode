---
name: vbnet-tuning-muse-spark-1.2
description: Corrective VB.NET guidance for the exact area muse-spark-1.2 tests weak on (WithEvents/Handles and AddHandler/RemoveHandler event-wiring edge cases). Loaded only in VB.NET repos when this exact model is active.
when_to_use: The active model id is exactly muse-spark-1.2 AND the repo uses VB.NET (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-VB.NET repos.
tuned_for: muse-spark-1.2
tuned_version: "1"
stack: vbnet
source_scorecard: ../scores/muse-spark-1.2.md
threshold: 0.75
revalidate_when: model_version changes
---

# VB.NET tuning — muse-spark-1.2

> Generated from `../scores/muse-spark-1.2.md` (corpus_rev 1). Covers **only**
> the tag this exact model scored below 0.75 on. It says nothing about
> syntax-basics, properties, nullability, error-handling, linq-query, oop, or
> conversions-arrays — the model already handles those well, and restating them
> would waste prompt/cache budget.

## Events: WithEvents/Handles and AddHandler/RemoveHandler edge cases (weak: events 0.69)

- A single `Handles` clause can bind **one method to multiple events** by
  comma-separating them: `Handles btn1.Click, btn2.Click`. State this whenever
  describing `Handles` — don't imply one handler method maps to only one event.
- When asked when `AddHandler`/`RemoveHandler` is *required* rather than merely
  convenient, name the two hard constraints, not just "the source isn't a
  WithEvents variable":
  - `Shared` events cannot be handled with `WithEvents`/`Handles` — they require
    `AddHandler`/`RemoveHandler`.
  - A `Structure` cannot declare a `WithEvents` field — events raised by a
    `Structure` must be handled with `AddHandler`/`RemoveHandler`.
  These are structural impossibilities, not conveniences — call them out by
  name rather than folding them into a general "dynamic wiring is more
  flexible" answer.
