# Part 3 — `wait` Tool

> REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans.

**Produces:** a `wait` tool — plain duration sleep, or join-on-job until a
process/agent run completes (with a 10-minute ceiling).

**Prerequisite:** Parts 1 and 2 merged (`Agent.Procs()`, `Agent.Runs()` exist).

**Key files:**
- Create: `internal/agent/wait_tool.go`
- Create: `internal/agent/wait_tool_test.go`
- Modify: `internal/agent/agent.go` (register the tool)

---

## Task 1: `wait` tool — plain duration + ceiling clamp

**Files:**
- Create: `internal/agent/wait_tool.go`
- Test: `internal/agent/wait_tool_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/wait_tool_test.go`:

```go
package agent

import (
	"strings"
	"testing"
	"time"
)

func TestWaitToolPlainDuration(t *testing.T) {
	wt := WaitTool{}
	start := time.Now()
	out, err := wt.Execute([]byte(`{"seconds":1}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("wait returned too early: %v", elapsed)
	}
	if !strings.Contains(out, "1") {
		t.Fatalf("output: %q", out)
	}
}

func TestWaitToolClampsToMax(t *testing.T) {
	wt := WaitTool{}
	d, clamped := resolveWaitDuration(0, 9999)
	if !clamped {
		t.Fatal("expected clamp flag")
	}
	if d != waitCeiling {
		t.Fatalf("duration = %v, want ceiling %v", d, waitCeiling)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestWaitTool -v`
Expected: FAIL — `WaitTool`, `resolveWaitDuration`, `waitCeiling` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/agent/wait_tool.go`:

```go
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// waitCeiling caps any wait so the session cannot hang.
const waitCeiling = 10 * time.Minute

// waitPollInterval is how often a targeted wait re-checks the job.
const waitPollInterval = 250 * time.Millisecond

// WaitTool sleeps for a duration, or blocks until a named job completes.
type WaitTool struct {
	procs *tool.ProcessRegistry
	runs  *AgentRunRegistry
}

func (t WaitTool) Name() string        { return "wait" }
func (t WaitTool) Description() string { return "Wait for a duration or until a background job finishes" }
func (t WaitTool) Parallel() bool      { return false }
func (t WaitTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "wait",
		"description": "Pause for a fixed duration, or (with 'for') block until a background process/agent completes. Capped at 10 minutes.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Seconds to wait. Ignored when 'minutes' is set.",
				},
				"minutes": map[string]interface{}{
					"type":        "integer",
					"description": "Minutes to wait. Takes precedence over 'seconds'.",
				},
				"for": map[string]interface{}{
					"type":        "string",
					"description": "Optional background process id (proc-N) or agent run id (agent-N) to block on. When set, returns as soon as that job completes.",
				},
			},
		},
	}
}

// resolveWaitDuration converts seconds/minutes into a capped duration.
func resolveWaitDuration(seconds, minutes int) (d time.Duration, clamped bool) {
	if minutes > 0 {
		d = time.Duration(minutes) * time.Minute
	} else {
		d = time.Duration(seconds) * time.Second
	}
	if d <= 0 {
		d = time.Second
	}
	if d > waitCeiling {
		return waitCeiling, true
	}
	return d, false
}

func (t WaitTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Seconds int    `json:"seconds"`
		Minutes int    `json:"minutes"`
		For     string `json:"for"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	d, clamped := resolveWaitDuration(params.Seconds, params.Minutes)

	if params.For != "" {
		return t.waitForJob(params.For, d, clamped), nil
	}

	time.Sleep(d)
	msg := fmt.Sprintf("Waited %s.", d)
	if clamped {
		msg += " (clamped to the 10-minute ceiling)"
	}
	return msg, nil
}

// waitForJob blocks until the named job completes or the deadline passes.
func (t WaitTool) waitForJob(id string, deadline time.Duration, clamped bool) string {
	end := time.Now().Add(deadline)
	for {
		if done, summary := t.jobDone(id); done {
			return summary
		}
		if time.Now().After(end) {
			suffix := ""
			if clamped {
				suffix = " (ceiling reached)"
			}
			return fmt.Sprintf("Timed out after %s waiting for %s; it is still running.%s", deadline, id, suffix)
		}
		time.Sleep(waitPollInterval)
	}
}

// jobDone reports whether the job has finished, with a result summary.
func (t WaitTool) jobDone(id string) (bool, string) {
	if strings.HasPrefix(id, "agent-") && t.runs != nil {
		run, ok := t.runs.Get(id)
		if !ok {
			return true, fmt.Sprintf("Error: unknown run id %q", id)
		}
		switch run.statusValue() {
		case RunDone:
			return true, fmt.Sprintf("[%s done]\n%s", id, run.Result)
		case RunFailed:
			return true, fmt.Sprintf("[%s failed]\n%s", id, run.Err)
		default:
			return false, ""
		}
	}
	if strings.HasPrefix(id, "proc-") && t.procs != nil {
		_, status, code, err := t.procs.Output(id)
		if err != nil {
			return true, fmt.Sprintf("Error: %v", err)
		}
		if status == tool.ProcRunning {
			return false, ""
		}
		return true, fmt.Sprintf("[%s %s exit=%d]", id, status, code)
	}
	return true, fmt.Sprintf("Error: unknown job id %q", id)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestWaitTool -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/wait_tool.go internal/agent/wait_tool_test.go
git commit -m "feat(agent): add wait tool with duration and ceiling clamp"
```

---

## Task 2: Targeted wait join + short-circuit

**Files:**
- Test: `internal/agent/wait_tool_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/wait_tool_test.go`:

```go
func TestWaitToolJoinShortCircuits(t *testing.T) {
	runs := NewAgentRunRegistry()
	run := runs.New("explore")
	run.finishOK("already done")
	wt := WaitTool{runs: runs}
	start := time.Now()
	out, err := wt.Execute([]byte(`{"minutes":10,"for":"` + run.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("join did not short-circuit: waited %v", elapsed)
	}
	if !strings.Contains(out, "already done") {
		t.Fatalf("output: %q", out)
	}
}

func TestWaitToolJoinUnknownID(t *testing.T) {
	wt := WaitTool{runs: NewAgentRunRegistry()}
	out, _ := wt.Execute([]byte(`{"seconds":1,"for":"agent-999"}`))
	if !strings.Contains(out, "unknown") {
		t.Fatalf("expected unknown-id error, got %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./internal/agent/ -run TestWaitToolJoin -v`
Expected: PASS — the Task 1 implementation already covers join + short-circuit.
If it fails, fix `jobDone` / `waitForJob` until green. (This task locks the
behaviour with tests; no new production code is expected.)

- [ ] **Step 3: Commit**

```bash
git add internal/agent/wait_tool_test.go
git commit -m "test(agent): cover wait join short-circuit and unknown id"
```

---

## Task 3: Register `wait` in `NewAgent`

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/agent_test.go`:

```go
func TestNewAgentHasWaitAndAgentStatus(t *testing.T) {
	a := NewAgent(nil, nil, nil)
	for _, name := range []string{"wait", "agent_status"} {
		if _, ok := a.tools[name]; !ok {
			t.Fatalf("agent missing tool %q", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestNewAgentHasWaitAndAgentStatus -v`
Expected: FAIL — `wait` tool not registered.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/agent.go`, in `NewAgent` next to the other tool wiring (after
`a.runs = NewAgentRunRegistry()` and `a.procs = tool.NewProcessRegistry()`):

```go
	a.tools["wait"] = WaitTool{procs: a.procs, runs: a.runs}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./internal/agent/ -run TestNewAgentHasWaitAndAgentStatus -v`
Expected: build OK, test PASS.

- [ ] **Step 5: Full regression**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): register wait tool in NewAgent"
```

---

## Note on scope

`wait for=<id>` resolves ids against the **main agent's** registries only. A
process started inside a subagent (a `proc-N` in that subagent's registry) is
not joinable from the main agent's `wait`. This is acceptable — the main LLM
only holds ids it started itself. Do not add cross-registry lookup.
