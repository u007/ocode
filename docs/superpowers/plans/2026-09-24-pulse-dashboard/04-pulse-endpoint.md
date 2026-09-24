# Part 04 — `GET /api/pulse` handler

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("`GET /api/pulse`", "Errors and edge cases", performance budget).

## Context and inputs available

- `SessionManager.Snapshot()` (`internal/server/session_manager.go:574`) →
  live entries: session id, project root, `turnActive`, `turnStartedAt`,
  `turnEndedAt`, `lastActivity`, `lastTurnErr` (field added earlier:
  `sessionEntry.lastTurnErr string`, "" = no error).
- `Handler.RunStates()` (`internal/server/run_states.go:42`) → running agents.
- `tailIsPermissionAsk(msgs)` (`run_states.go:135`) and
  `tailIsQuestionAsk(msgs)` (`handler_questions.go:74`) on the session's
  message tail; the ask parsers `parsePermissionAsk` / `parseQuestionAsk`
  (used in `handler.go` ~790) give the summary text.
- Agent activity snapshot (`internal/agent/activity.go:15`) → active tool name.
- `tool.ReadTodoSummary(projectRoot, sessionID string) (TodoSummary, bool, error)`
  with `TodoSummary{Done, Total int; Current string; Items []TodoSummaryItem{Text, State}}` — `bool` false means
  no todo file.
- Pure builders in `internal/server/pulse_rows.go`: `PulseInput`,
  `PulseRow`, `PulseAsk`, `PulseTodo`,
  `buildPulseRows(inputs, scope, now) []PulseRow`,
  `pagePulseRows(rows, cursor, limit) ([]PulseRow, string, error)`.
- `scope=all` disk source: the same session-listing path used by
  `GET /api/sessions` (`handler.go:1036`) — must NOT call the per-project
  list endpoint per project (see `docs/gotchas/web-all-sessions-dialog-slow.md`).
  Parent id for child detection comes from session refs (same source the
  web uses to hide children in `SessionDialog.tsx:42`).

## Files

- Create: `internal/server/handler_pulse.go`
- Modify: `internal/server/server.go` (route block near :215) — register
  `GET /api/pulse` behind `s.authMiddleware`.
- Test: `internal/server/handler_pulse_test.go`

## Interfaces

- Produces: HTTP `GET /api/pulse?scope=&cursor=&limit=` →
  `200 {"items": PulseRow[], "next_cursor": string|null}`; `400` with a
  JSON error for bad `scope`, `limit` (non-integer or outside 1–100), or
  cursor.

## Steps

- [ ] **Write failing handler tests** (httptest against the handler with a
  seeded `SessionManager`): one running, one pending permission, one idle
  errored, one idle 30h old, one child of the running session →
  `scope=live` returns permission first, running with `child_count=1`,
  errored; excludes 30h idle and the child.
- [ ] Tests for `scope=all` including the 30h session; for `scope=bogus`,
  `limit=0`, `limit=101`, `limit=abc`, bad cursor → 400.
- [ ] Test: todo file unparsable for one session → still 200, that row has
  `todo: null`, warn logged (assert via the debuglog test hook the package
  already uses, or the log buffer pattern in existing handler tests).
- [ ] Test: `next_cursor` is JSON `null` on the final page.
- [ ] Run `go test ./internal/server -run TestHandlePulse` → FAIL.
- [ ] **Implement**: parse/validate query; gather `PulseInput` per live
  entry (reading only the message tail needed for ask detection and last
  assistant line); for `all`, merge disk sessions within 7 days by
  `session_id` with live data winning; call `buildPulseRows` then
  `pagePulseRows`; encode.
- [ ] Run → PASS; run `go test ./internal/server/...`.
- [ ] **Measure**: add a benchmark `BenchmarkHandlePulseLive` with ~50 live
  entries; confirm well under 200ms. Time `scope=all` against the real
  local session store once (manual `curl -w '%{time_total}'`) and record
  the number in the Part 10 concept doc notes. If `all` exceeds 1s,
  stop and report to the user before optimizing.
- [ ] Commit: `feat(server): add GET /api/pulse cross-project live sessions endpoint`.
