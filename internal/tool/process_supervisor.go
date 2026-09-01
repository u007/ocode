package tool

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const ProcFailedToStart ProcStatus = "failed-to-start"

type ProcessKind string

const (
	ProcessKindBackgroundBash   ProcessKind = "background_bash"
	ProcessKindSubagentBash     ProcessKind = "subagent_bash"
	ProcessKindInteractiveShell ProcessKind = "interactive_shell"
	ProcessKindEditor           ProcessKind = "editor"
	ProcessKindBrowser          ProcessKind = "browser"
)

var ErrProcessSupervisorClosed = errors.New("process supervisor is shutting down")

type ProcessSupervisorOptions struct {
	GracePeriod time.Duration
}

type ProcessRegistration struct {
	ID                    string
	Name                  string
	Command               string
	Kind                  ProcessKind
	Cmd                   *exec.Cmd
	PID                   int
	OwnsProcessGroup      bool
	AllowGracefulShutdown bool
	StartedAt             time.Time

	waitFn     func() error
	gracefulFn func() error
	forceFn    func() error
}

type ProcessRecord struct {
	ID                    string
	Name                  string
	Command               string
	Kind                  ProcessKind
	PID                   int
	OwnsProcessGroup      bool
	AllowGracefulShutdown bool
	StartedAt             time.Time
	EndedAt               time.Time
	Status                ProcStatus
	ExitCode              int
	Retained              bool
	LastError             string
}

type ProcessSupervisor struct {
	mu           sync.Mutex
	records      map[string]*supervisedProcess
	order        []string
	callbacks    []func()
	gracePeriod  time.Duration
	shuttingDown bool
	shutdownDone chan struct{}
	shutdownErr  error
}

type supervisedProcess struct {
	mu      sync.Mutex
	record  ProcessRecord
	cmd     *exec.Cmd
	waitFn  func() error
	graceFn func() error
	forceFn func() error

	waitOnce sync.Once
	waitCh   chan waitResult
}

type waitResult struct {
	err      error
	exitCode int
}

func NewProcessSupervisor(opts ProcessSupervisorOptions) *ProcessSupervisor {
	grace := opts.GracePeriod
	if grace <= 0 {
		grace = 100 * time.Millisecond
	}
	return &ProcessSupervisor{
		records:     map[string]*supervisedProcess{},
		gracePeriod: grace,
	}
}

func (s *ProcessSupervisor) Register(spec ProcessRegistration) (ProcessRecord, error) {
	if spec.ID == "" {
		return ProcessRecord{}, fmt.Errorf("process id is required")
	}

	child := newSupervisedProcess(spec)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shuttingDown {
		return ProcessRecord{}, ErrProcessSupervisorClosed
	}
	if _, exists := s.records[spec.ID]; exists {
		return ProcessRecord{}, fmt.Errorf("process %q already registered", spec.ID)
	}
	s.records[spec.ID] = child
	s.order = append(s.order, spec.ID)
	return child.snapshot(), nil
}

