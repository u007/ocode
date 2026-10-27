package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestDiscoverServerMissing(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: "", ExitCode: 0}
	_, ok := DiscoverServer(ft, "")
	if ok {
		t.Fatal("expected ok=false for empty state file output")
	}
}

func TestDiscoverServerParsesState(t *testing.T) {
	ft := newFakeTransport()
	want := ServeState{PID: 123, Port: 4096, Token: "tok", Version: "0.8.85", StartedAt: time.Unix(1700000000, 0).UTC()}
	data, _ := json.Marshal(want)
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: string(data), ExitCode: 0}

	got, ok := DiscoverServer(ft, "")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDiscoverServerCorruptFileTreatedAsMissing(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: "not json", ExitCode: 0}
	if _, ok := DiscoverServer(ft, ""); ok {
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
		failCmd:       launchServerCmd("0.8.85", ""),
		stderr:        "nohup: command not found",
	}
	_, err := StartFreshServer(ft, "0.8.85", "")
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
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "200"}

	state, reused, err := EnsureRemoteServer(ft, "0.8.85", "")
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
		if c == launchServerCmd("0.8.85", "") {
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
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 1} // dead

	// After the launch command runs, the poll for the state file must see
	// the fresh content. The fake is call-order-agnostic (map keyed by exact
	// command), so make every subsequent "cat state file" call return the
	// fresh state by overwriting the same key the launch stage's poll loop
	// reads from.
	ft.execResults[launchServerCmd("0.8.85", "")] = ExecResult{ExitCode: 0}
	origExec := ft.execResults[stateCatCmd("")]
	_ = origExec
	// Simulate "file now exists" by having a second fakeTransport wrapper
	// switch its answer after the launch call is observed.
	state, reused, err := EnsureRemoteServer(&pollAfterLaunchFake{fakeTransport: ft, freshStateJSON: string(freshData)}, "0.8.85", "")
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

// killedAfterTermFake makes pidAliveCmd(pid) report dead once killTERMCmd(pid)
// has run, and the fresh state appear once the launch command has run.
type killedAfterTermFake struct {
	*fakeTransport
	pid            int
	slot           ServeSlot
	launchCmd      string
	freshStateJSON string
	killed         bool
	launched       bool
}

func (k *killedAfterTermFake) Exec(command string) (ExecResult, error) {
	switch {
	case command == killTERMCmd(k.pid):
		k.killed = true
	case command == pidAliveCmd(k.pid) && k.killed:
		k.execCalls = append(k.execCalls, command)
		return ExecResult{ExitCode: 1}, nil
	case command == k.launchCmd:
		k.launched = true
	case command == stateCatCmd(k.slot) && k.launched:
		return ExecResult{Stdout: k.freshStateJSON}, nil
	}
	return k.fakeTransport.Exec(command)
}

