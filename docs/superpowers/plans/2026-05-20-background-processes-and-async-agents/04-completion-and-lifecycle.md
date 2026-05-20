# Part 4 — Completion Push, Auto-Resume & Lifecycle

> REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans.

**Produces:** background-job completion events pushed to the TUI, a synthetic
message injected into the conversation, auto-resume of a `Step` when idle, and
teardown of jobs on `/clear` and TUI exit.

**Prerequisite:** Parts 1–3 merged (`Agent.Procs()`, `Agent.Runs()`,
registries with `SetOnDone`).

**Key files:**
- Modify: `internal/agent/agent.go` (job-events channel, wiring, `Shutdown`)
- Modify: `internal/agent/agent_runs.go` (`AgentRun.Sub`, `CancelAll`)
- Modify: `internal/agent/subagent.go` (set `run.Sub`)
- Modify: `internal/tui/model.go` (listen, inject, auto-resume)
- Modify: `internal/tui/commands.go` (`handleNewCmd` teardown)

---

## Task 1: Job-events channel on `Agent`

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/agent_test.go`:

```go
func TestAgentEmitsProcessJobEvent(t *testing.T) {
	a := NewAgent(nil, nil, nil)
	p := a.Procs().StartBackground("echo job-evt")
	select {
	case ev := <-a.JobEvents():
		if ev.Kind != "process" || ev.ID != p.ID {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no job event received")
	}
}
```

Ensure `"time"` is imported in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentEmitsProcessJobEvent -v`
Expected: FAIL — `JobEvents` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/agent.go`:

Add the event type near the top (after the `Agent` struct):

```go
// JobEvent describes a background job (process or agent run) that finished.
type JobEvent struct {
	Kind   string // "process" or "agent"
	ID     string
	Name   string // process command, or agent name
	Status string // exited/killed/done/failed
	Result string // output tail or result text
}
```

Add to the `Agent` struct:

```go
	jobEvents chan JobEvent
```

In `NewAgent`, after `a.procs`/`a.runs` are created and tools wired, add:

```go
	a.jobEvents = make(chan JobEvent, 32)
	a.procs.SetOnDone(func(p *tool.Process) {
		text, status, code, _ := a.procs.Output(p.ID)
		a.emitJob(JobEvent{
			Kind:   "process",
			ID:     p.ID,
			Name:   p.Command,
			Status: string(status),
			Result: fmt.Sprintf("exit %d\n%s", code, text),
		})
	})
	a.runs.SetOnDone(func(r *AgentRun) {
		result := r.Result
		status := "done"
		if r.statusValue() == RunFailed {
			result = r.Err
			status = "failed"
		}
		a.emitJob(JobEvent{
			Kind:   "agent",
			ID:     r.ID,
			Name:   r.Name,
			Status: status,
			Result: result,
		})
	})
```

Add methods near `Activity()`:

```go
// JobEvents is the channel the TUI reads background-job completions from.
func (a *Agent) JobEvents() chan JobEvent { return a.jobEvents }

// emitJob delivers a completion event, dropping it only if the buffer is full.
func (a *Agent) emitJob(ev JobEvent) {
	select {
	case a.jobEvents <- ev:
	default:
		emitDebug("JOB", "job event buffer full, dropped "+ev.ID)
	}
}
```

`fmt` is already imported in `agent.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./internal/agent/ -run TestAgentEmitsProcessJobEvent -v`
Expected: build OK, test PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): emit JobEvent on background job completion"
```

---

## Task 2: TUI listens for job completions

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add the message type and listener command**

In `internal/tui/model.go`, near the other `Msg` type declarations (around the
`activityUpdateMsg` definition), add:

```go
type jobCompletedMsg struct {
	ev agent.JobEvent
}
```

Near `listenActivity` (around line 3289), add:

```go
// listenJobs blocks on the agent's job-events channel and re-arms itself.
func listenJobs(a *agent.Agent) tea.Cmd {
	return func() tea.Msg {
		ev := <-a.JobEvents()
		return jobCompletedMsg{ev: ev}
	}
}
```

- [ ] **Step 2: Start the listener in `Init`**

Replace the body of `func (m model) Init() tea.Cmd`:

```go
func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink, waitForDebugLog(), waitCompactEvent(m.compactStartCh, m.compactCh)}
	if m.agent != nil {
		cmds = append(cmds, listenJobs(m.agent))
	}
	return tea.Batch(cmds...)
}
```

- [ ] **Step 3: Re-arm the listener after every agent swap**

The agent is replaced in several places (`handleModelCmd`, `runNewCmd`/`handleNewCmd`,
`authFinishedMsg`, the agent-switch path near model.go:1933). Each time a new
`agent.NewAgent(...)` is assigned to `m.agent`, the old job-events channel is
abandoned. Add a helper near `wireCompactCallbacks` and call it after each swap:

```go
// rearmJobListener returns a command that listens on the current agent's
// job-events channel. Call after assigning a new m.agent.
func (m *model) rearmJobListener() tea.Cmd {
	if m.agent == nil {
		return nil
	}
	return listenJobs(m.agent)
}
```

For each agent reassignment site, batch `m.rearmJobListener()` into the returned
command. Concretely, in `handleModelCmd` (which currently does not return a cmd),
and in the `authFinishedMsg` case, and in the agent-switch path: after the
`m.agent = agent.NewAgent(...)` line, ensure the surrounding handler returns a
`tea.Cmd` that includes `m.rearmJobListener()`. Where a handler currently returns
nothing, change it to `return m, m.rearmJobListener()`.

- [ ] **Step 4: Build to verify**

Run: `go build ./...`
Expected: build OK (no test yet — the handler comes next).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): listen for background job completion events"
```

---

## Task 3: Inject synthetic message + auto-resume

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add a pending-jobs buffer to the model**

In the `model` struct (near `queuedInputs`), add:

```go
	pendingJobMsgs []message
