package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
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

func TestServerHealthyRequiresPidAndHealth(t *testing.T) {
	state := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "0.8.85"}

	ft := newFakeTransport()
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "200"}
	if !serverHealthy(ft, state) {
		t.Fatal("expected alive: pid alive, health 200")
	}

	ftDeadPid := newFakeTransport()
	ftDeadPid.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 1}
	if serverHealthy(ftDeadPid, state) {
		t.Fatal("expected not alive: pid check failed")
	}

	ftBadHealth := newFakeTransport()
	ftBadHealth.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ftBadHealth.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "000"}
	if serverHealthy(ftBadHealth, state) {
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

	state, reused, err := EnsureRemoteServer(ft, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reused {
		t.Error("expected reused=true")
	}
	if state != existing {
		t.Errorf("got %+v, want %+v", state, existing)
	}
	if state.Outdated {
		t.Errorf("expected Outdated=false for a matching version, got true")
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
	state, reused, err := EnsureRemoteServer(&pollAfterLaunchFake{fakeTransport: ft, freshStateJSON: string(freshData)}, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reused {
		t.Error("expected reused=false")
	}
	if state != fresh {
		t.Errorf("got %+v, want %+v", state, fresh)
	}
	if state.Outdated {
		t.Errorf("expected a freshly started server to be not Outdated, got true")
	}
}

// TestEnsureRemoteServerReusesMismatchedAliveServer pins the new version
// policy: a discovered server that is alive and healthy is reused even when
// its version differs from the client's, and is flagged Outdated for the UI.
// No launch command may run — replacing it would orphan its terminals.
func TestEnsureRemoteServerReusesMismatchedAliveServer(t *testing.T) {
	existing := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "1.0.0"}
	data, _ := json.Marshal(existing)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "200"}

	state, reused, err := EnsureRemoteServer(ft, "2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reused {
		t.Fatal("expected reused=true for an alive mismatched server")
	}
	want := existing
	want.Outdated = true
	if state != want {
		t.Errorf("got %+v, want %+v", state, want)
	}
	for _, c := range ft.execCalls {
		if c == launchServerCmd("2.0.0") {
			t.Error("EnsureRemoteServer launched a fresh server instead of reusing the alive mismatched one")
		}
	}
}

// TestEnsureRemoteServerReplacesDeadMismatchedServer: a mismatched server
// whose pid is dead is still replaced with a fresh one (and never flagged
// Outdated).
func TestEnsureRemoteServerReplacesDeadMismatchedServer(t *testing.T) {
	stale := ServeState{PID: 42, Port: 4096, Token: "oldtok", Version: "1.0.0"}
	data, _ := json.Marshal(stale)
	fresh := ServeState{PID: 999, Port: 4097, Token: "newtok", Version: "0.8.85", StartedAt: time.Now().Round(0)}
	freshData, _ := json.Marshal(fresh)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 1} // dead
	ft.execResults[launchServerCmd("0.8.85")] = ExecResult{ExitCode: 0}

	state, reused, err := EnsureRemoteServer(&pollAfterLaunchFake{fakeTransport: ft, freshStateJSON: string(freshData)}, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reused {
		t.Error("expected reused=false for a dead mismatched server")
	}
	if state != fresh {
		t.Errorf("got %+v, want %+v", state, fresh)
	}
	if state.Outdated {
		t.Errorf("expected freshly started server to be not Outdated, got true")
	}
}

// TestEnsureRemoteServerMatchingVersionNotOutdated: the existing reuse path
// must not flag a same-version server Outdated.
func TestEnsureRemoteServerMatchingVersionNotOutdated(t *testing.T) {
	existing := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "2.0.0"}
	data, _ := json.Marshal(existing)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "200"}

	state, reused, err := EnsureRemoteServer(ft, "2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reused || state.Outdated {
		t.Fatalf("reused=%v outdated=%v, want reused=true outdated=false", reused, state.Outdated)
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
// — exactly what EnsureRemoteServer sees when serverHealthy rejects a
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

func TestTunnelArgsForwardsBrowsePortOnSameNumber(t *testing.T) {
	got := tunnelArgs(5001, 4096, 39321, "user@host")
	want := append(append([]string{"-N"}, keepaliveArgs...), "-L", "5001:127.0.0.1:4096", "-L", "39321:127.0.0.1:39321", "user@host")
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("tunnelArgs = %q, want %q", got, want)
	}
}