// StartSupervised starts a command under sup's supervision, placing it in its
// own process group (via setProcGroup, same as the bash paths) and registering
// a browser process record. The caller must set reg.ID (and may set reg.Name,
// reg.Command); Kind defaults to ProcessKindBrowser if left empty. reg.Cmd is
// set to cmd, reg.PID and reg.StartedAt are filled from the started process,
// and reg.OwnsProcessGroup is set true. The registration is marked
// AllowGracefulShutdown so Shutdown sends SIGTERM to the process group (so the
// manager's own shutdown hook, e.g. a CDP Browser.close, runs first and the
// default graceful/force steps then terminate the group).
//
// StartSupervised does NOT call cmd.Wait: the caller manages Wait and must call
// sup.MarkExited (or MarkKilled) once Wait returns. To keep the supervisor from
// reaping the process itself (which would race the caller's Wait), the
// registration's waitFn is set to a no-op that returns nil; graceful/force
// termination still signal the group as usual.
//
// If cmd.Start fails, the process is still registered (so the failure is
// visible in Snapshot()) and then marked ProcFailedToStart; the Start error is
// returned to the caller.
func StartSupervised(sup *ProcessSupervisor, cmd *exec.Cmd, reg ProcessRegistration) (ProcessRecord, error) {
	setProcGroup(cmd)

	reg.Cmd = cmd
	reg.OwnsProcessGroup = true
	reg.AllowGracefulShutdown = true
	if reg.Kind == "" {
		reg.Kind = ProcessKindBrowser
	}
	if reg.StartedAt.IsZero() {
		reg.StartedAt = time.Now()
	}
	// The manager owns Wait; a no-op keeps the supervisor from racing it.
	reg.waitFn = func() error { return nil }

	if err := cmd.Start(); err != nil {
		if _, rerr := sup.Register(reg); rerr != nil {
			// Even if registration fails (supervisor already closed), surface
			// the original Start error.
			return ProcessRecord{}, err
		}
		sup.MarkFailedToStart(reg.ID, err)
		return ProcessRecord{}, err
	}

	reg.PID = cmd.Process.Pid
	// Fast path: try normal registration.
	if record, err := sup.Register(reg); err == nil {
		return record, nil
	} else {
		// Register failed — check if it's a duplicate terminal browser record
		// that can be safely replaced. This allows Chrome to be relaunched after
		// exit/crash without leaking the supervisor ID. For non-browser or
		// still-running records we must not replace; instead we clean up the
		// just-started child to avoid an unmanaged leaked process.
		sup.mu.Lock()
		existing, exists := sup.records[reg.ID]
		if !exists {
			sup.mu.Unlock()
			// Lost race — retry once.
			if record, rerr := sup.Register(reg); rerr == nil {
				return record, nil
			}
			// Still failed after retry — leak-prevent kill.
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return ProcessRecord{}, err
		}
		recSnap := existing.snapshot()
		isTerminal := recSnap.Status != ProcRunning
		// Only allow automatic replacement for the stable browser ID or other
		// browser-kind records; generic proc-N collisions remain errors as
		// documented in process_test.go:NoPrefix_CollidesInSupervisor.
		canReplace := isTerminal && (reg.ID == "browse-chrome" || reg.Kind == ProcessKindBrowser || recSnap.Kind == ProcessKindBrowser)
		if !canReplace {
			sup.mu.Unlock()
			// Duplicate running (or non-browser terminal) — kill leaked child.
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return ProcessRecord{}, err
		}
		// Replace terminal browser record atomically. Preserve ordering.
		child := newSupervisedProcess(reg)
		sup.records[reg.ID] = child
		// order already contains ID; do not append again.
		snap := child.snapshot()
		sup.mu.Unlock()
		return snap, nil
	}
}

func (s *ProcessSupervisor) RegisterShutdownCallback(fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shuttingDown {
		return ErrProcessSupervisorClosed
	}
	s.callbacks = append(s.callbacks, fn)
	return nil
}

func (s *ProcessSupervisor) Lookup(id string) (ProcessRecord, bool) {
	s.mu.Lock()
	child, ok := s.records[id]
	s.mu.Unlock()
	if !ok {
		return ProcessRecord{}, false
	}
	return child.snapshot(), true
}

func (s *ProcessSupervisor) Snapshot() []ProcessRecord {
	s.mu.Lock()
	children := make([]*supervisedProcess, 0, len(s.order))
	for _, id := range s.order {
		children = append(children, s.records[id])
	}
	s.mu.Unlock()

	out := make([]ProcessRecord, 0, len(children))
	for _, child := range children {
		out = append(out, child.snapshot())
	}
	return out
}

func (s *ProcessSupervisor) MarkExited(id string, exitCode int) bool {
	child, ok := s.child(id)
	if !ok {
		return false
	}
	return child.markTerminal(ProcExited, exitCode, "")
}

