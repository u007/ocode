---
type: Plan
title: Part 01 — Per-session todo reader and todo_updated event
description: Part 01 of the Pulse dashboard implementation plan; TDD steps ticked, commit left unticked (no commit exists yet).
tags:
  - pulse
  - plan
  - "2026-09-24"
timestamp: 2026-09-28T10:05:39Z
---
# Part 01 — Per-session todo reader and `todo_updated` event

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
(sections "Server → Sources", "`todo_updated` event").

## Context

Todo lists live at `<project-root>/.ocode/todo/<session-id>.md`
(`internal/tool/todo_store.go:22`), items formatted `- [x]` pending,
`- [x]` done, `- [•]` in progress. The existing `ReadTodoSnapshot()`
(`todo_store.go:962`) reads only the process-current session, so Pulse needs a
reader keyed by project root + session id. No SSE event exposes todos today.

## Files

- Modify: `internal/tool/todo_store.go` — add per-id summary reader.
- Test: `internal/tool/todo_store_summary_test.go` (new).
- Modify: `internal/server/event_bus.go:55-84` — register `"todo_updated"`
  in the allowed event set; do NOT add it to `liveFrameEvents`
  (`session_manager.go:130` area) — not replayed, same as `agent_activity`.
- Modify: `internal/server/tui_status.go` — add wire type `TodoUpdatedEvent`.
- Modify: `internal/server/handler.go:783-806` — publish after `tool_result`
  when the tool is `todowrite`.
- Test: `internal/server/todo_updated_event_test.go` (new).

## Interfaces

- Produces (Go, `internal/tool`): `ReadTodoSummary(projectRoot, sessionID string) (TodoSummary, bool, error)`.
  `TodoSummary{Done int; Total int; Current string; Items []TodoSummaryItem}`,
  `TodoSummaryItem{Text string; State string /* "pending"|"in_progress"|"done" */}`;
  `Current` is the first `- [•]` item text, empty if none. `bool` false = file absent (not an error).
  Parse failure returns an error naming the path.
- Produces (Go, `internal/server`): `TodoUpdatedEvent{SessionID, Done, Total, Current, Items}`
  with JSON keys `session_id`, `done`, `total`, `current`, `items`
  (`[{text, state}]`); SSE event name
  `todo_updated`.

## Steps

- [x] **Write failing tests for `ReadTodoSummary`** in a temp dir: mixed
  pending/done/in-progress file → correct counts, current and ordered items with states; file absent →
  `ok=false, err=nil`; unparsable content (e.g. binary / no list lines but
  non-empty garbage per existing parser's rejection rules) → error containing
  the path; empty list → `Total=0`.
- [x] Run `go test ./internal/tool -run TestReadTodoSummary` → FAIL.
- [x] **Implement** by reusing the existing item parser used by
  `writeTodoFile`/`todoWrite` (do not write a second parser). Resolve the
  directory with the same `.ocode/todo` path helper the store uses.
- [x] Run the test → PASS.
- [x] **Write failing server test**: feed the broadcast loop in
  `handler.go` an assistant message with a `ToolCalls` entry named
  `todowrite` (id X) followed by a `tool` message with `ToolID` X, with a
  todo file on disk; subscribe to the bus; expect one `todo_updated` with
  matching counts. Second case: a non-todo tool → no `todo_updated`.
- [x] Run `go test ./internal/server -run TestTodoUpdatedEvent` → FAIL.
- [x] **Implement**: in the message loop at `handler.go:783`, keep a
  per-batch map of `ToolCall.ID → name` from assistant messages; when a
  `tool` message's `ToolID` maps to `todowrite`, call `ReadTodoSummary`
  with the session's project root and publish `todo_updated` via
  `h.broadcastEvent`. On reader error, log via `debuglog` with session id,
  path and error at warn, and publish nothing.
- [x] Run the test → PASS; run `go test ./internal/server/... ./internal/tool/...`.
- [x] Commit: `feat(server): publish todo_updated SSE event with per-session todo summary`.
