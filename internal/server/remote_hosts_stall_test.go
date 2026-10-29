package server

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/remote"
)

// waitForCondition polls cond (a cheap predicate) for up to two seconds.
// The registry publishes connect state from a background goroutine, so tests
// must not assume it is visible on the next statement.
func waitForCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// A caller only ever sees 503 "still connecting" for an in-flight connect, so
// before this the client had no way to tell a 3-second connect from a wedged
// one. status() must expose the in-flight attempt and its start time.
func TestStatusReportsInFlightConnect(t *testing.T) {
	reg := newRemoteHostRegistry(nil)
	release := make(chan struct{})
	var connects atomic.Int32
	reg.connect = func(remote.Target, string) (remoteHostWorkspace, error) {
		connects.Add(1)
		<-release
		return &fakeWorkspace{apiURL: "http://127.0.0.1:1", token: "t"}, nil
	}
	go func() { _, _ = reg.workspaceForPort("u@host", "/p", 0) }()
	waitForCondition(t, "connect to start", func() bool { return connects.Load() == 1 })

	st := reg.status("u@host", 0)
	if !st.Connecting {
		t.Fatal("status does not report the in-flight connect")
	}
	if st.ConnectStartedAt == 0 {
		t.Fatal("status omits the connect start time")
	}
	if st.Connected {
		t.Fatal("status reports connected before the connect finished")
	}

	close(release)
	waitForCondition(t, "connect to finish", func() bool { return reg.status("u@host", 0).Connected })
	if st := reg.status("u@host", 0); st.Connecting {
		t.Fatal("status still reports connecting after the connect succeeded")
	}
}

// A failed attempt is remembered for the cooldown; reporting it as "connecting"
// would keep a dead host looking like progress and arm the client's stall guard
// against an attempt that is no longer running.
func TestStatusDoesNotReportFailedConnectAsConnecting(t *testing.T) {
	reg := newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		return nil, errConnectSuperseded
	})
	if _, err := reg.workspaceForPort("u@host", "/p", 0); err == nil {
		t.Fatal("expected the connect to fail")
	}
	st := reg.status("u@host", 0)
	if st.Connecting {
		t.Fatal("status reports a failed attempt as connecting")
	}
	if st.ConnectStartedAt != 0 {
		t.Fatal("status carries a start time for a failed attempt")
	}
}

// The force path is the ONLY escape from a wedged connect: the normal drop
// refuses to evict an in-flight attempt (see TestDropDoesNotSupersedeInFlightConnect).
// Forcing must start a fresh attempt, and the abandoned one must clean up the
// workspace it eventually produces instead of leaking it.
func TestDropForceSupersedesInFlightConnect(t *testing.T) {
	old := remoteConnectWaitBound
	remoteConnectWaitBound = 30 * time.Millisecond
	t.Cleanup(func() { remoteConnectWaitBound = old })

	reg := newRemoteHostRegistry(nil)
	release := make(chan struct{})
	var connects atomic.Int32
	var abandonedDisconnects atomic.Int32
	abandoned := &fakeWorkspace{
		apiURL: "http://127.0.0.1:1",
		token:  "old",
		disconnect: func() error {
			abandonedDisconnects.Add(1)
			return nil
		},
	}
	reg.connect = func(remote.Target, string) (remoteHostWorkspace, error) {
		if connects.Add(1) == 1 {
			<-release
			return abandoned, nil
		}
		return &fakeWorkspace{apiURL: "http://127.0.0.1:2", token: "new"}, nil
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = reg.workspaceForPort("u@host", "/p", 0)
	}()
	waitForCondition(t, "first connect to start", func() bool { return connects.Load() == 1 })

	reg.dropForce("u@host", 0)

	ws, err := reg.workspaceForPort("u@host", "/p", 0)
	if err != nil {
		t.Fatalf("forced reconnect failed: %v", err)
	}
	if ws.Token() != "new" {
		t.Fatalf("forced reconnect returned the abandoned workspace (token %q)", ws.Token())
	}
	if connects.Load() != 2 {
		t.Fatalf("connects = %d, want 2 after a forced retry", connects.Load())
	}

	// The abandoned attempt must not be able to publish its workspace.
	close(release)
	<-firstDone
	waitForCondition(t, "abandoned workspace to be disconnected", func() bool {
		return abandonedDisconnects.Load() == 1
	})
	ws2, err := reg.workspaceForPort("u@host", "/p", 0)
	if err != nil {
		t.Fatalf("connected workspace disappeared: %v", err)
	}
	if ws2.Token() != "new" {
		t.Fatalf("surviving workspace token = %q, want new", ws2.Token())
	}
}

