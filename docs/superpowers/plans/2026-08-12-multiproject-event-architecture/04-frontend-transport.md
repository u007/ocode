# Part 04 — Frontend Transport: Single Event Bus

**Goal:** The web client consumes one `EventSource` on `/api/events` through a
single event-bus module; per-session EventSources, the logs EventSource, the
chat mirror, and git/spending polling are deleted. Reconnect reconciles open
sessions. Total long-lived connections: 2 (events SSE + terminal WebSocket).

**Context (self-contained):**
- Today the client opens: a permanent chat-mirror `EventSource`
  (`web/src/api/client.ts:709-737 connectSessionMirror`, consumed by
  `web/src/components/Layout/SessionTabSync.tsx:38-246`), one agent-runs
  `EventSource` per session (`web/src/hooks/useAgentRuns.ts:26-53`), a logs
  `EventSource` (`web/src/components/.../LogPanel.tsx:86`), a terminal
  WebSocket, 10s git polling (`CoworkSidebar.tsx:121-138`) and 60s spending
  polling (`App.tsx:76`). On HTTP/1.1's 6-per-origin cap this starves
  `POST /api/chat` → multi-minute hangs.
- Server side (Parts 02–03) provides `GET /api/events` streaming envelopes
  `{event, project, session_id, seq, data}` (projects viewed passed as a
  query param), and `GET /api/sessions/:id/state` →
  `{bootstrap_stage, turn_active, last_seq}`.
- Event types on the bus include the legacy chat-mirror payloads plus:
  `session_bootstrap`, `turn_started`, `turn_heartbeat`, `turn_done`,
  `turn_error`, `status`, `logs`, agent-runs, `git_status`, `spending`.
- Constraints: unknown-session events get a loud `console.warn`, never a
  silent drop; recovery after disconnect is state fetch + transcript refetch,
  never event replay; typecheck with `bun run typecheck` (tsgo).

**Files:**
- Create: `web/src/lib/eventBus.ts` (+ unit test) — single EventSource,
  reconnect with backoff, typed envelope parsing, per-event-type handler
  registry, seq-gap detection (warn + trigger reconcile)
- Modify: `web/src/components/Layout/SessionTabSync.tsx` — subscribe via
  eventBus instead of `connectSessionMirror`
- Modify: `web/src/hooks/useAgentRuns.ts` — consume agent-runs events from
  eventBus; delete its EventSource
- Modify: `LogPanel.tsx` — consume `logs` events; delete its EventSource
- Modify: `CoworkSidebar.tsx` — consume `git_status` events; delete 10s
  polling
- Modify: `web/src/App.tsx` — consume `spending` events; delete 60s polling
- Modify: `web/src/api/client.ts` — remove `connectSessionMirror` once no
  consumers remain; add `getSessionState(id)` helper
- Modify: `web/src/stores/chatStore.tsx` / `projectStore.tsx` — only as
  needed to accept the new event routing (no store redesign here; status
  redesign is Part 05)

**Interfaces produced (used by Part 05):**
- `eventBus.on(eventType, handler)` / `eventBus.off(...)`; handlers receive
  the full envelope (so they can route by `session_id`/`project`).
- `eventBus.onReconnect(handler)` — fired after the stream re-establishes;
  Part 05's watchdog and status logic hook this.
- `api.getSessionState(id)` → `{bootstrap_stage, turn_active, last_seq}`.

## Tasks

- [ ] **Task 1: eventBus module.** Test-first (vitest, mocking
  `EventSource`): envelopes route to registered handlers by event type;
  unknown-session chat events produce `console.warn`; a `seq` gap logs and
  fires the reconnect/reconcile callback; backoff reconnect re-subscribes
  with current project list. Implement `eventBus.ts`. Verify tests +
  `bun run typecheck`. Commit.

- [ ] **Task 2: swap the chat mirror.** Move `SessionTabSync`'s event
  handling onto `eventBus.on(...)` handlers, preserving its current routing
  into `chatStore` (message/turn events keyed by session id). Delete the
  `connectSessionMirror` call; keep behavior identical otherwise. Verify:
  existing frontend tests + manual chat round-trip. Commit.

  > **Subscription gotcha:** The bus dispatches per-event-type (no wildcard).
  > `SessionTabSync` must subscribe to each event type in `ROUTABLE_EVENTS`
  > individually via `eventBus.on(event, handler)`, not to a single
  > `"envelope"` event. `ROUTABLE_EVENTS` (defined in `sessionEvents.ts`)
  > lists all routable event names — the two process-global ones plus every
  > session-scoped event. Subscribing to the SSE frame name `"envelope"`
  > silently drops all live events. See the design spec §4.

- [ ] **Task 3: swap agent-runs, logs.** `useAgentRuns` and `LogPanel`
  subscribe via eventBus; their EventSources deleted. Verify manually (runs
  panel updates during a task; logs panel streams) + typecheck. Commit.

- [ ] **Task 4: swap git + spending polling.** `CoworkSidebar` git section and
  the spending display consume pushed events; polling intervals deleted.
  Verify manually (touch a file → git section updates without a 10s wait).
  Commit.

- [ ] **Task 5: reconcile on reconnect.** On `eventBus.onReconnect`, for every
  open tab with a real session id: call `api.getSessionState(id)` and refetch
  its messages; placeholder `new-*` tabs skipped. Test-first where feasible
  (vitest on the reconcile function with mocked api). Verify: kill/restart
  the server while the UI is open → transcript recovers, no stuck UI.
  Commit.

- [ ] **Task 6: regression + ship.** `bun run typecheck`; full manual pass:
  chat, logs, runs, git, spending all live with exactly one `/api/events`
  connection in DevTools Network. Remove `connectSessionMirror` from
  `client.ts` if now unused. Commit.
