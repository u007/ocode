# Resume a cancelled/completed Task subagent

## Problem

Today, once a `task`-dispatched subagent run reaches a terminal state (`RunCancelled`, `RunDone`, `RunFailed`), the `AgentRun` record is kept only so `task_status`/`agent_status` can report on it. `Cancel()` closes the subagent's `stopCh`, and the completion goroutine's `defer subAgent.shutdownTransient()` permanently closes the doc/memory maintenance channels via `sync.Once`-guarded shutdowns. There is no way for the caller (main agent or LLM) to continue that same subagent conversation — every follow-up requires a brand-new `task` dispatch with no memory of prior work.

## Goal

Let the caller resume a specific cancelled-or-completed subagent run with a new prompt, continuing from its existing conversation history, tools, and permissions — as if the original call had never stopped.

## Scope

- Resumable statuses: `RunCancelled` and `RunDone` only. `RunFailed` stays terminal (a fresh dispatch is the right recovery for an error) and `RunRunning`/`RunQueued` are rejected (already active).
- Resume reuses the same `task_id` — the existing `AgentRun` flips back to `Running` and keeps accumulating the same transcript. `task_status`/`agent_status` polling is unaffected.
- Resume's execution mode (`run_in_background` true/false) is independent of how the original run was launched.
- Out of scope: resuming a run that was part of a `shared_notes` group — resumed runs execute standalone, never rejoin a notes bus.

## Design

### 1. Task tool: `resume_task_id` param

Add `resume_task_id` (string, optional) to `TaskTool.Definition()`'s schema, alongside the existing `prompt`/`context`/`run_in_background` params.

In `TaskTool.Execute`, when `resume_task_id` is set, branch into a resume path before any of the normal "construct a fresh subagent" logic (spec lookup, `NewAgent`, tool/permission wiring, etc. — all skipped, since the original subagent already has all of that).

### 2. Eligibility check

Look up the run via `t.runs.Get(resume_task_id)`. Reject (return a tool error string, not a hard error) unless all of:
- the run exists,
- `run.Dispatcher` matches the calling agent's spec name (same ownership rule `CancelOwned` already enforces),
- `run.CurrentStatus()` is `RunCancelled` or `RunDone`,
- `run.Sub != nil`.

This reuses the existing dispatcher-ownership convention rather than introducing a new one.

### 3. Rebuilding conversation history

`Agent.Step` is stateless — it takes `messages []Message` as its sole input and returns only the delta; it never retains history on the `*Agent` itself. So the resume path cannot just call `subAgent.Step(newPromptMsg)`.

Instead: reuse `run.transcript`, which already accumulates the full conversation (seeded with the original prompt/context, then streamed via the `OnMessage` hook as the subagent runs — see `attachRunTranscript` in `subagent.go`). To resume:
1. Build the new message(s) the same way the original dispatch does (`context` → optional system message, `prompt` → user message).
2. Append them directly onto `run.transcript` (`run.appendTranscript`).
3. Pass `run.TranscriptPublic()` (now including the new message) as the full `messages` slice into `subAgent.Step(...)`.

No new history-tracking field is needed. Note the existing `transcriptCap` (200 messages) still applies — very long resumed conversations can lose early context, same constraint fresh dispatches don't have to worry about but resumes now inherit.

### 4. Un-cancelling the subagent

`shutdownTransient()` (`agent.go`) always runs (via `defer`) once a dispatch's run goes terminal, and it does two things resume must undo:
- `Cancel()` closes `stopCh` — undone today by `Agent.ResetCancellation()`, which already exists.
- `docMaintShutdown()`/`memoryMaintShutdown()` permanently close `docMaintCh`/`memoryMaintCh` via `sync.Once` — there is currently no way to re-arm these.

Add `Agent.RearmMaintenance()`, mirroring the setup `NewAgent` does at agent.go:640-649:
- recreate `docMaintCh`, `memoryMaintCh`, `docMaintDone`, `memoryMaintDone`,
- reset `docMaintShutdownOnce`, `memoryMaintShutdownOnce` (assign fresh `sync.Once{}` values), and `docMaintClosing` back to `false` (under the existing `docMaintMu`),
- restart `go a.memoryMaintenanceWorker()` and `go a.docMaintenanceWorker()`,
- call `a.ResetCancellation()`.

The resume path always calls `RearmMaintenance()` unconditionally — a resumable run (by definition terminal) always had `shutdownTransient` run against it, so this is never a no-op-turned-double-init.

### 5. Concurrency slot

Add `AgentRun.beginResume()` (mirrors the existing `markQueued`/`beginExecution` pair): transitions `RunCancelled`/`RunDone` → `RunRunning`, resets `EndedAt` to zero. Returns false (reject) if the run isn't in one of those two states — handles the race where two resume calls target the same run concurrently.

After `beginResume()` succeeds, the resume path goes through the *same* queue/acquire flow a fresh dispatch uses: `run.markQueued()` (if `t.runs.MaxConcurrent() > 0`) → `t.runs.Acquire(stopCh)` → `run.beginExecution()` → `subAgent.setOwnSlot(release)`. This keeps resumed runs subject to `max_concurrent_agents` exactly like fresh ones.

`run.Cancel` (the closure over `subAgent.Cancel`) does not need to be recreated — it's still valid since `subAgent` itself persists across resume.

### 6. Dispatch (sync vs background)

Once armed and slotted, the resume path forks on the resume call's own `run_in_background` (independent of the original run's mode) into the same two code paths `TaskTool.Execute` already has for fresh dispatch — same `executeSubAgent`/`executeSubAgentWithTranscript` call, same `defer subAgent.shutdownTransient()`, same `run.finishOK`/`run.finishErr` + `runs.notifyDone(run)` on completion. Practically this means factoring the shared "acquire slot → run subagent → tear down → finish run" logic out of the current fresh-dispatch code so both paths call it, rather than duplicating it for resume.

### 7. Loop protection

The existing re-dispatch guard (`Agent.NoteSubagentDispatch`, keyed by subagent name) that stops a model from repeatedly re-launching the same subagent also applies on the resume path, using `run.Name` as the key. Prevents an equivalent infinite-resume loop.

## Error handling

- Unknown `resume_task_id`, ownership mismatch, or ineligible status: return a descriptive tool-result error string (matching the existing pattern for unknown task IDs in `agent_status`/`task_status`), not a hard Go error — the LLM should be able to read and react to it.
- Loop-guard trip: same message format as the existing fresh-dispatch guard.

## Testing

- Unit tests in `internal/agent` covering: resume of a cancelled run (sync and background), resume of a done run, rejection of resuming a failed/running/queued run, ownership-mismatch rejection, unknown-id rejection, transcript continuity (new Step sees prior tool calls/results), and that `RearmMaintenance` actually unblocks queuing after a prior `shutdownTransient`.
- Existing `AgentRunRegistry`/`TaskTool` tests should continue to pass unchanged (resume is additive — no changes to fresh-dispatch behavior).
