package remote

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// ForwardManager supervises user-added extra SSH -L forwards for one
// connected --web session, on top of the fixed api/browse tunnel StartTunnel
// already opened. Each forward is its own `ssh -N -L localPort:127.0.0.1:
// remotePort target` child process, independently registered with sup so it
// shows up (and gets torn down) alongside the rest of the session's
// supervised processes.
//
// The manager — not the supervisor — performs each child's Wait, so a forward
// whose ssh dies on its own (network drop, laptop sleep/wake, host reboot) is
// observed the moment it happens rather than inferred later. That drives both
// the truthful IsLive below and the SetOnExit monitor hook.
type ForwardManager struct {
	sup    *tool.ProcessSupervisor
	target Target

	mu     sync.Mutex
	live   map[int]*forwardProcess // remotePort -> running ssh process + its reaper
	onExit func(remotePort, exitCode int, uptime time.Duration)
}

// forwardProcess is one running `ssh -N -L` child together with the reaper that
// owns its Wait. Stop blocks on done rather than calling Wait itself: two
// Waits on one process race, and the second one never returns.
type forwardProcess struct {
	cmd     *exec.Cmd
	started time.Time
	done    chan struct{}
	// stopped distinguishes a requested teardown from an unexpected exit so
	// the supervisor record reads killed rather than exited. Set before Kill.
	stopped atomic.Bool
}

// NewForwardManager returns a manager for target's extra forwards, supervised
// under sup (the same supervisor the session's fixed tunnel/remote server use).
func NewForwardManager(sup *tool.ProcessSupervisor, target Target) *ForwardManager {
	return &ForwardManager{sup: sup, target: target, live: make(map[int]*forwardProcess)}
}

// forwardRegistrationID must stay distinct from StartTunnel's
// "remote-tunnel-<apiPort>" IDs (keyed on a different number space) and from
// each other (keyed on remotePort, which PortMap already treats as unique).
func forwardRegistrationID(remotePort int) string {
	return "remote-portmap-" + strconv.Itoa(remotePort)
}

// SetOnExit registers a monitor callback, invoked from the reaper goroutine
// once a forward's child has been reaped, its supervisor record marked
// terminal, and its live entry cleared — so the callback may safely call back
// into the manager. uptime is how long the child lived, which is what lets a
// caller tell a forward that was healthy from one that flapped on start.
//
// Only an UNEXPECTED exit fires the callback. A child torn down on purpose
// (Stop, or Start's readiness-failure kill) is not a monitor event: reporting
// it would both double-count a failed restart (the Start caller already counts
// the error) and let a teardown/removal resurrect the retry state its caller
// just cleared, so the watchdog could re-open a forward that is being removed.
//
// Replaces any previous callback; nil clears it. Called before the manager is
// shared with request handlers, never concurrently with a live reaper.
func (m *ForwardManager) SetOnExit(fn func(remotePort, exitCode int, uptime time.Duration)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onExit = fn
}

// IsLive reports whether remotePort currently has a running forward process.
// Accurate because the reaper removes the entry as soon as the child exits —
// this is a liveness signal, not "Start was called and did not error".
func (m *ForwardManager) IsLive(remotePort int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.live[remotePort]
	return ok
}

// reap performs the Wait for one forward's child and retires everything the
// child owned: the supervisor record, the live entry, and the monitor hook.
// Every exit path funnels through here, which is what makes the supervisor
// record terminal — StartSupervised only replaces a stable ID whose record is
// terminal, so a forward left ProcRunning after a crash could never restart.
func (m *ForwardManager) reap(remotePort int, fp *forwardProcess) {
	err := fp.cmd.Wait()
	code := processExitCode(err)
	id := forwardRegistrationID(remotePort)
	// The PID-qualified variants are generation-aware: a reaper left over from
	// an earlier child of the same stable ID must not clobber the new record.
	if fp.stopped.Load() {
		m.sup.MarkKilledPID(id, fp.cmd.Process.Pid, code)
	} else {
		m.sup.MarkExitedPID(id, fp.cmd.Process.Pid, code)
	}

	m.mu.Lock()
	// Identity check: if this port was re-opened while we were reaping, the
	// newer child owns the entry and must keep it.
	if cur, ok := m.live[remotePort]; ok && cur == fp {
		delete(m.live, remotePort)
	}
	onExit := m.onExit
	m.mu.Unlock()

	// Closed after the live entry is gone so anyone blocked in Stop (or in
	// Start's readiness-failure path) observes a fully retired forward.
	close(fp.done)
	// A deliberately stopped child (Stop, or Start's readiness-failure kill)
	// must not fire the monitor hook: that would double-count a failed restart
	// and let a removal resurrect retry state just cleared by forget, so the
	// watchdog could re-open the forward. Only an unexpected exit is a
	// monitor event.
	if onExit != nil && !fp.stopped.Load() {
		onExit(remotePort, code, time.Since(fp.started))
	}
}

