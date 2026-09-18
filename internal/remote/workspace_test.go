package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/version"
)

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

	// serverHealthy: pid + health
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

// TestEnsureBinaryActivatesUploadedBinary is the regression test for the
// upload-without-activation bug: ensureBinary uploaded the remote binary to
// ~/.ocode/bin/<ver>/.ocode.partial and returned success, so the credential
// sync Connect runs next invoked the never-installed final path and failed
// with exit 127 ("No such file or directory"). On the web/desktop side that
// surfaced as a 502 "remote connect failed" for every proxied request to the
// host. ensureBinary must run the chmod+mv activation (and the --version
// verification) after the upload, exactly like the CLI ConnectWeb path.
func TestEnsureBinaryActivatesUploadedBinary(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "ocode-linux-amd64")
	if err := os.WriteFile(fixture, []byte("binary fixture"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := prepareLocalBuildFn
	prepareLocalBuildFn = func(goos, goarch, moduleDir string) (LocalBuild, error) {
		return LocalBuild{Path: fixture, Reused: true}, nil
	}
	defer func() { prepareLocalBuildFn = prev }()

	final := RemoteBinaryPath(version.Version)
	partial := remotePartialPath(version.Version)
	installCmd := fmt.Sprintf("chmod +x %s && mv %s %s",
		shellQuotePath(partial), shellQuotePath(partial), shellQuotePath(final))
	verifyCmd := shellQuotePath(final) + " --version"

	ft := newFakeTransport()
	// Binary is missing, remote is Linux/amd64, activation and verify succeed.
	ft.execResults["test -x "+shellQuotePath(final)] = ExecResult{ExitCode: 1}
	ft.execResults["uname -sm"] = ExecResult{Stdout: "Linux x86_64"}
	ft.execResults[installCmd] = ExecResult{ExitCode: 0}
	ft.execResults[verifyCmd] = ExecResult{Stdout: version.Version}

	rw := &RemoteWorkspace{Transport: ft}
	if err := rw.ensureBinary(); err != nil {
		t.Fatalf("ensureBinary: %v", err)
	}

	if ft.copyDestPath != partial {
		t.Errorf("upload dest = %q, want %q", ft.copyDestPath, partial)
	}
	ran := func(want string) bool {
		for _, c := range ft.execCalls {
			if c == want {
				return true
			}
		}
		return false
	}
	if !ran(installCmd) {
		t.Errorf("activation command did not run; exec calls: %v", ft.execCalls)
	}
	if !ran(verifyCmd) {
		t.Errorf("--version verification did not run; exec calls: %v", ft.execCalls)
	}
}