```

- [ ] **Step 2: Handle `jobCompletedMsg`**

In `Update`, add a new `case` alongside `activityUpdateMsg`:

```go
	case jobCompletedMsg:
		ev := msg.ev
		var header string
		if ev.Kind == "agent" {
			header = fmt.Sprintf("[Background agent %s (%s) %s]", ev.ID, ev.Name, ev.Status)
		} else {
			header = fmt.Sprintf("[Background process %s %s]", ev.ID, ev.Status)
		}
		body := header + "\n" + ev.Result
		injected := message{
			role: roleUser,
			text: body,
			raw:  &agent.Message{Role: "user", Content: body},
		}
		if m.streaming {
			// Mid-turn: buffer; consumed at the start of the next turn.
			m.pendingJobMsgs = append(m.pendingJobMsgs, injected)
		} else {
			// Idle: inject and auto-resume a Step (renders as a new turn).
			m.messages = append(m.messages, message{
				role: roleAssistant,
				text: hintStyle.Render("↩ " + header + " — resuming"),
				transient: true,
			})
			m.messages = append(m.messages, injected)
			m.renderTranscript()
			m.viewport.GotoBottom()
			cmd := m.askAgent()
			if m.agent != nil {
				return m, tea.Batch(cmd, listenJobs(m.agent))
			}
			return m, cmd
		}
		if m.agent != nil {
			return m, listenJobs(m.agent)
		}
		return m, nil
```

- [ ] **Step 3: Drain the buffer on `streamDoneMsg`**

In the `streamDoneMsg` case, in the `else` branch (where `msg.err == nil`), the
existing code drains `m.queuedInputs`. Immediately **before** that
`if len(m.queuedInputs) > 0` block, add a drain of pending job messages:

```go
			if len(m.pendingJobMsgs) > 0 && m.agent != nil {
				m.messages = append(m.messages, message{
					role: roleAssistant,
					text: hintStyle.Render("↩ background job(s) completed — resuming"),
					transient: true,
				})
				m.messages = append(m.messages, m.pendingJobMsgs...)
				m.pendingJobMsgs = nil
				m.renderTranscript()
				m.viewport.GotoBottom()
				return m, m.askAgent()
			}
