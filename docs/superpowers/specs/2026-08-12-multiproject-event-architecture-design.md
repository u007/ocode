# Multi-Project Session & Event Architecture — Design

Date: 2026-08-12
Status: Approved (design), pending implementation plan

## Problem

Five architectural defects in the desktop/web UI, confirmed by investigation:

1. **Project model fiction.** One server process serves one project root
   (`internal/server/handler.go:70`, `internal/desktop/boot.go:31-39`), but the
   web UI offers multi-project switching that only mutates client state
   (`projectStore.tsx:318-330`). Session *listing* resolves per-project
   (`handler_projects.go:99`) while session *loading* resolves against the
   server's own workdir (`session.go:287` → `GetStorageDir()`), so every
   cross-project session 404s on load/context/message
   (`handler.go:487, 552, 916`).
2. **Connection starvation.** Plain HTTP/1.1 (6 connections/origin cap) plus a
   permanent mirror EventSource (`client.ts:712`), one agent-runs EventSource
   per session (`useAgentRuns.ts:26-53`), a logs EventSource, a terminal
   WebSocket, 10s git polling and 60s spending polling. Requests (including
   `POST /api/chat`) queue behind zombie streams → multi-minute hangs,
   "unstable streaming".
3. **Synchronous bootstrap in a request handler.** `POST /api/chat` builds the
   agent session before responding (`handler.go:368-374`,
   `agent_session.go:47-66`): spawns plugins, may auto-start a model server,
   and blocks unbounded on `h.mcpCache.wait()`. First message in a new session
   can hang ~10 minutes with no feedback.
4. **Status seeded once, then abandoned.** `tuiStatus` is fetched once at mount
   (`App.tsx:186-203`), keyed to one tab. Headless `status` broadcasts carry no
   session id (`handler_tui_status.go:28-64`), and the client drops sessionless
   status events (`SessionTabSync.tsx:85`). Switching session/project empties
   the sidebar. The Context panel has dual sources (`tuiStatus.context_*` vs a
   `GET /sessions/:id/context` fallback, `CoworkSidebar.tsx:89-118`) and stale
   fallback is never cleared.
5. **No streaming-state contract.** `isStreaming` (`useChat.ts:28`) is cleared
   only by an exactly-routed `turn_done`/`error` event or a rejected submit
   promise. No heartbeat, no timeout, no reconcile. RC-bridge re-stamps frames
   (`handler_sse.go:283-285`) and the client silently drops unroutable events
   (`SessionTabSync.tsx:123-125`) → eternal spinner.

## Decisions

- **True multi-project server** — one server process serves all projects;
  session-scoped calls resolve per-session project root.
- **Full transport multiplex** — one SSE connection carries all event types;
  polling loops replaced by server push. Terminal keeps its WebSocket.
- **RC-bridge is first-class** — TUI sessions register into the same session
  registry; web drives them like any other session.
- Web + server ship together: breaking the web↔server API is fine. On-disk
  session format stays compatible (existing sessions must still load).

## Design

### 1. SessionManager (backend core)

New registry in `internal/server` — single authority for
session ID → `{projectRoot, agentSession, bootstrapState, turnState,
lastHeartbeat}`.

- **Resolution:** storage stays on-disk per project slug (format unchanged).
  An unknown session ID is resolved by checking registered projects' storage
  dirs (via `GetStorageDirForPath`), then cached. The search space is the
  server-side persisted project store (`h.projects`, `internal/projects`) —
  already the source for `HandleListProjects`. `session.Load` gains a
  for-dir variant; `effectiveWorkDir()`-based resolution is removed.
- **Eviction:** built agent sessions are released after an idle timeout
  (default 30 min, no active turn); the registry entry and on-disk session
  remain, and the agent rebuilds on the next message. Prevents unbounded
  agent/plugin processes as projects accumulate.
- **Creation:** `POST /api/chat` carries `project_path`; the manager binds the
  session to that root and the agent's workdir is that root.
- **All session-scoped handlers** (`get`, `context`, `message`, `chat`)
  resolve through the manager — cross-project 404s become impossible for
  sessions that exist.
- **RC first-class:** the TUI registers its live session into the registry
  under its real session ID. `HandleSendMessage` routes by ID; the
  "forward to the single TUI agent" path (`handler.go:521-550`) is deleted.

### 2. Unified event bus

- One server-side broadcaster; envelope: `{event, project, session_id, seq,
  data}`. All emitters publish tagged at source: agent turns, status, logs,
  agent-runs, git, spending, RC frames (tagged with the real session ID; the
  `handler_sse.go:283-285` re-stamping is removed).
