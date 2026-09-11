package remote

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/version"
)

// RemoteWorkspace manages a remote SSH workspace session: SSH connection,
// remote server lifecycle, and SSH tunnel to the local machine.
//
// The remote server is the execution authority — it owns the agent, LSP,
// git, files, and cron. The desktop app connects to it through the SSH tunnel.
type RemoteWorkspace struct {
	WorkspaceID string
	Target      Target
	RemotePath  string
	Transport   Transport
	State       ServeState
	APIPort     int // local port tunneled to remote API
	Sup         *tool.ProcessSupervisor
	tunnelCmd   *exec.Cmd
	tunnelID    string
	localAPIURL string
}

// NewRemoteWorkspace creates a remote workspace session. Connect must be
// called to establish the connection. workspaceID is persisted across
// sessions for reconnect — pass "" to generate a new one (caller must
// persist it for future reconnects).
func NewRemoteWorkspace(target Target, remotePath, workspaceID string, sup *tool.ProcessSupervisor) (*RemoteWorkspace, error) {
	if sup == nil {
		sup = tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	}
	id := workspaceID
	if id == "" {
		id = generateWorkspaceID()
	}
	return &RemoteWorkspace{
		WorkspaceID: id,
		Target:      target,
		RemotePath:  remotePath,
		Transport:   NewSSHTransport(target, sup),
		Sup:         sup,
	}, nil
}

func generateWorkspaceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Connect establishes the remote workspace: provisions the binary,
// starts or reuses the remote server, and establishes the SSH tunnel.
func (rw *RemoteWorkspace) Connect() error {
	if err := rw.ensureBinary(); err != nil {
		return fmt.Errorf("provision remote binary: %w", err)
	}

	state, err := rw.discoverOrStartServer()
	if err != nil {
		return fmt.Errorf("remote server: %w", err)
	}
	rw.State = state

	apiPort, err := FreeLocalPort()
	if err != nil {
		return fmt.Errorf("find API local port: %w", err)
	}

	// Tunnel: local apiPort → remote API port.
	// Browse port uses the same local number as remote (per StartTunnel
	// contract: -L localBrowsePort:127.0.0.1:remoteBrowsePort where both
	// are ServeState.BrowsePort). This preserves /api/browse/config URL
	// validity on the SPA side.
	cmd, err := StartTunnel(rw.Sup, rw.Target, apiPort, rw.State.Port, rw.State.BrowsePort)
	if err != nil {
		return fmt.Errorf("start tunnel: %w", err)
	}
	rw.tunnelCmd = cmd
	rw.tunnelID = fmt.Sprintf("remote-tunnel-%d", apiPort)
	rw.APIPort = apiPort
	rw.localAPIURL = fmt.Sprintf("http://127.0.0.1:%d", apiPort)

	return nil
}

// Disconnect tears down the SSH tunnel. The remote server stays running
// for resume on next connect. Uses ProcessSupervisor lifecycle APIs
// (MarkExited) rather than raw Process.Kill for proper bookkeeping.
func (rw *RemoteWorkspace) Disconnect() error {
	if rw.tunnelCmd != nil && rw.tunnelCmd.Process != nil {
		pid := rw.tunnelCmd.Process.Pid
		if err := rw.tunnelCmd.Process.Kill(); err != nil {
			// Process may have already exited; record it anyway
		}
		// Wait for process to fully exit, then mark in supervisor
		rw.tunnelCmd.Wait()
		rw.Sup.MarkExited(rw.tunnelID, 0)
		rw.tunnelCmd = nil
		rw.tunnelID = ""
		_ = pid // pid used only for Wait above
	}
	return nil
}

// Reconnect restarts the tunnel and refreshes the remote server state.
func (rw *RemoteWorkspace) Reconnect() error {
	if err := rw.Disconnect(); err != nil {
		return err
	}
	return rw.Connect()
}

// APIURL returns the local URL for the remote API via the SSH tunnel.
func (rw *RemoteWorkspace) APIURL() string {
	return rw.localAPIURL
}

