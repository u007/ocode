# Part 2 — Agent Run Registry & Async `task`

> REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans.

**Produces:** `AgentRunRegistry` in `internal/agent`, a `run_in_background` mode
for the `task` tool, the `agent_status` tool, and best-effort agent cancellation.

**Prerequisite:** Part 1 is merged (`Agent.Procs()` exists).

**Key files:**
- Create: `internal/agent/agent_runs.go`
- Create: `internal/agent/agent_runs_test.go`
- Modify: `internal/agent/agent.go` (`stopCh`, `Cancel`, `Step` check, wiring)
- Modify: `internal/agent/subagent.go` (`task` background path)

---

## Task 1: AgentRun + AgentRunRegistry

**Files:**
- Create: `internal/agent/agent_runs.go`
- Test: `internal/agent/agent_runs_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/agent_runs_test.go`:

```go
package agent

import (
	"strings"
	"testing"
)

func TestAgentRunRegistryLifecycle(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	if run.ID == "" || run.Name != "explore" {
		t.Fatalf("bad run: %+v", run)
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("new run status = %s, want running", run.statusValue())
	}
	run.appendTranscript(Message{Role: "assistant", Content: "line one\nline two"})
	run.finishOK("final result")
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want done", run.statusValue())
	}
	got, _ := r.Get(run.ID)
	if got.Result != "final result" {
		t.Fatalf("result = %q", got.Result)
	}
}

func TestAgentRunTranscriptCap(t *testing.T) {
	run := &AgentRun{ID: "agent-1", Name: "x", Status: RunRunning}
	for i := 0; i < transcriptCap+50; i++ {
		run.appendTranscript(Message{Role: "assistant", Content: "msg"})
	}
	if n := len(run.transcriptCopy()); n > transcriptCap {
		t.Fatalf("transcript not capped: %d", n)
	}
}

func TestAgentRunLastLines(t *testing.T) {
	run := &AgentRun{ID: "agent-1", Name: "x", Status: RunRunning}
	run.appendTranscript(Message{Role: "assistant", Content: "alpha\nbeta\ngamma"})
	lines := run.LastLines(2)
	if len(lines) != 2 || lines[0] != "beta" || lines[1] != "gamma" {
		t.Fatalf("LastLines = %v", lines)
	}
}

func TestAgentRunRegistryUnknown(t *testing.T) {
	r := NewAgentRunRegistry()
	if _, ok := r.Get("agent-999"); ok {
		t.Fatal("expected miss for unknown run")
	}
	_ = strings.TrimSpace("") // keep strings import used
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentRun -v`
Expected: FAIL — registry/types undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/agent/agent_runs.go`:

```go
package agent

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jamesmercstudio/ocode/internal/tool"
)

// RunStatus is the lifecycle state of an async subagent run.
type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunDone    RunStatus = "done"
	RunFailed  RunStatus = "failed"
)

// transcriptCap bounds the per-run stored message count.
const transcriptCap = 200

// AgentRun is one async subagent execution.
type AgentRun struct {
	ID        string
	Name      string
	Status    RunStatus
	Result    string
	Err       string
	StartedAt time.Time
	EndedAt   time.Time
	Procs     *tool.ProcessRegistry // the subagent's process registry

	mu         sync.Mutex
	transcript []Message
}

func (r *AgentRun) statusValue() RunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Status
}

func (r *AgentRun) appendTranscript(m Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transcript = append(r.transcript, m)
	if len(r.transcript) > transcriptCap {
		r.transcript = r.transcript[len(r.transcript)-transcriptCap:]
	}
}

func (r *AgentRun) transcriptCopy() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.transcript))
	copy(out, r.transcript)
	return out
}

