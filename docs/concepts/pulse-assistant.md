---
type: Concept
title: Pulse assistant — overview chat that reads and, with approval, acts on sessions (server side)
description: 'Server side of the Pulse assistant: one global chat session (pulse_ id, dedicated root, state.json) that reads every local session and terminal and, through permission-gated write tools, sends messages, runs allowlisted slash commands and resolves asks. Endpoints (assistant, pulse-model, pulse-system-prompt), config keys, model slot, system-prompt override and per-turn board plus memory injection (user-role, never system-role), the twelve tools with caps, the permission design, assistant-owned memory files, recap deadline, exclusion from the board, and known limits.'
tags:
  - pulse
  - assistant
  - sessions
  - tools
  - server
  - api
  - prompt-cache
timestamp: 2026-10-08T00:00:00Z
---
# Pulse assistant — overview chat (server side)

## Purpose

The assistant answers "what is going on?" on the Pulse dashboard
([pulse-dashboard.md](pulse-dashboard.md)): which sessions need attention, what a
session did, what a terminal printed. It is ONE global chat that can read every
local session and terminal of this server process. Its read tools run freely.
Its write tools (message a session, run a slash command, answer an ask) go
through the normal permission dialog, see "Permission design".

## Identity and root

- It is an ordinary server chat session, so persistence, SSE streaming,
  `GET /api/sessions/{id}`, `POST /api/sessions/{id}/message` and the per-session
  model override (`PUT/DELETE /api/sessions/{id}/model`) work unchanged.
- Its id starts with `pulse_` (`pulseSessionPrefix`, `isPulseSession`), followed by
  the same timestamp and random suffix other session ids use.
- Its project root is `<GlobalDataDir>/pulse` (`pulseAssistantRoot`, created 0o755
  on demand). The root is **not a project**: no CLAUDE.md, discovery or dir docs
  load for it, and it is not in `allowedProjectRoots` (that list is a security
  boundary). `sessionSearchRoots` adds it to the SessionManager's search space only,
  so a message sent after a restart still finds the transcript.
- Exactly one id is persisted, in `<root>/state.json` as `{"session_id": "..."}`,
  written atomically (`secretfile.WriteFileAtomic`) under `Handler.pulseMu`. It is
  never stored in `ocodeconfig.json`. A state file naming a non-`pulse_` id is
  rejected with a 500, not repaired.
- Minting persists an EMPTY transcript (title "Pulse assistant", pulse root) before
  the id is returned or the agent registered, so `GET /api/sessions/{id}` and the
  advisor pin find it. If `state.json` names an id whose transcript is gone, the
  next `GET /api/pulse/assistant` logs it and mints a new id.

## API

| Route | Response |
|---|---|
| `GET /api/pulse/assistant` | `200 {"session_id": "pulse_...", "model": "<effective model id>"}`. First call mints and persists the id; every call registers the session entry but does NOT build the agent: the first message builds it through the normal path (a pre-build under the endpoint's profile resolution differed from the message path's and forced a rebuild plus an illegal bootstrap transition). `400 no model configured` when neither slot nor chat model is set. |
| `GET /api/config/pulse-model` | `200 {"model": "..."}`, empty string when unset. |
| `PUT /api/config/pulse-model` | Body `{"model": "..."}`; empty string clears. `400` when the `model` key is absent. Returns `{"model": "..."}`. |
| `GET /api/config/pulse-system-prompt` | `200 {"prompt": "<configured or empty>", "default": "<built-in prompt with the tool list rendered>"}`. |
| `PUT /api/config/pulse-system-prompt` | Body `{"prompt": "..."}`; empty string clears; `400` when the key is absent. Persisted by `config.SavePulseSystemPrompt` and re-applied to the live assistant (`Agent.SetSystemPromptOverride` is atomic), so it takes effect on its next model call, no rebuild. |

All three sit behind `authMiddleware`, like `GET /api/pulse`. Handlers:
`HandlePulseAssistant`, `HandleGetPulseModel`, `HandleSetPulseModel`
(`internal/server/pulse_assistant.go`).

## Config keys

- `ocode.pulse_model` (string): model slot. Written only by
  `config.SavePulseModel`, a targeted load-modify-write (never a snapshot save).
- `ocode.pulse_system_prompt` (string): when non-empty, used **verbatim** as the
  assistant's system prompt (no tool list is appended). Edited through
  `/api/config/pulse-system-prompt`.

## Model slot resolution

