---
type: Design
title: Pulse — cross-project live sessions dashboard
description: 'Approved design spec for the Pulse cross-project live-sessions dashboard, implemented 2026-09-28, with as-shipped amendment notes recorded inline: activeView value "pulse" instead of a route (no client router), a ProjectSidebar main menu instead of a pinned tab entry, no dock-badge click (Wails v3 dock service is display-only; gap tracked in TODO.md; shipped entries are a Dashboard app-menu item and a tray Open Pulse), todo_updated published from the server tool-result broadcast and deliberately excluded from live frames, scope=all metadata-only cost with an unmeasured budget, and TitleForDir single-row title reads.'
tags:
  - design
  - spec
  - pulse
  - dashboard
  - sessions
  - web
  - desktop
  - superpowers
timestamp: 2026-09-28T09:32:28Z
---
# Pulse — cross-project live sessions dashboard

Date: 2026-09-24
Status: design approved in chat, awaiting spec review

**Amendments (2026-09-28):** implemented as shipped; corrections are marked
inline below as **As shipped (2026-09-28)** notes. The original approved
wording above and below is retained — this stays an approved-design record.

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

**As shipped (2026-09-28):** there is no client router, so Pulse is not a
route — it is the `activeView` value `"pulse"` (`web/src/App.tsx`) — and the
pinned tab-bar entry did not ship: the dashboard is global, so a
per-project tab-strip slot would wrongly imply it is scoped to the active
project. The entry point is instead a main menu at the top-left of the
`ProjectSidebar` header (a `Popover` with `role="menu"`, since the repo
ships no dropdown-menu primitive). `⌘J` and the header badge shipped as
specified.

**As shipped (2026-09-28):** the dock-badge click did **not** ship and was
not substituted silently: Wails v3 beta.12's `dock` service is display-only
(`SetBadge`/`RemoveBadge`/`SetCustomBadge`) and `pkg/events` exposes no
application-activated event, so a macOS dock click while ocode is already
running is indistinguishable from plain activation. The plan's own gate said
to stop and ask rather than swap in a different mechanism, so the gap is
recorded in `TODO.md`, and the shipped desktop entries are an app-menu
**Dashboard…** item on `Cmd/Ctrl+Shift+J` plus a tray **Open Pulse** item.

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
  flag, turn timing. `sessionEntry` does not record turn errors today
  (only `bootstrapErr`); add a last-turn-error field set where
  `turn_error` is published and cleared on the next `turn_started`.
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

**As shipped (2026-09-28):** live rows get their title from a new single-row
indexed read, `session.TitleForDir` (`internal/session/title.go`), not from a
full `ListRefsForDir` per request — the latter would re-scan a project with
thousands of legacy session files on every request and blow the p50 < 200ms
budget.

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

**As shipped (2026-09-28):** `scope=all` reads metadata only — one directory
scan plus one indexed query per project, no transcript bodies, no todo-file
reads — so its cost scales with the number of projects rather than with
history; disk-only rows therefore carry `todo: null`, and live state is
merged over them. The planned manual `curl -w '%{time_total}'` measurement
of the real `all` path was **not** performed during implementation (no live
server with a populated multi-project store was available in that session),
so this budget is recorded as unmeasured rather than as a claimed number.

### `todo_updated` event
Published on the EventBus whenever the todo tool writes a session's todo
file. Payload: `{ session_id, done, total, current }`. Added to the event
type list in `event_bus.go`. Not replayed on reconnect (same as
`agent_activity`).

**As shipped (2026-09-28):** the event is published from the server's
tool-result broadcast — right after the `todowrite` tool result in
`internal/server/handler.go` — not from the todo writer itself, and the
shipped payload also carries `items: [{text, state}]`. It is deliberately
**not** in `liveFrameEvents`: a plan is a momentary reading, so replaying a
buffered copy into a mid-turn reload would show a plan the session has
already moved past; clients recover on their next `/api/pulse` fetch.

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

**As shipped (2026-09-28):** that path only exists while a turn runs — live
frames are cleared at turn end — so idle and error cards (and needs-you rows
paused on an ask) take their tail from the last assistant message in the
transcript instead, with no text-delta subscription.

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

**As shipped (2026-09-28):** the pinned `UnifiedTabBar` entry did not ship —
see **Entry points** above. `⌘J` and `PulseBadge` shipped as listed.

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
