package remote

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/version"
)

// The desktop connect path (RemoteWorkspace.startFreshServer) must mirror the
// CLI path's stale-state-file fix (launchServerCmd, commit b418fb15): the
// launch command deletes the old serve.json SYNCHRONOUSLY before starting the
// new server. startFreshServer only runs because the discovered state was
// unusable (dead pid, version mismatch, unhealthy) — that stale file is often
// still on disk and still parses, so without the delete the poll loop can
// return the OLD server as the "fresh" one and the desktop silently
// reconnects to a version-mismatched remote server.
func TestWorkspaceStartFreshServerLaunchCmdDeletesStaleStateFile(t *testing.T) {
	rw := &RemoteWorkspace{
		Transport:  newFakeTransport(),
		RemotePath: "/home/user/proj",
	}
	rw.setStartFreshServerPollForTest(2, 1) // shrink: 2 × 1ms instead of 10s
	if _, err := rw.startFreshServer(); err == nil {
		t.Fatal("startFreshServer: got nil error with no state file, want poll timeout error")
	}

	var launchCmd string
	for _, c := range rw.Transport.(*fakeTransport).execCalls {
		if strings.Contains(c, "nohup") && strings.Contains(c, "serve --remote") {
			launchCmd = c
			break
		}
	}
	if launchCmd == "" {
		t.Fatalf("no launch command in exec calls: %v", rw.Transport.(*fakeTransport).execCalls)
	}
	statePath := shellQuotePath(rw.workspaceStatePath())
	if !strings.Contains(launchCmd, "rm -f "+statePath+"; ") {
		t.Errorf("launch command must delete the old state file synchronously (`;`-terminated, not part of the backgrounded && chain): %q", launchCmd)
	}
}

// rmAwareTransport models the real remote shell around the state file: cat
// sees the stale JSON until the launch command (containing the synchronous
// `rm -f <statePath>;`) runs; afterwards cat sees nothing — the file was
// deleted and this fake's fresh server never writes a new one. This makes the
// poll-loop contract directly observable: without the synchronous rm -f, the
// first poll iteration returns the stale server; with it, the flow must time
// out instead of adopting stale state.
type rmAwareTransport struct {
	*fakeTransport
	launched    bool
	staleStdout string
	catCmd      string
}

func (p *rmAwareTransport) Exec(command string) (ExecResult, error) {
	if strings.Contains(command, "nohup") && strings.Contains(command, "serve --remote") &&
		strings.Contains(command, "rm -f") {
		p.launched = true
		return p.fakeTransport.Exec(command)
	}
	if command == p.catCmd {
		if p.launched {
			return ExecResult{Stdout: ""}, nil // rm'd, never rewritten by this fake's server
		}
		return ExecResult{Stdout: p.staleStdout}, nil
	}
	return p.fakeTransport.Exec(command)
}

// startFreshServer must never return a stale state file as if it were the new
// server's: with the launch command deleting the file first, the poll loop can
// only ever observe nothing (keep polling) or the genuinely fresh state.
func TestWorkspaceStartFreshServerNeverReturnsStaleState(t *testing.T) {
	stale := ServeState{PID: 42, Port: 4096, Token: "oldtok", Version: "0.8.85"}
	staleData, _ := json.Marshal(stale)

	ft := newFakeTransport()
	catCmd := "cat " + shellQuotePath("~/.ocode/remote/serve.json") + " 2>/dev/null || true"
	tpt := &rmAwareTransport{
		fakeTransport: ft,
		staleStdout:   string(staleData),
		catCmd:        catCmd,
	}

	rw := &RemoteWorkspace{
		Transport:  tpt,
		RemotePath: "/home/user/proj",
	}
	rw.setStartFreshServerPollForTest(3, 1)
	state, err := rw.startFreshServer()
	if err == nil {
		t.Fatalf("startFreshServer returned %+v, want poll-timeout error (fresh server never writes its file in this fake)", state)
	}
	if state.Token == stale.Token || state.PID == stale.PID {
		t.Fatalf("returned the STALE server's state: %+v", state)
	}
}

