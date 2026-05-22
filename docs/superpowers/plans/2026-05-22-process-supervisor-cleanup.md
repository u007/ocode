# Process Supervisor Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce a single session-scoped process supervisor that tracks and cleans up all ocode-owned child processes so exits, resets, and signals do not leave zombies or orphaned children.

**Architecture:** Add one supervisor as the lifecycle owner for child processes and keep existing process registries as UI-facing views over that lifecycle state. Route every spawn path through supervisor-aware constructors, then replace duplicated quit/reset teardown with one shared cleanup path in the TUI and signal handling in `tui.Run()`.

**Tech Stack:** Go, Bubble Tea, `os/exec`, Unix process-group handling, existing `internal/tool`, `internal/agent`, and `internal/tui` packages.

---

### File Structure

**Create:**
- `internal/tool/process_supervisor.go` — session-scoped child lifecycle owner, shutdown sequencing, idempotency, termination policy, reap/wait handling.
- `internal/tool/process_supervisor_test.go` — lifecycle-focused tests for running, finished, failed-to-start, and shutdown behavior.

**Modify:**
- `internal/tool/process.go` — background process registry becomes supervisor-backed for spawn, status, and retained output.
- `internal/tool/process_tools.go` — keep tool behavior stable while reading supervisor-backed process state.
- `internal/tool/exec.go` — foreground shell execution paths gain supervisor registration hooks where ocode owns the child.
- `internal/agent/agent.go` — agent shutdown cancels work but delegates process lifecycle teardown to the shared supervisor.
- `internal/agent/agent_runs.go` — subagent cancellation cooperates with supervisor-owned child state instead of independently killing processes.
- `internal/tui/model.go` — add one generic cleanup method and route all exit/reset/rebuild paths through it.
- `internal/tui/tui.go` — install signal handling and deferred cleanup around Bubble Tea program execution.
- `internal/tool/process_test.go` — update existing background process tests to the new ownership model.
- `internal/tui/model_test.go` — add tests for shared cleanup routing from quit/reset paths.

**Potentially Modify:**
- any helper tied to editor launching if the current editor path creates `exec.Cmd` outside the supervisor-aware flow.

---

### Task 1: Add the Session-Scoped Process Supervisor

**Files:**
- Create: `internal/tool/process_supervisor.go`
- Test: `internal/tool/process_supervisor_test.go`

- [ ] Define the supervisor API around registration, terminal-state updates, lookup, shared shutdown, and retained records for the full session.
- [ ] Model child metadata needed by the design: command/name, kind, process handle, process-group behavior, timestamps, status, exit code, and retention state.
- [ ] Implement idempotent shutdown with the required sequence: freeze registration, snapshot children, cancel scheduling callback(s), graceful terminate, bounded wait, force kill, reap, mark terminal state.
- [ ] Keep terminal records available for the full session instead of pruning them on completion.
- [ ] Write focused tests first for idempotent shutdown, skipping finished children, force-killing running children, and preserving finished logs.
- [ ] Run `go test ./internal/tool/...` and confirm the new supervisor tests pass.

**Verify:**
- `go test ./internal/tool/...`

---

### Task 2: Make Background Process Management Supervisor-Backed

**Files:**
- Modify: `internal/tool/process.go`
- Modify: `internal/tool/process_tools.go`
- Modify: `internal/tool/process_test.go`

- [ ] Refactor `ProcessRegistry` so it becomes a status/output view over supervisor-managed child records instead of the primary lifecycle owner.
- [ ] Route `StartBackground` through supervisor registration before command start, and preserve the existing process IDs and output semantics where practical.
- [ ] Keep `bash_output`, `kill_shell`, `Output`, `Dump`, and `Snapshot` behavior stable while sourcing status from supervisor-backed records.
- [ ] Ensure failed-to-start processes are retained as terminal records with readable output.
- [ ] Update existing process tests to assert the same user-visible behavior plus the new retained-terminal-record semantics.
- [ ] Run the targeted process tests and then the full `internal/tool` package tests.

**Verify:**
- `go test ./internal/tool/... -run 'TestProcess|TestBash|TestKillShell'`
- `go test ./internal/tool/...`

---

