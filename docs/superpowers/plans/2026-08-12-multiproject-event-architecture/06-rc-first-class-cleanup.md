# Part 06 — RC-Bridge First-Class + Legacy Cleanup

**Goal:** A TUI `/rc` session registers into the SessionManager and is driven
from the web like any other session; the RC forwarding and frame re-stamping
paths are deleted, along with all legacy SSE endpoints and dead client code.

**Context (self-contained):**
- Today, when a TUI attaches via RC-bridge: `HandleSendMessage` ignores the
  `:id` path parameter and forwards every message to the single TUI agent
  (`internal/server/handler.go:521-550`); the mirror SSE handler re-stamps
  every untagged frame with the bridge's session id
  (`internal/server/handler_sse.go:283-285`); and `runTurn` suppresses
  headless events when a bridge is attached
  (`internal/server/agent_session.go:184-232`, `headless := h.RCBridge() ==
  nil`). The TUI registers with the server via
  `RegisterExternalSession` (`internal/server/server.go:495`).
- Parts 01–05 provide: `SessionManager.Register(sessionID, projectRoot)` and
  `Resolve`; `EventBus.Publish(event, project, sessionID, data)`; turn
  lifecycle events (`turn_started`/`turn_heartbeat`/`turn_done`/`turn_error`);
  a frontend consuming only `/api/events` + per-session status/state
  endpoints.
- Constraints: RC frames must be tagged with the real session id at source
  (this part removes the re-stamping compensation). Web + server ship
  together, so deleting legacy endpoints is safe once the frontend no longer
  calls them. Fail loudly on any frame that arrives untagged.

**Files:**
- Modify: `internal/server/server.go` (`RegisterExternalSession` area) — TUI
  registration goes through `SessionManager.Register` with the TUI session's
  real id and project root
- Modify: RC frame publishing path (the bridge's `Subscribe` consumer in
  `handler_sse.go` / its replacement) — publish frames to the EventBus tagged
  at source; delete the re-stamping
- Modify: `internal/server/handler.go` — delete the RC forwarding branch in
  `HandleSendMessage`; route by resolved session id uniformly
- Modify: `internal/server/agent_session.go` — remove the
  bridge-suppression of turn events (turn lifecycle events flow for bridged
  sessions too; the TUI's own rendering is unaffected since it does not
  consume the web bus)
- Delete (after verification): legacy SSE endpoints — the chat mirror
  (`/api/chat/messages` handler in `handler_sse.go`), logs SSE, per-session
  agent-runs SSE — and their route registrations; the mount-once
  `GET /api/tui-status` seeding contract may stay as a plain endpoint if the
  TUI uses it, but the sessionless `status` broadcast in
  `internal/server/handler_tui_status.go:86` is replaced by tagged bus
  publishes
- Delete: dead frontend code left behind by Parts 04–05 (`connectSessionMirror`
  if still present, unused helpers in `web/src/api/client.ts`)

**Interfaces:**
- Consumes: everything from Parts 01–05 as listed in Context.
- Produces: none new — this part converges the system on the interfaces that
  already exist.

## Tasks

- [ ] **Task 1: TUI session registration.** Test-first: after
  `RegisterExternalSession`, the SessionManager resolves the TUI session id
  to the TUI's project root; `GET /api/sessions/:id/status` and `/state`
  work for it. Implement registration wiring. Verify
  `go test ./internal/server/...`. Commit.

- [ ] **Task 2: at-source tagged RC frames.** Test-first: frames published
  from the bridge arrive on the EventBus carrying the real session id and
  project; an untagged frame is error-logged and dropped. Move the bridge
  subscription onto the bus; delete the `handler_sse.go:283-285`
  re-stamping. Verify. Commit.

- [ ] **Task 3: uniform send path.** Test-first: `POST /api/sessions/:id/message`
  with a bridged TUI session id reaches that session; a different (headless)
  session id in the same server reaches *its* agent — no global forwarding.
  Delete the `handler.go:521-550` branch and the bridge-suppression in
  `agent_session.go` turn events. Verify + manual: TUI `/rc`, drive the TUI
  session from the web, and run a second web-only session concurrently.
  Commit.

- [ ] **Task 4: delete legacy endpoints + dead code.** Confirm via grep that
  the web client references none of: `/api/chat/messages`, the logs SSE
  path, the agent-runs SSE path. Delete the handlers, routes, and any
  now-unused broadcast helpers; delete leftover dead frontend transport
  code. `go test ./internal/...` and `cd web && bun run typecheck`. Commit.

- [ ] **Task 5: final regression + docs.** Full manual QA matrix: desktop
  multi-project (switch, new session, mid-turn kill), TUI-bridged session
  from web, connection count = 2 in DevTools. Update `AGENTS.md`/`docs/`
  where they describe the old single-project or multi-stream architecture.
  Mark the spec's status line as implemented. Commit.
