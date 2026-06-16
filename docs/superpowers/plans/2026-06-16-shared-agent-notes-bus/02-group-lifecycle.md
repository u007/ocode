# Part 02 — Group detection, `shared_notes` toggle, lifecycle, touch hooks, failure/cancel/cap

**Design:** `docs/superpowers/specs/2026-06-16-shared-agent-notes-bus-design.md`

**Context:** Part 01 produced the `internal/notebus` primitive (append-only log, seq,
deltas, sidecar). This part wires a bus into a real concurrent agent group: it is
created when the `Step()` parallel-tool block (`internal/agent/agent.go:651`) dispatches
2+ `task`/`agent` calls with notes enabled, handed to each child agent, fed automatic
write-touches, and torn down (flush + archive) when the group finishes — including on
agent failure, user cancellation, and log-size-cap hits.

---

### Task 1: `shared_notes` toggle on the group-spawn path

**Files:**
- Modify: `internal/agent/subagent.go` (`TaskTool.Definition` `:120`, `Execute` `:175`)
- Modify: `internal/agent/agent.go` (parallel-tool block `:651`)
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**
  - The `task` tool definition accepts an optional `shared_notes` boolean (default
    false), documented in the schema.
  - When `Step()` runs a parallel batch containing 2+ subagent calls and at least one
    carries `shared_notes:true`, a single bus is created for that batch (assert via an
    injected bus factory / hook).
  - A batch with one subagent call, or with `shared_notes` unset, creates **no** bus.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/agent -run 'TestSharedNotesToggle|TestGroupBusCreation' -v`.

- [ ] **Step 3: Implement**
  Add `shared_notes` to the task args struct and schema. In the parallel-tool block,
  after partitioning, detect the concurrent-subagent group and, when enabled, construct
  one `notebus.Bus` (group id derived from parent session + batch index) before spawning
  the goroutines. Keep single-call and disabled paths unchanged (no bus).

- [ ] **Step 4: Verify** — toggle + creation tests pass; existing parallel-tool tests stay green.

---

### Task 2: Hand the bus to each child run + register agent ids

**Files:**
- Modify: `internal/agent/subagent.go` (`executeSubAgentWithTranscript` `:373`, child spawn `:318`)
- Modify: `internal/agent/child_session.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**
  - Each child run in an enabled group receives a stable short agent id (`a1`, `a2`, …)
    and a handle to the shared bus.
  - Ids are deterministic and unique within the group (stable across a reload).
  - Children in a disabled/single group receive no bus handle (nil) and behave exactly
    as today.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/agent -run 'TestGroupAgentIds|TestBusHandoff' -v`.

- [ ] **Step 3: Implement**
  Assign ids at group construction; thread the bus + id into each child agent
  (constructor field, not a global). Record id↔child-session-id in child metadata for
  reconcile and reload.

- [ ] **Step 4: Verify** — id/handoff tests pass.

---

### Task 3: Automatic write-touches (writes only)

**Files:**
- Modify: `internal/agent/agent.go` (tool-result path in the Step loop, after `:690`)
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**
  - When a child in an enabled group completes a `write`, `edit`, or `apply_patch` tool
    call, the bus gains exactly one `<oc-touch act="edit">` entry with the child's id and
    the target file path.
  - `read`, `glob`, `grep`, and all non-write tools produce **no** touch entry.
  - No bus → no touch logic runs (no nil deref, no overhead).

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/agent -run TestWriteTouches -v`.

- [ ] **Step 3: Implement**
  After a successful write-class tool call, if a bus + id are present, append a touch.
  Derive the file path from the tool args (one entry per touched file). Reads are never
  logged — this preserves the "nothing changed → inject nothing" cache behavior.

- [ ] **Step 4: Verify** — write-only touch test passes.

---

### Task 4: Per-agent completion tracking + teardown (flush/archive)

**Files:**
- Modify: `internal/agent/subagent.go` (group join/result collection)
- Modify: `internal/notebus/notebus.go` (completion + close API)
- Test: `internal/agent/agent_test.go`, `internal/notebus/notebus_test.go`

- [ ] **Step 1: Write the failing test**
  - The bus records each agent's terminal status: `completed` or `failed` (with reason).
  - When the group's last child returns, the bus is closed: persistence flushed, log
    archived (sidecar finalized), goroutine exited; a second close is a no-op.
  - An agent that returns an error is marked `failed`, and its id is queryable for the
    coverage-gap report (Part 05).

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/agent -run TestGroupTeardown -v` and `./internal/notebus -run TestClose -v`.

- [ ] **Step 3: Implement**
  Record terminal status as each child goroutine returns; close the bus once all have
  reported (success or failure). Flush + finalize sidecar on close.

- [ ] **Step 4: Verify** — teardown + status tests pass.

---

### Task 5: Cancellation flush + log-size cap

**Files:**
- Modify: `internal/notebus/notebus.go`
- Modify: `internal/agent/agent.go` (cancellation path)
- Test: `internal/notebus/notebus_test.go`

- [ ] **Step 1: Write the failing test**
  - On context cancellation, the bus flushes what it has, marks unreported agents
    `cancelled`, and exits without losing already-appended entries.
  - A soft per-group entry cap: once reached, further `Append` is rejected with a clear
    error and a single `log` line records how many were dropped (no silent truncation).
  - The cap rejection does not crash the owner goroutine; subsequent reads still work.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/notebus -run 'TestCancelFlush|TestSizeCap' -v`.

- [ ] **Step 3: Implement**
  Add the cap constant + drop counter; on cancel, flush and mark statuses. Log drops via
  the standard `log` package (lands in the TUI debug panel; never raw stdout).

- [ ] **Step 4: Verify** — cancel + cap tests pass.

---

## Part 02 Done When

- A 2+ subagent parallel batch with `shared_notes:true` creates exactly one bus; single
  / disabled batches create none and are byte-for-byte unchanged in behavior.
- Each child has a stable id + bus handle; write-class tools auto-emit touches; reads do
  not.
- Group teardown flushes/archives on success, failure, cancellation; size cap rejects
  loudly with a logged drop count.
- All new tests pass under `-race`; existing agent tests stay green.