- One SSE endpoint `/api/events` replaces the chat mirror
  (`/api/chat/messages`), the logs SSE, and per-session agent-runs SSE.
- Git status and spending become server-pushed events (server watches/computes,
  emits on change), replacing client 10s/60s polling.
- `seq` is a single global monotonic counter per server process, stamped on
  every envelope. It exists to detect gaps, not to replay: on reconnect the
  client reconciles state and refetches transcripts (below) instead of
  seq-based event replay. Git/spending events are emitted only for projects
  with at least one connected client showing them (subscriber-aware), not for
  every registered project.

### 3. Async bootstrap + turn state machine

- `POST /api/chat` validates, **persists the user message to the session**,
  registers the session, and returns **202 immediately** — a bootstrap failure
  after 202 never loses the message; retry re-uses it. Agent build runs in a
  goroutine emitting `session_bootstrap` stage events (`tools`, `mcp`,
  `model`).
- `mcpCache.wait()` gets a timeout (~30s): proceed without stragglers, emit a
  warning event.
- Turn lifecycle events: `turn_started` → `turn_heartbeat` (~10s while
  running) → `turn_done` | `turn_error`.
- Reconcile endpoint: `GET /api/sessions/:id/state` →
  `{bootstrap_stage, turn_active, last_seq}`. Reconcile = state fetch +
  transcript refetch, never event replay.

### 4. Frontend transport

- One `eventBus` module: single `EventSource` to `/api/events`, reconnect with
  backoff, routes by event type + session/project tag into stores.
- Deleted: `connectSessionMirror`, per-session `useAgentRuns` EventSources,
  `LogPanel` EventSource, git polling (`CoworkSidebar.tsx:121-138`), spending
  polling (`App.tsx:76`). Terminal WebSocket stays. Total: 2 long-lived
  connections.
- On reconnect: for every open session, reconcile via
  `GET /api/sessions/:id/state` **and refetch its messages** — events missed
  during the disconnect are recovered by refetch, not replay.
- Unknown-session events: loud `console.warn`, never a silent drop.

### 5. Status + streaming contract (frontend)

- **Status:** on tab activation (mount, session switch, project switch) fetch
  new `GET /api/sessions/:id/status` (includes `context_*`); events
  patch/invalidate. Placeholder tabs (`new-*` ids, no server session yet) skip
  the fetch and render project-level defaults; the slice is first populated by
  the `session_bootstrap`/`session_started` events after the first message. Removed: the mount-once seed (`App.tsx:186-203`), the
  sessionless status broadcast path, and the dual-source `fallbackContext` —
  one `sessionStatus` slice per session.
- **Streaming:** `isStreaming` derives from turn state, not a promise. Set on
  202/`turn_started`; cleared by `turn_done`/`turn_error`. Watchdog: no
  heartbeat for 30s → show "stalled" + auto-reconcile; reconcile reports no
  active turn → clear.

## Error handling

- Every dropped/unroutable event is logged (server: structured log; client:
  `console.warn`).
- Bootstrap failure emits `turn_error` carrying the failing stage.
- Bounded waits everywhere in the bootstrap path; timeouts surface as events,
  not silence.

## Testing

- **Go:** SessionManager cross-project resolution (load session from a
  non-workdir project; 404 only when truly missing); bootstrap timeout
  behavior; event envelope tagging incl. RC frames.
- **Frontend:** store tests for event routing (correct slice updated, unknown
  session warned) and reconcile clearing stale `isStreaming`.
- **Manual QA:** switch project → sidebar stays populated; new session in a
  second project → bootstrap stages visible, message round-trips; kill server
  mid-turn → spinner clears via reconcile.

## Implementation phasing

1. Backend SessionManager + per-session resolution (fixes cross-project 404s).
2. Unified event bus + `/api/events` (server side, old endpoints intact).
3. Async bootstrap + turn state machine + reconcile endpoint.
4. Frontend `eventBus` transport swap; delete old streams/polling.
5. Frontend status-on-activation + streaming watchdog; delete legacy paths.
6. RC-bridge registration as first-class session; delete forwarding/re-stamping.
   (Until this phase, RC frames keep the legacy re-stamping path — the
   at-source tagging in section 2 applies to headless emitters from phase 2 and
   to RC frames only from phase 6.)

Each phase independently shippable; old endpoints removed only after phase 4/5
land.