// TestEnsureRemoteServerRetiresMismatchedAliveServer pins the update policy: a
// healthy server from another version is killed and replaced by a fresh one at
// the local version, and stragglers from other versions are reaped, so a patch
// update does not leave old servers holding memory on the host.
func TestEnsureRemoteServerRetiresMismatchedAliveServer(t *testing.T) {
	existing := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "1.0.0"}
	data, _ := json.Marshal(existing)
	fresh := ServeState{PID: 77, Port: 4100, Token: "new", Version: "2.0.0"}
	freshData, _ := json.Marshal(fresh)

	ft := newFakeTransport()
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "200"}

	k := &killedAfterTermFake{fakeTransport: ft, pid: 42, launchCmd: launchServerCmd("2.0.0", ""), freshStateJSON: string(freshData)}
	state, reused, err := EnsureRemoteServer(k, "2.0.0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reused || state != fresh {
		t.Errorf("got reused=%v state=%+v, want a fresh %+v", reused, state, fresh)
	}
	var killedOld, launched, reaped bool
	for _, c := range ft.execCalls {
		switch c {
		case killTERMCmd(42):
			killedOld = true
		case launchServerCmd("2.0.0", ""):
			launched = true
		case otherVersionServersCmd("2.0.0", "-KILL"):
			reaped = true
		}
	}
	if !killedOld || !launched || !reaped {
		t.Errorf("killedOld=%v launched=%v reaped=%v, want all true; calls: %q", killedOld, launched, reaped, ft.execCalls)
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
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 1} // dead
	ft.execResults[launchServerCmd("0.8.85", "")] = ExecResult{ExitCode: 0}

	state, reused, err := EnsureRemoteServer(&pollAfterLaunchFake{fakeTransport: ft, freshStateJSON: string(freshData)}, "0.8.85", "")
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
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096)] = ExecResult{Stdout: "200"}

	state, reused, err := EnsureRemoteServer(ft, "2.0.0", "")
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
	if command == launchServerCmd("0.8.85", "") {
		p.launched = true
	}
	if command == stateCatCmd("") && p.launched {
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
	if command == launchServerCmd("0.8.85", "") {
		p.launched = true
		return p.fakeTransport.Exec(command)
	}
	if command == stateCatCmd("") {
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
	cmd := launchServerCmd("0.8.85", "")
	statePath := shellQuotePath(ServeSlot("").stateFile())
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

// TestLaunchServerCmdDetachesServerInSubshell is the regression test for the
// fresh-server connect hang. `mkdir X && nohup CMD &` backgrounds the WHOLE
// `mkdir && nohup` list, so the forked subshell waits on the long-lived server
// before exiting and keeps the ssh channel open: StartFreshServer's Exec blocks
// until the server dies, which the connect backstop reports as a 502 after its
// timeout. The nohup must run inside its own `( … & )` so the command's shell
// returns immediately while the server survives.
func TestLaunchServerCmdDetachesServerInSubshell(t *testing.T) {
	cmd := launchServerCmd("0.8.85", "")
	if !strings.Contains(cmd, "&& (PATH=") || !strings.Contains(cmd, " nohup ") || !strings.Contains(cmd, "&); echo launched") {
		t.Fatalf("launch command must detach the server in a `( nohup … & )` subshell: %q", cmd)
	}
	if strings.Contains(cmd, "& disown;") {
		t.Errorf("launch command still uses the hanging `… & disown` shape: %q", cmd)
	}
}

func TestStartFreshServerNeverReturnsStaleStateEvenIfObservableAfterLaunch(t *testing.T) {
	stale := ServeState{PID: 42, Port: 4096, Token: "oldtok", Version: "0.8.85"}
	staleData, _ := json.Marshal(stale)
	fresh := ServeState{PID: 999, Port: 4097, Token: "newtok", Version: "0.8.85", StartedAt: time.Now().Round(0)}
	freshData, _ := json.Marshal(fresh)

	ft := newFakeTransport()
	ft.execResults[stateCatCmd("")] = ExecResult{Stdout: string(staleData)}
	ft.execResults[launchServerCmd("0.8.85", "")] = ExecResult{ExitCode: 0}
	raceFake := &raceProneFake{fakeTransport: ft, freshStateJSON: string(freshData), appearAfterPolls: 2}

	origInterval := serveStatePollInterval
	serveStatePollInterval = time.Millisecond
	defer func() { serveStatePollInterval = origInterval }()

	state, err := StartFreshServer(raceFake, "0.8.85", "")
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

// TestTunnelArgsForwardsBrowsePortOnSameNumber pins the forward ORDER as well
// as the numbers: the browse forward precedes the api forward and ssh runs
// with ExitOnForwardFailure, so a local collision on the browse port exits
// ssh before the api listener exists (RemoteWorkspace.Connect waits on the
// api port for exactly that reason).
func TestTunnelArgsForwardsBrowsePortOnSameNumber(t *testing.T) {
	got := tunnelArgs(5001, 4096, 39321, "user@host")
	want := append(append([]string{"-N"}, keepaliveArgs...), "-o", "ExitOnForwardFailure=yes", "-L", "39321:127.0.0.1:39321", "-L", "5001:127.0.0.1:4096", "user@host")
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("tunnelArgs = %q, want %q", got, want)
	}
}

func TestTunnelArgsSkipsBrowseForwardWhenUnset(t *testing.T) {
	got := tunnelArgs(5001, 4096, 0, "user@host")
	want := append(append([]string{"-N"}, keepaliveArgs...), "-o", "ExitOnForwardFailure=yes", "-L", "5001:127.0.0.1:4096", "user@host")
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
	if err := KillServer(tpt, 42, ""); err != nil {
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
	if !slices.Contains(ft.execCalls, "rm -f "+shellQuotePath(ServeSlot("").stateFile())) {
		t.Fatalf("state file not removed: %v", ft.execCalls)
	}
}

func TestKillServer_StillAlive(t *testing.T) {
	oldAttempts, oldInterval := killServerPollAttempts, killServerPollInterval
	killServerPollAttempts, killServerPollInterval = 2, time.Millisecond
	defer func() { killServerPollAttempts, killServerPollInterval = oldAttempts, oldInterval }()

	ft := newFakeTransport()
	tpt := &aliveScriptTransport{fakeTransport: ft, pid: 7, alive: 100}
	if err := KillServer(tpt, 7, ""); err == nil {
		t.Fatal("expected an error when the pid is still alive after SIGKILL")
	}
	if !slices.Contains(ft.execCalls, killKILLCmd(7)) {
		t.Fatalf("expected SIGKILL escalation: %v", ft.execCalls)
	}
}

func TestKillServer_InvalidPID(t *testing.T) {
	ft := newFakeTransport()
	if err := KillServer(ft, 0, ""); err == nil {
		t.Fatal("expected an error for a non-positive pid")
	}
	if len(ft.execCalls) != 0 {
		t.Fatalf("expected no remote commands for an invalid pid, got %v", ft.execCalls)
	}
}

// scriptedStateFake answers successive state-file reads with replies, in order;
// the last reply repeats. It models a sibling client rewriting the state file
// between this client's launch poll and its orphan reap.
type scriptedStateFake struct {
	*fakeTransport
	slot    ServeSlot
	replies []string
	reads   int
}

func (s *scriptedStateFake) Exec(command string) (ExecResult, error) {
	if command != stateCatCmd(s.slot) {
		return s.fakeTransport.Exec(command)
	}
	s.execCalls = append(s.execCalls, command)
	i := min(s.reads, len(s.replies)-1)
	s.reads++
	return ExecResult{Stdout: s.replies[i]}, nil
}

// TestEnsureRemoteServerAdoptsSiblingStatePID is the concurrent-launch race: this
// client launches server A, a sibling launches B and rewrites the state file
// before this client's reap. The reap must keep B (the state-file server), and
// this client must return B, so no client is left holding a server the reaper
// then kills.
func TestEnsureRemoteServerAdoptsSiblingStatePID(t *testing.T) {
	const ver = "0.8.85"
	a := ServeState{PID: 100, Port: 4100, Token: "a", Version: ver}
	b := ServeState{PID: 200, Port: 4200, Token: "b", Version: ver}
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)

	ft := newFakeTransport()
	ft.execResults[healthProbeCmd(4200)] = ExecResult{Stdout: "200"}
	fake := &scriptedStateFake{fakeTransport: ft, replies: []string{"", string(aJSON), string(bJSON)}}

	state, reused, err := EnsureRemoteServer(fake, ver, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reused || state != b {
		t.Fatalf("reused=%v state=%+v, want the state-file server %+v", reused, state, b)
	}
	for _, sig := range []string{"-TERM", "-KILL"} {
		if !slices.Contains(ft.execCalls, orphanSlotServersCmd(ver, "", 200, sig)) {
			t.Errorf("reap (%s) did not keep the state-file pid 200: %q", sig, ft.execCalls)
		}
		if slices.Contains(ft.execCalls, orphanSlotServersCmd(ver, "", 100, sig)) {
			t.Errorf("reap (%s) still keyed on the launching client's pid 100", sig)
		}
	}
}

// TestEnsureRemoteServerSkipsReapWhenStateFileMissing: a missing state file at
// reap time means a sibling launch rm'd it and has not rewritten it yet. Reaping
// then would kill a server about to be discovered, so the reap must not run.
func TestEnsureRemoteServerSkipsReapWhenStateFileMissing(t *testing.T) {
	const ver = "0.8.85"
	a := ServeState{PID: 100, Port: 4100, Token: "a", Version: ver}
	aJSON, _ := json.Marshal(a)

	ft := newFakeTransport()
	fake := &scriptedStateFake{fakeTransport: ft, replies: []string{"", string(aJSON), ""}}

	state, _, err := EnsureRemoteServer(fake, ver, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != a {
		t.Fatalf("state = %+v, want this client's own server %+v", state, a)
	}
	for _, sig := range []string{"-TERM", "-KILL"} {
		if slices.Contains(ft.execCalls, orphanSlotServersCmd(ver, "", 100, sig)) {
			t.Errorf("orphan reap (%s) ran while the state file was missing", sig)
		}
	}
}

// stubbedShellTransport runs each command under the local sh with HOME and PATH
// pointed at a test sandbox, so the orphan reaper's real shell pipeline runs
// against a canned `ps` and a logging `kill` stub (placed first on PATH).
type stubbedShellTransport struct {
	*fakeTransport
	home string
	bin  string
}

func (l *stubbedShellTransport) Exec(command string) (ExecResult, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = []string{"HOME=" + l.home, "PATH=" + l.bin + ":/usr/bin:/bin"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}, err
}

// TestKillOrphanSlotServersReapsOnlyUnkeptSameSlotServers runs the real reaper
// shell pipeline. Only the same-version, same-slot server that is not the kept
// pid may be signalled; a sibling slot, the legacy slot, and another version are
// out of its blast radius.
func TestKillOrphanSlotServersReapsOnlyUnkeptSameSlotServers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("reaper is a POSIX shell pipeline")
	}
	const ver = "1.0.0"
	const slotA ServeSlot = "abcdef012345"
	const slotB ServeSlot = "0123456789ab"

	cases := []struct {
		name string
		slot ServeSlot
		want []int
	}{
		{name: "legacy slot", slot: "", want: []int{200}},
		{name: "named slot", slot: slotA, want: []int{200}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			bin := t.TempDir()
			sameVer := filepath.Join(home, ".ocode", "bin", ver, "ocode")
			otherVer := filepath.Join(home, ".ocode", "bin", "0.9.0", "ocode")
			serveLine := func(pid int, binPath string, s ServeSlot) string {
				return fmt.Sprintf("%d %s serve --remote --host 127.0.0.1 --port 0%s", pid, binPath, s.serveArgs())
			}
			lines := []string{
				serveLine(100, sameVer, tc.slot),  // kept pid
				serveLine(200, sameVer, tc.slot),  // orphan in this slot
				serveLine(300, sameVer, slotB),    // sibling project's slot
				serveLine(301, sameVer, ""),       // legacy slot (no flag)
				serveLine(400, otherVer, tc.slot), // other version
			}
			killLog := filepath.Join(bin, "kill.log")
			ps := "#!/bin/sh\ncat <<'EOT'\n" + strings.Join(lines, "\n") + "\nEOT\n"
			kill := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %s\n", killLog)
			for name, body := range map[string]string{"ps": ps, "kill": kill} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			tr := &stubbedShellTransport{fakeTransport: newFakeTransport(), home: home, bin: bin}
			if err := killOrphanSlotServers(tr, ver, tc.slot, 100); err != nil {
				t.Fatalf("killOrphanSlotServers: %v", err)
			}

			killed := map[int]bool{}
			if data, err := os.ReadFile(killLog); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					var sig string
					var pid int
					if _, err := fmt.Sscanf(line, "%s %d", &sig, &pid); err != nil {
						t.Fatalf("unparseable kill line %q: %v", line, err)
					}
					killed[pid] = true
				}
			}
			var got []int
			for pid := range killed {
				got = append(got, pid)
			}
			sort.Ints(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("signalled pids = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestServeSlotPaths covers the non-empty slot branch that RemoteWorkspace.slot()
// keeps off (see TODO.md): state and log naming, the serve flag, the launch and
// state-read commands, and the slot validity check.
func TestServeSlotPaths(t *testing.T) {
	slot := SlotForProject("/srv/projects/alpha")
	if slot != SlotForProject("/srv/projects/alpha") || slot == SlotForProject("/srv/projects/beta") {
		t.Fatalf("SlotForProject must be stable per path and distinct across paths: %q", slot)
	}
	if err := slot.valid(); err != nil {
		t.Fatalf("SlotForProject produced an invalid slot %q: %v", slot, err)
	}
	s := string(slot)
	if got, want := slot.stateFile(), remoteStateDir+"/serve-"+s+".json"; got != want {
		t.Errorf("stateFile = %q, want %q", got, want)
	}
	if got, want := slot.logFile(), remoteStateDir+"/serve-"+s+".log"; got != want {
		t.Errorf("logFile = %q, want %q", got, want)
	}
	if got, want := slot.serveArgs(), " --state-slot "+s; got != want {
		t.Errorf("serveArgs = %q, want %q", got, want)
	}
	if got := stateCatCmd(slot); !strings.Contains(got, shellQuotePath(slot.stateFile())) {
		t.Errorf("state read must target the slot state file: %q", got)
	}
	launch := launchServerCmd("1.0.0", slot)
	for _, want := range []string{"rm -f " + shellQuotePath(slot.stateFile()) + "; ", "--state-slot " + s, shellQuotePath(slot.logFile())} {
		if !strings.Contains(launch, want) {
			t.Errorf("slot launch command missing %q: %q", want, launch)
		}
	}
	for _, bad := range []ServeSlot{"../x", "ABCDEF", "0123456789abcdef0"} {
		if err := bad.valid(); err == nil {
			t.Errorf("slot %q must be rejected", bad)
		}
	}
}
