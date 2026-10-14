---
type: Concept
title: "Goal Condition Loop (/goal)"
description: 'The /goal session condition: a judge checks after every turn whether the condition is met, impossible, or should pause the goal. Covers command syntax, the per-turn evaluation order, the verdict table, pause and resume rules, the sticky 50-turn cap, the persisted goal key, the session goal endpoints, and how it differs from /orchestrate.'
tags: [goal, judge, agent-loop, server, tui, web, typesafe, auto-continue]
timestamp: 2026-10-14T00:00:00Z
---
# Goal Condition Loop (`/goal`)

## Overview

`/goal <condition>` keeps a session's agent taking turns until a separate judge decides the
condition is met, impossible, or should stop being pursued. The agent does not grade its own
work: after each natural stop, the goal judge (a typed decision call, not the chat model) reads
the condition and the conversation tail and returns a verdict. A `not_met` verdict sends a
user-role resume turn; anything else ends or pauses the loop.

The same engine runs in the TUI, for `/rc` web turns, and for web/desktop and headless server
sessions. The engine is `internal/agent/goal.go`.

## Command syntax

| Form | Effect | TUI | Web |
|---|---|---|---|
| `/goal` | Shows status (`GoalStatusText`): condition, status, turns, last reason, judge. | Instant | Queued like any command while a turn runs |
| `/goal <condition>` | Replaces any previous goal, starts it active, and starts the kickoff turn. | Queued while streaming | Queued while busy |
| `/goal clear` (also `stop`, `off`, `reset`, `none`, `cancel`) | Removes the goal. Recognised only as the lone argument. | Instant | Queued while busy |

- `/goal stop the flaky test` is a condition, not a clear: a clear verb counts only when it is the
  whole argument (`isGoalClearVerb`, `internal/tui/commands.go:1904`).
- A condition must be non-empty and at most `GoalMaxConditionLen` (4000) characters
  (`internal/agent/goal.go:29`, `SetGoal` at `:158`).
- The kickoff turn is `GoalKickoffPrompt` (`internal/agent/goal.go:226`): the condition plus "verify
  with tools before claiming it is met".

## Evaluation order

Each natural stop (a turn that ended without a pending ask) is handled in this order.

**Server** (`internal/server/agent_session.go`, `runTurn`):

1. Step-limit hard resume (`/max-step` cut-off). It is decided in `autoContinueShouldResume`
   (`:1392`) and counts against the auto-continue chain.
2. The triage judge stands aside while a goal is evaluable (`:1401`).
3. The goal judge runs (`:1295`), but only when the tail is not a pending permission or question
   ask. A `not_met` verdict appends the `[ocode:goal]` prompt and continues the loop. A goal resume
   does not advance `autoContinueCount`.

The server path and its precedence are also described in
`docs/concepts/server-auto-continue.md` (`/goal` Precedence).

**TUI** (`internal/tui/model.go`, `streamDoneMsg`): the step-limit hard resume fires first. Then the
goal judge is dispatched for TUI turns (`:5612`) and for `/rc` turns (`rcGoalJudge`, `:5372`, `:5449`).
The auto-continue judge is dispatched only when no goal is evaluable and the turn did not hit the
step limit (`:5614`). Judge calls run asynchronously (`maybeDispatchGoalJudge`, `:274`); the result
arrives as `goalJudgeFinishedMsg` (`:5845`).

## The judge (`GoalJudgeSync`)

`GoalJudgeSync` (`internal/agent/goal.go:304`) runs these steps in order. Each returns when it decides.

1. No goal, or the goal is already `met` or `impossible`: nothing is evaluated.
2. Turn accounting: `Turns` increments. `NoToolTurns` resets when this turn's assistant messages made
   a tool call, otherwise it increments (`goalTurnUsedTools`, `:101`).
3. Stall guard: `NoToolTurns >= goalStallTurns` (3) pauses the goal with `stalled`. No judge call.
4. Resolve the judge (`resolveDecider(slotGoal)`, `:349`). Unresolved pauses with `unavailable`.
5. Judge call (`:361`) with the condition, turn count, the last 12 messages (`goalTailN`), and
   `turn_ended`. A call error, a nil response, or a missing or invalid verdict pauses with `unavailable`.
