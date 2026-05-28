package tool

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jamesmercstudio/ocode/internal/hooks"
)

var (
	processHooksMu sync.RWMutex
	processHooks   *hooks.Pipeline
)

// SetHookPipeline wires the hook pipeline into the process/tool layer.
// Safe to call concurrently.
func SetHookPipeline(p *hooks.Pipeline) {
	processHooksMu.Lock()
	processHooks = p
	processHooksMu.Unlock()
}

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

	// supKey is the key used when reporting this process's lifecycle to the
	// shared ProcessSupervisor. Equals ID for an empty SupervisorIDPrefix and
	// "<prefix>"+ID otherwise. Captured at registration so finalizeManagedProcess
	// can MarkExited/MarkKilled the right record without re-resolving the prefix.
	supKey string

	mu           sync.Mutex
	buf          []byte // last <=procBufferCap bytes of the logical stream
	dropped      int    // count of bytes dropped off the front
	readCursor   int    // logical offset already returned by readSince
	cmd          *exec.Cmd
	notifyOnExit bool
	bgRequestCh  chan struct{}
	bgRequested  bool
}

// SupKey returns the supervisor-scoped key for this process (ID, optionally
// namespaced by the owning registry's SupervisorIDPrefix). Empty when the
// process was created without a registry.
func (p *Process) SupKey() string {
	if p == nil || p.supKey == "" {
		return ""
	}
	return p.supKey
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
	mu        sync.Mutex
	sup       *ProcessSupervisor
	supPrefix string
	procs     map[string]*Process
	order     []string
	counter   int
	onDone    func(*Process)
}

// supID returns the ID used when talking to the shared ProcessSupervisor.
// When multiple ProcessRegistry instances (e.g. subagents) share one
// supervisor, supPrefix namespaces their IDs so monotonically-issued
// "proc-N" counters from different registries do not collide in the
// supervisor's records map.
func (r *ProcessRegistry) supID(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.supIDLocked(id)
}

// supIDLocked is the lock-held variant of supID. Caller must hold r.mu.
func (r *ProcessRegistry) supIDLocked(id string) string {
	if r.supPrefix == "" {
		return id
	}
	return r.supPrefix + id
}

func NewProcessRegistry() *ProcessRegistry {
	return &ProcessRegistry{procs: map[string]*Process{}}
}

func (r *ProcessRegistry) nextIDLocked() string {
	r.counter++
	return "proc-" + strconv.Itoa(r.counter)
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

// SetSupervisorIDPrefix sets a namespace prefix prepended to this registry's
// IDs when registering with the shared ProcessSupervisor. Internal proc IDs
// (those returned to callers / used in wait_tool) are unchanged. Use this when
// multiple registries share one supervisor (e.g. subagents inheriting the
// parent agent's supervisor) so their proc-N counters do not collide.
func (r *ProcessRegistry) SetSupervisorIDPrefix(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.supPrefix = prefix
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

func (r *ProcessRegistry) onDoneCallback() func(*Process) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.onDone
}

// StartBackground launches command detached and returns its Process record.
func (r *ProcessRegistry) StartBackground(command string) *Process {
	r.mu.Lock()
	id := r.nextIDLocked()
	supKey := r.supIDLocked(id)
	p := &Process{ID: id, supKey: supKey, Command: command, Status: ProcRunning, StartedAt: time.Now(), notifyOnExit: true}
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
	processHooksMu.RLock()
	ph := processHooks
	processHooksMu.RUnlock()
	if ph != nil {
		cwd, _ := os.Getwd()
		extra := ph.RunShellEnv(cwd)
		if len(extra) > 0 {
			base := os.Environ()
			for k, v := range extra {
				base = append(base, k+"="+v)
			}
			cmd.Env = base
		}
	}
	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()

	// Single shared cmd.Wait() so the supervisor's defaultWait and the
	// pump-then-finalize goroutine below don't both call Wait concurrently
	// (which races on cmd internal state).
	waitState := newCommandWait()

	supID := supKey
	if sup != nil {
		reg := ProcessRegistration{
			ID:               supID,
			Command:          command,
			Kind:             ProcessKindBackgroundBash,
			Cmd:              cmd,
			OwnsProcessGroup: runtime.GOOS != "windows",
			StartedAt:        p.StartedAt,
			waitFn:           waitState.Wait,
		}
		if _, err := sup.Register(reg); err != nil {
			p.appendOutput([]byte("failed to start: " + err.Error()))
			finishProcess(p, 1, ProcExited, onDone)
			sup.MarkFailedToStart(supID, err)
			return p
		}
	}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		p.appendOutput([]byte("failed to start: " + err.Error()))
		finishProcess(p, 1, ProcExited, onDone)
		if sup != nil {
			sup.MarkFailedToStart(supID, err)
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

	go func() { waitState.Store(cmd.Wait()) }()

	go func() {
		wg.Wait()
		err := waitState.Wait()
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
				sup.MarkKilled(supID, code)
			} else {
				sup.MarkExited(supID, code)
			}
		}
	}()
	return p
}

// RegisterForeground adds a running foreground bash command to the registry so
// the UI can promote it to the background mid-execution.
func (r *ProcessRegistry) RegisterForeground(command string, cmd *exec.Cmd, startedAt time.Time, waitFn func() error) (*Process, error) {
	r.mu.Lock()
	id := r.nextIDLocked()
	supKey := r.supIDLocked(id)
	p := &Process{
		ID:          id,
		supKey:      supKey,
		Command:     command,
		Status:      ProcRunning,
		StartedAt:   startedAt,
		cmd:         cmd,
		bgRequestCh: make(chan struct{}),
	}
	r.procs[id] = p
	r.order = append(r.order, id)
	sup := r.sup
	r.mu.Unlock()

	if sup != nil {
		_, err := sup.Register(ProcessRegistration{
			ID:               supKey,
			Command:          command,
			Kind:             ProcessKindBackgroundBash,
			Cmd:              cmd,
			OwnsProcessGroup: runtime.GOOS != "windows",
			StartedAt:        startedAt,
			waitFn:           waitFn,
		})
		if err != nil {
			r.mu.Lock()
			delete(r.procs, id)
			for i, existing := range r.order {
				if existing == id {
					r.order = append(r.order[:i], r.order[i+1:]...)
					break
				}
			}
			r.mu.Unlock()
			return nil, err
		}
	}

	return p, nil
}

// RequestBackgroundLatest promotes the newest running foreground bash command
// into a background job. It returns that process's id and command.
func (r *ProcessRegistry) RequestBackgroundLatest() (string, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.order) - 1; i >= 0; i-- {
		p := r.procs[r.order[i]]
		if p == nil {
			continue
		}
		p.mu.Lock()
		ok := p.Status == ProcRunning && p.bgRequestCh != nil && !p.bgRequested
		if ok {
			p.bgRequested = true
			p.notifyOnExit = true
			close(p.bgRequestCh)
			id, command := p.ID, p.Command
			p.mu.Unlock()
			return id, command, true
		}
		p.mu.Unlock()
	}
	return "", "", false
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
	notifyOnExit := p.notifyOnExit
	p.mu.Unlock()
	if notifyOnExit && onDone != nil {
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
		sup.MarkKilled(p.SupKey(), code)
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
		key := p.SupKey()
		if key == "" {
			key = r.supID(id)
		}
		if rec, ok := sup.Lookup(key); ok {
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