// LastLines returns the last n non-empty text lines across the transcript.
func (r *AgentRun) LastLines(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var lines []string
	for _, m := range r.transcript {
		if m.Content == "" {
			continue
		}
		for _, ln := range strings.Split(m.Content, "\n") {
			if strings.TrimSpace(ln) != "" {
				lines = append(lines, strings.TrimSpace(ln))
			}
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func (r *AgentRun) finishOK(result string) {
	r.mu.Lock()
	r.Status = RunDone
	r.Result = result
	r.EndedAt = time.Now()
	r.mu.Unlock()
}

func (r *AgentRun) finishErr(err string) {
	r.mu.Lock()
	r.Status = RunFailed
	r.Err = err
	r.EndedAt = time.Now()
	r.mu.Unlock()
}

// AgentRunRegistry holds the main agent's async subagent runs.
type AgentRunRegistry struct {
	mu      sync.Mutex
	runs    map[string]*AgentRun
	order   []string
	counter int
	onDone  func(*AgentRun)
}

func NewAgentRunRegistry() *AgentRunRegistry {
	return &AgentRunRegistry{runs: map[string]*AgentRun{}}
}

// SetOnDone registers a callback fired (on its own goroutine) when a run ends.
func (r *AgentRunRegistry) SetOnDone(fn func(*AgentRun)) {
	r.mu.Lock()
	r.onDone = fn
	r.mu.Unlock()
}

// New registers a fresh run in the running state.
func (r *AgentRunRegistry) New(name string) *AgentRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counter++
	id := "agent-" + strconv.Itoa(r.counter)
	run := &AgentRun{ID: id, Name: name, Status: RunRunning, StartedAt: time.Now()}
	r.runs[id] = run
	r.order = append(r.order, id)
	return run
}

// fireDone invokes the onDone callback for a finished run.
func (r *AgentRunRegistry) fireDone(run *AgentRun) {
	r.mu.Lock()
	fn := r.onDone
	r.mu.Unlock()
	if fn != nil {
		go fn(run)
	}
}

// Get returns a run by id.
func (r *AgentRunRegistry) Get(id string) (*AgentRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	return run, ok
}

// RunInfo is a read-only snapshot for the TUI.
type RunInfo struct {
	ID        string
	Name      string
	Status    RunStatus
	LastLines []string
}

// Snapshot returns all runs in start order.
func (r *AgentRunRegistry) Snapshot() []RunInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RunInfo, 0, len(r.order))
	for _, id := range r.order {
		run := r.runs[id]
		out = append(out, RunInfo{
			ID: run.ID, Name: run.Name, Status: run.statusValue(),
			LastLines: run.LastLines(2),
		})
	}
	return out
}

