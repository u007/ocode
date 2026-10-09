---
type: Concept
title: Web/Desktop Server Locking and Liveness Rules
description: 'Rules for internal/server: Handler.mu is a map lock never a work lock, lock order, pending-ask recovery, async turns, per-project subprocess bounds, SSE event registration, turn heartbeat, routes and middleware.'
resource: CLAUDE.md
tags:
  - server
  - concurrency
  - web
  - desktop
timestamp: 2026-09-30T07:37:09Z
---
# Web/Desktop Server Locking and Liveness Rules

`internal/server.Handler.mu` guards the `h.agents` map (and a few small config
fields) and is taken by **every** endpoint — the session list, the run-state
polls behind the desktop dock badge, the config routes, the permission and
question resolvers. Holding it across slow work therefore does not slow one
session down, it freezes all of them: the recurring "a session is stuck and
won't run while another session is running" bug class.

Rules for anything in `internal/server`:

- **Never hold `h.mu` across an LLM call, a `Step`, a compaction, a recap, or
  agent construction.** Agent construction is slow in ways that are easy to
  miss: `tool.InitBuiltinTools` and `LoadExternalTools` touch the filesystem
  and can spawn plugin processes, `agent.NewAgent` may auto-start a local model
  server, and `mcpCache.wait()` blocks until the process-wide MCP enumeration
  finishes (unbounded — an unreachable MCP server makes it slow). Build outside
  the lock with `buildAgentSession`, then insert with `registerAgentSession`,
  which double-checks the map and shuts down the loser of a construction race.
  `ensureAgentSession` / `getOrCreateAgentSession` wrap both; use them.
- **Per-turn work belongs under `agentSession.mu`.** That serializes turns
  within one session and leaves different sessions fully parallel.
- **Lock order is `agentSession.mu` → `h.mu`, never the reverse.** A turn holds
  the session lock and then takes `h.mu` (via the title generator), so taking a
  session lock while holding `h.mu` deadlocks. To scan sessions for a pending
  ask, snapshot the candidates under `h.mu`, release it, then inspect each
  candidate under its own lock — that is what `findPendingSession` does. Reading
  `as.messages` under `h.mu` alone is a data race against a running turn. The
  same order applies to `SessionManager.mu` vs `agentSession.mu` (e.g.
  `EvictIdle` checking for a pending ask before releasing an agent).
- **A tool-call round can pause with more than one unresolved `PERMISSION_ASK`/
  question sentinel at once**, not just as the literal last message — parallel
  tool dispatch runs several calls before the pause check, and each one needing
  approval appends its own sentinel. Never assume "the pending ask" is
  `messages[len(messages)-1]`; scan the whole trailing tool-call round
  (`trailingToolRunStart` in `run_states.go`) by `ToolID` instead, and don't
  re-`Step()` the turn until every ask in that round is resolved — stepping on
  top of one still-raw sentinel feeds the model a malformed tool result, which
  it typically "fixes" by retrying the call and raising a brand new ask (looks
  like the same permission dialog popping back up after being answered).
- **A pending ask must be recoverable from live session state, not only from
  the persisted transcript:** `GET /api/sessions/{id}/state` carries
  `pending_asks` read from the live agent transcript (`livePendingAsks`), and
  any new surface that consumes asks keeps that fallback. Do not "fix" the
  async path by returning 409 pre-dispatch — the async contract deliberately
  persist-then-202s and leaves the refused message queued
  (`TestAsyncTurnRefusedWhilePermissionPending`). Triggers and failure modes:
  `docs/gotchas/pending-ask-recovery-live-session-state.md`.
- **Reading a live session's `as.messages` from an HTTP handler uses a
  non-blocking `as.mu.TryLock()`, never `Lock()`.** `runTurn` holds `as.mu` for
  the whole turn, and state endpoints are polled by the browser and the turn
  watchdog, so a blocking read re-creates the "stuck session" symptom. When the
  try-lock fails, skip the live read and fall back to persisted state. Follow
  `livePendingAsks`. A **write** that must know the pending-ask state (pulse
  `session_send`) does not fall back: a failed try-lock refuses the send, because
  skipping the ask check would dispatch over an unresolved ask. The same rule covers **every** fan-out over `h.agents`
  from a request or poll path, not just message reads: `PendingPermissionAsks`
  (desktop badge watcher, quit dialog) uses `TryLock` (mid-turn ⇒ not pending),
  `applyLimitsToLiveSessions` (Settings save) takes no `as.mu` at all because
  `SetMaxSteps`/`SetMaxConcurrent` are atomic, and `applyRedactionToLiveSessions`
  applies each session in a background goroutine. A blocking `as.mu.Lock()` in
  any of these froze the desktop UI and the Settings save until the running
  turn finished (2026-09-30).
- **Do not pin an HTTP connection for the length of a turn.** A browser allows
  six concurrent connections per origin over HTTP/1.1 (`ocode serve` and share
  URLs are plain HTTP; only the desktop webview gets HTTP/2 — see
  `docs/gotchas/desktop-webview-http2.md`), so a request held open per running
  turn starves the other sessions' requests. `POST /api/chat` and
  `POST /api/sessions/{id}/message` therefore take `"async": true` (the web
  client always sets it) and reply `202` once the turn is dispatched; output
  reaches the browser over the SSE mirror. The synchronous path stays for
  non-browser callers (scheduler, Telegram, external API clients).
- **One project must never block another: bound every subprocess and fan out
  shared loops.** `h.mu` is process-wide, so anything held across slow work
  freezes unrelated projects and sessions — including a background emitter
  goroutine, which has no request context to cancel it. Any new per-project
  work driven from a shared loop needs a per-item deadline
  (`exec.CommandContext`, cf. `gitStatusTimeout`) plus per-item fan-out
  (`forEachGitStatusConcurrently`, shared state single-writer on the loop
  goroutine), never a bare `exec.Command`. Case study:
  `docs/gotchas/project-endpoint-isolation.md`.

- **Registering a session-scoped SSE event is a two-part change.**
  `sessionScopedEvents` (`event_bus.go`) is the allowlist of event types that
  must carry a session id — publishing one of those without an id is an error
  (`event_bus.go`), and a scoped event whose NAME is absent from the map routes
  wrongly. Separately, momentary events (`agent_activity`, `todo_updated`) are
  deliberately NOT in `liveFrameEvents` (`session_manager.go`): replaying them on
  a reconnect shows stale mid-turn state. A new event name must be placed in both
  maps deliberately, not by pattern-matching an existing one.
- **Every path that holds `turnActive=true` must publish `turn_heartbeat`.**
  `runTurn` starts the ticker, but the permission-answer and question-answer
  *continuations* hold turn state too. A continuation without a heartbeat makes
  the client watchdog flag a false "stalled" after 30s while the turn is running
  fine. Use the shared `startTurnHeartbeat` helper (`agent_session.go`) rather
  than setting turn state by hand.
- **Push emitters only cover VIEWED projects.** `EventBus.ViewedProjects()`
  gates the git/spending emitters, so a project with no open tab receives no
  `git_status` push at all and must poll instead — a sidebar badge that needs
  live data has to fetch for itself rather than wait for an event.
- **Routes all live in `Server.registerRoutes()` (`server.go`), behind one of
  four middleware wrappers** (`authMiddleware`, `mediaAuthMiddleware`,
  `healthMiddleware`, `pluginAuthMiddleware` — health is the unauthenticated
  one). A new endpoint must pick its wrapper deliberately, and a
  session/project-scoped route must accept and thread `?host=` (see
  `concepts/web-session-host-scoping.md`).
