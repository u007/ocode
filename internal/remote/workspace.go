package remote

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
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
	tr, err := newTransportForTarget(target, sup)
	if err != nil {
		return nil, fmt.Errorf("build transport for %s: %w", target.String(), err)
	}
	return &RemoteWorkspace{
		WorkspaceID: id,
		Target:      target,
		RemotePath:  remotePath,
		Transport:   tr,
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
// syncs credentials, starts or reuses the remote server, and establishes
// the SSH tunnel (skipped for WSL — Windows forwards WSL2 localhost
// natively).
func (rw *RemoteWorkspace) Connect() error {
	if err := rw.ensureBinary(); err != nil {
		return fmt.Errorf("provision remote binary: %w", err)
	}

	// Sync credentials/config to the remote before server discovery so
	// a freshly provisioned server has provider keys. Fatal for Connect:
	// a sync failure blocks the turn with a logged error.
	progress := NewProgress(io.Discard, "sync")
	if err := runSyncStage(progress, rw.Transport, rw.Target.String(), version.Version); err != nil {
		log.Printf("sync credentials to %s: %v", rw.Target.String(), err)
		return fmt.Errorf("sync credentials to %s: %w", rw.Target.String(), err)
	}

	state, err := rw.discoverOrStartServer()
	if err != nil {
		return fmt.Errorf("remote server: %w", err)
	}
	rw.State = state

	// WSL targets share loopback with Windows — no SSH tunnel needed.
	// The browser can reach the WSL server's port directly.
	if rw.Target.Kind == KindWSL {
		rw.APIPort = rw.State.Port
		rw.localAPIURL = fmt.Sprintf("http://127.0.0.1:%d", rw.State.Port)
		return nil
	}

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

	if rw.State.BrowsePort > 0 {
		if err := waitForTunnelReady(rw.State.BrowsePort); err != nil {
			_ = rw.Disconnect()
			return fmt.Errorf("browse tunnel not ready (port %d): %w", rw.State.BrowsePort, err)
		}
	}

	return nil
}

// Disconnect tears down the SSH tunnel. The remote server stays running
// for resume on next connect. Safe for WSL targets (no tunnel registered):
// the nil guard below is a no-op. Uses ProcessSupervisor lifecycle APIs
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

// Token returns the authentication token from the remote server state.
func (rw *RemoteWorkspace) Token() string {
	return rw.State.Token
}

// SetAPIURL sets the local tunnel URL. Used in tests to simulate
// a connected remote workspace without SSH.
func (rw *RemoteWorkspace) SetAPIURL(url string) {
	rw.localAPIURL = url
}

// ServeState returns the workspace's discovered server state (version, pid,
// Outdated, ports). It satisfies the server-side host registry interface; the
// exported State field cannot also be a method name.
func (rw *RemoteWorkspace) ServeState() ServeState {
	return rw.State
}

// ServeTransport returns the transport used to reach the host. It satisfies
// the server-side host registry interface (which needs it for KillServer); the
// exported Transport field cannot also be a method name.
func (rw *RemoteWorkspace) ServeTransport() Transport {
	return rw.Transport
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

// discoverOrStartServer delegates to EnsureRemoteServer so the reuse-vs-fresh
// decision (and the Outdated flag) has a single implementation shared with the
// CLI's ConnectWeb path.
func (rw *RemoteWorkspace) discoverOrStartServer() (ServeState, error) {
	state, _, err := EnsureRemoteServer(rw.Transport, version.Version)
	if err != nil {
		return ServeState{}, err
	}
	return state, nil
}
