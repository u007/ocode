---
type: Concept
title: Server-Side Auto-Continue Loop
description: 'Server-side auto-continue loop pattern: bounded chain with step-limit cutoff, judge dispatch, and visible end-of-turn status.'
tags: [server, auto-continue, agent-loop, architecture, typesafe, scheduled-jobs]
timestamp: 2026-09-18T01:32:21Z
---
## Overview

The server-side auto-continue loop automatically re-polls the LLM after each turn completes when the agent has not explicitly stopped, enabling unattended multi-step workflows (scheduled jobs, headless API calls, cron dispatch). The loop is bounded: it chains a finite number of continuation steps before forcing a stop, preventing runaway token usage.

## Architecture

### Loop Lifecycle

1. **Initial turn** — The server dispatches the first `agent.Step` from the incoming message.
2. **Judge dispatch** — On turn completion, a lightweight "auto-continue judge" (the configured auto-continue model, e.g. `typesafe/<model>`) inspects the transcript tail and returns a typed **continue / end** decision with a confidence score.
3. **Continue gate** — If the judge says **end** (or confidence is below `permissions.auto.min_confidence`), the loop terminates and the final result is returned.
4. **Chain extension** — If the judge says **continue**, the loop increments the step counter and starts a new `agent.Step` with the accumulated transcript.
5. **Chain cap** — The loop stops after reaching `AutoContinueChainCap` (a Go constant, value 4 — see `internal/agent/autocontinue_typesafe.go:211`), regardless of the judge's verdict. The end-of-turn reason is surfaced as a transcript notice (server) or TUI transient hint (see "End-of-Turn Surfacing" below).

### Key Parameters

| Parameter | Default | Description |
|---|---|---|
| `auto_continue_enabled` | `false` | Master toggle for the auto-continue feature |
| `auto_continue_model` | (empty) | Model used for the continue/end judge decision |
| `permissions.auto.min_confidence` | 0.85 | Minimum confidence for the judge to allow continuation |
| `AutoContinueChainCap` | 4 (constant) | Maximum consecutive auto-fired resumes per turn — not a config key; defined in code |

### Typesafe Judge Path

When `auto_continue_model` is a `typesafe/*` route, the judge runs via `runAutoContinueJudgeTypesafe` — a single `Decide()` call answering a typed **continue/end** choice over the transcript tail. This never generates text; it returns a structured decision with a confidence score. The confidence is thresholded against `permissions.auto.min_confidence`.

## End-of-Turn Surfacing

The auto-continue loop does **not** set a `auto_continue` status field on the turn result. Instead, the termination reason is surfaced through two mechanisms:

**Server (headless/web):** An assistant transcript message with empty `Content` and a `Notice` string (via `appendTranscriptMessage` in `internal/server/agent_session.go`). Example notice: `↩ auto-continue — cut off by /max-step, resuming`.

**TUI:** A transient transcript hint via `agentAutoContinueDeclineDetail` (`internal/tui/model.go`), which returns one of:
- `"turn hit the /max-step cap (auto-continue is off) — reply summarized, work may remain"` — when auto-continue is disabled but the step limit was hit.
- `"turn hit the /max-step cap again — auto-continue chain cap (4) reached; send a message to resume manually"` — when the chain cap is exhausted.

A judge verdict line (e.g. "judge: continued — mid_task") is rendered inline by the judge's `Detail` string, not as a status field.

## Design Constraints

- **Bounded chain only** — no unbounded loops; `AutoContinueChainCap` (4) is enforced server-side.
- **Judge is advisory** — a low-confidence or error result always stops the loop.
- **Permission-aware** — auto-continue refuses to resume past an unresolved permission or question ask; the agent can raise asks mid-chain, but the loop stops until they are resolved.
- **Surfaced in transcript** — auto-continue outcomes are rendered as a transient TUI hint or a server transcript notice, not as an agent-status field.