// RunningCount returns the number of runs still running.
func (r *AgentRunRegistry) RunningCount() int {
	n := 0
	for _, ri := range r.Snapshot() {
		if ri.Status == RunRunning {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentRun -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent_runs.go internal/agent/agent_runs_test.go
git commit -m "feat(agent): add AgentRunRegistry with capped transcript"
```

---

## Task 2: Best-effort agent cancellation (`stopCh`)

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/agent_test.go`:

```go
func TestAgentCancelStopsBeforeNextStep(t *testing.T) {
	a := NewAgent(nil, nil, nil) // nil client → Step returns the stub message
	a.Cancel()
	if !a.cancelled() {
		t.Fatal("expected agent to report cancelled")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentCancel -v`
Expected: FAIL — `Cancel` / `cancelled` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/agent.go`, add to the `Agent` struct:

```go
	stopCh chan struct{}
	stopMu sync.Mutex
```

In `NewAgent`, initialise it (with the other field setup):

```go
	a.stopCh = make(chan struct{})
```

Add methods near `Activity()`:

```go
// Cancel signals the agent's Step loop to stop before the next LLM call.
// Best-effort: an in-flight HTTP call is not interrupted.
func (a *Agent) Cancel() {
	a.stopMu.Lock()
	defer a.stopMu.Unlock()
	select {
	case <-a.stopCh:
		// already closed
	default:
		close(a.stopCh)
	}
}

// cancelled reports whether Cancel has been called.
func (a *Agent) cancelled() bool {
	select {
	case <-a.stopCh:
		return true
	default:
		return false
	}
}
```

In `Step`, at the very top of the `for i := 0; ; i++ {` loop body, add:

```go
		if a.cancelled() {
			return newMsgs, nil
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./internal/agent/ -run TestAgentCancel -v`
Expected: build OK, test PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): best-effort Step cancellation via stopCh"
```

---

## Task 3: `task` tool gains `run_in_background`

**Files:**
- Modify: `internal/agent/subagent.go`
- Modify: `internal/agent/agent.go` (`AgentRunRegistry` field + wiring)
- Test: `internal/agent/agent_runs_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/agent_runs_test.go`:

```go
func TestTaskToolBackgroundReturnsRunID(t *testing.T) {
	main := NewAgent(nil, nil, nil) // nil client → subagent Step returns instantly
	tt := TaskTool{mainAgent: main, registry: DefaultAgentRegistry, runs: main.Runs()}
	out, err := tt.Execute(jsonRaw(`{"prompt":"do x","agent":"general","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !contains(out, "agent-1") {
		t.Fatalf("expected run id in output, got %q", out)
	}
	if main.Runs().RunningCount() > 1 {
		t.Fatalf("unexpected running count")
	}
}
```

Add helpers at the bottom of the test file if not already present:

```go
func jsonRaw(s string) []byte         { return []byte(s) }
func contains(s, sub string) bool     { return strings.Contains(s, sub) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestTaskToolBackground -v`
Expected: FAIL — `TaskTool` has no `runs` field, `Agent.Runs()` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/agent.go`:

Add to the `Agent` struct:

```go
	runs *AgentRunRegistry
```

In `NewAgent`, initialise it and pass it to the task tool:

```go
	a.runs = NewAgentRunRegistry()
```

Change the existing task-tool registration line to include `runs`:

```go
	a.tools["task"] = TaskTool{mainAgent: a, registry: DefaultAgentRegistry, runs: a.runs}
```

Add an accessor near `Activity()`:

```go
// Runs returns the registry of async subagent runs.
func (a *Agent) Runs() *AgentRunRegistry { return a.runs }
```

Note: `SetChildSessionPersistence` already does a `TaskTool` type-assert and
reassign — that keeps the `runs` field because it copies the whole struct.

In `internal/agent/subagent.go`:

Add the field to `TaskTool`:

```go
type TaskTool struct {
	mainAgent        *Agent
	registry         *AgentRegistry
	runs             *AgentRunRegistry
	persistChildSess func(sessionID, title string, messages []Message, metadata map[string]any) error
}
```

In `TaskTool.Definition()`, add to `properties`:

```go
				"run_in_background": map[string]interface{}{
					"type":        "boolean",
					"description": "Run the sub-agent in the background. Returns a run id immediately; poll with agent_status. The result is also pushed to you automatically on completion.",
				},
```

In `TaskTool.Execute`, extend the params struct and add the background branch
right after the `spec`/`tools` are resolved (after `tools := t.getToolsForDef(spec)`):

```go
	var params struct {
		Prompt          string `json:"prompt"`
		Agent           string `json:"agent"`
		Context         string `json:"context"`
		RunInBackground bool   `json:"run_in_background"`
	}
```

After `tools := t.getToolsForDef(spec)` and before the existing
`subAgent := NewAgent(...)` line, insert:

```go
	systemPrompt := spec.SystemPrompt
	if params.Context != "" {
		systemPrompt += "\nBackground Context: " + params.Context
	}
	subAgentMsgs := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: params.Prompt},
	}

	if params.RunInBackground {
		if t.runs == nil {
			return "", fmt.Errorf("background execution unavailable: no run registry")
		}
		run := t.runs.New(spec.Name)
		sub := NewAgent(t.mainAgent.client, tools, t.mainAgent.config)
		sub.mode = t.mainAgent.mode
		if spec.MaxSteps > 0 {
			sub.maxSteps = spec.MaxSteps
		}
		if len(spec.Permissions) > 0 {
			_, pm := buildPermissionManagerFromAgentWithDiags(spec.Permissions)
			sub.permissions = pm
		}
		run.Procs = sub.Procs()
		sub.OnMessage = func(m Message) { run.appendTranscript(m) }
		if t.mainAgent.activity != nil {
			t.mainAgent.activity.agentStarted(spec.Name)
		}
		go func() {
			resp, err := sub.Step(subAgentMsgs)
			if t.mainAgent.activity != nil {
				t.mainAgent.activity.agentDone(spec.Name)
			}
			if err != nil {
				run.finishErr(err.Error())
			} else {
				var b strings.Builder
				for _, m := range resp {
					if m.Role == "assistant" && m.Content != "" {
						b.WriteString(m.Content)
					}
				}
				run.finishOK(b.String())
			}
			t.runs.fireDone(run)
		}()
		return fmt.Sprintf("Started background agent %s (run %s). Poll with agent_status(id=%q); the result is pushed to you on completion.", spec.Name, run.ID, run.ID), nil
	}
```

The existing synchronous code below this point already rebuilds `systemPrompt`
and `subAgentMsgs`. Delete the now-duplicated synchronous declarations of
`systemPrompt` and `subAgentMsgs` (the lines between `subAgent.permissions = pm`
handling and `if t.mainAgent.activity != nil { ... agentStarted ... }`) so each
identifier is declared once. The synchronous path keeps using `subAgent` and the
shared `subAgentMsgs`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./internal/agent/ -run TestTaskTool -v`
Expected: build OK, test PASS. Also run `go test ./internal/agent/ -run TestSub -v`
to confirm the synchronous path still works.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/subagent.go internal/agent/agent_runs_test.go
git commit -m "feat(agent): task run_in_background spawns async subagent run"
```

---

## Task 4: `agent_status` tool

**Files:**
- Create: `internal/agent/agent_status_tool.go`
- Modify: `internal/agent/agent.go` (register the tool)
- Test: `internal/agent/agent_runs_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/agent_runs_test.go`:

```go
func TestAgentStatusTool(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.finishOK("the answer")
	st := AgentStatusTool{runs: r}
	out, err := st.Execute(jsonRaw(`{"id":"` + run.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !contains(out, "done") || !contains(out, "the answer") {
		t.Fatalf("agent_status output wrong: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentStatusTool -v`
Expected: FAIL — `AgentStatusTool` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/agent/agent_status_tool.go`:

```go
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AgentStatusTool reports the status (and on completion, the result) of an
// async subagent run.
type AgentStatusTool struct {
	runs *AgentRunRegistry
}

func (t AgentStatusTool) Name() string        { return "agent_status" }
func (t AgentStatusTool) Description() string { return "Check the status of a background agent run" }
func (t AgentStatusTool) Parallel() bool      { return true }
func (t AgentStatusTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "agent_status",
		"description": "Check a background agent run's status. While running, returns recent transcript lines; on completion returns the final result or error.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The run id returned by task(run_in_background).",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t AgentStatusTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if t.runs == nil {
		return "", fmt.Errorf("no run registry")
	}
	run, ok := t.runs.Get(params.ID)
	if !ok {
		return fmt.Sprintf("Error: unknown run id %q", params.ID), nil
	}
	switch run.statusValue() {
	case RunDone:
		return fmt.Sprintf("[%s status=done]\n%s", run.ID, run.Result), nil
	case RunFailed:
		return fmt.Sprintf("[%s status=failed]\n%s", run.ID, run.Err), nil
	default:
		lines := run.LastLines(4)
		return fmt.Sprintf("[%s status=running]\n%s", run.ID, strings.Join(lines, "\n")), nil
	}
}
```

In `internal/agent/agent.go`, in `NewAgent` (next to the other tool wiring):

```go
	a.tools["agent_status"] = AgentStatusTool{runs: a.runs}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./internal/agent/ -run TestAgentStatusTool -v`
Expected: build OK, test PASS.

- [ ] **Step 5: Full regression**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent_status_tool.go internal/agent/agent.go internal/agent/agent_runs_test.go
git commit -m "feat(agent): add agent_status poll tool"
```
