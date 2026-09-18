---
type: Concept
title: Server-Side Auto-Continue Loop
description: 'Server-side auto-continue loop pattern: bounded chain with step-limit cutoff, judge dispatch, and visible end-of-turn status.'
tags:
  - server
  - auto-continue
  - agent-loop
  - architecture
  - typesafe
  - scheduled-jobs
timestamp: 2026-09-17T14:17:30Z
---
## Overview

The server-side auto-continue loop automatically re-polls the LLM after each turn completes when the agent has not explicitly stopped, enabling unattended multi-step workflows (scheduled jobs, headless API calls, cron dispatch). The loop is bounded: it chains a finite number of continuation steps before forcing a stop, preventing runaway token usage.

## Architecture

### Loop Lifecycle

1. **Initial turn** — The server dispatches the first `agent.Step` from the incoming message.
2. **Judge dispatch** — On turn completion, a lightweight "auto-continue judge" (the configured auto-continue model, e.g. `typesafe/<model>`) inspects the transcript tail and returns a typed **continue / end** decision with a confidence score.
3. **Continue gate** — If the judge says **end** (or confidence is below `permissions.auto.min_confidence`), the loop terminates and the final result is returned.
4. **Chain extension** — If the judge says **continue**, the loop increments the step counter and starts a new `agent.Step` with the accumulated transcript.
5. **Step-limit cutoff** — The loop stops after reaching the **chain cap** (`max_auto_continue_steps`, default 10), regardless of the judge's verdict. The final turn's result is returned with an `auto_continue: step_limit` status.

### Key Parameters

| Parameter | Default | Description |
|---|---|---|
| `max_auto_continue_steps` | 10 | Maximum number of continuation steps before forced stop |
| `auto.min_confidence` | 0.5 | Minimum confidence for the judge to allow continuation |
| `AutoContinueModel` | `typesafe/<model>` | Model used for the continue/end decision |

### Typesafe Judge Path

When `AutoContinueModel` is a `typesafe/*` route, the judge runs via `runAutoContinueJudgeTypesafe` — a single `Decide()` call answering a typed **continue/end** choice over the transcript tail. This never generates text; it returns a structured decision with a confidence score. The confidence is thresholded against `permissions.auto.min_confidence`.

## Visible End-of-Turn Status

The auto-continue loop surfaces its termination reason to the client via status fields on the turn result:

| Status | Meaning |
|---|---|
| `auto_continue: completed` | Judge said end; turn completed normally |
| `auto_continue: step_limit` | Chain cap reached; forced stop |
| `auto_continue: judge_error` | Judge failed; loop stops for safety |

## Design Constraints

- **Bounded chain only** — no unbounded loops; the step limit is enforced server-side.
- **Judge is advisory** — a low-confidence or error result always stops the loop.
- **No user interaction** — auto-continue runs without user prompts; it cannot ask for permission mid-chain.
- **Transparent** — the loop status is visible in agent status and the TUI agent strip so users can see when auto-continue triggered and why it stopped.
