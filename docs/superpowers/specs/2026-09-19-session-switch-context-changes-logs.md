# Session-switch fixes: context gauge, Changes tab, Logs scoping

Status: approved 2026-09-19
Related: `docs/gotchas/compaction-context-gauge-stale-after-splice.md`,
`PLAN-changes-tab.md`, part 05 of
`docs/superpowers/plans/2026-08-12-multiproject-event-architecture/`.

## Problem

Switching chat or project in the web/desktop UI surfaced three symptoms:

1. The Context gauge becomes "Usage unknown — no provider reading yet"
   (StatusBar shows `ctx: ?/1M`).
2. The Changes tab becomes empty.
3. The Logs tab is cluttered with other chat sessions' entries.

### Root causes (verified against the running server)

- **Context**: commit `4788a78b` removed the persisted-transcript estimate
  fallback from `applySessionContext`
  (`internal/server/handler_session_state.go`) and `HandleSessionContext`
  (`internal/server/handler.go`). `context_current_tokens` now comes only from
  the RC/TUI bridge or a live agent's `LastInputTokens()`; with no live agent
  it is `0` and omitted, and the sidebar renders "unknown". Live proof:
  `GET /api/sessions/ses_2026-09-17-114850-67ab3570/status` returned
  `context_max_tokens: 1000000`, `context_current_tokens: <omitted>`.
- **Changes**: `changesSnapshot` (`internal/server/handler_changes.go`) reads
  `activeAgentForRuns(sessionID)` → `h.agents[sessionID]`. The registry is
  journaled to `snapshots.sqlite` and rehydrated only on agent build
  (`Agent.SetSessionID` → `Store.SwitchSession`); opening a session never
  builds an agent, so restored/evicted sessions return `[]`. Live proof:
  `GET /api/changes?session=ses_2026-09-17-114850-67ab3570` → `[]`.
- **Logs**: both `internal/server/handler_logs.go` and `LogPanel.tsx`
  deliberately keep untagged entries for every session, but session-scoped
  emitters produce untagged entries: `TokenUsage.DebugLog`
  (`internal/agent/telemetry.go`) uses the package-level `emitDebug`; `task`
  sub-agents (`internal/agent/subagent.go`) are built without `SetSessionID`;
  auxiliary clients (title/judge/advisor/compaction/discovery) are untagged.
  Live proof: `GET /api/logs` returned 500 entries, 193 untagged (LLM 54,
  TOKENS 47, TOOL 38, PERMISSION 19, TOOLS 18, AGENT 13…).
- **Remote projects compound all three**: the status/changes/logs panels do
  not pass the project SSH host (`useChat`, `FileTree`, `GitPanel` do), so
  requests for a remote session hit the local server: status 404, changes
  `[]`, logs = local ring buffer.

## Non-goals

- Hiding process-global/untagged log entries; startup/discovery entries stay
  visible in every tab (a `system` filter chip is a possible follow-up).
- Persisting `LastInputTokens` in session metadata (cleaner long-term, but
  needs schema work).
- Full undo support for rehydrated (non-live) changes; undo still requires a
  live agent.

## Design

### 1. Tag session-scoped log entries (server)

- `internal/agent/subagent.go`: after `shareChangeTrackingFrom(parent)`, set
  `subAgent.SetSessionID(parent.sessionID)` when non-empty. The shared store is
  already bound to that session so `SwitchSession` is a no-op; mirrors
  `ask.go:201`.
- `internal/agent/telemetry.go`: `TokenUsage.DebugLog(model, emit)` takes an
  emitter (`func(kind, msg string)`); the six `client.go` call sites pass
  `c.emitDebug` so TOKENS rows carry the client's session.
- Tests: sub-agent attribution; telemetry emitter routing.

### 2. Thread the project host into session-scoped panels (web)

- `api/client.ts`: optional `host` on `listChanges`, `getChangeDiff`,
  `undoChangeFile`, `undoChangeBlock`; add `getLogs(sessionId, host)` /
  `clearLogs(sessionId, host)`.
- `ChangesPanel`/`ChangesDiffView`/`ChangesFileList`, `LogPanel`,
  `useSessionStatus`, `CoworkSidebar`, `StatusPanel`: pass the host resolved by
  `resolveSessionHost(projectState, tabId)` (the same single-match trust rule as
  `useChat`/`FileTree`/`GitPanel`).
- Tests: assert `/api/remote/<host>/...` prefixing.

### 3. Persisted-transcript fallback for the context gauge (server)

- `applySessionContext` and `HandleSessionContext`: when neither the RC bridge
  nor a live agent offers a reading, estimate current tokens from the persisted
  transcript (`chars/4`). Provider/TUI values remain authoritative.
- Update the pinning test in `handler_session_state_test.go` to expect the
  estimate and add a case proving provider readings still win.

### 4. Journal-backed Changes for non-live sessions (server)

- `changesSnapshot`: when `activeAgentForRuns` is nil, compute the project's
  snapshots dir (`paths.GlobalDataDir()/project/<slug>/snapshots`), open a temp
  `snapshot.Store`, `SwitchSession(sessionID)` to rehydrate the journal, attach
  it to a throwaway `changes.Registry`, and list. Rows are display-only
  (`Undoable=false`) because the undo handlers resolve a live agent.
- Tests: seed a session journal via a real `Store`, drop the agent, assert the
  listing is non-empty and not undoable.

## Validation

- `go test ./internal/agent/ ./internal/server/ ./internal/changes/ ./internal/snapshot/`
  for the touched packages (note pre-existing concurrent-WIP failures, if any).
- `cd web && npm run typecheck && npm run test` for the SPA changes.
- Live re-probe of `/api/changes?session=…`, `/api/sessions/…/status`, and
  `/api/logs` against the running app to confirm the three symptoms are gone.