`effectiveSessionModel` for a `pulse_` session: per-session override (transcript
metadata) > `ocode.pulse_model` > the server's default chat model. Changing the
slot needs no restart: every message calls `reconcileProfileAgent`, which compares
the desired model with the live agent's and rebuilds on the next turn. A running
turn finishes on the old model.

## Prompt override and board snapshot

Built in `buildAgentSession` when `isPulseSession(id)`, via
`configurePulseAgent`:

- **Tools**: no builtins, no plugin/MCP tools. `Agent.RestrictToTools` replaces the
  whole tool map (NewAgent registers bash/task/advisor itself) and turns discovery
  off. The read tools and `memory_write` get explicit `allow` permission rules; the
  four write tools get none (see "Permission design").
- **System prompt**: `Agent.SetSystemPromptOverride` makes `BasePromptMessages`
  return one stable system message, replacing the env block, mode, recap and
  project context. Default text is `defaultPulseSystemPrompt` with the tool list
  filled from the **registered** tools (`pulseSystemPrompt`), so the prompt can
  never advertise a tool that is not installed.
- **Board**: `Agent.SetPulseSnapshot(h.pulseBoardSnapshot)`. Each Step appends one
  block wrapped `[ocode:pulse]` ... `[/ocode:pulse]`, one line per live row:
  `status | project basename | session_id | title (<=80 runes) | ask or current task (<=120 runes)`.
  Rows come from `buildPulseRows(gatherPulseInputs(live))` in board order, capped at
  60 (`pulseBoardMaxRows`), with a header stating shown-of-total.
- **Memory section**: the same block carries a second section wrapped
  `[ocode:pulse-memory]`: global memory plus the memory of every project with a row
  on the board (deduped, sorted), each clipped to 8 KB (`pulseMemoryPromptCap`) with
  a `(truncated, use memory_read)` marker. Omitted when nothing is saved. A memory
  read error is logged and shown in the section, never silently dropped.

**Why user-role.** Every system-role message is hoisted into the cached `system`
block by the provider builders (`collectAndRemoveSystemMessages`). The board
changes every turn, so a system-role copy would bust the whole prompt cache. It is
a tail block like `injectTodoTail`/`injectNotesTail`, never persisted. Memory
changes between turns too (`memory_write`, a project going live), so it rides the
same user-role block. The system prompt itself is constant per session, so it
caches.

## Tools

Read tools: `internal/server/pulse_tools.go`; write tools: `pulse_tools_write.go`;
memory tools: `pulse_memory.go`. Built by `h.pulseTools()`, all `Parallel()`. Results are JSON strings; lists are sorted; each result is capped at
`pulseToolResultCap` (24 KB) by `pulseCapped`, which keeps the most useful items
and appends `"truncated": true` as the trailing field. Errors are returned as
errors, never as an empty result. Arguments decode strictly: an unknown field is an
error.

| Tool | Args | Returns | Limits |
|---|---|---|---|
| `pulse_board` | `scope?` live (default) / all | `{items: [PulseRow]}` as `GET /api/pulse` | cap drops tail rows |
| `session_read` | `session_id`, `last?` 1-200 (30), `search?` | `{session_id, project_path, title, messages: [{index, role, content, tool_calls?: [{name, args}]}]}` | content 2000 runes, args 200 runes, rune-safe; cap drops OLDEST messages; `search` is case-insensitive over content, `index` is absolute; child (`_child_`) sessions readable; the assistant's own id is refused |
| `session_recap` | `session_id` | `{session_id, recap}` via `Handler.recapSessionCtx` | runs a model call under a 90 s deadline (`pulseRecapTimeout`); a timeout is an error naming it, never "Recap timed out." text; own id refused |
| `terminal_tabs` | none | `{projects: [{project, terminals}]}` from `internal/termtabs` | sorted by project then terminal id |
| `terminal_read` | `terminal_id`, `lines?` 1-1000 (100) | `{terminal_id, project, lines}` | ANSI stripped, `\r` redraws collapsed, 2000 runes per line; reads only the last 256 KB of the history log; same access gate and project boundary as `GET /api/terminal/{id}/history`; local projects only; cap drops OLDEST lines |
| `memory_read` | `scope` global/project, `project_path?` | `{scope, path, content}`; empty content = nothing saved | project scope needs a path in `allowedProjectRoots()` |
| `memory_write` | `scope`, `project_path?`, `content` | `{scope, path, bytes}`; full replace, atomic | 32 KB (`pulseMemoryCap`), over-cap rejected |
| `session_send` | `session_id`, `content` | `{session_id, accepted: true}`; starts an async turn via `HandleSendMessage` | **asks**; refuses own id, child ids, unknown sessions, and a target that is mid-turn, compacting or paused on an ask (never queued). The pre-check is re-checked at dispatch: a turn that starts after it refuses the send (`turnOptions.refuseIfBusy`, under `cancelMu`), and a synchronous turn can still slip past that check |
| `session_command` | `session_id`, `command`, `args?` | `{session_id, command, result}` where `result` is the operation's own JSON | **asks**; fixed allowlist `/btw` `/cancel` `/compact` `/recap` `/title`, anything else is an error listing them, no send-as-text fallback |
| `permission_resolve` | `session_id`, `request_id`, `decision` allow/deny | `{session_id, request_id, decision, accepted: true}` via `HandleResolvePermission` | **asks**; unknown request id is an error; always_* decisions are not offered |
| `question_answer` | `session_id`, `request_id`, `answers` | `{session_id, request_id, accepted: true}` via `HandleAnswerQuestion` (same payload as `POST /api/questions`) | **asks**; unknown request id is an error |
| `agent_runs` | `session_id?` | `{sessions: [{session_id, runs: [...nested children]}]}` | transcripts omitted, results 500 runes; sessions without runs omitted; the assistant is excluded |