// SetAPIURL sets the local tunnel URL. Used in tests to simulate
// a connected remote workspace without SSH.
func (rw *RemoteWorkspace) SetAPIURL(url string) {
	rw.localAPIURL = url
}

// BrowseURL returns the local URL for the browse origin via the SSH tunnel.
// Returns empty string when the remote server has no browse origin.
// The local port matches the remote port (per StartTunnel contract).
func (rw *RemoteWorkspace) BrowseURL() string {
	if rw.State.BrowsePort == 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", rw.State.BrowsePort)
}

// workspaceStatePath returns the workspace-specific state file path on
// the remote host. In V1 this is the same path as the legacy
// ~/.ocode/remote/serve.json (for compatibility with the current
// server launch). WorkspaceID is reserved for per-workspace state in
// a future iteration (spec Fix 2).
func (rw *RemoteWorkspace) workspaceStatePath() string {
	return "~/.ocode/remote/serve.json"
}

// ensureBinary checks if the remote binary exists; provisions it if not.
func (rw *RemoteWorkspace) ensureBinary() error {
	if BinaryExists(rw.Transport, version.Version) {
		return nil
	}

	goos, goarch, err := DetectPlatform(rw.Transport)
	if err != nil {
		return fmt.Errorf("platform detect: %w", err)
	}

	build, err := PrepareLocalBuild(goos, goarch, "")
	if err != nil {
		return fmt.Errorf("prepare local build: %w", err)
	}
	if !build.Reused {
		defer os.Remove(build.Path)
	}

	if err := UploadBinary(rw.Transport, version.Version, build.Path); err != nil {
		return fmt.Errorf("upload binary: %w", err)
	}

	return nil
}

// discoverOrStartServer checks for a reusable remote server or starts a fresh one.
func (rw *RemoteWorkspace) discoverOrStartServer() (ServeState, error) {
	if state, ok := rw.discoverServer(); ok && ServerAlive(rw.Transport, state, version.Version) {
		return state, nil
	}
	return rw.startFreshServer()
}

// discoverServer reads the remote state file on the remote host.
// V1 uses the legacy ~/.ocode/remote/serve.json path for compatibility
// with the current server launch. Per-workspace paths (spec Fix 2) will
// be added in a future iteration.
func (rw *RemoteWorkspace) discoverServer() (ServeState, bool) {
	catCmd := "cat " + shellQuotePath(rw.workspaceStatePath()) + " 2>/dev/null || true"
	res, err := rw.Transport.Exec(catCmd)
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return ServeState{}, false
	}
	var state ServeState
	if err := json.Unmarshal([]byte(res.Stdout), &state); err != nil {
		return ServeState{}, false
	}
	return state, true
}

// startFreshServer launches a detached remote server and waits for
// its state file to appear.
//
// The launch command cds to RemotePath before starting the server
// (spec Fix 1). V1 writes state to the legacy path for compatibility;
// per-workspace paths (spec Fix 2) will be added in a future iteration.
func (rw *RemoteWorkspace) startFreshServer() (ServeState, error) {
	launchCmd := fmt.Sprintf(
		"cd %s && nohup %s serve --remote --host 127.0.0.1 --port 0 </dev/null >%s 2>&1 & disown; echo launched",
		shellQuotePath(rw.RemotePath),
		shellQuotePath(RemoteBinaryPath(version.Version)),
		shellQuotePath("~/.ocode/remote/serve.log"),
	)

	if res, err := rw.Transport.Exec(launchCmd); err != nil {
		return ServeState{}, fmt.Errorf("launch remote server: %w: %s", err, res.Stderr)
	}

	for i := 0; i < 40; i++ {
		if state, ok := rw.discoverServer(); ok {
			return state, nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	return ServeState{}, fmt.Errorf("remote server did not write its state file within 10s — check ~/.ocode/remote/serve.log on the remote")
}
