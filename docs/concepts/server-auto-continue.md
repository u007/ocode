---
type: Concept
title: "Server-Side Auto-Continue Loop"
description: 'Server-side auto-continue loop pattern: per-session on/off gate, bounded chain with step-limit cutoff, typesafe/chat judge dispatch over a closed next-action set, the awaiting_confirmation/awaiting_decision split, and visible end-of-turn status.'
tags: [server, auto-continue, agent-loop, architecture, typesafe, scheduled-jobs, session]
timestamp: 2026-10-17T09:14:24Z
---
# Server-Side Auto-Continue Loop

## Overview

The server-side auto-continue loop automatically re-polls the LLM after each turn completes when the agent has not explicitly stopped, enabling unattended multi-step workflows (scheduled jobs, headless API calls, cron dispatch). The on/off gate is scoped to one chat session, not the process: each session keeps its own override, and the process-wide config value is only the default for sessions that never toggled (see "Per-Session Gate"). The loop is bounded: it chains a finite number of continuation steps before forcing a stop, preventing runaway token usage.

## Architecture

### Loop Lifecycle

1. **Initial turn** — The server dispatches the first `agent.Step` from the incoming message.
2. **Judge dispatch** — On turn completion, a lightweight "auto-continue judge" (the configured auto-continue model, e.g. `typesafe/<model>`) inspects the transcript tail and returns a typed **next action** (see "Action Set" below) with a confidence score.
3. **Continue gate** — If the judge picks an end action (`end`, `await_user`), its confidence is below the floor for the action's class (`autoContinueMinConfidenceDefault` for resume actions, `autoContinueAnswerMinConfidence` for answer actions, both in `internal/agent/autocontinue_typesafe.go`), or one of the awaiting-user vetoes applies, the loop terminates and the final result is returned. The floors are **decoupled from** `permissions.auto.min_confidence`, which governs the auto-permission judge. Different actions deserve different confidence bars: a resume is low-stakes and reversible (the chain is bounded), so it uses its own, lower floor; an answer given on the user's behalf uses the permission default.
4. **Chain extension** — If the judge picks a resume or answer action, the loop appends that action's **fixed user-role prompt**, records the action key in the chain history, increments the step counter and starts a new `agent.Step` with the accumulated transcript.
5. **Chain cap** — The loop stops after reaching `AutoContinueChainCap` (a Go constant in `internal/agent/autocontinue_typesafe.go`), regardless of the judge's verdict. The end-of-turn reason is surfaced as a transcript notice (server) or TUI transient hint (see "End-of-Turn Surfacing" below).

### Action Set

The judge does not answer "continue or end". It picks one key from a closed set (`autoContinueActions`), and every key maps to a **fixed resume prompt** — Jev cannot write prose, so the judge chooses between pre-written instructions rather than composing one. Keys and criteria are static; per-turn state (what already fired, whether the todo plan has open items, whether the advisor is on) only filters which keys are *offered* and rides the request state.

| key | class | fires when | once per chain |
|---|---|---|---|
| `continue` | resume | cut off mid-work / truncated | no |
| `next` | resume | current item done, todo plan has open items (offered only then; the gate reads the judged agent's own plan through `tool.ReadTodoSnapshot(a.todoScope())` — project root + session id, the same address the todo tools use — never the TUI's process-global current todo session, so it works per session on the server) | no |
| `retry` | resume | stopped on a transient failure (network, rate limit, flaky test) | yes |
| `fix_errors` | resume | latest output shows a failing build/tests and the reply stopped | no |
| `verify` | resume | claims done, shows no test/build run | yes |
| `review_first` | resume | large/risky diff declared done without self-review | yes |
| `write_plan` | resume | large task, no todo plan, reply is wandering | yes |
| `ask_advisor` | resume | torn between approaches or stuck; offered only when the advisor is enabled and installed | yes |
| `summarize` | resume | work done, no final summary | yes |
| `proceed` | answer | reply asks a yes/no go-ahead on a plan it already described; step is reversible | no |
| `skip` | answer | reply asks whether to do an optional/blocked sub-item | no |
| `reject` | answer | reply asks for a go-ahead on something off-spec or destructive | no |
| `end` | end | reply finished cleanly, or reports a blocking error | — |
| `await_user` | end | a decision only the user can make | — |

