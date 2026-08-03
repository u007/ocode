# Resume Cancelled/Completed Task Subagent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the `task` tool resume a specific cancelled-or-completed subagent run with a new prompt, continuing from its existing conversation history, tools, and permissions.

**Architecture:** Add a `resume_task_id` param to the `task` tool. On resume, skip fresh-subagent construction entirely, reuse the existing `*Agent` stored on the `AgentRun` (`run.Sub`), rebuild the full conversation from the run's already-accumulated transcript, re-arm the two maintenance goroutines and stop channel that `shutdownTransient()` tore down, then re-enter the same queue/acquire/execute/finish flow a fresh dispatch uses (extracted into two shared helpers so resume and fresh dispatch never drift apart).

**Tech Stack:** Go, package `internal/agent`, standard library `testing`.

## Global Constraints

- Resumable statuses: `RunCancelled` and `RunDone` only. `RunFailed`, `RunRunning`, `RunQueued` are rejected.
- Resume reuses the same `task_id` — the existing `AgentRun` flips back to `Running`, `task_status`/`agent_status` polling is unaffected.
- Resume's `run_in_background` is independent of how the original run was launched.
- Resuming a run that was part of a `shared_notes` group is out of scope — not handled specially, simply not supported (resumed runs never re-attach to a notes bus).
- Ownership check must mirror `AgentRunRegistry.CancelOwned`'s comparison (`run.Dispatcher != dispatcher`) exactly, including its edge case where a top-level dispatcher has `run.Dispatcher == ""`.
- No behavior change to fresh (non-resume) `task` dispatch — the refactor in Task 3 must be behavior-preserving.

Spec: `docs/superpowers/specs/2026-08-02-resume-cancelled-subagent-design.md`

---

### Task 1: `AgentRun.beginResume()`

**Files:**
- Modify: `internal/agent/agent_runs.go` (add method after `beginExecution`, around line 243)
- Test: `internal/agent/agent_runs_test.go`

**Interfaces:**
- Produces: `func (r *AgentRun) beginResume() bool` — transitions `RunCancelled`/`RunDone` → `RunRunning` and resets `EndedAt` to the zero `time.Time`. Returns `false` (no state change) if the run is not currently `RunCancelled` or `RunDone`.

- [x] **Step 1: Write the failing tests**

Add to `internal/agent/agent_runs_test.go`:

```go
func TestBeginResumeFromCancelled(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.tryFinishCancelled()
	if run.statusValue() != RunCancelled {
		t.Fatalf("setup: status = %s, want cancelled", run.statusValue())
	}

	if ok := run.beginResume(); !ok {
		t.Fatal("beginResume() = false, want true for a cancelled run")
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("status = %s, want running", run.statusValue())
	}
	if !run.EndedAt.IsZero() {
		t.Fatalf("EndedAt = %v, want zero", run.EndedAt)
	}
}

func TestBeginResumeFromDone(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.finishOK("original result")
	if run.statusValue() != RunDone {
		t.Fatalf("setup: status = %s, want done", run.statusValue())
	}

	if ok := run.beginResume(); !ok {
		t.Fatal("beginResume() = false, want true for a done run")
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("status = %s, want running", run.statusValue())
	}
}

func TestBeginResumeRejectsRunning(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore") // New() leaves the run in RunRunning

	if ok := run.beginResume(); ok {
		t.Fatal("beginResume() = true, want false for a running run")
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("status changed to %s, want unchanged running", run.statusValue())
	}
}

func TestBeginResumeRejectsFailed(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.finishErr("boom")

	if ok := run.beginResume(); ok {
		t.Fatal("beginResume() = true, want false for a failed run")
	}
	if run.statusValue() != RunFailed {
		t.Fatalf("status changed to %s, want unchanged failed", run.statusValue())
	}
}

func TestBeginResumeRejectsQueued(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.markQueued()
	if run.statusValue() != RunQueued {
		t.Fatalf("setup: status = %s, want queued", run.statusValue())
	}

	if ok := run.beginResume(); ok {
		t.Fatal("beginResume() = true, want false for a queued run")
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/... -run TestBeginResume -v`
Expected: FAIL — `beginResume` is undefined on `*AgentRun`.

- [x] **Step 3: Implement `beginResume`**

In `internal/agent/agent_runs.go`, add immediately after `beginExecution` (after the closing brace at the current line 243):

```go
// beginResume transitions a resumable run (Cancelled or Done) back into the
// Running state so it can be re-queued through the normal Acquire/
// beginExecution flow. Returns false if the run is not in one of those two
// states — e.g. a concurrent resume call already claimed it. Does not touch
// Err/Result: stale values from the prior terminal state are harmless (the
// RunRunning branch of task_status/agent_status never surfaces them) and are
// overwritten once the resumed run reaches its own finishOK/finishErr.
func (r *AgentRun) beginResume() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Status != RunCancelled && r.Status != RunDone {
		return false
	}
	r.Status = RunRunning
	r.EndedAt = time.Time{}
	return true
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/... -run TestBeginResume -v`
Expected: PASS (all 5 subtests)

