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
- Exactly one id is persisted as the CURRENT chat, in `<root>/state.json` as `{"session_id": "..."}`,
  written atomically (`secretfile.WriteFileAtomic`) under `Handler.pulseMu`. It is
  never stored in `ocodeconfig.json`. A state file whose id is not `pulse_` plus
  `[A-Za-z0-9_-]+` (`isValidPulseChatID`) is rejected with a 500, not repaired; fix or
  delete `state.json` to recover.
- Minting persists an EMPTY transcript (title "Pulse assistant", pulse root) before
  the id is returned or the agent registered, so `GET /api/sessions/{id}` and the
  advisor pin find it. If `state.json` names an id whose transcript is gone, the
  next `GET /api/pulse/assistant` logs it and mints a new id.

## API

| Route | Response |
|---|---|
| `PUT /api/pulse/assistant` | Body `{"session_id": "pulse_..."}`. Makes an earlier chat current. `400` for an id outside `pulse_` plus `[A-Za-z0-9_-]+` (`isValidPulseChatID`; the id becomes a path segment, so a traversal id is refused before any file is probed), `404` when its transcript is gone, `409` while the CURRENT chat is mid-turn, compacting or paused on an ask (switching would hide that chat). Returns the same body as GET. |
| `POST /api/pulse/assistant/new` | Starts an empty chat and makes it current under `pulseMu`; the previous transcript stays on disk. Same `409` rule as PUT. Registers only, never pre-builds the agent. Returns the same body as GET. |
| `GET /api/pulse/assistant/chats?limit=&offset=` | `{chats: [{session_id, title, created_at, updated_at}], total, offset, limit, current}`, newest first by `updated_at`. `limit` defaults to 20, capped at 100; `limit<1` or a negative offset is `400`. Lists only `pulse_` transcripts in the pulse root. |
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
  The same block then carries a **running terminals** section from
  `renderPulseTerminals(h.pulseTerminalRows())`: one line per terminal whose
  foreground is a program other than the shell (`terminal_id | project | title | command`),
  capped at 20 (`pulseBoardMaxTerminals`). Idle shells are left out; with none running
  the line reads `No programs running in terminals.` `pulseTerminalRows` memoizes its
  process-table walk for `terminalProcsPollInterval` (1.5 s), so the board, `terminal_tabs`
  and the dashboard poll share one walk. The memo is keyed by the registry generation, so a
  terminal opened or closed shows at once; a command started inside the interval can read as
  idle until the memo expires.
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
| `terminal_tabs` | none | `{projects: [{project, terminals: [{id, title, osc_title?, live, running, command?}]}]}`: open tabs from `internal/termtabs`, joined with the live registry | sorted by project then terminal id; `live` = a shell is running, `running` = a program other than the shell is in the foreground, `command` names it |
| `terminal_read` | `terminal_id`, `lines?` 1-1000 (100), `offset?` (default 0), `head?` (default false) | `{terminal_id, project, head?, offset?, lines, has_more_before?, has_more_after?, truncated?, line_clipped?}` | ANSI stripped, `\r` redraws collapsed, 2000 runes per line; a tail read counts `offset` back from the end, so repeated calls page backwards; `head` reads from the start and `offset` then counts forward; only the newest (or oldest) `pulseTerminalWindowBytes` (1 MiB) of the history log is reachable, read in 256 KB chunks; each call reads the output as it is at that moment, not a pinned snapshot, so paging a still-writing terminal can overlap or skip lines (the history endpoint pins a byte cursor; this read does not); same access gate and project boundary as `GET /api/terminal/{id}/history`; local projects only; the cap drops the far end (OLDEST lines of a tail page, NEWEST of a head page) and sets `truncated` |
| `memory_read` | `scope` global/project, `project_path?` | `{scope, path, content}`; empty content = nothing saved | project scope needs a path in `allowedProjectRoots()` |
| `memory_write` | `scope`, `project_path?`, `content` | `{scope, path, bytes}`; full replace, atomic | ~30k tokens (`pulseMemoryTokenBudget`) = 90 KB at 3 bytes/token (`pulseMemoryCap`), over-cap rejected; the system prompt tells the assistant to keep only important, durable notes |
| `session_send` | `session_id`, `content` | `{session_id, accepted: true}`; starts an async turn via `HandleSendMessage` | **asks**; refuses own id, child ids, unknown sessions, and a target that is mid-turn, compacting or paused on an ask (never queued). The pre-check is re-checked at dispatch: a turn that starts after it refuses the send (`turnOptions.refuseIfBusy`, under `cancelMu`), and a synchronous turn can still slip past that check |
| `session_command` | `session_id`, `command`, `args?` | `{session_id, command, result}` where `result` is the operation's own JSON | **asks**; fixed allowlist `/btw` `/cancel` `/compact` `/recap` `/title`, anything else is an error listing them, no send-as-text fallback |
| `permission_resolve` | `session_id`, `request_id`, `decision` allow/deny | `{session_id, request_id, decision, accepted: true}` via `HandleResolvePermission` | **asks**; unknown request id is an error; always_* decisions are not offered |
| `question_answer` | `session_id`, `request_id`, `answers` | `{session_id, request_id, accepted: true}` via `HandleAnswerQuestion` (same payload as `POST /api/questions`) | **asks**; unknown request id is an error |
| `agent_runs` | `session_id?` | `{sessions: [{session_id, runs: [...nested children]}]}` | transcripts omitted, results 500 runes; sessions without runs omitted; the assistant is excluded |

