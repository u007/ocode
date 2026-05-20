# Part 1 — Process Registry & Background Bash

> REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans.

**Produces:** `ProcessRegistry` in `internal/tool`, a `run_in_background` mode for
the `bash` tool, and the `bash_output` / `kill_shell` tools. Self-contained.

**Key files:**
- Create: `internal/tool/process.go`
- Create: `internal/tool/process_test.go`
- Modify: `internal/tool/exec.go`
- Create: `internal/tool/process_tools.go`
- Modify: `internal/agent/agent.go` (wiring in `NewAgent`)

---

## Task 1: ProcessRegistry core (records, ring buffer)

**Files:**
- Create: `internal/tool/process.go`
- Test: `internal/tool/process_test.go`

- [ ] **Step 1: Write the failing test**

```go
package tool

import (
	"strings"
	"testing"
)

func TestProcessRingBufferTruncates(t *testing.T) {
	p := &Process{ID: "proc-1", Command: "x", Status: ProcRunning}
	// Write more than the cap.
	big := strings.Repeat("a", procBufferCap+5000)
	p.appendOutput([]byte(big))
	text, _ := p.readSince()
	if len(text) > procBufferCap+64 {
		t.Fatalf("buffer not capped: got %d bytes", len(text))
	}
	if !strings.Contains(text, "truncated") {
		t.Fatalf("expected truncation marker, got prefix %q", text[:40])
	}
}

func TestProcessReadSinceIsIncremental(t *testing.T) {
	p := &Process{ID: "proc-1", Command: "x", Status: ProcRunning}
	p.appendOutput([]byte("hello "))
	first, _ := p.readSince()
	if first != "hello " {
		t.Fatalf("first read = %q", first)
	}
	p.appendOutput([]byte("world"))
	second, _ := p.readSince()
	if second != "world" {
		t.Fatalf("second read = %q, want incremental %q", second, "world")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run TestProcess -v`
Expected: FAIL — `Process`, `procBufferCap`, `appendOutput`, `readSince` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tool/process.go`:

```go
package tool

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// ProcStatus is the lifecycle state of a background process.
type ProcStatus string

const (
	ProcRunning ProcStatus = "running"
	ProcExited  ProcStatus = "exited"
	ProcKilled  ProcStatus = "killed"
)

// procBufferCap bounds the per-process combined stdout+stderr buffer.
const procBufferCap = 256 * 1024

// Process is one background shell process.
type Process struct {
	ID        string
	Command   string
	Status    ProcStatus
	ExitCode  int
	StartedAt time.Time
	EndedAt   time.Time

	mu         sync.Mutex
	buf        []byte // last <=procBufferCap bytes of the logical stream
	dropped    int    // count of bytes dropped off the front
	readCursor int    // logical offset already returned by readSince
	cmd        *exec.Cmd
}

// appendOutput appends process output, dropping oldest bytes past the cap.
func (p *Process) appendOutput(b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	if len(p.buf) > procBufferCap {
		over := len(p.buf) - procBufferCap
		p.buf = p.buf[over:]
		p.dropped += over
	}
}

// readSince returns logical-stream bytes not yet returned, advancing the
// cursor. The second return is the current status string.
func (p *Process) readSince() (string, ProcStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	logicalEnd := p.dropped + len(p.buf)
	start := p.readCursor
	var prefix string
	if start < p.dropped {
		prefix = fmt.Sprintf("[…truncated %d bytes]\n", p.dropped-start)
		start = p.dropped
	}
	out := prefix + string(p.buf[start-p.dropped:])
	p.readCursor = logicalEnd
	return out, p.Status
}