- [x] **Step 5: Commit**

```bash
git add internal/agent/agent_runs.go internal/agent/agent_runs_test.go
git commit -m "feat(agent): add AgentRun.beginResume for resuming cancelled/done runs"
```

---

### Task 2: `Agent.RearmMaintenance()`

**Files:**
- Modify: `internal/agent/agent.go` (add method after `ResetCancellation`, around line 3788)
- Test: `internal/agent/rearm_maintenance_test.go` (new file)

**Interfaces:**
- Consumes: `Agent.stopCh`/`stopMu` (existing), `Agent.docMaintCh`/`docMaintDone`/`docMaintShutdownOnce`/`docMaintMu`/`docMaintClosing`, `Agent.memoryMaintCh`/`memoryMaintDone`/`memoryMaintShutdownOnce` (existing fields, see `internal/agent/agent.go:158-170`), `docMaintChannelCap` constant (`internal/agent/doc_maintenance.go:15`), `Agent.memoryMaintenanceWorker`/`Agent.docMaintenanceWorker` (existing worker loops), `Agent.ResetCancellation()` (existing).
- Produces: `func (a *Agent) RearmMaintenance()` — undoes everything `shutdownTransient()` does: reopens `stopCh`, and recreates + restarts the doc/memory maintenance channels and worker goroutines so the agent can safely run `Step()` again and be torn down again afterward.

- [x] **Step 1: Write the failing test**

Create `internal/agent/rearm_maintenance_test.go`:

```go
package agent

import "testing"

// TestRearmMaintenanceAllowsSecondShutdown proves RearmMaintenance actually
// replaces the closed docMaintCh/memoryMaintCh (and their sync.Once guards)
// with fresh ones. Without a real re-init, a second shutdownTransient() would
// panic with "close of closed channel" — Go's testing package fails the test
// on any panic, so no explicit assertion is needed for that half.
func TestRearmMaintenanceAllowsSecondShutdown(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)

	a.shutdownTransient() // mirrors what happens when a dispatch goes terminal
	a.RearmMaintenance()
	a.shutdownTransient() // must not panic
}

func TestRearmMaintenanceReopensStopChannel(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)

	a.Cancel()
	select {
	case <-a.StopCh():
	default:
		t.Fatal("setup: stopCh should be closed after Cancel()")
	}

	a.RearmMaintenance()
	select {
	case <-a.StopCh():
		t.Fatal("stopCh still closed after RearmMaintenance")
	default:
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/... -run TestRearmMaintenance -v`
Expected: FAIL — `RearmMaintenance` is undefined on `*Agent`.

- [x] **Step 3: Implement `RearmMaintenance`**

In `internal/agent/agent.go`, add immediately after `ResetCancellation` (after the closing brace at the current line 3788):

```go
// RearmMaintenance undoes shutdownTransient(): it reopens the stop channel
// and recreates + restarts the doc/memory maintenance worker goroutines that
// shutdownTransient permanently closed via sync.Once-guarded shutdowns. Used
// when resuming a previously cancelled-or-completed subagent, whose
// shutdownTransient already ran once via the dispatch goroutine's defer.
// Mirrors the channel/goroutine setup in NewAgent (see agent.go:640-649).
func (a *Agent) RearmMaintenance() {
	a.docMaintMu.Lock()
	a.docMaintClosing = false
	a.docMaintMu.Unlock()

	a.memoryMaintCh = make(chan MemoryMaintenanceRequest, 64)
	a.docMaintCh = make(chan DocMaintenanceRequest, docMaintChannelCap)
	a.docMaintDone = make(chan struct{})
	a.memoryMaintDone = make(chan struct{})
	a.docMaintShutdownOnce = sync.Once{}
	a.memoryMaintShutdownOnce = sync.Once{}

	go a.memoryMaintenanceWorker()
	go a.docMaintenanceWorker()

	a.ResetCancellation()
}
```

Check the top of `internal/agent/agent.go` already imports `"sync"` (it does — `stopMu sync.Mutex` and other `sync.*` fields are already declared in the `Agent` struct), so no new import is needed.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/... -run TestRearmMaintenance -v`
Expected: PASS (both tests)

- [x] **Step 5: Run the full package test suite to check for regressions**

Run: `go test ./internal/agent/...`
Expected: PASS (no pre-existing test should be affected — `RearmMaintenance` is a new, additive method)

- [x] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/rearm_maintenance_test.go
git commit -m "feat(agent): add Agent.RearmMaintenance to re-init a transient agent's workers"
```

---

### Task 3: Extract shared dispatch helpers in `TaskTool` (no behavior change)

This task refactors the existing fresh-dispatch code in `TaskTool.Execute` into two reusable methods, `runBackgroundDispatch` and `runSyncDispatch`, WITHOUT changing observable behavior. Task 4 will make the resume path call these same two methods instead of duplicating ~130 lines of queue/acquire/execute/finish logic.

**Files:**
- Modify: `internal/agent/subagent.go`

