# Part 02 — Unified Tagged Event Bus + `/api/events`

**Goal:** One server-side broadcaster where every event is an envelope
`{event, project, session_id, seq, data}`, streamed over a single new SSE
endpoint. Old SSE endpoints stay functional until Part 06.

**Context (self-contained):**
- Today there are several independent streams: the chat mirror
  (`internal/server/handler_sse.go`, endpoint `/api/chat/messages`), a logs
  SSE, and per-session agent-runs SSE. Headless `status` broadcasts carry no
  session id (`internal/server/handler_tui_status.go:86`,
  `buildStatusSnapshot()` sets no `SessionID`).
- RC-bridge frames are re-stamped with the bridge's session id in
  `handler_sse.go:283-285` — leave that legacy path untouched here; at-source
  tagging for RC frames happens in Part 06.
- A `SessionManager` exists (Part 01) with `Resolve`/`Register` and entries
  carrying project root — use it to tag session-scoped events with their
  project.
- Constraints: `seq` is a single global monotonic counter per server process,
  for gap detection only (no replay). Git/spending events are emitted only
  for projects with at least one connected client showing them
  (subscriber-aware). Fail loudly: events published with a missing required
  tag are logged as errors.

**Files:**
- Create: `internal/server/event_bus.go` (+ `event_bus_test.go`) — envelope
  type, publish/subscribe, seq stamping
- Create: `internal/server/handler_events.go` — `GET /api/events` SSE handler
- Modify: `internal/server/agent_session.go`, `handler_tui_status.go`,
  logs + agent-runs emitters — publish to the bus (in addition to legacy
  paths for now)
- Modify: server route registration (where `/api/chat/messages` is mounted)
  to add `/api/events`
- Create: server-side git-status watcher + spending emitter (small file(s) in
  `internal/server`), publishing tagged events per subscribed project

**Interfaces produced (used by later parts):**
- `EventBus.Publish(event string, project string, sessionID string, data any)`
  — stamps `seq`, fans out to subscribers.
- `EventBus.Subscribe(ctx) <-chan Envelope` / matching unsubscribe semantics.
- SSE wire format on `/api/events`: one SSE message per envelope; envelope
  JSON field names: `event`, `project`, `session_id`, `seq`, `data`.
- Subscription registration tells the server which projects a client views
  (query param on `/api/events`, updatable by reconnect) — drives
  git/spending emission scope.

## Tasks

- [ ] **Task 1: bus core.** Test-first: publish → subscriber receives
  envelope with monotonic `seq`; two subscribers both receive; slow/closed
  subscriber does not block others (bounded buffer + drop-with-log,
  mirroring the existing broadcast pattern in `handler_sse.go`). Implement
  `event_bus.go`. Verify `go test ./internal/server/...`. Commit.

- [ ] **Task 2: `/api/events` endpoint.** Test-first with `httptest`: client
  receives published envelopes as SSE; disconnect unsubscribes cleanly.
  Implement `handler_events.go` + route registration. Verify. Commit.

- [ ] **Task 3: tag existing emitters.** Test-first: a turn event published
  via the bus carries the session's id and project (from the
  SessionManager); a status snapshot published on config change carries
  session/project when session-scoped. Wire agent turn events, status,
  logs, and agent-runs emitters to also publish on the bus (legacy streams
  untouched). Publishing session-scoped events without a session id logs an
  error. Verify. Commit.

- [ ] **Task 4: server-push git + spending.** Test-first: with one subscriber
  declaring project P, git-status changes under P produce a tagged event;
  a registered-but-unviewed project Q produces none; last subscriber leaving
  stops the watcher. Implement watcher/emitter reusing whatever git-status
  computation the existing `GET` endpoint uses (the one the web client polls
  every 10s from `CoworkSidebar.tsx:121-138`), and the spending computation
  polled from `App.tsx:76`. Verify. Commit.

- [ ] **Task 5: regression + ship.** `go test ./internal/...`. Manual: open
  `/api/events` with `curl -N`, trigger a chat turn and a config change,
  observe tagged envelopes with increasing `seq`. Old UI still fully works
  (legacy endpoints untouched). Commit.