// newTestSupervisor creates a minimal ProcessSupervisor for tests.
func newTestSupervisor() *tool.ProcessSupervisor {
	return tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
}

// helper: build a fakeTransport pre-loaded with the standard Connect flow
// responses for a given serve state. The caller can tweak execResults further.
func newConnectFakeTransport(state ServeState) *fakeTransport {
	ft := newFakeTransport()
	ft.execDefault = ExecResult{ExitCode: 0} // binary exists by default

	// State file discovery
	stateJSON, _ := json.Marshal(state)
	catCmd := "cat " + shellQuotePath("~/.ocode/remote/serve.json") + " 2>/dev/null || true"
	ft.execResults[catCmd] = ExecResult{Stdout: string(stateJSON)}

	// ServerAlive: pid + health
	ft.execResults[pidAliveCmd(state.PID)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(state.Port)] = ExecResult{Stdout: "200"}

	// Sync exec (remote-receive-config)
	syncCmd := shellQuotePath(RemoteBinaryPath(version.Version)) + " remote-receive-config"
	ft.execResults[syncCmd] = ExecResult{ExitCode: 0}

	return ft
}

// syncHostKey returns a unique host key for a test so the sync cache
// never matches a previously-cached hash from another test or a real
// connect. Without this, SetCachedHash from one test causes CachedHash
// to match in a later test, skipping the sync entirely.
func syncHostKey(suffix string) string {
	return "test-sync-" + suffix + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
}

// TestConnectSyncsCredentials verifies that Connect calls runSyncStage:
// the transport must see a remote-receive-config ExecStdin call after
// ensureBinary and before server discovery.
func TestConnectSyncsCredentials(t *testing.T) {
	// Hermitize the sync cache so tests never touch the developer's real
	// remote-sync.json.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	state := ServeState{PID: 42, Port: 4096, Token: "tok", Version: version.Version}
	ft := newConnectFakeTransport(state)
	hostKey := syncHostKey("sync")

	rw := &RemoteWorkspace{
		Target:     Target{Kind: KindWSL, Distro: hostKey},
		Transport:  ft,
		RemotePath: "/home/user/proj",
		Sup:        newTestSupervisor(),
	}

	if err := rw.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Verify the sync stage was called via ExecStdin with remote-receive-config.
	found := false
	for _, c := range ft.execStdinCalls {
		if strings.Contains(c, "remote-receive-config") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected remote-receive-config in exec stdin calls, got: %v", ft.execStdinCalls)
	}
}

// TestConnectWSLSkipsTunnel verifies that for a wsl: target, Connect does not
// start an SSH tunnel and sets the API URL directly to the remote server's port.
func TestConnectWSLSkipsTunnel(t *testing.T) {
	// Hermitize the sync cache so tests never touch the developer's real
	// remote-sync.json.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	state := ServeState{PID: 7, Port: 5000, Token: "wtok", Version: version.Version}
	ft := newConnectFakeTransport(state)

	rw := &RemoteWorkspace{
		Target:     Target{Kind: KindWSL, Distro: "Ubuntu"},
		Transport:  ft,
		RemotePath: "/home/user/proj",
		Sup:        newTestSupervisor(),
	}

	if err := rw.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// No tunnel should be started for WSL targets.
	if rw.tunnelCmd != nil {
		t.Error("expected no tunnel for WSL target, but tunnelCmd is set")
	}

	// APIURL should point directly at the remote port (no local tunnel port).
	want := fmt.Sprintf("http://127.0.0.1:%d", state.Port)
	if got := rw.APIURL(); got != want {
		t.Errorf("APIURL = %q, want %q", got, want)
	}

	// APIPort should equal the remote server's port (no local port allocation).
	if rw.APIPort != state.Port {
		t.Errorf("APIPort = %d, want %d", rw.APIPort, state.Port)
	}
}