**Interfaces:**
- Produces:
  - `func (t TaskTool) runBackgroundDispatch(specName string, subAgent *Agent, run *AgentRun, messages []Message, verb string) string` — launches `subAgent` asynchronously against `messages`, advances `run` through queued/running/terminal, returns the immediate "task started" response string. `verb` is used only in that response string (`"started"` for fresh dispatch).
  - `func (t TaskTool) runSyncDispatch(specName string, subAgent *Agent, run *AgentRun, messages []Message) (string, error)` — blocks until `subAgent` finishes executing `messages`, advances `run` (if non-nil) through queued/running/terminal, persists the child session, returns the subagent's final text result.

- [x] **Step 1: Record baseline test pass**

Run: `go test ./internal/agent/...`
Expected: PASS. Note this baseline — it must still fully pass after this task's refactor.

- [x] **Step 2: Extract `runBackgroundDispatch`**

In `internal/agent/subagent.go`, find the background-mode block inside `TaskTool.Execute` (currently spanning from `// Background mode` through the closing `return fmt.Sprintf(...), nil` and its enclosing `}`, i.e. lines 428-508):

```go
	// Background mode
	if params.RunInBackground && t.runs != nil {
		run := t.runs.New(spec.Name)
		run.Background = true
		run.Procs = subAgent.Procs()
		run.Sub = subAgent
		run.Cancel = subAgent.Cancel
		if t.mainAgent != nil && t.mainAgent.spec != nil {
			run.Dispatcher = t.mainAgent.spec.Name
		}
		attachRunTranscript(run)

		// Only surface a Queued state when a concurrency limit is actually
		// configured — otherwise Acquire below is a no-op and the run goes
		// straight to Running, same as before this limiter existed.
		limited := t.runs.MaxConcurrent() > 0
		if limited {
			run.markQueued()
		}
		var stopCh <-chan struct{}
		if t.mainAgent != nil {
			stopCh = t.mainAgent.StopCh()
		}
		runs := t.runs

		go func() {
			// Tear down the transient sub-agent's goroutines once this background
			// run reaches a terminal state. The AgentRun record (transcript +
			// result + run.Sub for ModelLabel) is retained so status/resume still
			// work, but the two maintenance-worker goroutines no longer leak. This
			// mirrors the synchronous path's shutdownTransient; it must only run
			// AFTER completion — never while the agent is still running, or it
			// would Cancel() and abort the run.
			defer subAgent.shutdownTransient()

			// Wait for a concurrency slot (no-op when unlimited). If the session
			// is cancelled while this dispatch is still queued, stopCh closes and
			// we bail without ever starting the sub-agent.
			release, aerr := runs.Acquire(stopCh)
			if aerr != nil {
				run.tryFinishCancelled()
				runs.notifyDone(run)
				return
			}
			// Own the slot via subAgent (not a bare defer) so that if subAgent
			// itself dispatches further nested "task" calls, it can temporarily
			// hand this slot back to the shared pool instead of self-deadlocking
			// (see Agent.pauseOwnSlotForNestedCall).
			subAgent.setOwnSlot(release)
			defer subAgent.releaseOwnSlot()
			// task_cancel or session shutdown may have cancelled this specific run
			// while it was queued (CancelOwned marks Queued runs Cancelled too);
			// honor that instead of starting work the caller already gave up on.
			// Re-check the stop channel after Acquire as well: cancellation can race
			// with the final slot admission, including when the limit is unlimited.
			if run.statusValue() == RunCancelled || isClosed(stopCh) {
				run.tryFinishCancelled()
				runs.notifyDone(run)
				return
			}
			if !run.beginExecution() {
				runs.notifyDone(run)
				return
			}

			result, err := t.executeSubAgent(spec.Name, subAgent, subAgentMsgs)
			if err != nil {
				run.finishErr(err.Error())
				runs.notifyDone(run)
				return
			}
			run.finishOK(result)
			runs.notifyDone(run)
		}()

		state := "running"
		if limited {
			state = "queued"
		}
		return fmt.Sprintf("task_id: %s (agent: %s)\nstate: %s\n\n<task_result>\nBackground task started. Poll with task_status or agent_status.\n</task_result>", run.ID, spec.Name, state), nil
	}
```

Replace it with:

```go
	// Background mode
	if params.RunInBackground && t.runs != nil {
		run := t.runs.New(spec.Name)
		run.Background = true
		run.Procs = subAgent.Procs()
		run.Sub = subAgent
		run.Cancel = subAgent.Cancel
		if t.mainAgent != nil && t.mainAgent.spec != nil {
			run.Dispatcher = t.mainAgent.spec.Name
		}
		attachRunTranscript(run)

		return t.runBackgroundDispatch(spec.Name, subAgent, run, subAgentMsgs, "started"), nil
	}
```

Then add the extracted method. Place it directly after `Execute` ends (i.e. right after the closing `}` of `TaskTool.Execute`, before `func (t TaskTool) executeSubAgent`):