func TestTunnelArgsSkipsBrowseForwardWhenUnset(t *testing.T) {
	got := tunnelArgs(5001, 4096, 0, "user@host")
	want := append(append([]string{"-N"}, keepaliveArgs...), "-L", "5001:127.0.0.1:4096", "user@host")
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("tunnelArgs = %q, want %q", got, want)
	}
}

// TestTunnelArgsHasKeepalive pins the fix for a desktop remote tunnel that
// hangs forever on a silently-dead transport: without ServerAlive* the ssh -N
// process never exits, so the proxied terminal never disconnects.
func TestTunnelArgsHasKeepalive(t *testing.T) {
	joined := strings.Join(tunnelArgs(5001, 4096, 0, "user@host"), " ")
	for _, opt := range []string{"ServerAliveInterval=15", "ServerAliveCountMax=3", "ConnectTimeout=15"} {
		if !strings.Contains(joined, opt) {
			t.Fatalf("tunnel ssh missing %s: %q", opt, joined)
		}
	}
}

// aliveScriptTransport answers pidAliveCmd with a scripted number of "alive"
// responses before reporting dead, so KillServer's term-then-kill sequence is
// observable without real processes.
type aliveScriptTransport struct {
	*fakeTransport
	pid   int
	alive int
}

func (a *aliveScriptTransport) Exec(command string) (ExecResult, error) {
	if command == pidAliveCmd(a.pid) {
		a.fakeTransport.execCalls = append(a.fakeTransport.execCalls, command)
		if a.alive > 0 {
			a.alive--
			return ExecResult{ExitCode: 0}, nil
		}
		return ExecResult{ExitCode: 1}, nil
	}
	return a.fakeTransport.Exec(command)
}

func TestKillServer_TermThenKill(t *testing.T) {
	oldAttempts, oldInterval := killServerPollAttempts, killServerPollInterval
	killServerPollAttempts, killServerPollInterval = 5, time.Millisecond
	defer func() { killServerPollAttempts, killServerPollInterval = oldAttempts, oldInterval }()

	ft := newFakeTransport()
	tpt := &aliveScriptTransport{fakeTransport: ft, pid: 42, alive: 2}
	if err := KillServer(tpt, 42); err != nil {
		t.Fatalf("KillServer: %v", err)
	}
	if !slices.Contains(ft.execCalls, killTERMCmd(42)) {
		t.Fatalf("missing SIGTERM command in %v", ft.execCalls)
	}
	if slices.Contains(ft.execCalls, killKILLCmd(42)) {
		t.Fatalf("escalated to SIGKILL though the pid died from SIGTERM: %v", ft.execCalls)
	}
	aliveProbes := 0
	for _, c := range ft.execCalls {
		if c == pidAliveCmd(42) {
			aliveProbes++
		}
	}
	if aliveProbes != 3 {
		t.Fatalf("pid probes = %d, want 3 (alive, alive, dead): %v", aliveProbes, ft.execCalls)
	}
	if !slices.Contains(ft.execCalls, "rm -f "+shellQuotePath(remoteStateFilePath)) {
		t.Fatalf("state file not removed: %v", ft.execCalls)
	}
}

func TestKillServer_StillAlive(t *testing.T) {
	oldAttempts, oldInterval := killServerPollAttempts, killServerPollInterval
	killServerPollAttempts, killServerPollInterval = 2, time.Millisecond
	defer func() { killServerPollAttempts, killServerPollInterval = oldAttempts, oldInterval }()

	ft := newFakeTransport()
	tpt := &aliveScriptTransport{fakeTransport: ft, pid: 7, alive: 100}
	if err := KillServer(tpt, 7); err == nil {
		t.Fatal("expected an error when the pid is still alive after SIGKILL")
	}
	if !slices.Contains(ft.execCalls, killKILLCmd(7)) {
		t.Fatalf("expected SIGKILL escalation: %v", ft.execCalls)
	}
}

func TestKillServer_InvalidPID(t *testing.T) {
	ft := newFakeTransport()
	if err := KillServer(ft, 0); err == nil {
		t.Fatal("expected an error for a non-positive pid")
	}
	if len(ft.execCalls) != 0 {
		t.Fatalf("expected no remote commands for an invalid pid, got %v", ft.execCalls)
	}
}