// TestDisconnectWSLNoTunnelCleanup verifies that Disconnect on a WSL workspace
// (no tunnel registered) does not touch the supervisor.
func TestDisconnectWSLNoTunnelCleanup(t *testing.T) {
	// Hermitize the sync cache so tests never touch the developer's real
	// remote-sync.json.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	state := ServeState{PID: 7, Port: 5000, Token: "wtok", Version: version.Version}
	ft := newConnectFakeTransport(state)

	rw := &RemoteWorkspace{
		Target:     Target{Kind: KindWSL, Distro: "Ubuntu"},
		Transport:  ft,
		RemotePath: "/home/user/proj",
		Sup:        newTestSupervisor(),
	}

	if err := rw.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// The WSL Connect path must leave no tunnel state, which is the structural
	// reason Disconnect cannot touch the supervisor: its whole body is guarded
	// on tunnelCmd being non-nil.
	if rw.tunnelCmd != nil || rw.tunnelID != "" {
		t.Fatalf("WSL workspace unexpectedly has tunnel state: cmd=%v id=%q", rw.tunnelCmd, rw.tunnelID)
	}

	// Snapshot supervisor state before Disconnect to verify non-interference.
	before := rw.Sup.Snapshot()

	// Disconnect should succeed without error (no tunnel to tear down).
	if err := rw.Disconnect(); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}

	after := rw.Sup.Snapshot()

	// The supervisor must have no registered processes after Disconnect —
	// the WSL path never starts a tunnel, so no process was registered and
	// Disconnect must not register or remove any.
	if len(after) != 0 {
		t.Errorf("supervisor has %d registered processes after Disconnect, want 0: %v", len(after), after)
	}
	if len(before) != len(after) {
		t.Errorf("supervisor registered-process count changed: %d before, %d after", len(before), len(after))
	}
}

// TestConnectFatalOnSyncFailure verifies that RemoteWorkspace.Connect aborts
// when the sync stage (remote-receive-config) fails. The fake transport
// injects an error for the sync command, and Connect must return it rather
// than swallowing it (the no-fallback rule).
func TestConnectFatalOnSyncFailure(t *testing.T) {
	// Hermitize the sync cache so tests never touch the developer's real
	// remote-sync.json. OcodeGlobalDataDir uses os.UserHomeDir() (HOME) on
	// macOS; XDG_DATA_HOME on Linux. Setting HOME covers both.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	state := ServeState{PID: 11, Port: 5001, Token: "stok", Version: version.Version}
	ft := newConnectFakeTransport(state)

	// Inject an exec error on the sync command.
	syncCmd := shellQuotePath(RemoteBinaryPath(version.Version)) + " remote-receive-config"
	syncErr := fmt.Errorf("remote-receive-config: connection reset")
	ft.execErrs[syncCmd] = syncErr

	rw := &RemoteWorkspace{
		Target:     Target{Kind: KindSSH, Host: "test-fatal-sync"},
		Transport:  ft,
		RemotePath: "/home/user/proj",
		Sup:        newTestSupervisor(),
	}

	// Connect must return the sync error, not nil.
	if err := rw.Connect(); err == nil {
		t.Fatal("Connect succeeded despite sync failure, want fatal error")
	}
}

// TestWorkspaceDiscoverOrStartServerFlagsOutdated proves the desktop host
// registry path shares the new reuse policy: an alive, healthy, mismatched
// server is reused (not replaced) and the returned state is flagged Outdated
// so the sidebar can show the amber marker.
func TestWorkspaceDiscoverOrStartServerFlagsOutdated(t *testing.T) {
	existing := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "0.0.0-old"}
	ft := newConnectFakeTransport(existing)

	rw := &RemoteWorkspace{
		Transport:  ft,
		RemotePath: "/home/user/proj",
	}

	state, err := rw.discoverOrStartServer()
	if err != nil {
		t.Fatalf("discoverOrStartServer: %v", err)
	}
	if !state.Outdated {
		t.Fatalf("expected Outdated=true for a mismatched alive server, got %+v", state)
	}
	for _, c := range ft.execCalls {
		if strings.Contains(c, "nohup") && strings.Contains(c, "serve --remote") {
			t.Fatalf("discoverOrStartServer launched a fresh server instead of reusing: %q", c)
		}
	}
}
