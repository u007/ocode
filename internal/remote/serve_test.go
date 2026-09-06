package remote

import (
	"encoding/json"
	"fmt"
	"net"
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
	ft.execResults[healthProbeCmd(4096, "tok")] = ExecResult{Stdout: "200"}
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
	ftBadHealth.execResults[healthProbeCmd(4096, "tok")] = ExecResult{Stdout: "000"}
	if ServerAlive(ftBadHealth, state, "0.8.85") {
		t.Fatal("expected not alive: health probe did not return 200 (curl missing or server wedged)")
	}
}

func TestEnsureRemoteServerReusesLiveMatchingServer(t *testing.T) {
	existing := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "0.8.85"}
	data, _ := json.Marshal(existing)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096, "tok")] = ExecResult{Stdout: "200"}

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
}

// pollAfterLaunchFake wraps fakeTransport so that DiscoverServer's polling
// loop (in StartFreshServer) sees the "missing/dead" state until the launch
// command has been issued at least once, then sees the fresh state — without
// needing StartFreshServer to expose its retry internals to the test.
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
