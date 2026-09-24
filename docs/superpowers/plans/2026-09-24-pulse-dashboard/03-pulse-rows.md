# Part 03 — Pure Pulse row builder

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("`GET /api/pulse`" → PulseRow, status derivation, sort, live inclusion).

## Context

Keep all derivation logic pure and I/O-free so it is testable without a
running server. The handler (a later part) gathers inputs and calls these.

## Files

- Create: `internal/server/pulse_rows.go`
- Test: `internal/server/pulse_rows_test.go`

## Interfaces

- Produces types (JSON tags snake_case exactly as listed):
  - `PulseStatus` string constants: `needs_permission`, `needs_question`,
    `running`, `error`, `idle`.
  - `PulseTask{Kind string /* "todo"|"tool"|"text" */; Text string}`
  - `PulseTodo{Done, Total int; Current string; Items []PulseTodoItem}`,
    `PulseTodoItem{Text, State string}` (JSON `text`, `state`; state ∈
    `pending|in_progress|done`)
  - `PulseAsk{Kind string /* "permission"|"question" */; Summary string}`
  - `PulseRow{SessionID, ProjectPath, Title string; Status PulseStatus; CurrentTask *PulseTask; Todo *PulseTodo; PendingAsk *PulseAsk; TurnStartedAt, UpdatedAt time.Time; ChildCount int}`
  - `PulseInput{SessionID, ParentID, ProjectPath, Title string; Running bool; LastTurnErr string; PendingAsk *PulseAsk; ActiveTool string; ActiveToolArgs string; LastAssistantLine string; Todo *PulseTodo; TurnStartedAt, UpdatedAt time.Time}`
- Produces functions:
  - `derivePulseStatus(in PulseInput) PulseStatus` — first match: pending
    permission → `needs_permission`; pending question → `needs_question`;
    `Running` → `running`; `LastTurnErr != ""` → `error`; else `idle`.
  - `derivePulseTask(in PulseInput) *PulseTask` — needs-you rows: nil (the
    ask carries the text); else in-progress todo `Current` → `todo`; else
    `ActiveTool` (+ args truncated to 80 runes) → `tool`; else
    `LastAssistantLine` → `text`; else nil.
  - `buildPulseRows(inputs []PulseInput, scope string, now time.Time) []PulseRow`
    — drops children (non-empty `ParentID`) and adds `ChildCount` to the
    parent; applies inclusion (`live`: non-idle/error always, idle/error
    only if `UpdatedAt` ≥ now−24h; `all`: `UpdatedAt` ≥ now−7d or
    non-idle/error); sorts per rank → `UpdatedAt` desc → `SessionID` asc.
  - `pagePulseRows(rows []PulseRow, cursor string, limit int) (page []PulseRow, next string, err error)`
    — cursor = base64url of `rank|updated_at RFC3339Nano|session_id` of the
    last returned row; resumes strictly after it; invalid cursor → error.
    `next` is "" when no more rows.

## Steps

- [ ] **Write failing table tests** for `derivePulseStatus`, including
  precedence: pending permission + `Running` → `needs_permission`;
  question + error → `needs_question`.
- [ ] **Write failing tests** for `derivePulseTask`: each fallback tier,
  tool-args truncation at 80 runes (multibyte-safe), needs-you → nil.
- [ ] **Write failing tests** for `buildPulseRows`: child excluded and
  counted; 23h-old idle included in live, 25h-old idle excluded from live
  but included in all; 8-day-old idle excluded from all; sort order with
  equal `UpdatedAt` tiebroken by id.
- [ ] **Write failing tests** for `pagePulseRows`: 120 rows at limit 50 →
  three pages, no duplicates or gaps, last `next` = ""; garbage cursor →
  error.
- [ ] Run `go test ./internal/server -run TestPulse` → FAIL.
- [ ] **Implement** the file.
- [ ] Run → PASS.
- [ ] Commit: `feat(server): add pure Pulse row derivation, sort and cursor paging`.