### Task 3: Connect Agent and Subagent Cleanup to the Shared Supervisor

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_runs.go`

- [ ] Inject or share the supervisor into the main agent and any subagent-owned process launch path.
- [ ] Narrow `Agent.Shutdown()` so agent cancellation and subagent loop cancellation remain there, but process termination authority lives in the supervisor.
- [ ] Update `CancelAll()` behavior for async runs so it no longer independently owns process killing in a way that conflicts with supervisor shutdown.
- [ ] Preserve existing job event and status update behavior after the lifecycle ownership move.
- [ ] Add or update tests to cover cancellation plus supervisor-backed process cleanup interactions.
- [ ] Run agent package tests that touch background run/process cleanup.

**Verify:**
- `go test ./internal/agent/... -run 'Test.*(Job|Run|Cancel|Process)'`
- `go test ./internal/agent/...`

---

### Task 4: Track Foreground Interactive Shell and Editor Children

**Files:**
- Modify: `internal/tool/exec.go`
- Modify: `internal/tui/model.go`
- Potentially modify helper(s) used by external editor launch paths

- [ ] Identify every foreground child ocode starts directly, especially `!command` and editor launches.
- [ ] Refactor spawn helpers so `exec.Cmd` creation happens in a place where the supervisor can register the child before Bubble Tea or another helper takes control.
- [ ] Apply per-kind termination policy without changing normal interactive behavior while the app is still running.
- [ ] Ensure child completion unregisters only from the active-running set while keeping terminal logs/state visible for the session.
- [ ] Add targeted tests around shell/editor command registration if those paths are testable without brittle UI coupling.
- [ ] Run the relevant TUI/tool tests.

**Verify:**
- `go test ./internal/tui/... -run 'TestShell|Test.*Editor'`
- `go test ./internal/tool/...`

---

### Task 5: Replace Duplicated TUI Teardown with One Shared Cleanup Path

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`
- Modify: `internal/tui/commands.go` if command handlers need to call the shared path explicitly

- [ ] Add one generic model cleanup method that saves session state, shuts down the agent, and invokes supervisor shutdown exactly once.
- [ ] Route `/exit`, double `ctrl+c`, leader-key quit, mouse quit, `/new`/`/clear`, and agent/model rebuild paths through that method.
- [ ] Make `/new` explicitly kill all tracked jobs before the new session starts.
- [ ] Preserve existing user-visible quit/reset flow while removing duplicated shutdown branches.
- [ ] Add tests proving the quit/reset routes use the shared cleanup path.
- [ ] Run the relevant TUI tests.

**Verify:**
- `go test ./internal/tui/... -run 'Test.*(CtrlC|Shell|Exit|Clear|New)'`
- `go test ./internal/tui/...`

---

### Task 6: Add Signal-Driven Cleanup in `tui.Run()`

**Files:**
- Modify: `internal/tui/tui.go`

- [ ] Add bounded signal handling for `SIGINT` and `SIGTERM` that routes through the same cleanup path instead of exiting immediately.
- [ ] Add deferred cleanup around `tea.NewProgram(m).Run()` so nonstandard returns still tear down tracked children.
- [ ] Ensure signal handling does not bypass cleanup with an early `os.Exit` before shutdown completes or times out.
- [ ] Add focused tests where practical; if direct signal tests are too brittle, keep unit coverage on the extracted cleanup/signal helper boundaries.
- [ ] Run the TUI package tests.

**Verify:**
- `go test ./internal/tui/...`

---

### Task 7: End-to-End Verification and Documentation Sync

**Files:**
- Modify: relevant docs only if implementation meaningfully changes user-facing cleanup behavior or command semantics

- [ ] Run focused package tests for `internal/tool`, `internal/agent`, and `internal/tui`.
- [ ] Run the full project test suite if it is practical in this repo.
- [ ] Manually verify the key scenarios from the design: background bash, interactive `!command`, external editor, `/exit`, double `ctrl+c`, leader quit, mouse quit, `/new`, and external `SIGTERM`.
- [ ] Confirm no leftover child processes remain after each shutdown path.
- [ ] Update README or related docs only if the implementation introduces a user-visible cleanup guarantee or operational note worth documenting.

**Verify:**
- `go test ./internal/tool/...`
- `go test ./internal/agent/...`
- `go test ./internal/tui/...`
- `go test ./...`

---

### Spec Coverage Check

- One session-scoped supervisor: covered by Tasks 1 and 2.
- Background and foreground child tracking: covered by Tasks 2 and 4.
- Shared cleanup path for exit/reset/rebuild/signals: covered by Tasks 5 and 6.
- `/new` kills all tracked jobs: covered by Task 5.
- Finished logs visible for full session: covered by Tasks 1 and 2.
- Fixed shutdown order with freeze, cancel, terminate, force kill, reap: covered by Task 1.
- Agent/subagent integration without split teardown ownership: covered by Task 3.

### Notes

- This plan is intentionally high-level because project planning rules require plans to describe what changes and where, not code-level diffs.
- Do not start implementation by patching quit handlers first. The supervisor ownership model must land before cleanup routing is changed, or teardown paths will stay split.