```go
// runBackgroundDispatch launches subAgent asynchronously against messages,
// advancing run through the queued/running/terminal lifecycle exactly like a
// fresh background task dispatch. Shared by fresh dispatch and task resume;
// verb only affects the human-readable "state" line in the returned summary
// ("started" for a fresh dispatch, "resumed" for a resume).
func (t TaskTool) runBackgroundDispatch(specName string, subAgent *Agent, run *AgentRun, messages []Message, verb string) string {
	// Only surface a Queued state when a concurrency limit is actually
	// configured — otherwise Acquire below is a no-op and the run goes
	// straight to Running, same as before this limiter existed.
	limited := t.runs.MaxConcurrent() > 0
	if limited {
		run.markQueued()
	}
	var stopCh <-chan struct{}
	if t.mainAgent != nil {
		stopCh = t.mainAgent.StopCh()
	}
	runs := t.runs

	go func() {
		// Tear down the transient sub-agent's goroutines once this background
		// run reaches a terminal state. The AgentRun record (transcript +
		// result + run.Sub for ModelLabel) is retained so status/resume still
		// work, but the two maintenance-worker goroutines no longer leak. This
		// mirrors the synchronous path's shutdownTransient; it must only run
		// AFTER completion — never while the agent is still running, or it
		// would Cancel() and abort the run.
		defer subAgent.shutdownTransient()

		// Wait for a concurrency slot (no-op when unlimited). If the session
		// is cancelled while this dispatch is still queued, stopCh closes and
		// we bail without ever starting the sub-agent.
		release, aerr := runs.Acquire(stopCh)
		if aerr != nil {
			run.tryFinishCancelled()
			runs.notifyDone(run)
			return
		}
		// Own the slot via subAgent (not a bare defer) so that if subAgent
		// itself dispatches further nested "task" calls, it can temporarily
		// hand this slot back to the shared pool instead of self-deadlocking
		// (see Agent.pauseOwnSlotForNestedCall).
		subAgent.setOwnSlot(release)
		defer subAgent.releaseOwnSlot()
		// task_cancel or session shutdown may have cancelled this specific run
		// while it was queued (CancelOwned marks Queued runs Cancelled too);
		// honor that instead of starting work the caller already gave up on.
		// Re-check the stop channel after Acquire as well: cancellation can race
		// with the final slot admission, including when the limit is unlimited.
		if run.statusValue() == RunCancelled || isClosed(stopCh) {
			run.tryFinishCancelled()
			runs.notifyDone(run)
			return
		}
		if !run.beginExecution() {
			runs.notifyDone(run)
			return
		}

		result, err := t.executeSubAgent(specName, subAgent, messages)
		if err != nil {
			run.finishErr(err.Error())
			runs.notifyDone(run)
			return
		}
		run.finishOK(result)
		runs.notifyDone(run)
	}()

	state := "running"
	if limited {
		state = "queued"
	}
	return fmt.Sprintf("task_id: %s (agent: %s)\nstate: %s\n\n<task_result>\nBackground task %s. Poll with task_status or agent_status.\n</task_result>", run.ID, specName, state, verb)
}
```

- [x] **Step 3: Extract `runSyncDispatch`**

Find the synchronous-mode block that follows (currently lines 510-588, from the `// Synchronous mode.` comment through the end of `Execute`):

```go
	// Synchronous mode. t.mainAgent is about to block on subAgent's full
	// execution, so it isn't doing concurrent work itself for the duration —
	// hand its own concurrency slot (if it holds one) back to the shared
	// pool for the duration of this call, otherwise a low
	// max_concurrent_agents limit would deadlock against t.mainAgent's own
	// blocked ancestors on any synchronous nested dispatch.
	resumeMainSlot := t.mainAgent.pauseOwnSlotForNestedCall()
	defer resumeMainSlot()

	var run *AgentRun
	if t.runs != nil {
		run = t.runs.New(spec.Name)
		run.Procs = subAgent.Procs()
		run.Sub = subAgent
		run.Cancel = subAgent.Cancel
		if t.mainAgent != nil && t.mainAgent.spec != nil {
			run.Dispatcher = t.mainAgent.spec.Name
		}
		attachRunTranscript(run)

		if t.runs.MaxConcurrent() > 0 {
			run.markQueued()
		}
		var stopCh <-chan struct{}
		if t.mainAgent != nil {
			stopCh = t.mainAgent.StopCh()
		}
		release, aerr := t.runs.Acquire(stopCh)
		if aerr != nil {
			run.tryFinishCancelled()
			return "", aerr
		}
		if run.statusValue() == RunCancelled || isClosed(stopCh) {
			release()
			run.tryFinishCancelled()
			return "", fmt.Errorf("task cancelled while queued")
		}
		if !run.beginExecution() {
			release()
			return "", fmt.Errorf("task cancelled while queued")
		}
		subAgent.setOwnSlot(release)
		defer subAgent.releaseOwnSlot()
	}
	// Each synchronous sub-agent dispatch builds a fresh Agent with two
	// maintenance-worker goroutines (memory + doc) that would otherwise leak
	// forever — memoryMaintCh is never closed by the caller and the sync path
	// never calls Shutdown. Tear the transient agent down once this call
	// returns so we don't accumulate one leaked Agent + goroutines per
	// knowledge_lookup / synchronous task. The sub-agent shares the parent's
	// snapshotStore, so shutdownTransient stops only the goroutines/loop and
	// must NOT Reset the shared store.
	defer subAgent.shutdownTransient()
	result, resp, err := t.executeSubAgentWithTranscript(spec.Name, subAgent, subAgentMsgs)
	if err != nil {
		if run != nil {
			run.finishErr(err.Error())
			t.runs.notifyDone(run)
		}
		return "", err
	}

	sessionID := childSessionID("parent", spec.Name)
	metadata := childSessionMetadata("parent", spec.Name)
	if t.persistChildSess != nil {
		if err := t.persistChildSess(sessionID, fmt.Sprintf("Child: %s", spec.Name), resp, metadata); err != nil {
			emitDebug("SESSION", fmt.Sprintf("failed to persist child session: %v", err))
		}
	}

	if run != nil {
		run.finishOK(result)
		t.runs.notifyDone(run)
	}
	if sessionID != "" {
		result += fmt.Sprintf("\n\n(Child session: %s)", sessionID)
	}
	return fallbackWarning + result, nil
}
```

