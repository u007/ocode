package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDiscoverServerMissing(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: "", ExitCode: 0}
	_, ok := DiscoverServer(ft)
	if ok {
		t.Fatal("expected ok=false for empty state file output")
	}
}

func TestDiscoverServerParsesState(t *testing.T) {
	ft := newFakeTransport()
	want := ServeState{PID: 123, Port: 4096, Token: "tok", Version: "0.8.85", StartedAt: time.Unix(1700000000, 0).UTC()}
	data, _ := json.Marshal(want)
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data), ExitCode: 0}

	got, ok := DiscoverServer(ft)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDiscoverServerCorruptFileTreatedAsMissing(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: "not json", ExitCode: 0}
	if _, ok := DiscoverServer(ft); ok {
		t.Fatal("corrupt state file must be treated as missing, not surfaced as an error")
	}
}

func TestServerAliveRequiresPidAndVersionAndHealth(t *testing.T) {
	state := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "0.8.85"}

	ft := newFakeTransport()
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "200"}
	if !ServerAlive(ft, state, "0.8.85") {
		t.Fatal("expected alive: pid alive, version matches, health 200")
	}

	ftDeadPid := newFakeTransport()
	ftDeadPid.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 1}
	if ServerAlive(ftDeadPid, state, "0.8.85") {
		t.Fatal("expected not alive: pid check failed")
	}

	ftVersionMismatch := newFakeTransport()
	ftVersionMismatch.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	if ServerAlive(ftVersionMismatch, state, "0.9.0") {
		t.Fatal("expected not alive: version mismatch")
	}

	ftBadHealth := newFakeTransport()
	ftBadHealth.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ftBadHealth.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "000"}
	if ServerAlive(ftBadHealth, state, "0.8.85") {
		t.Fatal("expected not alive: health probe did not return 200 (curl missing or server wedged)")
	}
}

func TestHealthProbeCmdDoesNotLeakToken(t *testing.T) {
	// /api/health is deliberately unauthenticated (handler_health.go); the
	// probe must never put a token into the executed command string, since
	// that would be visible in the remote host's process listing (ps) for
	// the probe's duration.
	cmd := healthProbeCmd(4096)
	if strings.Contains(cmd, "Authorization") || strings.Contains(cmd, "-H ") {
		t.Errorf("health probe command must not carry an Authorization header: %q", cmd)
	}
	if !strings.Contains(cmd, "/api/health") {
		t.Errorf("health probe command must hit /api/health: %q", cmd)
	}
}

// launchFailsWithStderrFake wraps fakeTransport so a single named command
// returns both a non-nil error and populated Stderr — mirroring what a real
// Transport does on a failed remote command (see SSHTransport.Exec).
// fakeTransport's own execErrs path can't express this: it discards the
// ExecResult whenever an error is configured.
type launchFailsWithStderrFake struct {
	*fakeTransport
	failCmd string
	stderr  string
}

func (l *launchFailsWithStderrFake) Exec(command string) (ExecResult, error) {
	if command == l.failCmd {
		return ExecResult{Stderr: l.stderr}, errors.New("exit status 127")
	}
	return l.fakeTransport.Exec(command)
}

func TestStartFreshServerLaunchFailureIncludesStderr(t *testing.T) {
	ft := &launchFailsWithStderrFake{
		fakeTransport: newFakeTransport(),
		failCmd:       launchServerCmd("0.8.85"),
		stderr:        "nohup: command not found",
	}
	_, err := StartFreshServer(ft, "0.8.85")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "nohup: command not found") {
		t.Errorf("expected launch error to include remote stderr, got: %v", err)
	}
}