func (s *ProcessSupervisor) MarkKilled(id string, exitCode int) bool {
	child, ok := s.child(id)
	if !ok {
		return false
	}
	return child.markTerminal(ProcKilled, exitCode, "")
}

// MarkExitedPID marks the record as exited only if the PID matches the
// expected value. This makes Wait completion generation-aware: after a Chrome
// crash and relaunch the old waiter holds the old PID and will not clobber the
// new browser record.
func (s *ProcessSupervisor) MarkExitedPID(id string, pid int, exitCode int) bool {
	child, ok := s.child(id)
	if !ok {
		return false
	}
	child.mu.Lock()
	defer child.mu.Unlock()
	if child.record.PID != pid {
		return false
	}
	if child.record.Status != ProcRunning {
		return false
	}
	child.record.Status = ProcExited
	child.record.ExitCode = exitCode
	child.record.EndedAt = time.Now()
	child.record.Retained = true
	return true
}

// MarkKilledPID is the PID-qualified variant of MarkKilled.
func (s *ProcessSupervisor) MarkKilledPID(id string, pid int, exitCode int) bool {
	child, ok := s.child(id)
	if !ok {
		return false
	}
	child.mu.Lock()
	defer child.mu.Unlock()
	if child.record.PID != pid {
		return false
	}
	if child.record.Status != ProcRunning {
		return false
	}
	child.record.Status = ProcKilled
	child.record.ExitCode = exitCode
	child.record.EndedAt = time.Now()
	child.record.Retained = true
	return true
}

