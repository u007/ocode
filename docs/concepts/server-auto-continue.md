---
type: Concept
title: "Server-Side Auto-Continue Loop"
description: 'Server-side auto-continue loop pattern: bounded chain with step-limit cutoff, typesafe/chat judge dispatch, the decisive awaiting_user stop, and visible end-of-turn status.'
tags: [server, auto-continue, agent-loop, architecture, typesafe, scheduled-jobs]
timestamp: 2026-10-04T12:33:29Z
---
# Server-Side Auto-Continue Loop

## Overview

The server-side auto-continue loop automatically re-polls the LLM after each turn completes when the agent has not explicitly stopped, enabling unattended multi-step workflows (scheduled jobs, headless API calls, cron dispatch). The loop is bounded: it chains a finite number of continuation steps before forcing a stop, preventing runaway token usage.

## Architecture

### Loop Lifecycle

1. **Initial turn** — The server dispatches the first `agent.Step` from the incoming message.
2. **Judge dispatch** — On turn completion, a lightweight "auto-continue judge" (the configured auto-continue model, e.g. `typesafe/<model>`) inspects the transcript tail and returns a typed **continue / end** decision with a confidence score.
3. **Continue gate** — If the judge says **end**, its confidence is below the auto-continue confidence floor (`autoContinueMinConfidenceDefault` in `internal/agent/autocontinue_typesafe.go`), or the typesafe reason is `awaiting_user`, the loop terminates and the final result is returned. The floor is **decoupled from** `permissions.auto.min_confidence`, which governs the auto-permission judge. Different actions deserve different confidence bars: auto-continue is low-stakes and reversible (the chain is bounded), so it uses its own, lower floor.
4. **Chain extension** — If the judge says **continue**, the loop increments the step counter and starts a new `agent.Step` with the accumulated transcript.
5. **Chain cap** — The loop stops after reaching `AutoContinueChainCap` (a Go constant in `internal/agent/autocontinue_typesafe.go`), regardless of the judge's verdict. The end-of-turn reason is surfaced as a transcript notice (server) or TUI transient hint (see "End-of-Turn Surfacing" below).

### Key Parameters

| Parameter | Default | Description |
|---|---|---|
| `auto_continue_enabled` | `false` | Master toggle for the auto-continue feature |
| `auto_continue_model` | (empty) | Model used for the continue/end judge decision |
| `autoContinueMinConfidenceDefault` | (constant) | Confidence floor for auto-continue triage only — not a config key; decoupled from `permissions.auto.min_confidence` |
| `AutoContinueChainCap` | (constant) | Maximum consecutive auto-fired resumes per turn — not a config key; defined in code |

### Typesafe Judge Path

When `auto_continue_model` is a decision-backend route (any id where `isDecisionModel` returns true — currently `typesafe/*` and Cloudflare Workers AI clef models), the judge runs via `runAutoContinueJudgeTypesafe` — a single `Decide()` call answering a typed **continue/end** choice over the transcript tail. This never generates text; it returns a structured decision with a confidence score. The confidence is thresholded against `resolveAutoContinueMinConfidence()`, which is intentionally lower than the auto-permission floor (`resolveAutoJudgeMinConfidence()`) because a false-positive continue is cheap — the chain is capped.

The same call also answers a typed **reason** describing how the last reply ended. The closed set, in the order it is presented to the judge, is:

- `finished` — the reply completed the request; nothing is missing.
- `mid_task` — the reply stopped mid-task and work visibly remains (it says it will continue, or the task is plainly unfinished).
- `truncated` — the reply looks mechanically truncated (an unclosed code block, a cut-off sentence).
- `awaiting_user` — the reply is waiting on the user: it ends by asking a question or requesting feedback, confirmation, approval, or a decision.
- `errored` — the reply reports an error or failure and stopped.