Replace it with:

```go
	// Synchronous mode.
	var run *AgentRun
	if t.runs != nil {
		run = t.runs.New(spec.Name)
		run.Procs = subAgent.Procs()
		run.Sub = subAgent
		run.Cancel = subAgent.Cancel
		if t.mainAgent != nil && t.mainAgent.spec != nil {
			run.Dispatcher = t.mainAgent.spec.Name
		}
		attachRunTranscript(run)
	}

	result, err := t.runSyncDispatch(spec.Name, subAgent, run, subAgentMsgs)
	if err != nil {
		return "", err
	}
	return fallbackWarning + result, nil
}
```

Then add the extracted method right after it (before `func (t TaskTool) executeSubAgent`):

```go
// runSyncDispatch blocks until subAgent finishes executing messages, updating
// run (if non-nil) through the queued/running/terminal lifecycle exactly like
// a fresh synchronous task dispatch. Shared by fresh dispatch and task
// resume.
func (t TaskTool) runSyncDispatch(specName string, subAgent *Agent, run *AgentRun, messages []Message) (string, error) {
	// t.mainAgent is about to block on subAgent's full execution, so it isn't
	// doing concurrent work itself for the duration — hand its own concurrency
	// slot (if it holds one) back to the shared pool for the duration of this
	// call, otherwise a low max_concurrent_agents limit would deadlock against
	// t.mainAgent's own blocked ancestors on any synchronous nested dispatch.
	resumeMainSlot := t.mainAgent.pauseOwnSlotForNestedCall()
	defer resumeMainSlot()

	if run != nil {
		if t.runs.MaxConcurrent() > 0 {
			run.markQueued()
		}
		var stopCh <-chan struct{}
		if t.mainAgent != nil {
			stopCh = t.mainAgent.StopCh()
		}
		release, aerr := t.runs.Acquire(stopCh)
		if aerr != nil {
			run.tryFinishCancelled()
			return "", aerr
		}
		if run.statusValue() == RunCancelled || isClosed(stopCh) {
			release()
			run.tryFinishCancelled()
			return "", fmt.Errorf("task cancelled while queued")
		}
		if !run.beginExecution() {
			release()
			return "", fmt.Errorf("task cancelled while queued")
		}
		subAgent.setOwnSlot(release)
		defer subAgent.releaseOwnSlot()
	}
	// Each synchronous sub-agent dispatch builds (or, for a resume, re-arms) two
	// maintenance-worker goroutines (memory + doc) that would otherwise leak
	// forever — memoryMaintCh is never closed by the caller and the sync path
	// never calls Shutdown. Tear the transient agent down once this call
	// returns so we don't accumulate one leaked Agent + goroutines per
	// knowledge_lookup / synchronous task. The sub-agent shares the parent's
	// snapshotStore, so shutdownTransient stops only the goroutines/loop and
	// must NOT Reset the shared store.
	defer subAgent.shutdownTransient()
	result, resp, err := t.executeSubAgentWithTranscript(specName, subAgent, messages)
	if err != nil {
		if run != nil {
			run.finishErr(err.Error())
			t.runs.notifyDone(run)
		}
		return "", err
	}

	sessionID := childSessionID("parent", specName)
	metadata := childSessionMetadata("parent", specName)
	if t.persistChildSess != nil {
		if err := t.persistChildSess(sessionID, fmt.Sprintf("Child: %s", specName), resp, metadata); err != nil {
			emitDebug("SESSION", fmt.Sprintf("failed to persist child session: %v", err))
		}
	}

	if run != nil {
		run.finishOK(result)
		t.runs.notifyDone(run)
	}
	if sessionID != "" {
		result += fmt.Sprintf("\n\n(Child session: %s)", sessionID)
	}
	return result, nil
}
```

