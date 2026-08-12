# Part 01 — Backend SessionManager & Cross-Project Resolution

**Goal:** A server-side registry that is the single authority for
session ID → project root + agent lifecycle, so every session-scoped handler
resolves sessions from any registered project (no more cross-project 404s),
with idle agent eviction.

**Context (self-contained):**
- Today one server serves one project root; session *listing* is per-project
  (`internal/server/handler_projects.go:99` → `session.ListRefsForDir`), but
  session *loading* resolves against the server's own workdir
  (`internal/session/session.go:287 Load` → `GetStorageDir()` →
  `effectiveWorkDir()`), so sessions from other projects 404 on
  `GET /api/sessions/:id`, `.../context`, `POST .../message`
  (`internal/server/handler.go:487, 552, 916`).
- The persisted server-side project list already exists: `h.projects`
  (`internal/projects`), used by `HandleListProjects`.
- Agent sessions are built per session in
  `internal/server/agent_session.go` (`ensureAgentSession`,
  `buildAgentSession`).
- Constraints: on-disk session format unchanged; fail loudly; idle agent
  eviction after 30 min without an active turn.

**Files:**
- Create: `internal/server/session_manager.go` (+ `session_manager_test.go`)
- Modify: `internal/session/session.go` — add a load-for-dir variant beside
  `Load`
- Modify: `internal/server/handler.go` — session-scoped handlers resolve via
  the manager
- Modify: `internal/server/agent_session.go` — agent workdir comes from the
  registry entry's project root

**Interfaces produced (used by later parts):**
- `SessionManager.Resolve(sessionID) (entry, error)` — finds the session's
  project root by checking each registered project's storage dir, caches the
  mapping, errors only when the session exists in no registered project.
- `SessionManager.Register(sessionID, projectRoot)` — binds a new session.
- Registry entry carries: project root, lazily-built agent session,
  bootstrap/turn state placeholders (filled in Part 03), last-activity time.
- `SessionManager.EvictIdle()` — releases built agent sessions idle > 30 min
  (no active turn); registry entry and on-disk session remain; agent rebuilds
  on next message.

## Tasks

- [ ] **Task 1: load-for-dir session loading.** Test-first: loading a session
  that lives under a different project's storage dir succeeds when the dir is
  given explicitly, and still fails for a nonexistent ID. Implement the
  for-dir variant in `internal/session` reusing the existing storage-dir
  helpers (`GetStorageDirForPath`); do not change `Load`'s behavior yet.
  Verify `go test ./internal/session/...`. Commit.

- [ ] **Task 2: SessionManager resolution.** Test-first with two temp project
  roots each containing a session on disk and both registered in a test
  project store: `Resolve` finds each session's correct root; unknown IDs
  error; second `Resolve` of the same ID hits the cache (assert via a
  counting stub or by removing the file after first resolve). Implement
  `session_manager.go` searching `h.projects`-style registered roots.
  Verify `go test ./internal/server/...`. Commit.

- [ ] **Task 3: handlers resolve via manager.** Test-first at the HTTP layer:
  `GET /api/sessions/:id` and `GET /api/sessions/:id/context` succeed for a
  session belonging to a registered non-workdir project; 404 only for truly
  missing sessions. Wire the manager into `Handler`, replace
  `effectiveWorkDir()`-based lookup in the session-scoped handlers
  (`handler.go:487, 552, 916` areas). `POST /api/chat` accepts a
  `project_path` field and calls `Register` for new sessions; the agent's
  workdir in `buildAgentSession` comes from the registry entry. Verify
  `go test ./internal/server/...`. Commit.

- [ ] **Task 4: idle eviction.** Test-first: an entry with a built agent and
  last-activity older than the threshold is released by `EvictIdle` (agent
  handle nil afterwards, entry still resolvable); an entry with an active
  turn is not evicted. Implement eviction plus a periodic trigger wired into
  the server lifecycle (respect existing shutdown handling). Log evictions.
  Verify `go test ./internal/server/...`. Commit.

- [ ] **Task 5: regression + ship.** Run `go test ./internal/...`. Manual
  check: desktop app, add a second project, open one of its listed sessions —
  transcript and context load (previously 404). Update
  `docs/` if any existing doc describes single-project session resolution.
  Commit.
