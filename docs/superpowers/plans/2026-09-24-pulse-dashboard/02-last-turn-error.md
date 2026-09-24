# Part 02 — Record last turn error on `sessionEntry`

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("Server → Sources", status derivation `error`).

## Context

`sessionEntry` (`internal/server/session_manager.go:48`) tracks
`turnActive`, `turnStartedAt`, `turnEndedAt` via `setTurnActive`
(`session_manager.go:380`) but only records `bootstrapErr` for failures.
Pulse's `error` status needs to know whether the most recent turn failed.
`turn_error` is published from the agent turn path in
`internal/server/agent_session.go` (find the `"turn_error"` publish site).

## Files

- Modify: `internal/server/session_manager.go` — new field + setter +
  exposure through `SnapshotEntry`/`Snapshot`.
- Modify: `internal/server/agent_session.go` — call the setter where
  `turn_error` is published.
- Test: `internal/server/session_manager_turn_error_test.go` (new).

## Interfaces

- Produces: `sessionEntry.lastTurnErr string` ("" = last turn did not fail),
  set by `(*SessionManager).setTurnError(sessionID, msg string)`, cleared by
  `setTurnActive(sessionID, true)`. Readable from the values returned by
  `Snapshot()` and `SnapshotEntry()`.

## Steps

- [ ] **Write failing tests**: register an entry; `setTurnError` → snapshot
  shows the message; subsequent `setTurnActive(id, true)` clears it;
  `setTurnActive(id, false)` after an error keeps it; unknown session id is
  a no-op (no panic, no entry created).
- [ ] Run `go test ./internal/server -run TestSessionManagerTurnError` → FAIL.
- [ ] **Implement** field + setter under `m.mu`; clear in the `active` branch
  of `setTurnActive`.
- [ ] **Wire** the setter at the `turn_error` publish site in
  `agent_session.go` with the same error text sent on the event.
- [ ] Run the test → PASS; run `go test ./internal/server/...`.
- [ ] Commit: `feat(server): track last turn error per live session`.
