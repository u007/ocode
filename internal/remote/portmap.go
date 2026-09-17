package remote

import (
	"fmt"
	"os/exec"
	"strconv"
	"sync"

	"github.com/u007/ocode/internal/tool"
)

// ForwardManager supervises user-added extra SSH -L forwards for one
// connected --web session, on top of the fixed api/browse tunnel StartTunnel
// already opened. Each forward is its own `ssh -N -L localPort:127.0.0.1:
// remotePort target` child process, independently registered with sup so it
// shows up (and gets torn down) alongside the rest of the session's
// supervised processes.
type ForwardManager struct {
	sup    *tool.ProcessSupervisor
	target Target

	mu   sync.Mutex
	live map[int]*exec.Cmd // remotePort -> running ssh process
}

// NewForwardManager returns a manager for target's extra forwards, supervised
// under sup (the same supervisor the session's fixed tunnel/remote server use).
func NewForwardManager(sup *tool.ProcessSupervisor, target Target) *ForwardManager {
	return &ForwardManager{sup: sup, target: target, live: make(map[int]*exec.Cmd)}
}

// forwardRegistrationID must stay distinct from StartTunnel's
// "remote-tunnel-<apiPort>" IDs (keyed on a different number space) and from
// each other (keyed on remotePort, which PortMap already treats as unique).
func forwardRegistrationID(remotePort int) string {
	return "remote-portmap-" + strconv.Itoa(remotePort)
}

// IsLive reports whether remotePort currently has a running forward process.
func (m *ForwardManager) IsLive(remotePort int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.live[remotePort]
	return ok
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
	if err := waitForTunnelReady(pm.LocalPort); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		m.sup.MarkExited(forwardRegistrationID(pm.RemotePort), 0)
		return fmt.Errorf("forward for remote port %d did not become ready: %w", pm.RemotePort, err)
	}

	m.mu.Lock()
	m.live[pm.RemotePort] = cmd
	m.mu.Unlock()
	return nil
}

// Stop closes the running forward for remotePort, if any. A no-op when not live.
func (m *ForwardManager) Stop(remotePort int) error {
	m.mu.Lock()
	cmd, ok := m.live[remotePort]
	if ok {
		delete(m.live, remotePort)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	m.sup.MarkExited(forwardRegistrationID(remotePort), 0)
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