- [x] **Step 4: Build and run the full package test suite to confirm no regression**

Run: `go build ./... && go test ./internal/agent/...`
Expected: PASS — every pre-existing test (including `TestTaskSubagentInheritsParentPermissionRules`, `TestTaskCancelTool*`, `TestAgentRunRegistry*`, etc.) must still pass unchanged. If anything fails, the refactor introduced a behavior difference — compare the extracted method body character-for-character against the original block before proceeding.

- [x] **Step 5: Commit**

```bash
git add internal/agent/subagent.go
git commit -m "refactor(agent): extract TaskTool background/sync dispatch into shared helpers"
```

---

### Task 4: `resume_task_id` param and resume path

**Files:**
- Modify: `internal/agent/subagent.go`

**Interfaces:**
- Consumes: `AgentRun.beginResume()` (Task 1), `Agent.RearmMaintenance()` (Task 2), `t.runBackgroundDispatch`/`t.runSyncDispatch` (Task 3), `AgentRunRegistry.Get`, `run.appendTranscript`, `run.TranscriptPublic()`, `Agent.NoteSubagentDispatch`, `subagentDispatchLimit` (`internal/agent/agent.go:416`).
- Produces: `resume_task_id` task-tool schema param; `TaskTool.Execute` resume branch; `taskToolParams` (named type replacing the previous anonymous params struct — needed so `executeResume` can take it as a parameter).

- [x] **Step 1: Name the params struct and add `resume_task_id`**

In `internal/agent/subagent.go`, find the params struct declaration at the top of `TaskTool.Execute` (currently):

```go
func (t TaskTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Prompt          string `json:"prompt"`
		Agent           string `json:"agent"`
		SubagentType    string `json:"subagent_type"`
		Context         string `json:"context"`
		Description     string `json:"description"`
		RunInBackground bool   `json:"run_in_background"`
		Background      bool   `json:"background"`
		SharedNotes     bool   `json:"shared_notes"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
```

Replace with a package-level named type (add it directly above `func (t TaskTool) Execute`) plus the updated declaration:

```go
type taskToolParams struct {
	Prompt          string `json:"prompt"`
	Agent           string `json:"agent"`
	SubagentType    string `json:"subagent_type"`
	Context         string `json:"context"`
	Description     string `json:"description"`
	RunInBackground bool   `json:"run_in_background"`
	Background      bool   `json:"background"`
	SharedNotes     bool   `json:"shared_notes"`
	ResumeTaskID    string `json:"resume_task_id"`
}

