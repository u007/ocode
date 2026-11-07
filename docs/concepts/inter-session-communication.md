---
type: Concept
title: Inter-session communication — shared tools and HTTP surface for agents to inspect and drive other sessions
description: 'How a normal (non-Pulse) ocode session inspects and acts on other sessions: the sessionCommTools() subset of the Pulse tool set, the allow/ask permission split, session_send queue semantics, GET /api/sessions/{id}/comm-status, POST /api/sessions/{id}/queue, and the caller-context, remote-host and cross-process limits. Terminal, memory and activation tools stay Pulse-only.'
tags:
  - sessions
  - tools
  - server
  - api
  - permissions
timestamp: 2026-11-07T00:00:00Z
---
# Inter-session communication — shared tools and HTTP surface for agents to inspect and drive other sessions

## Purpose

Any non-Pulse session can see what other sessions are doing and, with the operator's
approval, message them, start or close them, run an allowlisted slash command, and
resolve their pending asks. The capability is the session-communication subset of the
Pulse assistant tool set ([pulse-assistant.md](pulse-assistant.md)), now exposed to
every non-Pulse agent, plus two general HTTP endpoints and a queue path. It lets one
agent hand work to another, check whether a session is blocked on an ask, or resume a
session that stopped.

## Exposure: `sessionCommTools()`

`internal/server/pulse_tools.go` defines `sessionCommTools()` as `h.pulseTools()` minus
every `terminal_*`, `memory_*` and `activation_*` tool. `buildAgentSession`
(`internal/server/agent_session.go`) appends that slice to the builtin tool list for
every non-Pulse session and, for each tool with `ask == false`, installs an
`agent.PermissionAllow` rule. Ask tools get no rule, so each call raises the normal
permission dialog for that session.

Pulse itself is unchanged: `configurePulseAgent` + `RestrictToTools` installs the full
set. Terminal, memory and activation tools are deliberately Pulse-only: they expose
terminal contents, the assistant's private notes, or the activation gate, none of which
belong to a general coding session.

The exact shared set:

`pulse_board`, `session_read`, `session_recap`, `session_ask`, `project_list`,
`agent_runs`, `session_send`, `session_new`, `session_close`, `session_command`,
`permission_resolve`, `question_answer`, `question_dismiss`, `host_connect`.

## Tools

Full argument and return shapes are in [pulse-assistant.md](pulse-assistant.md)
("Tools"); this table records the communication contract.

| Tool | Kind | What it does |
|---|---|---|
| `pulse_board` | read | Lists dashboard rows (status, project, host, title, current task or ask, todo progress) for this server and connected remote hosts. |
| `project_list` | read | Lists every local and remote project, including whether a remote host is connected. |
| `session_read` | read | Reads another session transcript, optionally `search`-filtered. Also returns `goal` and `todo` when the target has them. |
| `session_recap` | read (model call) | Produces the target session recap under a 90 s deadline. |
| `session_ask` | read | Returns the pending permission/question asks a session is paused on, so a caller can resolve the right request id. |
| `agent_runs` | read | Lists live sub-agent runs, optionally restricted to one session. |
| `host_connect` | allow rule | Starts a bounded connect to a saved remote host; never reconnects a connected host. |
| `session_send` | ask | Sends a message, starting a turn; `queue: true` handles a busy target. |
| `session_new` | ask | Starts a new local session with an initial message and returns its id. |
| `session_close` | ask | Tears down the resident agent for an idle session. |
| `session_command` | ask | Runs one allowlisted slash command (`/btw`, `/cancel`, `/compact`, `/recap`, `/title`). |
| `permission_resolve` | ask | Answers a pending permission ask with allow/deny. |
| `question_answer` | ask | Answers a pending question ask. |
| `question_dismiss` | ask | Cancels a pending question without answering it. |

## Queue semantics

`session_send` with `queue: true` is the safe way to hand work to a target that is
mid-turn, compacting or paused on an ask. The tool:

1. persists the message as a normal user message (`persistUserMessage`),
2. appends it to the target pending queue (`SessionManager.PushPending`), and
3. starts `scheduleQueueDrain`, which waits up to ~60 s (300 × 200 ms) for the target to
   go idle.

The drain gives up if the target is still active, and it only dispatches when a resident
agent exists; otherwise the persisted message stays durable and is picked up by the next
real turn. The dispatch is `dispatchTurn(..., turnOptions{retryLast: true})`, so the
queued message runs in a resumed turn rather than being appended to a fresh one.

The queue is capped at 10 messages. `session_send` returns an error past the cap;
`POST /api/sessions/{id}/queue` returns `429`. On an idle target `queue: true` is ignored
and the send starts immediately.

## HTTP surface

Both routes sit behind `authMiddleware` and are registered in `Server.registerRoutes`;
handlers are in `internal/server/handler_session_comm.go`.

| Route | Response |
|---|---|
| `GET /api/sessions/{id}/comm-status` | `{session_id, project_path, title, turn_active, goal?, todo?, last_message, message_count}`. `404` for an unknown session. Uses the live transcript when `as.mu.TryLock()` succeeds, else the persisted copy; `last_message` is the last non-empty assistant message; `goal`/`todo` are omitted when unset. |
| `POST /api/sessions/{id}/queue` | Body `{"content": "..."}`. `202 {session_id, queued: true}` after persisting and queueing; `400` when content is empty; `404` for an unknown session; `429` when the queue already holds 10 messages. |

`comm-status` is the cheap poll a supervising agent or external tool uses before sending:
it answers whether the target is running, what it is waiting on, and what it last said,
without loading the whole transcript.

## Safety and limits

- **Self-target is not guarded.** The tools have no caller context, so a session can
  target its own id; `TestSessionSendSelfTargetNotGuarded` records this rather than
  pretending it is refused. `session_read` does refuse `pulse_` ids (the assistant
  transcript) and write tools refuse `pulse_` ids, child (`_child_`) ids and unknown
  sessions.
- **Writes ask.** Every tool marked ask in the table above keeps the normal permission
  dialog; the allow-rule split is the only thing that makes read tools quiet. There is no
  separate comm permission mode.
- **Remote hosts are read-only here.** `session_read`, `session_recap` and `session_ask`
  accept a board row `host`; `session_send`, `session_command`, `permission_resolve`,
  `question_answer`, `question_dismiss` and `session_close` refuse a remote session with
  an error naming the host.
- **Cross-process blindness.** A session held by another `ocode` process is invisible
  until it persists, the same limitation as the Pulse assistant.
- **`session_read` prefers live state.** It takes `as.mu.TryLock()` and falls back to
  disk rather than blocking behind a running turn.

## Relation to the Pulse assistant

The Pulse assistant keeps the full tool set and its board/memory/activation features.
This feature is only the shared subset. A normal session gets no `[ocode:pulse]` board,
no memory injection, no activation condition, and no terminal tools. See
[pulse-assistant.md](pulse-assistant.md) for the full set and its permission design.

## References

- `internal/server/pulse_tools.go` (`sessionCommTools`, `pulseTools`)
- `internal/server/pulse_tools_write.go` (`session_send`, `scheduleQueueDrain`)
- `internal/server/agent_session.go` (`buildAgentSession` non-Pulse wiring)
- `internal/server/handler_session_comm.go` (`comm-status`, queue)
- Tests: `internal/server/session_comm_test.go`, `pulse_tools_test.go`, `pulse_tools_write_test.go`