// snapshotStatus returns status and exit code under the lock.
func (p *Process) snapshotStatus() (ProcStatus, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Status, p.ExitCode
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -run TestProcess -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tool/process.go internal/tool/process_test.go
git commit -m "feat(tool): add Process record with capped ring buffer"
```

---

## Task 2: ProcessRegistry — start, stream, exit, kill

**Files:**
- Modify: `internal/tool/process.go`
- Test: `internal/tool/process_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tool/process_test.go`:

```go
func TestProcessRegistryRunAndCapture(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("echo hello-bg")
	if p.ID == "" {
		t.Fatal("expected non-empty process id")
	}
	// Poll until the process exits (cap the wait).
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, _, code, err := r.Output(p.ID)
	if err != nil {
		t.Fatalf("Output err: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "hello-bg") {
		t.Fatalf("output missing echo: %q", out)
	}
}

func TestProcessRegistryUnknownID(t *testing.T) {
	r := NewProcessRegistry()
	if _, _, _, err := r.Output("proc-999"); err == nil {
		t.Fatal("expected error for unknown id")
	}
	if _, err := r.Kill("proc-999"); err == nil {
		t.Fatal("expected error killing unknown id")
	}
}
```

Add `"time"` to the test file imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run TestProcessRegistry -v`
Expected: FAIL — `NewProcessRegistry`, `StartBackground`, `Output`, `Kill` undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/tool/process.go`:

```go
import (
	"bufio"
	"io"
	"runtime"
	"strconv"
	"syscall"
)

// ProcessRegistry holds an agent's background processes.
type ProcessRegistry struct {
	mu      sync.Mutex
	procs   map[string]*Process
	order   []string
	counter int
	onDone  func(*Process)
}

func NewProcessRegistry() *ProcessRegistry {
	return &ProcessRegistry{procs: map[string]*Process{}}
}

// SetOnDone registers a callback fired (on its own goroutine) when a process
// exits or is killed.
func (r *ProcessRegistry) SetOnDone(fn func(*Process)) {
	r.mu.Lock()
	r.onDone = fn
	r.mu.Unlock()
}

// StartBackground launches command detached and returns its Process record.
func (r *ProcessRegistry) StartBackground(command string) *Process {
	r.mu.Lock()
	r.counter++
	id := "proc-" + strconv.Itoa(r.counter)
	p := &Process{ID: id, Command: command, Status: ProcRunning, StartedAt: time.Now()}
	r.procs[id] = p
	r.order = append(r.order, id)
	onDone := r.onDone
	r.mu.Unlock()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	p.cmd = cmd

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		p.appendOutput([]byte("failed to start: " + err.Error()))
		r.finish(p, 1, ProcExited, onDone)
		return p
	}

	pump := func(rc io.Reader) {
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			p.appendOutput(append(sc.Bytes(), '\n'))
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pump(stdout) }()
	go func() { defer wg.Done(); pump(stderr) }()

	go func() {
		wg.Wait()
		err := cmd.Wait()
		code := 0
		status := ProcExited
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		p.mu.Lock()
		if p.Status == ProcKilled {
			status = ProcKilled
		}
		p.mu.Unlock()
		r.finish(p, code, status, onDone)
	}()
	return p
}

// finish records terminal state and fires onDone once.
func (r *ProcessRegistry) finish(p *Process, code int, status ProcStatus, onDone func(*Process)) {
	p.mu.Lock()
	if p.Status != ProcRunning && p.EndedAt.IsZero() == false {
		p.mu.Unlock()
		return
	}
	if status != ProcKilled {
		p.Status = status
	}
	p.ExitCode = code
	p.EndedAt = time.Now()
	p.mu.Unlock()
	if onDone != nil {
		go onDone(p)
	}
}

// Output returns incremental output, status, and exit code for a process.
func (r *ProcessRegistry) Output(id string) (text string, status ProcStatus, exitCode int, err error) {
	r.mu.Lock()
	p, ok := r.procs[id]
	r.mu.Unlock()
	if !ok {
		return "", "", 0, fmt.Errorf("unknown process id %q", id)
	}
	out, st := p.readSince()
	_, code := p.snapshotStatus()
	return out, st, code, nil
}

// Kill terminates a process group. Idempotent.
func (r *ProcessRegistry) Kill(id string) (string, error) {
	r.mu.Lock()
	p, ok := r.procs[id]
	r.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("unknown process id %q", id)
	}
	st, code := p.snapshotStatus()
	if st != ProcRunning {
		return fmt.Sprintf("process %s already %s (exit %d)", id, st, code), nil
	}
	p.mu.Lock()
	p.Status = ProcKilled
	cmd := p.cmd
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		if runtime.GOOS == "windows" {
			_ = cmd.Process.Kill()
		} else {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	return fmt.Sprintf("process %s killed", id), nil
}

// KillAll terminates every running process (lifecycle teardown).
func (r *ProcessRegistry) KillAll() {
	r.mu.Lock()
	ids := append([]string(nil), r.order...)
	r.mu.Unlock()
	for _, id := range ids {
		_, _ = r.Kill(id)
	}
}

// ProcInfo is a read-only snapshot for the TUI.
type ProcInfo struct {
	ID       string
	Command  string
	Status   ProcStatus
	ExitCode int
}

// Snapshot returns all processes in start order.
func (r *ProcessRegistry) Snapshot() []ProcInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProcInfo, 0, len(r.order))
	for _, id := range r.order {
		p := r.procs[id]
		st, code := p.snapshotStatus()
		out = append(out, ProcInfo{ID: id, Command: p.Command, Status: st, ExitCode: code})
	}
	return out
}

