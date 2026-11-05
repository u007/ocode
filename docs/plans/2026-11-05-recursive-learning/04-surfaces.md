# 04 — Surfaces: TUI, web/desktop, API

Rule: both hosts show the same lesson store and the same signals; the trigger for reflection lives in the
agent core so the web/desktop server stops being the host that never learns (today the memory planner is
queued only from the TUI).

## Shared core (`internal/agent`, new `internal/learning`)

| piece | where | note |
|---|---|---|
| signal store | `internal/learning` `Store.AppendSignal(project, session, signal)` | two producers: the agent loop (tool result publish, permission path, cancel path, compaction, goal/auto-continue verdicts, `undo_file_change`) and the hosts (ratings, `/feedback`, Changes-tab undo). Keyed by project slug + session id, so no live agent is required |
| lesson store | `internal/learning` (store, lifecycle, flock, materialised view, dedupe) | project + global/user files under `$GD`; no `.ocode/` fallback |
| `learn` sub-agent | `internal/agent/subagent.go` built-ins, next to `context` | only holder of `lesson_propose` / `lesson_update`. Enforced the way `doc_write` is: the tools are **constructed only when building the `learn` agent** (the `newDocToolsWithJudge`-for-`context` pattern in `doc_tools.go`), never registered in the builtin set. `filterMainOnlyTools` is the wrong hook: it strips main-agent-only tools from sub-agents, the opposite direction. Test `TestLessonToolsAreOnlyForTheLearnAgent` |
| reflection trigger | agent turn-done path (budgeted) and a deferred `reflect_pending` marker on session end, drained at the next agent build in the project or by the consolidation job | shared by TUI, server, desktop, scheduler runner; never queued on a worker that shutdown drops |
| `signals_summary` metadata | `sessionOwnedMetadataKeys` | session-owned like `goal`, or the TUI carry-forward overwrites it |
| `slotLearning` decider | `resolveDecider` | own floor; `deciderLabel` for usage attribution |
| lessons tail | `injectLessonsTail` next to `injectTodoTail` | user-role, bounded, omitted when empty |
| new session-id-keyed journal | signals keyed by session id | add to the `/reset-id` move list |
| hook events | `Stop` (turn done) and `SessionEnd`, plugin-hook format | fills the gap DSH's hook protocol and Claude-Code plugins both have; reflection is the first consumer |

## TUI

| surface | behaviour |
|---|---|
| `/lessons` | list for this project + user/global scopes, grouped by status (`candidate`, `trial`, `active`, `archived`), each with id, scope, kind, model, one-line guidance, shown/withheld counts and the targeted signal rates. Instant command (read-only). |
| `/lessons show <id>` | guidance + every evidence quote with session/seq, trial-window numbers |
| `/lessons approve <id>` / `reject <id>` / `archive <id>` / `edit <id>` | mutations; queued while streaming like other state-mutating commands |
| `/learn` | now runs the `learn` sub-agent on the current session and reports the candidates, instead of printing a skill-creator prompt |
| `/feedback <text>` | one session remark, categorised by Jev choice; log-only |
| `ctrl+x +` / `ctrl+x -` | rate the last assistant message; a one-line confirmation, optional note on `-` | 
| sidebar | a `lessons N candidates` row next to the todo/goal rows; `N` from the store, no LLM call |
| transcript | a `[ocode:lessons]` block renders collapsed as a `lessons (3)` chip like other marker blocks; never shown as a user message |
| Changes tab | unchanged; undo already produces the signal |
| Approval card | after a reflection pass finishes, a one-line notice `2 lesson candidates — /lessons to review` (same channel as discovery notices, single-width ASCII prefix) |

Mouse: the pending row and the chip are click targets on the chat tab; same confirm-before-act pattern as
other dialogs for `approve`.

## Web / desktop

| surface | behaviour |
|---|---|
| per-message rating | thumbs up/down on each assistant message in the chat, optional category + note popover (Radix, existing UI kit). Writes a `message_rating` signal; nothing enters history; the message shows a small state icon |
| `/feedback` | web command parity with the TUI |
| Lessons panel in `CoworkSidebar` | candidates as **approval cards** (DSH personalized-recommendations UX): guidance, scope, evidence quotes expandable, Approve / Reject / Edit; tabs for trial / active / archived with the shown-vs-withheld metric |
| Settings → Learning form | `learning_enabled` (default on once shipped), reflection trigger (`turn-end` / `session-end` / `manual`), auto-promotion of `memory_fact` / `tool_guidance` (default on), trial window size, K and byte cap of the tail, model for reflection (small model default) |
| Pulse board | lessons pending across projects as one row per project; the Pulse assistant gets read-only `lessons_list` (it never proposes or approves) |
| Cron panel | per-job "tune this job" toggle and its candidates (optional, off by default, built last if at all) |
| Sessions list | `signals_summary` badge (errors / undos / ratings) from session metadata, no file read |

## API

| route | wrapper | note |
|---|---|---|
| `GET /api/lessons?project=&status=&scope=` | project-scoped | sorted by status then id, paginated |
| `GET /api/lessons/{id}` | project-scoped | includes evidence and window stats |
| `POST /api/lessons/{id}/approve` / `reject` / `archive` / `edit` | project-scoped, mutating | cites revision; 409 on stale |
| `POST /api/sessions/{id}/learn` | session-scoped, async 202 | runs the reflector; result over SSE |
| `POST /api/sessions/{id}/feedback` | session-scoped | one remark |
| `PUT /api/sessions/{id}/messages/{seq}/rating` | session-scoped | compare-and-set per message like DSH |
| `GET /api/sessions/{id}/signals` | session-scoped | read of the session's signals |
| SSE `lesson_candidate` | session-scoped (the reflection ran on that session); momentary, so **not** in `liveFrameEvents` | two-part change rule applies |
| SSE `lesson_status` | **project-scoped push** (approve/reject/archive and the consolidation job carry no session id), emitted only for viewed projects (`EventBus.ViewedProjects()`); non-viewed projects poll `GET /api/lessons` | the sidebar count on other projects is a poll, per the viewed-projects rule |
| remote projects | all routes thread `?host=`; the lesson store lives on the host that runs the agent (`ocode serve --remote`), never merged locally |

All reads of live session state use `TryLock` with persisted fallback; the reflector runs outside both
`Handler.mu` and `agentSession.mu`.

## Parity checklist

- Reflection trigger: core, so TUI = server = desktop = scheduler.
- Memory root: fixed to the session root first, or the lessons tail will inherit the same wrong-project bug.
- Markers: `[ocode:lessons]` added to the sentinel/marker list every consumer pattern-matches
  (`isSentinelToolContent`, web marker renderer, TUI chip renderer), one PR.
- Compaction: the tail is volatile and re-sent, so compaction neither keeps nor summarises it; the signal
  extraction from the summary happens at compaction time, not from the summary text later.
