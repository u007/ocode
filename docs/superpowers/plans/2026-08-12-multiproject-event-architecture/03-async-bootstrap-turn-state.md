# Part 03 — Async Bootstrap, Turn State Machine, Reconcile & Status Endpoints

**Goal:** `POST /api/chat` persists the user message and returns 202
immediately; agent bootstrap runs async with stage events and a bounded MCP
wait; turns emit started/heartbeat/done/error; new reconcile and per-session
status endpoints give the frontend server truth to derive state from.

**Context (self-contained):**
- Today `HandleChat` (`internal/server/handler.go:368-374`) builds the agent
  session synchronously via `ensureAgentSession` → `buildAgentSession`
  (`internal/server/agent_session.go:47-66`): spawns plugin processes, may
  auto-start a local model server, and blocks unbounded on
  `h.mcpCache.wait()`. First message in a new session can hang minutes with
  no feedback; the client spinner is tied to this unsettled promise.
- Part 01 provides `SessionManager` entries with bootstrap/turn state
  placeholders; Part 02 provides
  `EventBus.Publish(event, project, sessionID, data)`.
- Headless status snapshots (`internal/server/handler_tui_status.go:28-64
  buildStatusSnapshot`) set no `session_id` and no `context_*` fields — the
  new per-session status endpoint must include both.
- Constraints: MCP wait timeout 30s (proceed without stragglers + warning
  event); heartbeat every ~10s while a turn runs; message persisted before
  202 so bootstrap failure never loses it; bootstrap failure emits
  `turn_error` carrying the failing stage.

**Files:**
- Modify: `internal/server/handler.go` — `HandleChat` persist-then-202;
  `HandleSendMessage` same turn-state semantics
- Modify: `internal/server/agent_session.go` — bootstrap goroutine, stage
  events, MCP timeout, turn lifecycle events + heartbeat ticker
- Create: `internal/server/handler_session_state.go` —
  `GET /api/sessions/:id/state` and `GET /api/sessions/:id/status`
- Modify: `internal/server/session_manager.go` — bootstrap/turn state fields
  become real (stage enum, turn-active flag, updated timestamps)
- Tests beside each file

**Interfaces produced (used by later parts):**
- Bus events: `session_bootstrap` (data: stage ∈ `model|tools|mcp` in build
  order, plus a
  terminal `ready` and `warning` for MCP timeout), `turn_started`,
  `turn_heartbeat`, `turn_done`, `turn_error` (data includes failing stage
  when bootstrap-caused) — all session- and project-tagged.
- `GET /api/sessions/:id/state` → `{bootstrap_stage, turn_active, last_seq}`.
- `GET /api/sessions/:id/status` → the per-session status snapshot including
  `session_id`, cwd, model info, and `context_*` fields (superset of today's
  `GET /api/tui-status` snapshot plus the context data currently only on
  `GET /api/sessions/:id/context`).

## Tasks

- [ ] **Task 1: turn state on the registry.** Test-first: entry transitions
  idle → bootstrapping(stage) → ready → turn-active → idle; illegal
  transitions error-log. Implement state fields + transition methods on the
  SessionManager entry. Verify `go test ./internal/server/...`. Commit.

- [ ] **Task 2: persist-then-202.** Test-first at HTTP layer: `POST /api/chat`
  returns 202 with the session id before any agent build completes (stub a
  slow build); the user message is already on disk in the session file when
  the 202 returns. Rework `HandleChat` accordingly; bootstrap moves into a
  goroutine owned by the SessionManager entry (single-flight per session).
  Verify. Commit.

- [ ] **Task 3: bootstrap stage events + MCP timeout.** Test-first: a stubbed
  bootstrap emits `session_bootstrap` stage events in order on the bus; a
  stubbed MCP cache that never completes causes a `warning` event after the
  timeout and bootstrap proceeds; a failing stage emits `turn_error` with
  that stage and the persisted message remains. Implement in
  `agent_session.go` (`buildAgentSession` decomposed into observable
  stages; `h.mcpCache.wait()` wrapped with the 30s bound). Verify. Commit.

- [ ] **Task 4: turn lifecycle + heartbeat.** Test-first: running a turn
  emits `turn_started`, periodic `turn_heartbeat` (shrink the interval in
  tests), then `turn_done`; an erroring turn emits `turn_error`; heartbeat
  stops after completion. Implement around the existing turn-run path
  (`runTurn`), headless and bridged alike. Verify. Commit.

- [ ] **Task 5: state + status endpoints.** Test-first: `state` reflects the
  registry entry through a full bootstrap+turn cycle; `status` returns
  session-tagged snapshot with `context_*` for a session in a non-workdir
  project (uses Part 01 resolution). Implement
  `handler_session_state.go` + routes. Verify. Commit.

- [ ] **Task 6: regression + ship.** `go test ./internal/...`. Manual: new
  session message → 202 is immediate, `curl -N /api/events` shows bootstrap
  stages then turn events; kill the model provider mid-turn → `turn_error`
  arrives, `state` shows `turn_active: false`. Old UI still works. Commit.