**Answer-class actions act on the user's behalf**, so they are gated three ways:

- They are admitted only with the typed reason `awaiting_confirmation`. Any other reason (including `awaiting_decision`) vetoes them: nobody asked.
- They use `autoContinueAnswerMinConfidence` (= the permission default `autoJudgeMinConfidenceDefault`, 0.80), not the 0.6 resume floor. This is the constant, not `resolveAutoJudgeMinConfidence()`: a user who raised the permission floor has said nothing about auto-answers, and a lowered permission floor must not loosen this one.
- `proceed` is vetoed when the reply's own quoted commands (fenced blocks and inline backtick spans, via `replyProposesHarmfulCommand`) are classified harmful by `IsHarmfulBashCommand` — git history rewrites, force pushes, `git stash`, destructive `rm`. `reject` on the same reply is the safe answer and stays allowed.

**Once-per-chain** actions are dropped from the offered set when their key is already in the chain history (`chain_actions_fired` in the state). The history has the same lifetime as the chain counter: the server keeps it in `runTurn` alongside `autoContinueCount`, the TUI in `m.autoContinueActions` (reset by `settleAutoContinueChain` and on new session). Each fired action counts toward `AutoContinueChainCap`.

### Key Parameters

| Parameter | Default | Description |
|---|---|---|
| `auto_continue_enabled` | `false` | Process-wide seed/default for new chat sessions — not a live global switch. Each chat session persists its own override in transcript metadata under the same key (`auto_continue_enabled`), seeded on build via `autoContinueSeed` (session override, else this default). The web sidebar toggles one session through `HandleSetAutoContinue` with `session_id`, which persists to that session's metadata and pushes a session-scoped status snapshot; a request without `session_id` keeps the legacy global-config path. |
| `auto_continue_model` | (empty) | Model used for the next-action judge decision |
| `autoContinueMinConfidenceDefault` | (constant, 0.6) | Confidence floor for resume-class actions — not a config key; decoupled from `permissions.auto.min_confidence` |
| `autoContinueAnswerMinConfidence` | (constant, 0.80) | Confidence floor for answer-class actions (`proceed`/`skip`/`reject`); equals the permission default, deliberately not the configured permission floor |
| `AutoContinueChainCap` | (constant) | Maximum consecutive auto-fired resumes per turn — not a config key; defined in code |

### Per-Session Gate

Auto-continue is per chat session, not per process. The gate a turn actually uses resolves in this order:

1. **Live agent** — `Agent.AutoContinueEnabled()` reads an `atomic.Bool` field owned by that agent (`autoContinueEnabled`), seeded from the session's metadata override (or the process default) at build time and flippable at runtime via `SetAutoContinueEnabled`. It never re-reads `cfg.Ocode.AutoContinueEnabled`, so a later global-config write cannot retroactively change a built session.
2. **Persisted metadata** — with no live agent, the server falls back to the session's transcript metadata key `auto_continue_enabled` (`sessionAutoContinueEnabled`, in `internal/server/autocontinue_session.go`).
3. **Process default** — a session with no override uses the process-wide `cfg.Ocode.AutoContinueEnabled` (`autoContinueFlag`).

Because the override is per session, toggling one chat never affects another: `HandleSetAutoContinue` with `session_id` (`internal/server/handler_slash_parity.go`) writes only that session's metadata and live agent, then pushes a session-scoped `status` snapshot. A request without `session_id` keeps the legacy global-config write (`config.SaveAutoContinueEnabled` + `pushStatusSnapshot`), which only seeds future sessions that never toggled.

### Typesafe Judge Path

When `auto_continue_model` is a decision-backend route (any id where `isDecisionModel` returns true — currently `typesafe/*` and Cloudflare Workers AI clef models), the judge runs via `runAutoContinueJudgeTypesafe` — a single `Decide()` call answering a typed **next action** choice (the `verdict` question, criteria = the offered action set) over the transcript tail. This never generates text; it returns a structured decision with a confidence score. The confidence is thresholded against the floor for the action's class (see "Action Set").

