---
type: Gotcha
title: Agent Replacement — Input Queuing & Stream Event Epochs
description: Architectural decision and solution pattern for queuing user input during agent replacement and using stream event epochs to prevent stale events from mutating the new session.
tags:
  - gotcha
  - tui
  - agent
  - streaming
  - race-condition
  - replacement
  - epoch
timestamp: 2026-08-26T12:32:06Z
---
## Problem

When a user switches models (`/model`) or starts a new session (`/new`), the TUI must cleanly tear down the old agent and install the new one. Two race conditions make this non-trivial:

1. **Input queued against a dying agent**: If the user types a prompt or slash command during the replacement window, it could be dispatched to the retiring agent instead of the fresh one.
2. **Stale stream events mutating the new session**: The old agent's in-flight LLM stream may emit events (assistant messages, reasoning deltas, stream completion) *after* the new agent is installed. If these events reach the TUI, they corrupt the new transcript or prematurely stop the new stream.

## Solution: Two-Layer Guard

### Layer 1 — Input Queuing (`modelSwitchPending` / `sessionResetPending`)

When the model or session replacement begins, the TUI sets a pending flag (`modelSwitchPending` or `sessionResetPending`). While either flag is set:

- User input (plain prompts, shell commands `!…`, slash commands `/…`) is intercepted in `handleChatKeys` and appended to `queuedItems` via `queueReplacementWork`.
- A single, coalesced notice — *"Queued until the model/session replacement completes."* — is appended to the transcript as a transient assistant message. Multiple queued items do not produce multiple notices.
- The input field is cleared after queueing.

Once the replacement completes (new agent installed, MCP enumeration finished), `drainReplacementQueueIfReady` dispatches each queued item in order against the current agent/session. The drain guards against premature execution by checking all of: `replacementQueuePending`, `modelSwitchPending`, `sessionResetPending`, `mcpReady`, agent non-nil, and not already streaming.

**Key invariant**: Queued work is dispatched through the *current* agent/session, never the retiring one. The old agent's injection queue is not used.

### Layer 2 — Stream Event Epochs (`agentEpoch`)

Every stream event type carries an `epoch uint64` field:

| Type | Field |
|------|-------|
| `streamMsgEvent` | `epoch` |
| `streamStartedMsg` | `epoch` |
| `streamDoneMsg` | `epoch` |
| `deltaMsg` | `epoch` |

The model maintains `agentEpoch uint64`, which is incremented in two places:

1. **`invalidateAgentEvents()`** — called at the start of replacement. Bumps the epoch, closes the cancel channel, resets streaming state, and cancels the current agent + all sub-agents.
2. **`installAgent()`** — bumps the epoch *before* publishing the new agent pointer, so any old events already in the Bubble Tea message queue are rejected immediately.

The helper `currentStreamEpoch(epoch)` returns `true` when `epoch == 0 || epoch == m.agentEpoch`. Epoch 0 is the legacy/test default (hand-built messages). All real stream events carry a non-zero epoch stamped from `m.agentEpoch` at dispatch time.

Every `Update` handler for stream events checks `currentStreamEpoch` before mutating state. A stale epoch causes the event to be silently dropped — the old assistant message is not appended, the reasoning delta is not rendered, and a stale `streamDone` does not stop the current stream.

### Generation Guards on Completion Messages

The background model-switch and session-reset work emit typed completion messages (`modelSwitchDoneMsg`, `sessionResetDoneMsg`) that carry a `gen uint64` field. The `Update` handler compares `gen` against the current `modelSwitchGen` / `sessionResetGen` and drops stale completions from a superseded switch. This prevents a slow `NewAgent` construction from clobbering a newer replacement.

## Files

| File | Role |
|------|------|
| `internal/tui/model.go` | `invalidateAgentEvents`, `installAgent`, `currentStreamEpoch`, `queueReplacementWork`, `drainReplacementQueueIfReady`, epoch checks in `Update` handlers |
| `internal/tui/replacement_race_test.go` | Unit tests covering all three layers: input queuing during `modelSwitchPending`/`sessionResetPending`, notice coalescing, MCP-gated drain, and stale stream event rejection |

## Edge Cases and Gotchas

- **Epoch bump must happen *before* publishing the new agent pointer.** If the pointer is set first, a concurrent goroutine could dispatch a stream event against the new agent before the epoch advances, and the stale event would pass the epoch check.
- **Legacy callers emit epoch 0.** The `currentStreamEpoch` check treats 0 as always-valid so hand-built test messages and pre-epoch callers do not break.
- **The drain waits for MCP readiness.** Even after the agent is installed, queued commands (especially slash commands that need tool definitions) are held until MCP enumeration completes.
- **Queue notice is transient.** The "Queued until replacement completes" message is marked `transient: true` so it does not persist in the saved session transcript.
- **`drainReplacementQueueIfReady` can re-arm the pending flag.** If items remain or a new replacement starts mid-drain, the pending flag is re-set and the notice stays visible.