func TestEnsureRemoteServerReusesLiveMatchingServer(t *testing.T) {
	existing := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "0.8.85"}
	data, _ := json.Marshal(existing)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "200"}

	state, reused, staleVersionPID, err := EnsureRemoteServer(ft, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reused {
		t.Error("expected reused=true")
	}
	if state != existing {
		t.Errorf("got %+v, want %+v", state, existing)
	}
	if staleVersionPID != 0 {
		t.Errorf("expected staleVersionPID=0 on reuse, got %d", staleVersionPID)
	}
	for _, c := range ft.execCalls {
		if c == launchServerCmd("0.8.85") {
			t.Error("EnsureRemoteServer launched a fresh server when an alive one was discovered")
		}
	}
}

func TestEnsureRemoteServerStartsFreshWhenStale(t *testing.T) {
	stale := ServeState{PID: 42, Port: 4096, Token: "oldtok", Version: "0.8.85"}
	data, _ := json.Marshal(stale)
	fresh := ServeState{PID: 999, Port: 4097, Token: "newtok", Version: "0.8.85", StartedAt: time.Now().Round(0)}
	freshData, _ := json.Marshal(fresh)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 1} // dead

	// After the launch command runs, the poll for the state file must see
	// the fresh content. The fake is call-order-agnostic (map keyed by exact
	// command), so make every subsequent "cat state file" call return the
	// fresh state by overwriting the same key the launch stage's poll loop
	// reads from.
	ft.execResults[launchServerCmd("0.8.85")] = ExecResult{ExitCode: 0}
	origExec := ft.execResults[remoteStateCatCmd]
	_ = origExec
	// Simulate "file now exists" by having a second fakeTransport wrapper
	// switch its answer after the launch call is observed.
	state, reused, staleVersionPID, err := EnsureRemoteServer(&pollAfterLaunchFake{fakeTransport: ft, freshStateJSON: string(freshData)}, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reused {
		t.Error("expected reused=false")
	}
	if state != fresh {
		t.Errorf("got %+v, want %+v", state, fresh)
	}
	if staleVersionPID != 0 {
		t.Errorf("expected staleVersionPID=0 for a dead-pid stale server, got %d", staleVersionPID)
	}
}

// TestEnsureRemoteServerReportsStaleVersionPID covers the spec's "version
// mismatch on reuse" case: the discovered server is alive but running a
// different version, so it must be left running (never killed
// automatically) while EnsureRemoteServer reports its pid for the caller to
// print as an operator notice.
func TestEnsureRemoteServerReportsStaleVersionPID(t *testing.T) {
	stale := ServeState{PID: 42, Port: 4096, Token: "oldtok", Version: "0.8.84"}
	data, _ := json.Marshal(stale)
	fresh := ServeState{PID: 999, Port: 4097, Token: "newtok", Version: "0.8.85", StartedAt: time.Now().Round(0)}
	freshData, _ := json.Marshal(fresh)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0} // still running, just wrong version
	ft.execResults[launchServerCmd("0.8.85")] = ExecResult{ExitCode: 0}

	state, reused, staleVersionPID, err := EnsureRemoteServer(&pollAfterLaunchFake{fakeTransport: ft, freshStateJSON: string(freshData)}, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reused {
		t.Error("expected reused=false")
	}
	if state != fresh {
		t.Errorf("got %+v, want %+v", state, fresh)
	}
	if staleVersionPID != 42 {
		t.Errorf("expected staleVersionPID=42, got %d", staleVersionPID)
	}
}

// pollAfterLaunchFake wraps fakeTransport so that DiscoverServer's polling
// loop (in StartFreshServer) sees the fresh state as soon as the launch
// command has been issued — a stand-in for "the new server has already
// written its state file" that doesn't model the realistic intermediate
// window (rm'd, not yet rewritten). See raceProneFake below for a fake that
// exercises that window and proves the stale file is never returned.
type pollAfterLaunchFake struct {
	*fakeTransport
	freshStateJSON string
	launched       bool
}

func (p *pollAfterLaunchFake) Exec(command string) (ExecResult, error) {
	if command == launchServerCmd("0.8.85") {
		p.launched = true
	}
	if command == remoteStateCatCmd && p.launched {
		return ExecResult{Stdout: p.freshStateJSON}, nil
	}
	return p.fakeTransport.Exec(command)
}

