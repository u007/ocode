# Pulse — cross-project live sessions dashboard

Date: 2026-09-24
Status: design approved in chat, awaiting spec review

## Goal

One glanceable view of every live chat session across all local projects,
in web and desktop. Answer "what's running, and what's waiting on me?" in
under a second, and jump to any session — with its project's terminals,
browser tabs and side pane restored — in one click.

Not a session manager: no rename, delete, archive or bulk actions.

## Scope

In v1:
- Local server only (the process serving the page).
- Live sessions (running, waiting on user, or activity within 24h); "All"
  toggle extends to 7 days.
- Web and desktop (desktop renders the same web page).

Deferred (each gets a TODO.md entry):
- Remote hosts fan-out.
- Inline approve/deny of permission asks from a card.
- Desktop global (OS-level) hotkey.
- Child (sub-agent) sessions as their own cards.
- Wiring `todo_updated` into the CoworkSidebar TODO stub.

## UX

### Entry points
- A pinned **Pulse** tab in `UnifiedTabBar`, route `/pulse`.
- `⌘J` toggles between Pulse and the previously active session tab.
- Header badge `● <running> · ◆ <needs-you>`, always visible; click opens
  Pulse. Hidden when both counts are zero.
- Desktop: clicking the existing dock badge opens Pulse.

### Layout
Responsive card grid in three sections, fixed order:

1. **Needs you** — status `needs_permission` or `needs_question`.
2. **Running** — status `running`.
3. **Recent** — `idle` or `error`, last activity ≤ 24h (≤ 7d under "All").
   Rendered as compact cards.

Sections with zero cards are hidden. Within a section, cards sort by
`updated_at` descending.

Toolbar: text filter (matches project name or title, case-insensitive)
and a `Live | All` toggle. "All" paginates at 50 via "Load more".

Empty state: "No live sessions" plus the 5 most recent sessions as
compact cards.

### Card (collapsed)
- Status glyph: ◆ needs you, ● running (pulsing), ✕ error, ○ idle.
- Project name (basename of project path; full path in `title` attr).
- Session title.
- Current task line, first available of:
  1. in-progress todo item (`- [•]`),
  2. running tool name + short argument summary,
  3. last line of the last assistant message.
  For needs-you cards the line is the pending ask summary instead
  (e.g. "Approve: bash …", "Question: …").
- Todo progress bar (`done/total`), hidden when there is no todo list.
- Elapsed time: turn duration while running, else "N ago".
- Child-session count chip when `child_count > 0`.

### Card (expanded)
Triggered by hover (150ms delay) or keyboard focus. Expands in place as an
overlay (absolute-positioned, raised z-index) so the grid never reflows.
Shows:
- Live stream tail: last ~6 lines, appended as text streams in.
- Full todo list with state marks.
- Running tool with its arguments.
- Pending ask text (read-only in v1).

Collapses on mouse leave / blur. `Esc` collapses.

### Actions
- Click or `⏎`: jump to the session (see Jump).
- Double-click: jump, then open the side pane focused on the pending ask.
  On a card with no pending ask, behaves as single click.
- Arrow keys move focus between cards in reading order.

### Accessibility
- Grid is a list (`role="list"`), cards are `role="listitem"` with a
  focusable button covering the card.
- Status is conveyed by glyph shape and an `aria-label` ("Needs you",
  "Running", "Error", "Idle"), never by color alone.
- Pulsing animation respects `prefers-reduced-motion`.

## Server

### `GET /api/pulse`
Handler: `internal/server/handler_pulse.go`, route registered in
`server.go`.

Query:
- `scope`: `live` (default) or `all`. Any other value → 400.
- `cursor`: opaque string from a previous response.
- `limit`: 1–100, default 50. Out of range → 400.

Response: `{ items: PulseRow[], next_cursor: string | null }`.

`PulseRow`:
- `session_id`
- `project_path`
- `title`
- `status`: `needs_permission | needs_question | running | error | idle`
- `current_task`: `{ kind: "todo" | "tool" | "text", text }` or null
- `todo`: `{ done, total, current }` or null
- `pending_ask`: `{ kind: "permission" | "question", summary }` or null
- `turn_started_at`, `updated_at` (RFC 3339)
- `child_count`

Sources:
- Live registry: `SessionManager.Snapshot()` for project root, running
  flag, turn timing, last turn error.
- `Handler.RunStates()` for running agents.
- `tailIsPermissionAsk` / `tailIsQuestionAsk` on the session's message
  tail for needs-you status and the ask summary.
- Todo file `<root>/.ocode/todo/<session-id>.md` parsed per session id
  (new helper in `internal/tool/todo_store.go` that reads a given id;
  the existing `ReadTodoSnapshot()` only reads the process-current
  session).