func (t TaskTool) Execute(args json.RawMessage) (string, error) {
	var params taskToolParams
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
```

- [x] **Step 2: Add the schema property**

In `TaskTool.Definition()`, inside the `"properties"` map (after the `"shared_notes"` entry), add:

```go
				"resume_task_id": map[string]interface{}{
					"type":        "string",
					"description": "Resume a previously cancelled or completed task instead of starting a new one. Set to the task_id of a run whose state is cancelled or done; prompt becomes the follow-up instruction and the sub-agent continues with its full prior conversation history.",
				},
```

- [x] **Step 3: Branch into the resume path**

In `TaskTool.Execute`, find:

```go
	if params.Background {
		params.RunInBackground = true
	}

	spec := t.findAgent(params.Agent)
```

Insert the resume branch between them:

```go
	if params.Background {
		params.RunInBackground = true
	}

	if params.ResumeTaskID != "" {
		return t.executeResume(params)
	}

	spec := t.findAgent(params.Agent)
```

- [x] **Step 4: Implement the eligibility check and `executeResume`**

Add these two methods to `internal/agent/subagent.go`, placed after `TaskTool.Execute` ends (before `func (t TaskTool) executeSubAgent`):

```go
// resumeEligibleRun looks up taskID and validates it can be resumed: it must
// exist, be owned by the calling agent (same comparison as
// AgentRunRegistry.CancelOwned), and be in RunCancelled or RunDone. Returns
// the run and its subagent-type name (run.Name) on success.
func (t TaskTool) resumeEligibleRun(taskID string) (*AgentRun, string, error) {
	if t.runs == nil {
		return nil, "", fmt.Errorf("no agent run registry")
	}
	run, ok := t.runs.Get(taskID)
	if !ok {
		return nil, "", fmt.Errorf("unknown task: %s", taskID)
	}
	dispatcher := ""
	if t.mainAgent != nil && t.mainAgent.spec != nil {
		dispatcher = t.mainAgent.spec.Name
	}
	if run.Dispatcher != dispatcher {
		return nil, "", fmt.Errorf("task %s is not owned by %q", taskID, dispatcher)
	}
	status := run.statusValue()
	if status != RunCancelled && status != RunDone {
		return nil, "", fmt.Errorf("task %s cannot be resumed from state %q (only cancelled or done tasks can be resumed)", taskID, status)
	}
	if run.Sub == nil {
		return nil, "", fmt.Errorf("task %s has no resumable sub-agent state", taskID)
	}
	return run, run.Name, nil
}

// executeResume continues a cancelled-or-completed subagent run with a new
// prompt. It reuses the original *Agent (run.Sub) and its full prior
// transcript instead of constructing a fresh subagent, so tools, permissions,
// and conversation history all carry over unchanged.
func (t TaskTool) executeResume(params taskToolParams) (string, error) {
	run, specName, err := t.resumeEligibleRun(params.ResumeTaskID)
	if err != nil {
		return "", err
	}
	subAgent := run.Sub

	// Re-dispatch guard: refuse repeated identical resumes without any
	// intervening user input, same protection fresh dispatch has (see
	// subagentDispatchLimit).
	if t.mainAgent != nil {
		if count := t.mainAgent.NoteSubagentDispatch(specName); count > subagentDispatchLimit {
			return fmt.Sprintf("Error: refusing to resume subagent %q — it has been launched %d times in a row without any new user input. This usually means the conversation is in a feedback loop. Wait for the user to provide new direction before retrying.", specName, count), nil
		}
	}

	if !run.beginResume() {
		return fmt.Sprintf("Error: task %s changed state and can no longer be resumed (currently %s).", params.ResumeTaskID, run.statusValue()), nil
	}

	// Build the follow-up message(s) the same way a fresh dispatch builds its
	// initial ones (context -> optional system message, prompt -> user
	// message), then append them onto the run's existing transcript — which
	// already holds the full prior conversation, seeded at the original
	// dispatch and streamed via subAgent.OnMessage as it ran. The resulting
	// TranscriptPublic() is exactly the message slice Step needs, since
	// Agent.Step is stateless and retains no history of its own between
	// calls.
	var resumeMsgs []Message
	if params.Context != "" {
		resumeMsgs = append(resumeMsgs, Message{
			Role:    "system",
			Content: "Background Context: " + params.Context,
		})
	}
	resumeMsgs = append(resumeMsgs, Message{Role: "user", Content: params.Prompt})
	for _, msg := range resumeMsgs {
		run.appendTranscript(msg)
	}
	messages := run.TranscriptPublic()

	// Undo shutdownTransient(): reopen stopCh and restart the maintenance
	// workers it tore down when this run first went terminal.
	subAgent.RearmMaintenance()

	if params.RunInBackground {
		return t.runBackgroundDispatch(specName, subAgent, run, messages, "resumed"), nil
	}
	return t.runSyncDispatch(specName, subAgent, run, messages)
}
```

- [x] **Step 5: Build**

Run: `go build ./...`
Expected: builds cleanly.

- [x] **Step 6: Run the full package test suite**

Run: `go test ./internal/agent/...`
Expected: PASS — no existing test references `resume_task_id`, so nothing should break yet. Task 5 adds the tests that actually exercise this new code.

- [x] **Step 7: Commit**

```bash
git add internal/agent/subagent.go
git commit -m "feat(agent): add resume_task_id to the task tool for resuming cancelled/done subagents"
```

---

### Task 5: End-to-end resume tests

**Files:**
- Test: `internal/agent/subagent_resume_test.go` (new file)

**Interfaces:**
- Consumes: `TaskTool{mainAgent, registry, runs}` (existing), `TaskTool.Execute` (existing, now resume-aware from Task 4), `NewAgentRunRegistry`/`AgentRunRegistry.New` (existing), `NewAgent` (existing), `Agent.SetSpec`/`AgentSpec` (existing), `captureClient` (existing test helper in `agent_test.go`, same package).

- [x] **Step 1: Write the eligibility-rejection tests (failing first is not applicable here — these exercise pre-existing validation paths introduced in Task 4, so write and run them together)**

Create `internal/agent/subagent_resume_test.go`:

```go
package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// buildResumeCallerAgent returns a minimal *Agent that plays the role of the
// caller dispatching the resume — its spec.Name becomes the ownership
// "dispatcher" identity that resumeEligibleRun checks against.
func buildResumeCallerAgent(t *testing.T, dispatcherName string) *Agent {
	t.Helper()
	caller := NewAgent(&MockClient{}, nil, nil, nil)
	caller.SetSpec(&AgentSpec{Name: dispatcherName})
	return caller
}

func TestTaskToolResumeUnknownID(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}

	_, err := tool.Execute(json.RawMessage(`{"prompt":"continue","resume_task_id":"agent-run-999"}`))
	if err == nil {
		t.Fatal("expected error for unknown resume_task_id")
	}
	if !strings.Contains(err.Error(), "unknown task") {
		t.Fatalf("error = %q, want 'unknown task'", err)
	}
}

func TestTaskToolResumeWrongDispatcher(t *testing.T) {
	caller := buildResumeCallerAgent(t, "attacker")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.tryFinishCancelled()
	sub := NewAgent(&MockClient{}, nil, nil, nil)
	run.Sub = sub

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	_, err := tool.Execute(json.RawMessage(`{"prompt":"continue","resume_task_id":"` + run.ID + `"}`))
	if err == nil {
		t.Fatal("expected error for dispatcher mismatch")
	}
	if !strings.Contains(err.Error(), "not owned by") {
		t.Fatalf("error = %q, want 'not owned by'", err)
	}
}

