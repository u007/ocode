---
type: Concept
title: Pulse — cross-project live-sessions dashboard
description: 'Concept doc for the Pulse cross-project live-sessions dashboard: GET /api/pulse contract, status/task derivation, scope=all cost model, todo_updated SSE, client store, card filter, hover-overlay contract, jump sequence, entry points (no client router), desktop wiring, and known limits.'
tags:
  - pulse
  - dashboard
  - sessions
  - sse
  - server
  - web
  - desktop
  - api
timestamp: 2026-09-29T04:38:57Z
---
# Pulse — cross-project live-sessions dashboard

## Purpose

Pulse is one glanceable view of **every live chat session across all local
projects**, answering two questions: *what is running?* and *what is waiting on
me?* — with a one-click jump into any session. It is deliberately **not** a
session manager: no rename, delete, archive, or bulk actions. v1 is
**local-server only**; the desktop app renders the same SPA.

- Design spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
- Implementation plan: `docs/superpowers/plans/2026-09-24-pulse-dashboard/` (tasks 01–10 + INDEX)
- **Superseded spec items:** the spec's "route `/pulse`" and "pinned Pulse tab
  in `UnifiedTabBar`" were both wrong, and its shipped replacement — a
  hamburger main menu in the `ProjectSidebar` header — is itself superseded;
  all are replaced as described under
  [Entry points](#entry-points-and-the-no-client-router-correction).

## API: `GET /api/pulse`

Handler: `internal/server/handler_pulse.go` (gathers inputs, validates query).
Derivation: `internal/server/pulse_rows.go` — **all** of it (status precedence,
task fallback chain, inclusion windows, sort, cursor paging) is pure and
I/O-free. That split is the reason every rule below is testable without
starting a server.

Query parameters:

| Param | Meaning |
|---|---|
| `scope` | `live` (default, 24h idle window) \| `all` (7d window). Omitted ⇒ `live`; **present-but-empty `scope=` is 400** — a client computed a blank value, and reading it as `live` would hide that bug. |
| `cursor` | Opaque: base64url of `rank\|RFC3339Nano updated_at\|session_id`. |
| `limit` | 1–100, default 50. Malformed scope/limit/cursor ⇒ **400** (a client bug, not something to silently coerce). |

Response: `{items: PulseRow[], next_cursor: string|null}`. `next_cursor` is an
**explicit JSON `null` on the last page**, never an omitted key (`omitempty` is
deliberately off) — clients can distinguish "end of list" from "field missing".

### Row shape

`session_id, project_path, title, status, current_task{kind:todo|tool|text, text}|null,
todo{done,total,current,items[]}|null, pending_ask{kind:permission|question,
summary}|null, turn_started_at, updated_at, child_count`.

### Status precedence (`derivePulseStatus`)

First match wins:

1. pending permission → `needs_permission`
2. pending question → `needs_question`
3. turn running → `running`
4. last turn errored → `error`
5. else → `idle`

An ask **outranks `running`**: the turn is *paused* on the user, so "needs you"
is the truthful headline even when `turnActive` is still set, and a stale error
must never mask a fresh ask.

### `current_task` derivation (`derivePulseTask`)

- **nil for needs-you rows** — the ask is the headline; a task line would
  duplicate it.
- Otherwise the fallback chain: in-progress todo item → active tool (name plus
  args truncated to **80 runes** via `truncateRunes`, *not* bytes — a byte slice
  would split a multi-byte character into mojibake) → last assistant line → nil.

### Sort and paging

rank (`needs_*`=0, `running`=1, else 2, from `pulseStatusRank`) → `updated_at`
desc → `session_id` asc. The order is defined server-side in
`pulseStatusRank`/`buildPulseRows` and **mirrored** by `sortPulseRows` in
`web/src/stores/pulseStore.tsx` (which re-implements only the sort, never the
derivation rules). The two must stay in step, and each is pinned by tests on its
own side — changing one without the other desynchronizes server pages from the
client's optimistic re-sort.

### Child sessions

Child ids (`<parent>_child_<agent>_<ts>`, infix `_child_` as minted by
`internal/agent/child_session.go`) **never get their own row**. They are counted
onto the parent as `child_count`.

## `scope=all` cost model

`gatherDiskPulseInputs` reads **metadata only**: one directory scan plus one
indexed query per project — no transcript bodies, no todo file reads — so cost
scales with the number of *projects*, not with chat history. Consequences:

- Disk-only rows carry `todo: null` and show project + title + age (reading a
  todo file per historical session would make `all` scale with history).
- Live rows are merged **OVER** the disk listing (`gatherPulseInputs`), so a
  session that is both persisted and resident appears **once**, with its fresher
  live state.
- Remote-host projects are **skipped** (`p.Host != ""`): their transcripts live
  on another machine and v1 is local-only.

## `lastTurnErr` on `sessionEntry`

`internal/server/session_manager.go` carries `lastTurnErr` on the registry
entry. It exists because once `turn_active` drops, a failed turn is otherwise
**indistinguishable from an idle one** — the dashboard would silently downgrade
`error` to `idle`.

- **Recorded** where `turn_error` is published (session manager error path).
- **Cleared** by `setTurnActive(id, true)` (a new turn starts fresh).
- **Kept** by `setTurnActive(id, false)` (turn end must not erase the failure).
- `setTurnError` on an unknown session id is a **no-op** — otherwise an orphan
  entry would appear on the dashboard as a project-less ghost session.

## `todo_updated` SSE event

Payload: `{session_id, done, total, current, items[]}`, published from the
`todowrite` tool result via `Handler.publishTodoUpdated`
(`internal/server/handler.go`).

- The tool-result message carries only `ToolID`, so the callback keeps a
  **per-turn `ToolID → tool name` map** built from assistant messages
  (`toolNames`), consumed on read and capped by `todoToolNameMapCap` (=512) —
  the cap turns a pathologically long session into a cheap reset instead of
  unbounded growth. A result whose `ToolID` was never declared is skipped.
- Backed by `tool.ReadTodoSummary(projectRoot, sessionID)`
  (`internal/tool/todo_store.go`), which resolves `.ocode/todo/<id>.md` from the
  **passed** project root — the process-global `todoDir()` resolves from the
  *process cwd*, which is wrong for a server serving many projects. It reuses
  the existing item parser.
- **Not in `liveFrameEvents`** (see the comment in
  `internal/server/event_bus.go`): a plan is a momentary reading, so replaying a
  buffered copy into a mid-turn reload would show one the session has already
  moved past.
- A corrupt plan is **logged and skipped** (row emitted without a todo) rather
  than failing the dashboard; the same rule applies to a broken title index.

## The client store: `web/src/stores/pulseStore.tsx`

Seeded from `GET /api/pulse`, kept live by SSE events forwarded through
`pulseEventSink`. Deliberate non-behaviours, contractual on purpose:

- **Never synthesizes a row.** An event for an unknown session triggers one
  debounced 300ms refetch (`UNKNOWN_SESSION_REFETCH_MS`) — inventing a card the
  user cannot open is worse than one that arrives late.
- **A failed refetch keeps the last known rows** alongside the recorded error
  instead of blanking the dashboard (an error banner plus stale-but-real rows
  beats an empty view).
- **Reseeds on every `eventBus.onReconnect`** — events were lost while the bus
  was down; a refetch is the only truthful recovery, never a replay.

The store is intentionally NOT driven by the per-tab chat slices: a dashboard
must see sessions with no open tab.

## Card filter: `web/src/components/Pulse/pulseFilter.ts`

Pure, client-side, and deliberately **not** a server query — filtering already-resident
rows avoids a round trip per keystroke and keeps SSE-updated rows filtering
correctly without a refetch.

- `pulseProjectBasename(path)` — last path segment, trailing slashes stripped
  first so `/proj/alpha/` and `/proj/alpha` are one project rather than one
  labelled with an empty string. **Shared with `PulseCard`**, which labels the
  card from it: a filter matching on the full path while the card shows a
  basename makes the visible label and the searchable thing disagree, which
  reads as a broken filter.
- `pulseRowMatchesFilter(row, needle)` — case-insensitive substring of the
  project **basename** or the session title, both `.trim()`ed; an empty query
  matches everything. The title is checked separately from the basename rather
  than against the full path, so a query cannot match a directory segment the
  user never typed.

The filter is applied in `PulseView` (`filtered = rows.filter(...)`) and the
**section grouping is computed over the filtered set**, not the raw rows — so an
empty result collapses the sections entirely and shows the
`No sessions match "…"` line instead. Keyboard users get roving arrow-key
navigation over `[role="listitem"] > button`, which is why the card's button
must stay a *direct* child of the listitem.

**The filter only sees loaded rows.** Paging is an explicit *Load more* button
(`PulseView.tsx`, driven by `loadMore`/`hasMore`/`nextCursor` in the store), not
infinite scroll, so the resident set is the first page plus whatever the user
chose to load. A query therefore cannot match a session still behind the
cursor. That is the accepted trade for keystroke-local filtering; if it ever
matters, the fix is a server-side `q` parameter, not a bigger client fetch.

## The card hover overlay

`PulseCard` is one button; hovering (or focusing) it expands an absolutely
positioned panel after `EXPAND_DELAY_MS` (150ms — a mouse crossing the grid
would otherwise flash a panel over every card it passes; focus is a deliberate
act so it expands with no delay at all). The panel is a **sibling** of the
button, so showing it cannot resize the card and its content stays out of the
button's click target and accessible name.

Content, in precedence order: tail error → tail lines → todo items →
`current_task` → `pending_ask`. Two rules that are easy to get wrong:

- **`current_task` is rendered for kind `tool` *and* kind `text`.** Kind `text`
  is the server's last-resort fallback — the last assistant line, from
  `derivePulseTask` — returned for exactly the `idle` and `error` rows that make
  up most of the dashboard. Gating on `tool` alone drops it, and the card then
  expands into an empty panel while that same line sits visible in the card body
  directly above it. Kind `todo` is deliberately *excluded*: the items list
  already contains the current item, so rendering it again duplicates a line.
- **The panel is never blank.** With no content at all it shows either
  `Loading…` (the seed fetch is in flight, `tail.loading`) or an explicit
  "nothing to preview" line. A blank bordered panel reads as a broken card, and
  the genuinely-empty case is real and reachable: a disk-only `scope=all` row
  carries `todo: null`, and a session whose assistant only ever made tool calls
  has no last assistant line either, so the server sends `current_task: null`.

`usePulseTail` (`web/src/components/Pulse/usePulseTail.ts`) supplies the tail and
is gated on `expanded`, so a collapsed card subscribes to and fetches nothing.
Its `loading` flag tracks the **seed fetch only** — the running path's `text`
subscription never settles, so live deltas arriving after the seed must not keep
the spinner up. Every write is gated on a generation counter, so a fetch that
resolves after the session changed, or after the hook was disabled, cannot write
into the card that replaced it.

## SSE routing order

`web/src/lib/sessionEvents.ts` forwards to `pulseEventSink` **before** the
`sessionIsTracked` gate — the tracked-session gate exists to protect the chat
slices from never-opened sessions, but a dashboard must see exactly those.
The sink is a no-op when no `PulseProvider` is mounted and never routes into
the chat store.

`todo_updated` must also be in the web's `SESSION_SCOPED_EVENTS` set; without it
the router returns before the forward and todo progress only appears on a full
refetch. The Go `sessionScopedEvents` (`internal/server/event_bus.go`) and web
`SESSION_SCOPED_EVENTS` lists must **stay in step** — same rule as the sort.

## Jump sequence: `web/src/lib/jumpToSession.tsx`

Clicking a card crosses a project boundary. The order is the whole contract:

1. `selectProject(project)` **then `await`** (it is async — it revalidates the
   project list).
2. `openSessionTab(id, title, projectPath)`.

The reverse order binds the tab to whichever project happened to be active —
an empty transcript and API calls against the wrong root. Project identity is
**path + host, never path alone**: a local project and a remote one may share a
path. An unknown project is **refused with a `console.error`** rather than
opening a tab bound to no project. `useJumpToPendingAsk` additionally opens the
side pane **after** the tab exists, because the pane key (`sideChatKey(id)`) is
per-session and would otherwise attach to nothing. The view transition is
supplied by App via `PulseJumpProvider` (the module never reaches into
`activeView` itself).

## Entry points and the "no client router" correction

**This app has no client router.** A view is the `activeView` state union in
`web/src/App.tsx`, so Pulse is the value `"pulse"` and there is **no `/pulse`
URL**. (The spec's "route `/pulse`" was wrong; corrected here.)

Entry points:

- **(a) Dashboard icon button beside the "Projects" heading** — a direct
  `LayoutDashboard` icon button in the `ProjectSidebar` header row, between the
  `h2 "Projects"` and the chevron-left collapse button:
  `data-testid="open-dashboard"`, `aria-label="Open dashboard"`, a Radix
  `Tooltip` reading "Dashboard — all projects", calling the `onOpenDashboard`
  prop. Two earlier shapes were rejected on the same grounds: the spec's
  pinned session-tab pill, because the dashboard is *global* — a per-project
  row would wrongly imply it is project-scoped — and the first shipped shape,
  a hamburger `Menu` popover whose single item was "Dashboard", which buried a
  global, all-projects view two clicks deep *inside* the project list and so
  read as project-scoped too. The `onOpenDashboard` prop is optional: omit it
  and the button is not rendered at all, so a half-mounted tree never offers a
  dead button. The button is also the first Radix `Tooltip` in
  `renderExpandedInner()`'s shared body, so that body now wraps itself in
  `TooltipProvider delayDuration={300}`: both callers (the mobile drawer branch
  and the desktop expanded column) previously rendered it with no provider
  ancestor, and Radix throws "Tooltip must be used within `TooltipProvider`"
  (only the collapsed rail had brought its own).
- **(b) Header badge** — `PulseBadge` renders `● running · ◆ needs you` and
  renders **nothing at zero** (a permanent "● 0 · ◆ 0" is noise).
- **(c) Cmd/Ctrl+J** to toggle in and out (`onTogglePulse` in
  `web/src/hooks/useKeyboard.ts`), returning to the **previous view** via
  `previousViewRef` in App.tsx. It carries the same xterm guard as Cmd+W: if
  `!metaKey` and the target is inside `.xterm`, it returns — Ctrl+J is
  readline's "kill line" and must not be stolen mid-command.

**`"pulse"` is deliberately NOT persisted** per project via
`saveViewStateForProject` — the dashboard is global, so persisting it would
make the *next* project open straight into it. Leaving Pulse therefore also
leaves the previously stored view untouched, so returning lands back where the
user was.

## Desktop

- **App menu "Dashboard…" on Cmd/Ctrl+Shift+J** — plain Cmd+J would be consumed
  by the native menu accelerator *before* the webview sees the key, stealing the
  in-app toggle. Same pattern for Windows/Linux in the File menu
  (`cmd/ocode-desktop/main.go`).
- **Tray "Open Pulse" item** (`window.Show()` + `window.Focus()`).
- Both `ExecJS` a `CustomEvent("ocode:open-pulse")` into the page, exactly like
  the existing `ocode:open-settings`, received by
  `web/src/components/Pulse/PulseShellSignal.tsx`. The load-bearing reason: the
  webview is served over plain `http://` by ocode's own `embed.FS`-backed
  server, so Wails' `wails://` scheme handler **never runs**, `window._wails` is
  never injected, and `window.EmitEvent` is a **structural no-op**, not a timing
  issue.
- Because the user invoked this from the tray/menu bar, the page then asks the
  shell to raise the window over the minimal `_wails.invoke` bridge:
  `invokeWails("ocode:focus-window")` → `application.Options.RawMessageHandler`
  (`web/src/lib/wails.ts` `focusDesktopWindow`) — otherwise the dashboard opens
  behind another app and reads as "nothing happened".
- **There is no dock-badge-click handler.** Wails v3 beta.12's `dock` service is
  display-only and `pkg/events` exposes no application-activated event, so a
  macOS dock click while ocode is running is indistinguishable from plain
  activation. The tray item is the reachable equivalent; the gap is recorded in
  `TODO.md`.

## Known limits

- **Cross-process blindness:** the desktop app and a separately-running dev
  server each have their own in-process session registry (see
  [cross-process-session-sync.md](cross-process-session-sync.md)), so each sees
  only *its own* live sessions.
- A session restored after server start, or idle-evicted at
  `defaultSessionIdleTimeout`, has no live state and appears only under
  `scope=all`.
- **Remote hosts are absent** (v1 local-only).
- **Disk-only rows have no todo** (cost model above), and a row with no todo, no
  ask, and no last assistant line has nothing for the hover overlay to show — it
  says so rather than opening a blank panel (see
  [The card hover overlay](#the-card-hover-overlay)).
- The running card's **live tail** comes from buffered SSE frames while a turn
  runs and from the last assistant message otherwise, because the server clears
  `live_frames` at turn end (`web/src/components/Pulse/usePulseTail.ts`).

## Tests

- `internal/server/pulse_rows_test.go` — pure derivation: status precedence,
  task fallback, windows, sort, cursor paging.
- `internal/server/handler_pulse_test.go` — validation (400s incl. empty
  `scope=`), merge, `next_cursor` null.
- `internal/server/session_manager_turn_error_test.go` — `lastTurnErr`
  clear/keep semantics.
- `internal/server/todo_updated_event_test.go` — event shape, unknown-ToolID
  skip, cap.
- `web/src/stores/pulseStore.test.tsx` — no synthesized rows, error keeps rows,
  reconnect reseed, `sortPulseRows` mirror.
- `web/src/lib/sessionEvents.pulse.test.ts` — sink forwarded before the
  tracked-session gate; `todo_updated` routing.
- `web/src/lib/jumpToSession.test.tsx` — select-then-open order, path+host
  identity, unknown-project refusal.
- `web/src/hooks/useKeyboard.pulse.test.ts` / `.browser.test.ts` — Cmd/Ctrl+J
  toggle, xterm guard.
- `web/src/components/Pulse/` — `PulseView`, `PulseCard`, `PulseBadge`,
  `PulseShellSignal`, `pulseFilter`, `usePulseTail` suites.
  - `PulseCard` pins the overlay's **content**, not just that it appears: the
    `text`-kind task reaches it, and it is never a blank panel (loading vs
    genuinely-nothing are distinguished). Asserting only
    `expect(overlay()).toBeInTheDocument()` is what let an empty overlay ship
    green — every overlay test must also assert something *in* it.
  - `usePulseTail` pins the `loading` flag across the seed, the failure path,
    and the disabled state.

## References

- Endpoint: `internal/server/handler_pulse.go`
- Pure derivation: `internal/server/pulse_rows.go`
- Client store: `web/src/stores/pulseStore.tsx`
- Card, overlay, and filter: `web/src/components/Pulse/PulseCard.tsx`, `web/src/components/Pulse/usePulseTail.ts`, `web/src/components/Pulse/pulseFilter.ts`
- SSE routing: `web/src/lib/sessionEvents.ts`
- Jump helper: `web/src/lib/jumpToSession.tsx`
- View wiring: `web/src/App.tsx` (`activeView` union, `openPulse`/`togglePulse`)
- Desktop shell: `cmd/ocode-desktop/main.go`, `web/src/components/Pulse/PulseShellSignal.tsx`, `web/src/lib/wails.ts`
- Design spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
- Plan: `docs/superpowers/plans/2026-09-24-pulse-dashboard/`