6. Confidence floor (`goalMinConfidenceDefault` = 0.6, `:37`, applied at `:390`): a `met` or
   `impossible` verdict below the floor is treated as `not_met`.
7. Apply the verdict (`:394`): `met` and `impossible` are terminal, at the cap too. `not_met` with
   the reason `awaiting_user` pauses. A `not_met` on a turn with `Turns >= GoalMaxTurns` (50) pauses
   as `capped`. Any other `not_met` stays active and resumes.

Write-back (`:317`): the verdict is stored only if `goalGen` is unchanged since the snapshot. If the
goal was cleared, replaced, or changed while the judge ran, the result is `stale` and nothing is
written.

## Verdict table

| Verdict | Status after | Resume | When |
|---|---|---|---|
| `met` | `met` (terminal) | no | Judge says met at or above the floor |
| `impossible` | `impossible` (terminal) | no | Judge says impossible at or above the floor. The condition contradicts itself or needs something the assistant cannot reach; difficulty is not impossibility |
| `not_met` | `active` | yes | Work remains, or a completion claim is unverified |
| `not_met` | `paused` | no | Reason `awaiting_user`: the reply asks the user for a decision, confirmation or input |
| `stalled` | `paused` | no | Three consecutive judged turns without a tool call. No judge call |
| `capped` | `paused` | no | `not_met` on a turn where `Turns` reached 50. The judge still runs, so `met` and `impossible` on that turn stay terminal |
| `unavailable` | `paused` | no | Judge model unconfigured or without a key, judge error, nil or invalid response |
| `stale` | unchanged | no | Goal changed while judging. Verdict dropped, nothing written |

A `met` or `impossible` verdict below the floor is shown as `not_met` in the detail text. Its resume
text says the evaluator could not confirm it, not the judge's reason.

The judge's reason is advisory. The closed reason set is `all_satisfied`, `work_remains`,
`unverified_claim`, `blocked_by_error`, `awaiting_user`, `cannot_be_done` (`goalReasons`,
`internal/agent/goal.go:81`). Only `awaiting_user` changes the outcome, and only for `not_met`.

## Pause, resume, and clear

- **A paused goal stays set.** Pausing never clears the condition. The engine never clears a goal;
  only `/goal clear`, `/new` or `/clear`, switching to another session in the TUI, or a fresh
  session removes it (`ClearGoal`, `internal/agent/goal.go:174`).
- **A human turn reactivates a paused goal** (`GoalMarkHumanTurn`, `:214`). It sets the status to
  `active` and resets `NoToolTurns`. Terminal (`met`, `impossible`) goals stay terminal. Active goals
  are left alone. The server calls it on sync and async human entries (`agent_session.go:1801`,
  `:2039`). The TUI calls `settleGoalTurn` (`model.go:196`).
- **The 50-turn cap is sticky.** A human turn does not reset `Turns`. After a `capped` pause, the
  goal reactivates on the next human message and the judge runs again. A `not_met` caps again, but a
  `met` verdict closes the goal, so a finished goal is never stranded by the cap. Only a new
  `/goal <text>` starts fresh state (`SetGoal`, `:158`).
- **A pending ask never auto-resumes.** The server skips the goal judge while the tail is an
  unresolved permission or question ask (`agent_session.go:1295`). The TUI runs its post-turn
  decisions only when `queueDrainBlocked()` is false (`model.go:9355`).
- **A deferred resume waits for an idle session.** In the TUI, a `Resume` verdict that cannot fire
  right away (the session is streaming, compacting, or has a queue drain blocked) sets
  `goalResumePending`. `compactFinishedMsg` fires it once the session is idle (`goalResumeReady`,
  `model.go:300`, `:5773`). Any new turn clears the flag, because that turn's own judge decides the
  goal (`askAgent`, `model.go:16002`).
- **The resume message is user-role.** `GoalResumeMessage` (`goal.go:236`) produces a `[ocode:goal]`
  user message: the condition, the evaluator's last reason, and a continue instruction. It is never
  system-role, so the cached system prefix stays stable.

## Persistence