### Terminal tools

`terminal_tabs` and `terminal_read` let the assistant see terminals: which are open,
which is running a program, and what a terminal printed. They are read-only. Nothing
in this set writes to a terminal, and the terminal send tool is not built.

**Scope: Pulse assistant only.** Both tools are defined in `pulseTools()`
(`internal/server/pulse_tools.go`). That set reaches an agent in one place:
`configurePulseAgent`, called from `buildAgentSession` only when `isPulseSession(id)`.
The built-in toolset that ordinary sessions get (`tool.InitBuiltinToolsWithComputerDriver`)
does not contain either name. The Pulse agent is restricted to the Pulse set with
`RestrictToTools`. `TestTerminalToolsAreOnlyForThePulseAssistant`
(`internal/server/handler_pulse_terminal_scope_test.go`) pins both directions: an
ordinary agent never lists them, and a Pulse agent lists only Pulse tools and includes
them. Do not register a terminal tool anywhere else. Add it to `pulseTools()` and the
scope test will say whether it leaked.

**What the tools return**

- `terminal_tabs` returns the open tabs, grouped by project, from `internal/termtabs`.
  Each terminal carries `live` (a shell is running), and when live also `running` (a
  program other than the shell is in the foreground) and `command` (that program's
  command line). The live join reads the process table on each call.
- `terminal_read` returns N lines of a terminal's output history with ANSI codes stripped.
  `lines` is 1–1000, default 100. By default it returns the last lines. `offset` pages
  back from the end. `head: true` reads from the start instead, and then `offset` counts
  forward. Only the newest or oldest `pulseTerminalWindowBytes` (1 MiB) is reachable,
  read in 256 KB chunks, because the history reader caps each call. The page reports
  `has_more_before` and `has_more_after`. A still-writing terminal is not pinned: each call
  reads the output as it is at that moment, so paging can overlap or skip lines. The
  history HTTP endpoint pins a byte cursor; this read does not. When the window holds no
  newline on the read side, one line is cut at the window edge: its bytes are returned (a
  line-based offset cannot reach them otherwise), and `line_clipped: true` marks the first
  line (tail) or last line (head) as a fragment.

**Access**

- `terminal_read`, the per-turn board's running-terminal block, and every `command` value
  go through `terminalAccessAllowed()`: server auth or a loopback bind. The gate is
  checked inside `pulseTerminalRows`, so the board and `terminal_tabs` share it. On
  Windows the gate is a stub that returns false.
- `terminal_tabs` still lists tab ids and titles without the gate. Only the live fields
  are gated. This is a known gap, recorded in the Known limits below.

**The same data for the UI.** The web Pulse dashboard does not use these tools. It reads
`GET /api/pulse/terminals` for the live terminals (see `pulse-dashboard.md`, "Terminals
section"). The terminal endpoints `GET /api/terminal` and `GET /api/terminal/{id}/history`
serve the Processes tab and the terminal panel. All of these apply the same gate.

## Permission design

The read tools and `memory_write` get explicit `allow` rules in
`configurePulseAgent`. The four tools marked **asks** (`pulseTool.ask`) get NO
rule, so the permission manager's default applies: every call becomes a normal
permission ask that the assistant window renders as a dialog, and the operator can "always
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

- `terminal_tabs` lists tab ids and titles without the terminal access gate; only the
  live fields (`running`, `command`) are gated. Gate the whole tool, or accept that tab
  ids and titles reach the model, and record which in this list.
- `terminal_read` returns only the newest or oldest 1 MiB of a terminal's history, and a
  still-writing terminal is not pinned, so paging can overlap or skip lines.
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