The same call also answers a typed **reason** describing how the last reply ended. The closed set, in the order it is presented to the judge, is:

- `finished` — the reply completed the request; nothing is missing.
- `mid_task` — the reply stopped mid-task and work visibly remains (it says it will continue, or the task is plainly unfinished).
- `truncated` — the reply looks mechanically truncated (an unclosed code block, a cut-off sentence).
- `awaiting_confirmation` — the reply asks for a yes/no go-ahead on a plan or step it has already described in full, the step is reversible and no open design choice remains.
- `awaiting_decision` — the reply is waiting on a decision only the user can make: an open choice between approaches, an ambiguous or missing requirement, a destructive or irreversible action, or any question that is not a plain go-ahead.
- `errored` — the reply reports an error or failure and stopped.

For `finished`, `mid_task`, `truncated` and `errored` the verdict decides and the reason is advisory prose in the outcome detail. The two awaiting reasons are **decisive**:

- `awaiting_decision` vetoes every non-end action at **any** confidence; the turn ends. The veto exists because the judge sometimes reads a question-ending reply (e.g. "Which option do you want?") as unfinished mid-task work; resuming would answer the user's question on their behalf.
- `awaiting_confirmation` admits **only** the answer class (`proceed`/`skip`/`reject`). A resume-class action such as `continue` on a confirmation question is vetoed: it would be an implicit yes the judge never stated. Conversely an answer-class action with any other reason is vetoed because nobody asked.

The rubric sent to the judge (`typesafeAutoContinueInstructions`) carries this split explicitly, tells the judge never to choose `proceed` for a step that deletes data, rewrites git history, force-pushes or deploys, and lists the actions already fired in the chain.

**Since 2026-10-04**, the auto-continue judge is reached through the same `Decider` seam as the other judges. `runAutoContinueJudgeTypesafe` takes a `Decider` parameter instead of a concrete `*TypesafeClient`. The caller resolves it via `resolveDecider(slotAutoContinue)` (`internal/agent/decider.go`), which reads the `auto_continue_model` config key (unchanged; not moved or renamed). The key still accepts any decision-backend id — `isDecisionModel` is the single gate, not a hardcoded `typesafe/` prefix.

**Since 2026-10-14** the verdict is the action set above and `AutoContinueJudgeSync`/`AutoContinueJudgeAsync` take the chain history (`prior []string`) and return an `AutoContinueDecision` (`Resume`, `Action`, `Prompt`, `Detail`); the hosts send `Prompt` verbatim. The step-limit path bypasses the judge and fires `continue` with `AutoContinueStepLimitPrompt`.

### Chat Judge Path

When `auto_continue_model` is anything other than a decision-backend route, the judge runs via `runAutoContinueJudge` (`internal/agent/agent.go`): the transcript tail is sent to the configured chat model as one user message listing the offered action keys with their criteria, and exactly one key is expected back (first word, trailing punctuation stripped). Any error, empty response, or word that is not an offered key fails closed to **end**. A bare `YES`/`NO` is no longer an accepted answer.

The chat judge is **never offered the answer class**: it returns no typed reason, so there is nothing to gate an auto-answer with. Its prompt tells it to answer `await_user` when the reply is waiting on the user — "if it ends by asking a question, or requests clarification, confirmation, feedback, approval, or a decision, answer await_user — the user must respond first. A reply that ends in a question is not a cut-off mid-task reply." Once-per-chain filtering applies the same way, and the prompt names the actions already fired.

### Awaiting-User Stop Coverage

The decisive stop is pinned by mutation-verified tests (each fails against the pre-fix code):