```

`askAgent` builds the agent message list from `m.messages`, so appending the
injected `roleUser` messages and calling it resumes the conversation. Multiple
buffered completions are consumed by the single resumed `Step`.

- [ ] **Step 4: Build and smoke-check**

Run: `go build ./...`
Expected: build OK.

Manual check (if an LLM is configured): start a backgrounded `bash` with
`sleep 3; echo done`, let the turn finish, confirm the TUI shows
`↩ ... resuming` and a new turn appears when the process exits.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): inject job results and auto-resume when idle"
```

---

## Task 4: Lifecycle teardown on `/clear` and exit

**Files:**
- Modify: `internal/agent/agent_runs.go`
- Modify: `internal/agent/subagent.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/model.go`
- Test: `internal/agent/agent_runs_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/agent_runs_test.go`:

```go
func TestAgentRunRegistryCancelAll(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	cancelled := false
	run.Sub = NewAgent(nil, nil, nil)
	run.Cancel = func() { cancelled = true }
	r.CancelAll()
	if !cancelled {
		t.Fatal("CancelAll did not invoke run cancel")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentRunRegistryCancelAll -v`
Expected: FAIL — `AgentRun.Sub`, `AgentRun.Cancel`, `CancelAll` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/agent_runs.go`, add two fields to the `AgentRun` struct:

```go
	Sub    *Agent       // the subagent (for teardown)
	Cancel func()       // cancels the subagent's Step loop
```

Add a method to `AgentRunRegistry`:

```go
// CancelAll cancels every running subagent and kills their background
// processes. Used on /clear and TUI exit.
func (r *AgentRunRegistry) CancelAll() {
	r.mu.Lock()
	runs := make([]*AgentRun, 0, len(r.runs))
	for _, run := range r.runs {
		runs = append(runs, run)
	}
	r.mu.Unlock()
	for _, run := range runs {
		if run.statusValue() != RunRunning {
			continue
		}
		if run.Cancel != nil {
			run.Cancel()
		}
		if run.Procs != nil {
			run.Procs.KillAll()
		}
	}
}
```

In `internal/agent/subagent.go`, in the `params.RunInBackground` branch, after
`run.Procs = sub.Procs()` add:

```go
		run.Sub = sub
		run.Cancel = sub.Cancel
```

In `internal/agent/agent.go`, add a `Shutdown` method near `Activity()`:

```go
// Shutdown cancels the agent, kills its background processes, and cancels all
// async subagent runs. Call on /clear and TUI exit.
func (a *Agent) Shutdown() {
	a.Cancel()
	if a.procs != nil {
		a.procs.KillAll()
	}
	if a.runs != nil {
		a.runs.CancelAll()
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./internal/agent/ -run TestAgentRunRegistryCancelAll -v`
Expected: build OK, test PASS.

- [ ] **Step 5: Call `Shutdown` from the TUI**

In `internal/tui/commands.go`, in `handleNewCmd`, at the very top (before
`m.saveSession()`), add:

```go
	if m.agent != nil {
		m.agent.Shutdown()
	}
```

In `internal/tui/model.go`, find the quit path (the `tea.KeyMsg`/key handler
that returns `tea.Quit` — search for `tea.Quit`). Immediately before returning
`tea.Quit`, add:

```go
	if m.agent != nil {
		m.agent.Shutdown()
	}
```

If `tea.Quit` is returned from multiple sites, add the guard at each site.

- [ ] **Step 6: Full regression**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/agent_runs.go internal/agent/subagent.go internal/agent/agent.go internal/tui/commands.go internal/tui/model.go internal/agent/agent_runs_test.go
git commit -m "feat: kill background jobs on /clear and TUI exit"
```