// Restart runs while a connect is in flight: it has no pid to kill, so the only
// way to make it do anything is to supersede the attempt. Before the force it
// was a silent no-op.
func TestRestartSupersedesInFlightConnect(t *testing.T) {
	old := remoteConnectWaitBound
	remoteConnectWaitBound = 30 * time.Millisecond
	t.Cleanup(func() { remoteConnectWaitBound = old })

	reg := newRemoteHostRegistry(nil)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var connects atomic.Int32
	reg.connect = func(remote.Target, string) (remoteHostWorkspace, error) {
		if connects.Add(1) == 1 {
			<-release
			return &fakeWorkspace{apiURL: "http://127.0.0.1:1", token: "old"}, nil
		}
		return &fakeWorkspace{apiURL: "http://127.0.0.1:2", token: "new"}, nil
	}
	go func() { _, _ = reg.workspaceForPort("u@host", "/p", 0) }()
	waitForCondition(t, "connect to start", func() bool { return connects.Load() == 1 })

	if _, err := reg.restart("u@host", "/p", 0); err != nil {
		t.Fatalf("restart while connecting: %v", err)
	}
	if connects.Load() != 2 {
		t.Fatalf("connects = %d, want a fresh attempt after restart", connects.Load())
	}
}

// End to end through the real mux: the status route must serialize the new
// fields, and connect?force=1 must supersede a running attempt instead of
// returning "still connecting" for the full remoteConnectTimeout.
func TestRemoteLifecycleForceConnectAndStatusWhileConnecting(t *testing.T) {
	old := remoteConnectWaitBound
	remoteConnectWaitBound = 30 * time.Millisecond
	t.Cleanup(func() { remoteConnectWaitBound = old })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var connects atomic.Int32
	s := newLifecycleServer(t, "user@realhost", "/p", 0, func(remote.Target, string) (remoteHostWorkspace, error) {
		if connects.Add(1) == 1 {
			<-release
			return &fakeWorkspace{apiURL: "http://127.0.0.1:1", token: "old"}, nil
		}
		return &fakeWorkspace{apiURL: "http://127.0.0.1:2", token: "new"}, nil
	})
	go func() { _, _ = s.handler.remoteHosts.workspaceForPort("user@realhost", "/p", 0) }()
	waitForCondition(t, "connect to start", func() bool { return connects.Load() == 1 })

	w := doLifecycle(s, http.MethodGet, "/api/remote/user%40realhost/status")
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, body %s", w.Code, w.Body.String())
	}
	var st remoteHostStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !st.Connecting || st.ConnectStartedAt == 0 {
		t.Fatalf("status body does not expose the in-flight connect: %s", w.Body.String())
	}

	w = doLifecycle(s, http.MethodPost, "/api/remote/user%40realhost/connect?force=1")
	if w.Code != http.StatusOK {
		t.Fatalf("forced connect code = %d, body %s", w.Code, w.Body.String())
	}
	if connects.Load() != 2 {
		t.Fatalf("connects = %d, want a fresh attempt", connects.Load())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode forced connect status: %v", err)
	}
	if !st.Connected || st.Connecting {
		t.Fatalf("forced connect did not report a connected host: %s", w.Body.String())
	}
}

// A non-force connect must keep the old behaviour: it joins the running attempt
// and reports "still connecting" rather than superseding it.
func TestRemoteLifecycleConnectWithoutForceJoinsAttempt(t *testing.T) {
	old := remoteConnectWaitBound
	remoteConnectWaitBound = 30 * time.Millisecond
	t.Cleanup(func() { remoteConnectWaitBound = old })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var connects atomic.Int32
	s := newLifecycleServer(t, "user@realhost", "/p", 0, func(remote.Target, string) (remoteHostWorkspace, error) {
		connects.Add(1)
		<-release
		return &fakeWorkspace{apiURL: "http://127.0.0.1:1", token: "t"}, nil
	})
	go func() { _, _ = s.handler.remoteHosts.workspaceForPort("user@realhost", "/p", 0) }()
	waitForCondition(t, "connect to start", func() bool { return connects.Load() == 1 })

	w := doLifecycle(s, http.MethodPost, "/api/remote/user%40realhost/connect")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("connect code = %d, want 503 while an attempt is in flight", w.Code)
	}
	if connects.Load() != 1 {
		t.Fatalf("connects = %d, want the running attempt to be joined", connects.Load())
	}
}