For every reason **except `awaiting_user`** the verdict decides and the reason is advisory prose in the outcome detail. `awaiting_user` is **decisive**: when the judge categorizes the reply as waiting on the user, a `continue` verdict is vetoed at **any** confidence and the turn ends (reason `typesafeAutoContinueAwaitingUser` in `runAutoContinueJudgeTypesafe`). The veto exists because the judge sometimes reads a question-ending reply (e.g. "Which option do you want?") as unfinished mid-task work; resuming would answer the user's question on their behalf. The rubric sent to the judge carries this explicitly — the `end` criterion reads "the last reply completed the request, is waiting on the user (it asks a question or requests feedback/confirmation/input), or ends on a blocking error", and the instructions state that a reply ending in a question "is not a cut-off mid-task reply" and that the next move belongs to the user. The `awaiting_user` reason label is exactly that criterion.

**Since 2026-10-04**, the auto-continue judge is reached through the same `Decider` seam as the other judges. `runAutoContinueJudgeTypesafe` (`internal/agent/autocontinue_typesafe.go:104`) takes a `Decider` parameter instead of a concrete `*TypesafeClient`. The caller resolves it via `resolveDecider(slotAutoContinue)` (`internal/agent/decider.go:112`), which reads the `auto_continue_model` config key (unchanged; not moved or renamed). The key still accepts any decision-backend id — `isDecisionModel` is the single gate, not a hardcoded `typesafe/` prefix.

### Chat Judge Path

When `auto_continue_model` is anything other than a decision-backend route, the judge runs via `runAutoContinueJudge` (`internal/agent/agent.go`): the transcript tail is sent to the configured chat model as one user message and a bare `YES`/`NO` is expected. Any error, empty response, or answer that does not unambiguously start with `YES` fails closed to **stop**.

The prompt also covers the awaiting-user stop, independently of the configured model: it instructs the judge to answer **NO** when the reply is waiting on the user — "if it ends by asking a question, or requests clarification, confirmation, feedback, approval, or a decision, answer NO — the user must respond first. A reply that ends in a question is not a cut-off mid-task reply."

### Awaiting-User Stop Coverage

The decisive stop is pinned by mutation-verified tests (each fails against the pre-fix code):

- `internal/agent/autocontinue_typesafe_test.go` — `TestAutoContinueTypesafeAwaitingUserVetoesContinue` (a `continue` verdict with the `awaiting_user` reason does not resume) and `TestAutoContinueGenericJudgePromptCoversAwaitingUser` (the chat-judge prompt carries the instruction).
- `internal/server/agent_session_autocontinue_test.go` — `TestRunTurnAutoContinueAwaitingUserVetoesContinue` (full `runTurn` chain emits no resume prompt).

Note: when `auto_continue_model` is set but `auto_continue_enabled` is `false`, the TUI `/autocontinue model <name>` command and the web `/autocontinue model <name>` command both print a warning reminding the user to run `/autocontinue on` (or flip the sidebar toggle) to actually enable the feature.

## End-of-Turn Surfacing

The auto-continue loop does **not** set a `auto_continue` status field on the turn result. Instead, the termination reason is surfaced through two mechanisms:

**Server (headless/web):** An assistant transcript message with empty `Content` and a `Notice` string (via `appendTranscriptMessage` in `internal/server/agent_session.go`). The notice states why the loop resumed or stopped.

**TUI:** A transient transcript hint via `agentAutoContinueDeclineDetail` (`internal/tui/model.go`), which returns a hint for each decline case:
- auto-continue is disabled but the step limit was hit.
- the chain cap is exhausted, so the user must resume manually.

A judge verdict line is rendered inline by the judge's `Detail` string, not as a status field.

## Design Constraints

- **Bounded chain only** — no unbounded loops; `AutoContinueChainCap` is enforced server-side.
- **Judge is advisory, with one decisive exception** — a low-confidence or error result always stops the loop, and the verdict decides for every triage reason except `awaiting_user`, where the typesafe judge's typed reason vetoes a `continue` at any confidence because the reply has handed the next move to the user.
- **Naturally-ended turns only** — both judges are consulted only for a turn that ended on its own. A step-limit cutoff resumes deterministically without triage, and a pending permission/question sentinel blocks resume at the dispatcher (`autoContinueShouldResume` in `internal/server/agent_session.go`).
- **Permission-aware** — auto-continue refuses to resume past an unresolved permission or question ask; the agent can raise asks mid-chain, but the loop stops until they are resolved.
- **Surfaced in transcript** — auto-continue outcomes are rendered as a transient TUI hint or a server transcript notice, not as an agent-status field.