// RunningCount returns the number of processes still running.
func (r *ProcessRegistry) RunningCount() int {
	n := 0
	for _, pi := range r.Snapshot() {
		if pi.Status == ProcRunning {
			n++
		}
	}
	return n
}
```

Remove the unused `strconv` note: `strconv` IS used. Ensure imports block merges with Task 1's (one import block).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -run TestProcess -v`
Expected: PASS (all four process tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tool/process.go internal/tool/process_test.go
git commit -m "feat(tool): ProcessRegistry with detached start, streaming, kill"
```

---

## Task 3: `bash` tool gains `run_in_background`

**Files:**
- Modify: `internal/tool/exec.go`
- Test: `internal/tool/process_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tool/process_test.go`:

```go
import "encoding/json"

func TestBashToolBackgroundReturnsID(t *testing.T) {
	r := NewProcessRegistry()
	bt := &BashTool{Procs: r}
	out, err := bt.Execute(json.RawMessage(`{"command":"echo hi","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "proc-1") {
		t.Fatalf("expected process id in output, got %q", out)
	}
	if len(r.Snapshot()) != 1 {
		t.Fatalf("expected 1 registered process, got %d", len(r.Snapshot()))
	}
}

func TestBashToolForegroundUnchanged(t *testing.T) {
	bt := &BashTool{}
	out, err := bt.Execute(json.RawMessage(`{"command":"echo sync-ok"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "sync-ok") {
		t.Fatalf("foreground output wrong: %q", out)
	}
}
```

(Use the existing test file's import block — add `encoding/json` there.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run TestBashTool -v`
Expected: FAIL — `BashTool` has no `Procs` field.

- [ ] **Step 3: Write minimal implementation**

In `internal/tool/exec.go`: add the `Procs` field, advertise the new param, and
branch in `Execute`.

```go
type BashTool struct {
	Procs *ProcessRegistry
}
```

In `Definition()`, add to `properties`:

```go
"run_in_background": map[string]interface{}{
	"type":        "boolean",
	"description": "Run the command in the background. Returns a process id immediately; poll with bash_output and stop with kill_shell.",
},
```

Replace `Execute`:

```go
func (t BashTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Command         string `json:"command"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.RunInBackground {
		if t.Procs == nil {
			return "", fmt.Errorf("background execution unavailable: no process registry")
		}
		p := t.Procs.StartBackground(params.Command)
		return fmt.Sprintf("Started background process %s. Poll with bash_output(id=%q), stop with kill_shell(id=%q).", p.ID, p.ID, p.ID), nil
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", params.Command)
	} else {
		cmd = exec.Command("bash", "-c", params.Command)
	}

	output, err := cmd.CombinedOutput()
	res := string(output)
	if err != nil {
		if res == "" {
			return fmt.Sprintf("Command failed: %v", err), nil
		}
		return fmt.Sprintf("Command failed (%v). Output:\n%s", err, res), nil
	}
	if strings.TrimSpace(res) == "" {
		return "Command executed successfully (no output).", nil
	}
	return res, nil
}
```

`BashTool` is used by value as `t` in method receivers but stored as `&BashTool{}`
in `LoadBuiltins`. Methods stay value receivers — copying the struct copies the
`*ProcessRegistry` pointer, which is correct.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -run TestBashTool -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tool/exec.go internal/tool/process_test.go
git commit -m "feat(tool): bash run_in_background mode"
```

---

## Task 4: `bash_output` and `kill_shell` tools

**Files:**
- Create: `internal/tool/process_tools.go`
- Test: `internal/tool/process_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tool/process_test.go`:

```go
func TestBashOutputTool(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("echo poll-me")
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	bo := BashOutputTool{Procs: r}
	out, err := bo.Execute(json.RawMessage(`{"id":"` + p.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "poll-me") {
		t.Fatalf("bash_output missing process text: %q", out)
	}
}

func TestKillShellTool(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("sleep 30")
	ks := KillShellTool{Procs: r}
	out, err := ks.Execute(json.RawMessage(`{"id":"` + p.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "killed") {
		t.Fatalf("expected kill confirmation, got %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run "TestBashOutputTool|TestKillShellTool" -v`
Expected: FAIL — `BashOutputTool`, `KillShellTool` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tool/process_tools.go`:

```go
package tool

import (
	"encoding/json"
	"fmt"
)

// BashOutputTool returns incremental output of a background process.
type BashOutputTool struct {
	Procs *ProcessRegistry
}

func (t BashOutputTool) Name() string        { return "bash_output" }
func (t BashOutputTool) Description() string { return "Read new output from a background process" }
func (t BashOutputTool) Parallel() bool      { return true }
func (t BashOutputTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "bash_output",
		"description": "Read output produced since the last bash_output call for a background process, plus its status and exit code.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The background process id returned by bash(run_in_background).",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t BashOutputTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if t.Procs == nil {
		return "", fmt.Errorf("no process registry")
	}
	text, status, code, err := t.Procs.Output(params.ID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}
	header := fmt.Sprintf("[%s status=%s", params.ID, status)
	if status != ProcRunning {
		header += fmt.Sprintf(" exit=%d", code)
	}
	header += "]\n"
	if text == "" {
		return header + "(no new output)", nil
	}
	return header + text, nil
}

// KillShellTool terminates a background process.
type KillShellTool struct {
	Procs *ProcessRegistry
}

func (t KillShellTool) Name() string        { return "kill_shell" }
func (t KillShellTool) Description() string { return "Kill a background process" }
func (t KillShellTool) Parallel() bool      { return true }
func (t KillShellTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "kill_shell",
		"description": "Terminate a background process started with bash(run_in_background). Idempotent.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The background process id to kill.",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t KillShellTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if t.Procs == nil {
		return "", fmt.Errorf("no process registry")
	}
	msg, err := t.Procs.Kill(params.ID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}
	return msg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tool/ -v`
Expected: PASS (all process tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tool/process_tools.go internal/tool/process_test.go
git commit -m "feat(tool): add bash_output and kill_shell tools"
```

---

## Task 5: Wire the process registry into `NewAgent`

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/agent_test.go`:

```go
func TestNewAgentHasProcessTools(t *testing.T) {
	a := NewAgent(nil, nil, nil)
	for _, name := range []string{"bash", "bash_output", "kill_shell"} {
		if _, ok := a.tools[name]; !ok {
			t.Fatalf("agent missing tool %q", name)
		}
	}
	if a.Procs() == nil {
		t.Fatal("agent has no process registry")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestNewAgentHasProcessTools -v`
Expected: FAIL — `Procs` method and `bash_output`/`kill_shell` tools missing.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/agent.go`, add a field to the `Agent` struct:

```go
	procs *tool.ProcessRegistry
```

In `NewAgent`, after the `toolMap` loop and before `a.tools["agent"] = ...`,
overwrite the bash tool with a registry-backed one and register the poll tools:

```go
	a.procs = tool.NewProcessRegistry()
	a.tools["bash"] = &tool.BashTool{Procs: a.procs}
	a.tools["bash_output"] = tool.BashOutputTool{Procs: a.procs}
	a.tools["kill_shell"] = tool.KillShellTool{Procs: a.procs}
```

Add an accessor near `Activity()`:

```go
// Procs returns this agent's background-process registry.
func (a *Agent) Procs() *tool.ProcessRegistry { return a.procs }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./internal/agent/ -run TestNewAgentHasProcessTools -v`
Expected: build OK, test PASS.

- [ ] **Step 5: Full regression**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): wire per-agent ProcessRegistry and process tools"
```
