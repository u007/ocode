package tool

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"
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

func (p *Process) snapshotViewState() (ProcStatus, int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Status, p.ExitCode, !p.EndedAt.IsZero()
}

// ProcessRegistry holds an agent's background processes.
// When a ProcessSupervisor is attached, lifecycle (registration, shutdown)
// delegates to the supervisor while output buffering and incremental reads
// remain local. Without a supervisor the registry is self-contained.
type ProcessRegistry struct {
	mu      sync.Mutex
	sup     *ProcessSupervisor
	procs   map[string]*Process
	order   []string
	counter int
	onDone  func(*Process)
}

func NewProcessRegistry() *ProcessRegistry {
	return &ProcessRegistry{procs: map[string]*Process{}}
}

// SetSupervisor attaches a session-scoped process supervisor. After this call:
//   - StartBackground registers new processes with the supervisor.
//   - KillAll delegates through supervisor.Shutdown.
//   - Snapshot reads from supervisor records.
func (r *ProcessRegistry) SetSupervisor(sup *ProcessSupervisor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sup = sup
}

// Supervisor returns the attached process supervisor (may be nil).
func (r *ProcessRegistry) Supervisor() *ProcessSupervisor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sup
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
	sup := r.sup
	r.mu.Unlock()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()

	if sup != nil {
		reg := ProcessRegistration{
			ID:               id,
			Command:          command,
			Kind:             ProcessKindBackgroundBash,
			Cmd:              cmd,
			OwnsProcessGroup: runtime.GOOS != "windows",
			StartedAt:        p.StartedAt,
		}
		if _, err := sup.Register(reg); err != nil {
			p.appendOutput([]byte("failed to start: " + err.Error()))
			finishProcess(p, 1, ProcExited, onDone)
			sup.MarkFailedToStart(id, err)
			return p
		}
	}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		p.appendOutput([]byte("failed to start: " + err.Error()))
		finishProcess(p, 1, ProcExited, onDone)
		if sup != nil {
			sup.MarkFailedToStart(id, err)
		}
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
		finishProcess(p, code, status, onDone)
		if sup != nil {
			if status == ProcKilled {
				sup.MarkKilled(id, code)
			} else {
				sup.MarkExited(id, code)
			}
		}
	}()
	return p
}

// finishProcess records terminal state on a Process and fires onDone once.
func finishProcess(p *Process, code int, status ProcStatus, onDone func(*Process)) {
	p.mu.Lock()
	if p.Status != ProcRunning && !p.EndedAt.IsZero() {
		p.mu.Unlock()
		return
	}
	if status != ProcKilled {
		p.Status = status
	} else {
		p.Status = ProcKilled
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
	sup := r.sup
	r.mu.Unlock()
	if !ok {
		return "", "", 0, fmt.Errorf("unknown process id %q", id)
	}
	out, _ := p.readSince()
	st, code := r.viewState(p, id, sup)
	return out, st, code, nil
}

// Kill terminates a process group. Idempotent.
func (r *ProcessRegistry) Kill(id string) (string, error) {
	r.mu.Lock()
	p, ok := r.procs[id]
	sup := r.sup
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
	if sup != nil {
		sup.MarkKilled(id, code)
	}
	return fmt.Sprintf("process %s killed", id), nil
}

// KillAll terminates every running process (lifecycle teardown). When a
// supervisor is attached this delegates through TerminateAll so the session
// can continue registering new processes afterward; callers that need permanent
// shutdown should invoke supervisor.Shutdown directly.
func (r *ProcessRegistry) KillAll() {
	r.mu.Lock()
	sup := r.sup
	r.mu.Unlock()

	if sup != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sup.TerminateAll(ctx)
		return
	}
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
	sup := r.sup
	ids := append([]string(nil), r.order...)
	procs := make(map[string]*Process, len(r.procs))
	for id, p := range r.procs {
		procs[id] = p
	}
	r.mu.Unlock()

	if sup != nil {
		out := make([]ProcInfo, 0, len(ids))
		for _, id := range ids {
			p := procs[id]
			if p == nil {
				continue
			}
			st, code := r.viewState(p, id, sup)
			out = append(out, ProcInfo{
				ID:       id,
				Command:  p.Command,
				Status:   st,
				ExitCode: code,
			})
		}
		return out
	}
	out := make([]ProcInfo, 0, len(ids))
	for _, id := range ids {
		p := procs[id]
		if p == nil {
			continue
		}
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

// Dump returns the full current buffer without advancing any cursor.
func (r *ProcessRegistry) Dump(id string) (string, ProcStatus, int, error) {
	r.mu.Lock()
	p, ok := r.procs[id]
	sup := r.sup
	r.mu.Unlock()
	if !ok {
		return "", "", 0, fmt.Errorf("unknown process id %q", id)
	}
	p.mu.Lock()
	out := ""
	if p.dropped > 0 {
		out = fmt.Sprintf("[…truncated %d bytes]\n", p.dropped)
	}
	out += string(p.buf)
	p.mu.Unlock()
	st, code := r.viewState(p, id, sup)
	return out, st, code, nil
}

func (r *ProcessRegistry) viewState(p *Process, id string, sup *ProcessSupervisor) (ProcStatus, int) {
	localStatus, localCode, localDone := p.snapshotViewState()
	if localDone {
		return localStatus, localCode
	}
	if sup != nil {
		if rec, ok := sup.Lookup(id); ok {
			return viewStatus(rec.Status), rec.ExitCode
		}
	}
	return localStatus, localCode
}

func viewStatus(status ProcStatus) ProcStatus {
	if status == ProcFailedToStart {
		return ProcExited
	}
	return status
}