// processExitCode maps a Wait result to the code recorded on the supervisor's
// record. -1 means "killed by a signal, or otherwise unknown" — a child that
// ssh tore down reports a negative code via exec.ExitError.ExitCode too.
func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// Start opens the SSH forward for pm if it isn't already running. A no-op
// (not an error) when already live, so callers can call it unconditionally
// on reconnect/enable.
func (m *ForwardManager) Start(pm ProjectPortMap) error {
	m.mu.Lock()
	if _, ok := m.live[pm.RemotePort]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	args := tunnelArgs(pm.LocalPort, pm.RemotePort, 0, m.target.String())
	if m.target.Port > 0 {
		args = append(args[:len(args)-1], "-p", strconv.Itoa(m.target.Port), args[len(args)-1])
	}
	cmd := exec.Command("ssh", args...)
	if _, err := tool.StartSupervised(m.sup, cmd, tool.ProcessRegistration{
		ID:      forwardRegistrationID(pm.RemotePort),
		Name:    "ssh-portmap",
		Command: cmd.String(),
		Kind:    tool.ProcessKindRemote,
		// Stop marks this ID's record terminal; without ReplaceTerminal a
		// later Start for the same port (the panel's Disable → Enable, or a
		// remove → re-add) would fail at registration with "already
		// registered" — the supervisor keeps terminal records, and this ID is
		// deliberately stable per port. The flag only replaces a terminal
		// record, never a running forward.
		ReplaceTerminal: true,
	}); err != nil {
		return fmt.Errorf("open forward for remote port %d: %w", pm.RemotePort, err)
	}
	// The reaper is started before the readiness probe, so the entry is never
	// registered for a child that has already exited (the old code registered
	// after the probe and had no reaper at all, which is how a crashed forward
	// stayed "live" forever).
	fp := &forwardProcess{cmd: cmd, started: time.Now(), done: make(chan struct{})}
	m.mu.Lock()
	m.live[pm.RemotePort] = fp
	m.mu.Unlock()
	go m.reap(pm.RemotePort, fp)

	if err := waitForTunnelReady(pm.LocalPort); err != nil {
		fp.stopped.Store(true)
		_ = cmd.Process.Kill()
		// The reaper owns Wait and the terminal record; blocking on done keeps
		// the failure path from racing it or leaking a terminal-but-running
		// record that the next Start would collide with.
		<-fp.done
		return fmt.Errorf("forward for remote port %d did not become ready: %w", pm.RemotePort, err)
	}
	return nil
}

// Stop closes the running forward for remotePort, if any. A no-op when not
// live. Stop is safe to call on a forward whose child has already exited on
// its own: the entry is gone, so there is nothing to kill.
func (m *ForwardManager) Stop(remotePort int) error {
	m.mu.Lock()
	fp, ok := m.live[remotePort]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if fp.cmd.Process != nil {
		fp.stopped.Store(true)
		// Kill may lose the race with an exit the reaper has already seen; the
		// reaper closes done either way, so this cannot hang.
		_ = fp.cmd.Process.Kill()
	}
	<-fp.done
	return nil
}

// ProjectPortMap is the runtime shape ForwardManager operates on — a
// dependency-free mirror of internal/projects.PortMap (this package cannot
// import internal/projects: projects already imports internal/remote for
// Target parsing, and the reverse would cycle).
type ProjectPortMap struct {
	RemotePort int
	LocalPort  int
	Enabled    bool
}

// PortMapHook lets ConnectWeb's caller (internal/remotecli, which can import
// internal/projects for persistence — internal/remote cannot, since projects
// already imports remote for Target parsing) drive user-added extra port
// forwards during a --web session's foreground wait. A nil hook (the default)
// means ConnectWeb's wait loop still reads stdin but has no port-map command
// to run against it.
type PortMapHook interface {
	// Load returns the project's persisted forwards, so ConnectWeb can
	// auto-start the enabled ones once the fixed tunnel is up.
	Load() ([]ProjectPortMap, error)
	// Handle processes one line of stdin input against fm (the session's
	// ForwardManager) and the persisted store. output is printed verbatim
	// (a trailing newline is added); ok is false when line wasn't a
	// recognized command.
	Handle(fm *ForwardManager, line string) (output string, ok bool)
}