func (s *ProcessSupervisor) MarkFailedToStart(id string, err error) bool {
	child, ok := s.child(id)
	if !ok {
		return false
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	return child.markTerminal(ProcFailedToStart, 1, message)
}

func (s *ProcessSupervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.shutdownDone != nil {
		done := s.shutdownDone
		s.mu.Unlock()
		select {
		case <-done:
			s.mu.Lock()
			err := s.shutdownErr
			s.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	s.shuttingDown = true
	s.shutdownDone = make(chan struct{})
	done := s.shutdownDone
	callbacks := append([]func(){}, s.callbacks...)
	children := make([]*supervisedProcess, 0, len(s.order))
	for _, id := range s.order {
		children = append(children, s.records[id])
	}
	s.mu.Unlock()

	for _, fn := range callbacks {
		if fn != nil {
			fn()
		}
	}

	var errs []error
	for _, child := range children {
		if err := s.shutdownChild(child, ctx); err != nil {
			errs = append(errs, err)
		}
	}

	err := errors.Join(errs...)
	s.mu.Lock()
	s.shutdownErr = err
	close(done)
	s.mu.Unlock()
	return err
}

// TerminateAll kills every running child without permanently shutting down the
// supervisor. New registrations remain allowed after this call. Use this for
// in-session rebuilds and resets; use Shutdown for final application exit.
func (s *ProcessSupervisor) TerminateAll(ctx context.Context) error {
	s.mu.Lock()
	if s.shuttingDown {
		done := s.shutdownDone
		s.mu.Unlock()
		select {
		case <-done:
			s.mu.Lock()
			err := s.shutdownErr
			s.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	children := make([]*supervisedProcess, 0, len(s.order))
	for _, id := range s.order {
		children = append(children, s.records[id])
	}
	s.mu.Unlock()

	var errs []error
	for _, child := range children {
		if err := s.shutdownChild(child, ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *ProcessSupervisor) child(id string) (*supervisedProcess, bool) {
	s.mu.Lock()
	child, ok := s.records[id]
	s.mu.Unlock()
	return child, ok
}

func (s *ProcessSupervisor) shutdownChild(child *supervisedProcess, ctx context.Context) error {
	record := child.snapshot()
	if record.Status != ProcRunning {
		return nil
	}

	var errs []error
	terminated := false
	if record.AllowGracefulShutdown {
		if err := child.graceful(); err != nil {
			errs = append(errs, fmt.Errorf("graceful terminate %s: %w", record.ID, err))
		} else {
			terminated = true
		}
	}

	if result, ok := waitForResult(ctx, child.wait(), s.gracePeriod); ok {
		child.finishAfterWait(result, terminated)
		return errors.Join(errs...)
	}

	if err := child.force(); err != nil {
		errs = append(errs, fmt.Errorf("force kill %s: %w", record.ID, err))
	} else {
		terminated = true
	}

	if result, ok := waitForResult(ctx, child.wait(), s.gracePeriod); ok {
		child.finishAfterWait(result, terminated)
		return errors.Join(errs...)
	}

	child.markTerminal(ProcKilled, 1, "shutdown timed out waiting for process reap")
	err := fmt.Errorf("process %s did not exit after force kill", record.ID)
	err = errors.Join(err, errors.Join(errs...))
	return err
}

func newSupervisedProcess(spec ProcessRegistration) *supervisedProcess {
	startedAt := spec.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	pid := spec.PID
	if pid == 0 && spec.Cmd != nil && spec.Cmd.Process != nil {
		pid = spec.Cmd.Process.Pid
	}

	child := &supervisedProcess{
		record: ProcessRecord{
			ID:                    spec.ID,
			Name:                  spec.Name,
			Command:               spec.Command,
			Kind:                  spec.Kind,
			PID:                   pid,
			OwnsProcessGroup:      spec.OwnsProcessGroup,
			AllowGracefulShutdown: spec.AllowGracefulShutdown,
			StartedAt:             startedAt,
			Status:                ProcRunning,
			Retained:              true,
		},
		cmd: spec.Cmd,
	}

	child.waitFn = spec.waitFn
	if child.waitFn == nil {
		child.waitFn = child.defaultWait
	}
	child.graceFn = spec.gracefulFn
	if child.graceFn == nil {
		child.graceFn = child.defaultGraceful
	}
	child.forceFn = spec.forceFn
	if child.forceFn == nil {
		child.forceFn = child.defaultForce
	}

	return child
}

func (p *supervisedProcess) snapshot() ProcessRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.record
}

func (p *supervisedProcess) markTerminal(status ProcStatus, exitCode int, lastError string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.record.Status != ProcRunning && !p.record.EndedAt.IsZero() {
		return false
	}
	p.record.Status = status
	p.record.ExitCode = exitCode
	p.record.LastError = lastError
	p.record.EndedAt = time.Now()
	p.record.Retained = true
	return true
}

func (p *supervisedProcess) finishAfterWait(result waitResult, terminated bool) {
	status := ProcExited
	if terminated {
		status = ProcKilled
	}
	p.markTerminal(status, result.exitCode, errorText(result.err))
}

func (p *supervisedProcess) wait() <-chan waitResult {
	p.waitOnce.Do(func() {
		p.waitCh = make(chan waitResult, 1)
		go func() {
			err := p.waitFn()
			p.waitCh <- waitResult{err: err, exitCode: exitCode(err)}
		}()
	})
	return p.waitCh
}

func (p *supervisedProcess) graceful() error {
	return p.graceFn()
}

func (p *supervisedProcess) force() error {
	return p.forceFn()
}

func (p *supervisedProcess) defaultWait() error {
	if p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}

func (p *supervisedProcess) defaultGraceful() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	record := p.snapshot()
	return terminateProcess(p.cmd.Process, record.OwnsProcessGroup)
}

func (p *supervisedProcess) defaultForce() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	record := p.snapshot()
	return forceKillProcess(p.cmd.Process, record.OwnsProcessGroup)
}

func waitForResult(ctx context.Context, ch <-chan waitResult, timeout time.Duration) (waitResult, bool) {
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-ch:
		return result, true
	case <-timer.C:
		return waitResult{}, false
	case <-ctx.Done():
		return waitResult{}, false
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