- `internal/agent/autocontinue_typesafe_test.go` — `TestAutoContinueTypesafeAwaitingUserVetoesContinue` (a `continue` verdict with the `awaiting_decision` reason does not resume) and `TestAutoContinueGenericJudgePromptCoversAwaitingUser` (the chat-judge prompt carries the instruction).
- `internal/agent/autocontinue_actions_test.go` — the action-set gating: `proceed` resumes only with `awaiting_confirmation` and at the 0.80 floor, `continue` on a confirmation is vetoed, `awaiting_decision` vetoes every non-end key, `proceed` is vetoed on a harmful quoted command while `reject` passes, `ask_advisor`/`next` are offered only when available, the `next` gate reads the judged session's own plan file (a sibling session's open item or a corrupt file never offers it), once-per-chain keys are filtered, and the chat judge parses keys and is never offered the answer class.
- `internal/server/agent_session_autocontinue_test.go` — `TestRunTurnAutoContinueAwaitingUserVetoesContinue` (full `runTurn` chain emits no resume prompt) and `TestRunTurnAutoContinueConfirmationProceeds` (a `proceed` verdict sends the fixed proceed prompt and records the action).
- `internal/tui/autocontinue_test.go` — `TestAutoContinueJudgeFinishedFiresActionPrompt` (the TUI sends the verdict's own prompt and tracks the chain history).

Note: when `auto_continue_model` is set but `auto_continue_enabled` is `false`, the TUI `/autocontinue model <name>` command and the web `/autocontinue model <name>` command both print a warning reminding the user to run `/autocontinue on` (or flip the sidebar toggle) to actually enable the feature.

### `/goal` Precedence

A session with an evaluable `/goal` (active or paused) gives the natural-stop decision to the goal judge, so the triage judge above is not consulted for that turn. The order in `runTurn` is:

1. The step-limit hard resume, unchanged. It counts against `autoContinueCount`.
2. The goal judge (`GoalJudgeSync`), run only when the tail is not a pending permission or question ask. A `not_met` verdict appends a `[ocode:goal]` user prompt and resumes. A goal resume does **not** advance `autoContinueCount`.
3. The triage judge, reached only when no goal is evaluable.

The goal lives in the session metadata under the `goal` key. It is written after every judged turn and by the `/api/sessions/{id}/goal` endpoints, and restored in `buildAgentSession`. A human message reactivates a paused goal. Engine: `internal/agent/goal.go`. Server host: `internal/server/handler_session_goal.go`.

## End-of-Turn Surfacing

The auto-continue loop does **not** set a `auto_continue` status field on the turn result. Instead, the termination reason is surfaced through two mechanisms:

**Server (headless/web):** An assistant transcript message with empty `Content` and a `Notice` string (via `appendTranscriptMessage` in `internal/server/agent_session.go`). The notice states why the loop resumed or stopped.

**TUI:** A transient transcript hint via `agentAutoContinueDeclineDetail` (`internal/tui/model.go`), which returns a hint for each decline case:
- auto-continue is disabled but the step limit was hit.
- the chain cap is exhausted, so the user must resume manually.

A judge verdict line is rendered inline by the judge's `Detail` string, not as a status field.

## Design Constraints

- **Bounded chain only** — no unbounded loops; `AutoContinueChainCap` is enforced server-side.
- **Judge is advisory, with two decisive reasons** — a low-confidence or error result always stops the loop, and the verdict decides for every triage reason except the awaiting pair: `awaiting_decision` vetoes every non-end action at any confidence, and `awaiting_confirmation` admits only an explicit typed answer (`proceed`/`skip`/`reject`) at the permission-grade floor.
- **Every resume prompt is fixed text keyed by action** — no judge composes prose; a new action is a new row in `autoContinueActions`, never a per-turn string.
- **Naturally-ended turns only** — both judges are consulted only for a turn that ended on its own. A step-limit cutoff resumes deterministically without triage, and a pending permission/question sentinel blocks resume at the dispatcher (`autoContinueShouldResume` in `internal/server/agent_session.go`).
- **Permission-aware** — auto-continue refuses to resume past an unresolved permission or question ask; the agent can raise asks mid-chain, but the loop stops until they are resolved.
- **Per-session gate** — the on/off switch belongs to one chat session: each session's override lives in its transcript metadata, and the process-wide config is only the default for sessions that never toggled.
- **Surfaced in transcript** — auto-continue outcomes are rendered as a transient TUI hint or a server transcript notice, not as an agent-status field.
