# Part 05 — Frontend Status on Activation + Streaming Watchdog

**Goal:** Sidebar/status panels stay populated across session and project
switches (status fetched per-session on tab activation, patched by events),
and the streaming spinner can no longer hang forever (turn-state-derived with
heartbeat watchdog + reconcile).

**Context (self-contained):**
- Today `tuiStatus` is fetched once at app mount (`web/src/App.tsx:186-203`,
  deps `[dispatch]`) and keyed to whatever tab was active; switching
  session/project hits the empty slice (`web/src/stores/chatStore.tsx:74-80`)
  → sidebar empties (Bug 1). The Context panel has two sources:
  `tuiStatus.context_*` vs a `GET /api/sessions/:id/context` fallback
  (`web/src/components/Layout/CoworkSidebar.tsx:89-118`), and stale fallback
  is never cleared. `StatusPanel.tsx:29-58` fetches with `[]` deps.
- `isStreaming` (`web/src/hooks/useChat.ts:28`) is cleared only by an
  exactly-routed `turn_done`/`error` or a rejected submit promise → eternal
  spinner (Bug 2).
- Server (Part 03) provides `GET /api/sessions/:id/status` (session-tagged
  snapshot incl. `context_*`) and `GET /api/sessions/:id/state`
  (`{bootstrap_stage, turn_active, last_seq}`). Client (Part 04) provides
  `eventBus.on(...)`, `eventBus.onReconnect(...)`, `api.getSessionState(id)`.
  Bus events: `status`, `session_bootstrap`, `turn_started`,
  `turn_heartbeat`, `turn_done`, `turn_error` — all session-tagged.
- Constraints: placeholder `new-*` tabs skip the status fetch and render
  project-level defaults until `session_bootstrap` arrives; watchdog stall
  threshold 30s; fail loudly (`console.warn`) on unroutable events;
  typecheck with `bun run typecheck`.

**Files:**
- Modify: `web/src/stores/chatStore.tsx` — one `sessionStatus` slice per
  session (replaces `tuiStatus` seeding semantics); turn-state fields
  (`turnActive`, `lastHeartbeatAt`, `bootstrapStage`)
- Modify: `web/src/App.tsx` — delete the mount-once status seed
  (`App.tsx:186-203`) and `StatusMetricsHydrator` special-casing
- Create: `web/src/hooks/useSessionStatus.ts` (+ test) — fetch-on-activation
  hook: given the active session id, fetches `/api/sessions/:id/status` into
  the store, subscribes to patching events
- Modify: `web/src/components/Layout/CoworkSidebar.tsx` — single status
  source; delete `fallbackContext` dual-path
- Modify: `web/src/components/Status/StatusPanel.tsx`,
  `web/src/components/common/StatusBar.tsx` — read the per-session slice;
  re-fetch on session change (fix `[]`-deps effects)
- Create: `web/src/hooks/useTurnWatchdog.ts` (+ test) — stall detection +
  auto-reconcile
- Modify: `web/src/hooks/useChat.ts` — `isStreaming` derived from store turn
  state, not local promise state

**Interfaces:**
- Consumes (from Parts 03–04): endpoints and events listed in Context.
- Produces: store selectors `getSessionStatus(state, sessionId)` and
  `getTurnState(state, sessionId)` used by sidebar/status/chat components.

## Tasks

- [ ] **Task 1: per-session status slice + fetch-on-activation.** Test-first
  (vitest): activating a tab with a real session id triggers exactly one
  status fetch and populates that session's slice; switching back uses the
  cached slice then refreshes; `new-*` tabs fetch nothing and expose
  project defaults. Implement store changes + `useSessionStatus`, delete the
  App.tsx mount-once seed. Verify tests + typecheck. Commit.

- [ ] **Task 2: event patching.** Test-first: a session-tagged `status` event
  patches only that session's slice; `session_bootstrap` populates a
  previously-placeholder tab (the tab's id is remapped to the real session
  id the way `SessionTabSync` does today); events for unknown sessions
  `console.warn`. Wire eventBus handlers into the store. Verify. Commit.

- [ ] **Task 3: single-source Context panel + status components.** Remove
  `fallbackContext` from `CoworkSidebar`; Context section reads
  `context_*` from the session slice. Fix `StatusPanel`/`StatusBar` to read
  the per-session slice and re-fetch on session change. Verify manually:
  switch session and project — sidebar, status bar, and status panel stay
  populated (Bug 1 acceptance). Commit.

- [ ] **Task 4: turn-state-derived streaming.** Test-first: store turn state
  transitions on `turn_started`/`turn_done`/`turn_error`; `useChat`'s
  `isStreaming` mirrors it (set optimistically on 202, confirmed by
  `turn_started`); a rejected submit clears it with an error. Rework
  `useChat.ts`. Verify. Commit.

- [ ] **Task 5: watchdog + reconcile.** Test-first (fake timers): no
  `turn_heartbeat` for 30s while turn-active → UI state becomes "stalled"
  and `api.getSessionState(id)` is called; reconcile reporting
  `turn_active: false` clears streaming; reconcile also runs on
  `eventBus.onReconnect` and on tab activation of a turn-active session.
  Implement `useTurnWatchdog`. Verify. Commit.

- [ ] **Task 6: regression + ship.** `bun run typecheck`; full manual QA from
  the spec: switch project → sidebar populated; new session in second
  project → bootstrap stages visible, message round-trips; kill server
  mid-turn → spinner clears via reconcile (Bug 2 acceptance). Commit.