func TestTaskToolResumeRejectsRunningStatus(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore") // New() leaves status RunRunning
	run.Dispatcher = "build"
	run.Sub = NewAgent(&MockClient{}, nil, nil, nil)

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	_, err := tool.Execute(json.RawMessage(`{"prompt":"continue","resume_task_id":"` + run.ID + `"}`))
	if err == nil {
		t.Fatal("expected error for resuming a running task")
	}
	if !strings.Contains(err.Error(), "cannot be resumed") {
		t.Fatalf("error = %q, want 'cannot be resumed'", err)
	}
}

func TestTaskToolResumeRejectsFailedStatus(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.finishErr("boom")
	run.Sub = NewAgent(&MockClient{}, nil, nil, nil)

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	_, err := tool.Execute(json.RawMessage(`{"prompt":"continue","resume_task_id":"` + run.ID + `"}`))
	if err == nil {
		t.Fatal("expected error for resuming a failed task")
	}
	if !strings.Contains(err.Error(), "cannot be resumed") {
		t.Fatalf("error = %q, want 'cannot be resumed'", err)
	}
}

func TestTaskToolResumeCancelledSucceedsSync(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.appendTranscript(Message{Role: "user", Content: "original prompt"})
	run.appendTranscript(Message{Role: "assistant", Content: "original answer"})
	run.tryFinishCancelled()

	capture := &captureClient{}
	sub := NewAgent(capture, nil, nil, nil)
	sub.shutdownTransient() // mirrors the real teardown that ran when this run first went terminal
	run.Sub = sub
	run.Cancel = sub.Cancel

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	result, err := tool.Execute(json.RawMessage(`{"prompt":"keep going","resume_task_id":"` + run.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(result, "ok") { // captureClient's Chat always responds with content "ok"
		t.Fatalf("result = %q, want the resumed sub-agent's response", result)
	}
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want done", run.statusValue())
	}

	// Transcript continuity: the resumed Step call must have seen the full
	// prior conversation plus the new follow-up prompt, in order.
	var contents []string
	for _, m := range capture.Messages {
		contents = append(contents, m.Content)
	}
	want := []string{"original prompt", "original answer", "keep going"}
	for _, w := range want {
		found := false
		for _, c := range contents {
			if c == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("captured messages %v missing expected content %q", contents, w)
		}
	}
	if contents[len(contents)-1] != "keep going" {
		t.Fatalf("last captured message = %q, want the new follow-up prompt last", contents[len(contents)-1])
	}
}

func TestTaskToolResumeDoneSucceedsBackground(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.appendTranscript(Message{Role: "user", Content: "original prompt"})
	run.finishOK("original result")

	capture := &captureClient{}
	sub := NewAgent(capture, nil, nil, nil)
	sub.shutdownTransient()
	run.Sub = sub
	run.Cancel = sub.Cancel

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	result, err := tool.Execute(json.RawMessage(`{"prompt":"keep going","resume_task_id":"` + run.ID + `","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(result, "resumed") {
		t.Fatalf("result = %q, want it to mention 'resumed'", result)
	}
	if !strings.Contains(result, run.ID) {
		t.Fatalf("result = %q, want it to reuse task_id %s", result, run.ID)
	}

	select {
	case <-run.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("background resume did not finish within 5s")
	}
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want done", run.statusValue())
	}
	if run.Result != "ok" {
		t.Fatalf("Result = %q, want the resumed sub-agent's response", run.Result)
	}
}
```

- [x] **Step 2: Run the new tests**

Run: `go test ./internal/agent/... -run TestTaskToolResume -v`
Expected: PASS (all 6 tests). If `TestTaskToolResumeCancelledSucceedsSync` or `TestTaskToolResumeDoneSucceedsBackground` fail with an empty `capture.Messages` or a result that doesn't contain `"ok"`, the most likely cause is `RearmMaintenance`/`ResetCancellation` not being called before `Step()` — `Agent.Step` snapshots `StopCh()` at entry and returns immediately without calling `Chat` if it's still closed from the original cancellation.

- [x] **Step 3: Run the full package test suite**

Run: `go test ./internal/agent/...`
Expected: PASS — full suite green, including all tests from Tasks 1-4.

- [x] **Step 4: Commit**

```bash
git add internal/agent/subagent_resume_test.go
git commit -m "test(agent): add end-to-end coverage for resuming cancelled/done task subagents"
```

---

## Final Verification

- [x] Run `go build ./...` — builds cleanly.
- [x] Run `go vet ./internal/agent/...` — no new warnings.
- [x] Run `go test ./internal/agent/...` — full suite passes.
- [x] Manually re-read `docs/superpowers/specs/2026-08-02-resume-cancelled-subagent-design.md` against the implemented code to confirm every design section (task-tool param, eligibility check, history rebuild, un-cancelling, concurrency slot, dispatch, loop protection) has a corresponding task above.
