---
type: Concept
title: "Server-Side Auto-Continue Loop"
description: 'Decoupled auto-continue confidence floor from permissions.auto.min_confidence: 0.6 vs 0.85, added model-set warning note.'
tags: []
timestamp: 2026-09-18T11:28:43Z
---
---
title: Server-Side Auto-Continue Loop
type: Concept
description: Server-side auto-continue loop pattern: bounded chain with step-limit cutoff, judge dispatch, and visible end-of-turn status.
tags:
  - server
  - auto-continue
  - agent-loop
  - architecture
  - typesafe
  - scheduled-jobs
---

# Server-Side Auto-Continue Loop

## Overview

The server-side auto-continue loop automatically re-polls the LLM after each turn completes when the agent has not explicitly stopped, enabling unattended multi-step workflows (scheduled jobs, headless API calls, cron dispatch). The loop is bounded: it chains a finite number of continuation steps before forcing a stop, preventing runaway token usage.

## Architecture

### Loop Lifecycle

1. **Initial turn** — The server dispatches the first `agent.Step` from the incoming message.
2. **Judge dispatch** — On turn completion, a lightweight "auto-continue judge" (the configured auto-continue model, e.g. `typesafe/<model>`) inspects the transcript tail and returns a typed **continue / end** decision with a confidence score.
3. **Continue gate** — If the judge says **end** (or confidence is below `autoContinueMinConfidenceDefault`, default 0.6 — see `internal/agent/autocontinue_typesafe.go`), the loop terminates and the final result is returned. This floor is **decoupled from** `permissions.auto.min_confidence` (0.85), which remains the shared threshold for the auto-permission judge and the discovery relevance judge. Different actions deserve different confidence bars: auto-continue is low-stakes and reversible (the chain is bounded at 4), so it uses a lower floor.
4. **Chain extension** — If the judge says **continue**, the loop increments the step counter and starts a new `agent.Step` with the accumulated transcript.
5. **Chain cap** — The loop stops after reaching `AutoContinueChainCap` (a Go constant, value 4 — see `internal/agent/autocontinue_typesafe.go:211`), regardless of the judge's verdict. The end-of-turn reason is surfaced as a transcript notice (server) or TUI transient hint (see "End-of-Turn Surfacing" below).

### Key Parameters

| Parameter | Default | Description |
|---|---|---|
| `auto_continue_enabled` | `false` | Master toggle for the auto-continue feature |
| `auto_continue_model` | (empty) | Model used for the continue/end judge decision |
| `autoContinueMinConfidenceDefault` | 0.6 | Confidence floor for auto-continue triage only (`internal/agent/autocontinue_typesafe.go`) — decoupled from `permissions.auto.min_confidence` (0.85), which governs auto-permission and discovery |
| `AutoContinueChainCap` | 4 (constant) | Maximum consecutive auto-fired resumes per turn — not a config key; defined in code |

### Typesafe Judge Path

When `auto_continue_model` is a `typesafe/*` route, the judge runs via `runAutoContinueJudgeTypesafe` — a single `Decide()` call answering a typed **continue/end** choice over the transcript tail. This never generates text; it returns a structured decision with a confidence score. The confidence is thresholded against `resolveAutoContinueMinConfidence()` (default 0.6), which is intentionally lower than the permission/discovery floor (`resolveAutoJudgeMinConfidence()`, default 0.85) because a false-positive continue is cheap — the chain is capped at 4 steps.

Note: when `auto_continue_model` is set but `auto_continue_enabled` is `false`, the TUI `/autocontinue model <name>` command and the web `/autocontinue model <name>` command both print a warning reminding the user to run `/autocontinue on` (or flip the sidebar toggle) to actually enable the feature.

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