- Session metadata key `goal` (`GoalMetadataKey`, `goal.go:41`) holds `condition`, `status`, `turns`,
  `no_tool_turns`, `last_reason`, and `set_at` (RFC 3339). `GoalStateToMetadata` writes it;
  `GoalStateFromMetadata` (`goal.go:433`) reads it. A malformed record is ignored.
- Written after every judged turn (`goalShouldResume`, `agent_session.go:1469`), by the server
  endpoints (`persistSessionGoal`, `internal/server/handler_session_goal.go:55`), and on TUI save
  (`sessionSidebarMetadata`, `model.go:21217`).
- Restored when a server agent is built (`agent_session.go:636`) and when a TUI session loads
  (`restoreSessionMetadata`, `model.go:21254`). The key is listed in `sessionOwnedMetadataKeys`
  (`model.go:21189`), so the TUI's carry-forward copy never overwrites it.

## Endpoints

All three are behind `authMiddleware` (`internal/server/server.go:289-291`).

| Method and path | Request | Response |
|---|---|---|
| `GET /api/sessions/{id}/goal` | none | `{active, condition, status, turns, last_reason, status_text}`. Never builds an agent: a live agent answers, otherwise the persisted record. Zero fields when there is no goal |
| `PUT /api/sessions/{id}/goal` | `{"condition": "..."}` | Same shape plus `kickoff_prompt`. 400 on an empty or overlong condition |
| `DELETE /api/sessions/{id}/goal` | none | 204 |

`active` is true for `active` and `paused` goals, the same rule as `GoalEvaluable`
(`handler_session_goal.go:41`). Terminal goals report `active: false` and keep their status.

For the session an `/rc` bridge serves, the TUI owns the agent and its persistence. The endpoints act on
the bridged agent (`rc.Agent()`, `bridgedGoalAgent`) and write no server metadata. A PUT or DELETE
reaches the TUI's agent at once, and the TUI's next save persists it. A bridged session with no TUI
agent answers `409`.

The web `/goal` command (`web/src/components/Chat/commands.ts`, `handleGoal`) calls these endpoints
and sends `kickoff_prompt` as the turn.

## Judge configuration

- Config key `ocode.goal.judge_model`, default `typesafe/jev-latest` (`defaultJudgeModel`,
  `internal/agent/decider.go:30`; slot read at `:94-95`).
- The goal judge has its own confidence floor, `goalMinConfidenceDefault`. It does not read
  `permissions.auto.min_confidence` or the auto-continue floor.
- There is no fallback to another backend. An unset or keyless judge pauses the goal as
  `unavailable`, so the failure stays visible.

## Difference from /orchestrate

`/orchestrate <goal>` is a one-shot alias for `/orchestrator <goal>`. In the TUI it switches the
primary agent to the orchestrator and sends the goal once; the rewrite happens in `handleCommand`
(`internal/tui/model.go`, `orchestrateAlias`). The web sends a prompt asking the active agent to run
the orchestrator pipeline (`handleOrchestrate`, `web/src/components/Chat/commands.ts`), and
`ocode orchestrate` runs the pipeline headlessly (`internal/cli/orchestrate.go`).

`/goal` does not switch agents. The current agent keeps taking turns, and the judge decides when to
stop. Nothing in `/goal` runs the orchestrator pipeline.

## Known gaps

Tracked in `TODO.md`:

- `ocode run "/goal ..."` is not parsed headlessly. `runAttach` posts raw content to `/api/chat`,
  and the server does not parse slash commands.
- Evaluation does not defer while subagents or background shells run.
- There is no check-in cadence while a goal is active.

## Code map

- Engine: `internal/agent/goal.go`, `internal/agent/decider.go` (`slotGoal`).
- Server: `internal/server/handler_session_goal.go`, `internal/server/agent_session.go`,
  `internal/server/server.go`.
- TUI: `internal/tui/commands.go` (`runGoalCmd`), `internal/tui/model.go` (judge dispatch, deferred
  resume, persistence).
- Web: `web/src/components/Chat/commands.ts` (`handleGoal`).
- Related: `docs/concepts/server-auto-continue.md`, `docs/concepts/tui-slash-command-queuing.md`.