// raceProneFake models the realistic timing StartFreshServer must be safe
// against: before launch, `cat` returns a stale (already on disk) state file
// — exactly what EnsureRemoteServer sees when ServerAlive rejects a
// discovered server as dead/mismatched/unhealthy but the old process never
// got around to deleting its own file. Once the launch command runs, `cat`
// must never again see that stale content — the launch command's `rm -f`
// now runs synchronously, so by the time Exec("launch...") returns, the file
// is gone. This fake enforces exactly that: `appearAfterPolls` poll
// iterations see "missing" (the deleted-but-not-yet-rewritten window) before
// the fresh state appears.
type raceProneFake struct {
	*fakeTransport
	freshStateJSON   string
	appearAfterPolls int
	launched         bool
	pollsSinceLaunch int
}

func (p *raceProneFake) Exec(command string) (ExecResult, error) {
	if command == launchServerCmd("0.8.85") {
		p.launched = true
		return p.fakeTransport.Exec(command)
	}
	if command == remoteStateCatCmd {
		if !p.launched {
			return p.fakeTransport.Exec(command) // pre-launch: stale file still there
		}
		p.pollsSinceLaunch++
		if p.pollsSinceLaunch <= p.appearAfterPolls {
			return ExecResult{Stdout: ""}, nil // rm'd, not yet rewritten by the new server
		}
		return ExecResult{Stdout: p.freshStateJSON}, nil
	}
	return p.fakeTransport.Exec(command)
}

func TestLaunchServerCmdDeletesStaleStateFileSynchronously(t *testing.T) {
	cmd := launchServerCmd("0.8.85")
	statePath := shellQuotePath(remoteStateFilePath)
	if !strings.Contains(cmd, "rm -f "+statePath) {
		t.Errorf("launch command must delete the old state file before starting: %q", cmd)
	}
	// The delete must run synchronously — terminated by `;`, not swept into
	// the backgrounded `&&` chain — so it is guaranteed complete by the time
	// Exec returns. `rm -f X && mkdir ...` would NOT guarantee that (the
	// whole chain including the rm would run in the background), so this
	// must specifically assert the `;` separator, not just "rm appears
	// somewhere before a '&'".
	if !strings.Contains(cmd, "rm -f "+statePath+"; ") {
		t.Errorf("rm -f must be `;`-terminated (run synchronously, not part of the backgrounded && chain): %q", cmd)
	}
}

func TestStartFreshServerNeverReturnsStaleStateEvenIfObservableAfterLaunch(t *testing.T) {
	stale := ServeState{PID: 42, Port: 4096, Token: "oldtok", Version: "0.8.85"}
	staleData, _ := json.Marshal(stale)
	fresh := ServeState{PID: 999, Port: 4097, Token: "newtok", Version: "0.8.85", StartedAt: time.Now().Round(0)}
	freshData, _ := json.Marshal(fresh)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(staleData)}
	ft.execResults[launchServerCmd("0.8.85")] = ExecResult{ExitCode: 0}
	raceFake := &raceProneFake{fakeTransport: ft, freshStateJSON: string(freshData), appearAfterPolls: 2}

	origInterval := serveStatePollInterval
	serveStatePollInterval = time.Millisecond
	defer func() { serveStatePollInterval = origInterval }()

	state, err := StartFreshServer(raceFake, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == stale {
		t.Fatal("StartFreshServer returned the stale state — the old file must never be reused after a fresh launch")
	}
	if state != fresh {
		t.Errorf("got %+v, want %+v", state, fresh)
	}
}

func TestFreeLocalPort(t *testing.T) {
	port, err := FreeLocalPort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("got invalid port %d", port)
	}
	// The port must actually be free to bind immediately after.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port %d not actually free: %v", port, err)
	}
	ln.Close()
}
