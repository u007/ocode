# Part 02 — Background-run cancellation (`task_cancel`)

Spec: `docs/superpowers/specs/2026-07-06-okf-context-agent-design.md` (section: Main-agent tools / task_cancel). Read it before starting. Independent of Part 01; may run in parallel with it.

Global constraints (self-contained copy): cancellation is caller-decided, never automatic; an agent may cancel only runs it dispatched; cancellation is cooperative (per-run `Cancel func()` stops at next step boundary); caught errors logged with context; `go build ./...` + `go test ./internal/agent/` per task; TDD; commit per task with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

Background (verified against code): `AgentRun` lives in `internal/agent/agent_runs.go` (~line 25) with `Cancel func()` (~line 40) and `tryFinishCancelled` (~line 169) which currently marks cancelled runs as `RunFailed` with `Err: "cancelled"`. The registry records no dispatcher. The `TaskTool` instance and its `runs` registry (`internal/agent/agent.go:607`) are shared with subagents, so ownership must be enforced per-call, not per-instance. Background dispatch happens in `TaskTool.Execute` (`internal/agent/subagent.go`, background branch); `task_status` exists at `internal/agent/agent.go:609` and `subagent.go:522`.

---

## Task 4: Dispatcher identity + distinct cancelled status

**Files:**
- Modify: `internal/agent/agent_runs.go` — `AgentRun` struct, status constants, `tryFinishCancelled`, registry add/list paths
- Modify: `internal/agent/subagent.go` — background-dispatch site stamps the dispatcher
- Modify: any TUI rendering that switches on run status (grep for the `RunFailed` constant and the `"cancelled"` err string; update switch/labels to the new status)
- Test: `internal/agent/agent_runs_test.go` (extend or create alongside existing tests)

**Interfaces produced:**
- `AgentRun.Dispatcher string` — identity of the dispatching agent. Use the dispatching agent's name for the main agent (its primary-agent name, e.g. `build`) and the subagent's run/agent identity for nested dispatch; stamped in `TaskTool.Execute` at background-run creation from the caller context already threaded into `Execute` (the same place `NoteSubagentDispatch` gets caller identity).
- New status constant `RunCancelled` alongside the existing run states; `tryFinishCancelled` (and the `CancelAll` path that calls it) now sets `RunCancelled` instead of `RunFailed`, keeping `Err: "cancelled"` for message compatibility.
- Registry method `func (r *AgentRunRegistry) CancelOwned(taskID, dispatcher string) error` — errors distinctly for: unknown task id; dispatcher mismatch; run already terminal (returns current state, not an error — cancelling a finished run is a no-op reported as such). On success invokes the run's `Cancel()` and `tryFinishCancelled`.

**Steps:**
- [ ] Grep all consumers of run status (`RunFailed`, `"cancelled"`, status switches in `internal/tui/` and `subagent.go` polling) and list them before changing anything; every consumer must handle `RunCancelled`.
- [ ] Write failing tests: `CancelOwned` unknown id; wrong dispatcher rejected; correct dispatcher cancels a running run and status becomes `RunCancelled` (not `RunFailed`); cancelling an already-done run is a reported no-op; `CancelAll` marks `RunCancelled`.
- [ ] Run tests → fail.
- [ ] Implement struct field, constant, `CancelOwned`, stamping at dispatch, and consumer updates (TUI renders cancelled distinctly from failed).
- [ ] `go test ./internal/agent/ -v -run 'Run|Cancel'` → PASS; `go build ./...`.
- [ ] Commit: `feat(agent): dispatcher identity and RunCancelled status for background runs`.

---

## Task 5: `task_cancel` tool

**Files:**
- Create: `internal/agent/task_cancel.go` — the tool
- Modify: `internal/agent/agent.go` (~line 609) — register alongside `task_status`
- Test: `internal/agent/task_cancel_test.go`

**Interfaces consumed:** `AgentRunRegistry.CancelOwned(taskID, dispatcher string) error`, `RunCancelled` from Task 4.

**Interfaces produced:**
- Tool named `task_cancel`, parameter `task_id` (string, required). Follows the existing tool-struct pattern used by `task_status` (same file area — mirror its Name/Description/Schema/Execute shape and registration). Description text must state: cancels a background task **you** dispatched; cooperative — stops at the next step boundary; use after another racing agent already returned a sufficient answer.
- Execute resolves the caller's dispatcher identity the same way Task 4 stamps it, calls `CancelOwned`, and returns a short human-readable result string for each outcome (cancelled / already finished / not found / not owned). Errors are returned as tool errors, never panics.

**Steps:**
- [ ] Write failing tests mirroring `task_status` test conventions: successful cancel of an owned running task; not-owned rejection; unknown id; already-finished no-op message.
- [ ] Run tests → fail.
- [ ] Implement tool + registration (always registered wherever `task_status` is — both main agent and inherited by subagents; ownership guard makes shared registration safe).
- [ ] `go test ./internal/agent/ -v -run TaskCancel` → PASS; `go build ./...`.
- [ ] Commit: `feat(agent): task_cancel tool for caller-decided background-run cancellation`.