## Permission design

The read tools and `memory_write` get explicit `allow` rules in
`configurePulseAgent`. The four tools marked **asks** (`pulseTool.ask`) get NO
rule, so the permission manager's default applies: every call becomes a normal
permission ask that the drawer renders as a dialog, and the operator can "always
allow" from it like any tool. `memory_write` is exempt because it only replaces the
assistant's own size-capped notes under its private root. The system prompt tells
the model to use write tools only when the operator's CURRENT message explicitly
asks for that action on that session.

The write tools call the existing HTTP handlers in-process (`pulseCall`, a
response recorder with no network), so the real rules (RC-bridge routing, profile
reconcile, ask matching, always-allow guards) stay in one place. A handler status
of 400 or above becomes a tool error carrying the handler's message.

## Memory layout

`<pulse root>/memory/global.md` and `<pulse root>/memory/projects/<slug>.md`, where
`<slug>` is `memory.ProjectSlug` (the same key `internal/memory` uses; memoised in
`pulseSlugCache` because it spawns git). Separate from the user/project memory of
`internal/memory`.

Live state wins over disk in `session_read`: a resident session's in-memory
transcript is used when its lock can be taken without blocking (`TryLock`, because
a running turn holds it for the whole turn), otherwise the persisted copy.

Deviations from the first spec, kept deliberately: `agent_runs` takes an optional
`session_id` and groups by session because `/api/agents/runs` is per-session (with
no session it returns nothing); the run view omits message transcripts, which would
exhaust the cap on one run; the ANSI stripper is a small regexp in
`pulse_ansi.go` because the repo's existing strippers are unexported in `tui` and
`shell`.

## Exclusion from the board

`buildPulseRows` skips any `pulse_` id (and its children) in both passes, so the
assistant never appears as a card, never counts as a child, and is absent from
`pulse_board`, the injected board and `agent_runs`. Tested in `pulse_rows_test.go`.

## Not built yet

- Always-allow decisions through `permission_resolve` (only allow/deny are exposed;
  harmful operations must stay a human click).
- Reading or writing memory for remote (SSH/WSL) projects.

## Known limits

- Cross-process blindness carries over from Pulse: the assistant sees only this
  server process's live sessions (and, for `scope=all`, the disk listing of local
  projects). Sessions held by another `ocode` process are invisible until they
  persist.
- Remote (SSH/WSL) projects: sessions are not listed by the disk scan and remote
  terminals are not readable.
- The board is a point-in-time snapshot built when the turn starts; the model must
  call tools for fresher data.
- Idle eviction (30 min) drops the live agent like any session; `GET
  /api/pulse/assistant` or the next message rebuilds it from the transcript.

## References

- `internal/server/pulse_assistant.go`, `pulse_tools.go`, `pulse_terminal.go`
- `internal/agent/pulse_assistant.go` (`SetSystemPromptOverride`, `SetPulseSnapshot`, `RestrictToTools`)
- `internal/server/agent_session.go` (`buildAgentSession` pulse branch)
- Tests: `handler_pulse_assistant_test.go`, `pulse_tools_test.go`, `pulse_rows_test.go`, `internal/agent/pulse_assistant_test.go`