- `scope=all`: sessions from the on-disk session list with
  `updated_at` within 7 days, merged with live rows by `session_id`
  (live data wins). Must reuse the existing session list path and must
  not call the per-project list endpoint per project.

Status derivation (first match wins): pending permission ask →
`needs_permission`; pending question → `needs_question`; turn running →
`running`; last turn ended in error → `error`; else `idle`.

`scope=live` includes: all sessions in the live registry whose status is
not `idle`/`error`, plus idle/error sessions with `updated_at` ≤ 24h.
Child sessions are excluded as rows and counted on their parent.

Sort: status rank (`needs_permission`, `needs_question` = 0, `running` =
1, `error`/`idle` = 2), then `updated_at` desc, then `session_id` asc as
tiebreak. Cursor encodes the last row's sort key.

Performance: `scope=all` must be measured during planning against the
~5.5k legacy session files noted in
`docs/gotchas/web-all-sessions-dialog-slow.md`. Budget: p50 < 200ms for
`scope=live`.

### `todo_updated` event
Published on the EventBus whenever the todo tool writes a session's todo
file. Payload: `{ session_id, done, total, current }`. Added to the event
type list in `event_bus.go`. Not replayed on reconnect (same as
`agent_activity`).

### Existing events consumed
`agent_activity`, `turn_started`, `turn_done`, `turn_error`,
`turn_heartbeat`, `permission`, `permission_resolved`, `question`,
`question_resolved`, text-delta stream events, title updates.

## Web

### `stores/pulseStore.tsx`
- Holds `PulseRow` by `session_id`, plus pagination state and load error.
- Seeds from `GET /api/pulse` on Pulse mount and on EventBus reconnect.
- Header badge reads counts from this store, so the store is mounted at
  app level and seeded once on app load (live scope only).
- Updated by events for **all** sessions: `sessionEvents.ts` forwards
  events to the pulse store before the `sessionIsTracked` filter drops
  untracked ones. Tracked-session behavior is unchanged.
- Unknown `session_id` in an event → refetch `/api/pulse` (debounced),
  never synthesize a row client-side.

### Live tail
On card expand: fetch `GET /api/sessions/{id}/state` once for buffered
frames, render the last ~6 lines, then append from text-delta events
while expanded. Discard tail state on collapse.

### `lib/jumpToSession.ts`
`jumpToSession(projectPath, host, sessionId, title)`: `selectProject`
then `openSessionTab`. Terminals and browser tabs restore via their
existing project-scoped stores; side pane restores via
`sidePaneState`. Host and project path are threaded explicitly per
`docs/concepts/web-session-host-scoping.md`. Double-click calls
`jumpToSession` then opens the side pane on the pending ask.

### Components
- `components/Pulse/PulseView.tsx` — toolbar, sections, empty/error
  states.
- `components/Pulse/PulseCard.tsx` — collapsed/expanded card.
- `components/Pulse/PulseBadge.tsx` — header counts.
- `⌘J` binding in `hooks/useKeyboard.ts`.
- Pinned tab entry in `UnifiedTabBar`.

Built from existing `components/ui` primitives (Badge, Tooltip,
ScrollArea, Progress). The expand overlay is CSS, not a popover.

## Errors and edge cases
- `/api/pulse` failure: visible error banner with Retry; no fallback to
  stale data; error logged with the request attempted.
- Session re-keyed: rows keyed by current id per
  `docs/concepts/session-rekey-reset-id.md`; a re-key event replaces the
  row.
- Cross-process: desktop and a separately run dev server have separate
  live registries; each Pulse shows only its own process's live
  sessions. Documented, not worked around.
- Todo file missing or unparsable: `todo: null`; parse failure logged at
  warn with the path.
- Session deleted while displayed: next refetch drops it; a jump to a
  missing session shows the existing "session not found" handling.

## Testing
Go:
- Status derivation for each status, including precedence.
- Sort order, cursor pagination, `limit`/`scope` validation (400s).
- `live` vs `all` inclusion rules; child exclusion and `child_count`.
- `todo_updated` published on todo write with correct counts.
- Per-id todo reader parses `- [ ]`, `- [x]`, `- [•]`.

Web:
- pulseStore: seed, event updates for untracked sessions, refetch on
  reconnect, refetch on unknown id.
- PulseCard: hover and focus expand, `Esc` collapse, reduced-motion.
- jumpToSession: calls `selectProject` before `openSessionTab` with host.
- PulseView: sections order, filter, empty state, error banner + retry.

## Docs
- New `docs/concepts/pulse-dashboard.md`.
- `docs/index.md`, `CHANGES.md`, `TODO.md` (deferred items).
