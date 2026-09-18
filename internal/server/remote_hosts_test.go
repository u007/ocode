package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/version"
)

// fakeWorkspace implements remoteHostWorkspace for testing without SSH.
type fakeWorkspace struct {
	apiURL     string
	token      string
	disconnect func() error
	state      remote.ServeState
	transport  remote.Transport
}

func (f *fakeWorkspace) APIURL() string { return f.apiURL }
func (f *fakeWorkspace) Token() string  { return f.token }
func (f *fakeWorkspace) Disconnect() error {
	if f.disconnect != nil {
		return f.disconnect()
	}
	return nil
}
func (f *fakeWorkspace) ServeState() remote.ServeState    { return f.state }
func (f *fakeWorkspace) ServeTransport() remote.Transport { return f.transport }

func TestWorkspaceFor_SingleConnect(t *testing.T) {
	// 20 goroutines calling workspaceFor("h", "/p") concurrently should
	// produce exactly ONE connect call and all receive the SAME workspace.
	var connectCount atomic.Int32
	ws := &fakeWorkspace{apiURL: "http://127.0.0.1:9999", token: "tok123"}

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		connectCount.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate slow connect
		return ws, nil
	})

	const n = 20
	var wg sync.WaitGroup
	results := make([]remoteHostWorkspace, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = reg.workspaceFor("h", "/p")
		}(i)
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c := connectCount.Load(); c != 1 {
		t.Fatalf("expected exactly 1 connect call, got %d", c)
	}
	for i, r := range results {
		if r != ws {
			t.Fatalf("result %d: expected same workspace pointer, got %v", i, r)
		}
	}
}

func TestWorkspaceFor_ConnectError_Retries(t *testing.T) {
	// A connect error is returned to every waiter, and the NEXT call retries
	// (a second connect call is observed).
	var callCount atomic.Int32
	fakeErr := errors.New("ssh connect failed")

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		n := callCount.Add(1)
		if n == 1 {
			return nil, fakeErr
		}
		return &fakeWorkspace{apiURL: "http://127.0.0.1:9998", token: "tok2"}, nil
	})

	// First call — should fail.
	_, err := reg.workspaceFor("h", "/p")
	if !errors.Is(err, fakeErr) {
		t.Fatalf("expected fakeErr, got %v", err)
	}
	if c := callCount.Load(); c != 1 {
		t.Fatalf("expected 1 connect call, got %d", c)
	}

	// Second call — should retry and succeed.
	ws, err := reg.workspaceFor("h", "/p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws.APIURL() != "http://127.0.0.1:9998" {
		t.Fatalf("unexpected API URL: %s", ws.APIURL())
	}
	if c := callCount.Load(); c != 2 {
		t.Fatalf("expected 2 connect calls, got %d", c)
	}
}

func TestWorkspaceFor_ConcurrentConnectError(t *testing.T) {
	// Multiple concurrent callers during a failed connect all get the error,
	// and a subsequent call retries.
	var connectCount atomic.Int32
	fakeErr := errors.New("ssh timeout")

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		connectCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		return nil, fakeErr
	})

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = reg.workspaceFor("h", "/p")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, fakeErr) {
			t.Fatalf("goroutine %d: expected fakeErr, got %v", i, err)
		}
	}
	if c := connectCount.Load(); c != 1 {
		t.Fatalf("expected exactly 1 connect call, got %d", c)
	}

	// Next call retries.
	var callCount atomic.Int32
	reg2 := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		n := callCount.Add(1)
		if n == 1 {
			return nil, fakeErr
		}
		return &fakeWorkspace{apiURL: "http://127.0.0.1:9997", token: "tok3"}, nil
	})

	// First batch — fail.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = reg2.workspaceFor("h", "/p")
		}(i)
	}
	wg.Wait()

	// Retry — succeed.
	ws, err := reg2.workspaceFor("h", "/p")
	if err != nil {
		t.Fatalf("retry: unexpected error: %v", err)
	}
	if ws.APIURL() != "http://127.0.0.1:9997" {
		t.Fatalf("unexpected API URL: %s", ws.APIURL())
	}
}

func TestDrop_TriggersReconnect(t *testing.T) {
	// drop("h") makes the next call reconnect (a second connect call observed).
	var connectCount atomic.Int32
	ws1 := &fakeWorkspace{apiURL: "http://127.0.0.1:9996", token: "tok4"}
	ws2 := &fakeWorkspace{apiURL: "http://127.0.0.1:9995", token: "tok5"}

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		n := connectCount.Add(1)
		if n == 1 {
			return ws1, nil
		}
		return ws2, nil
	})

	// First call.
	got, err := reg.workspaceFor("h", "/p")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got != ws1 {
		t.Fatal("expected ws1")
	}

	// Drop.
	reg.drop("h")

	// Next call should reconnect.
	got, err = reg.workspaceFor("h", "/p")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got != ws2 {
		t.Fatal("expected ws2 after drop")
	}
	if c := connectCount.Load(); c != 2 {
		t.Fatalf("expected 2 connect calls, got %d", c)
	}
}

func TestDrop_DisconnectsWorkspace(t *testing.T) {
	// drop calls Disconnect on the workspace exactly once.
	var disconnected atomic.Int32
	ws := &fakeWorkspace{
		apiURL:     "http://127.0.0.1:9994",
		token:      "tok6",
		disconnect: func() error { disconnected.Add(1); return nil },
	}

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		return ws, nil
	})

	_, err := reg.workspaceFor("h", "/p")
	if err != nil {
		t.Fatalf("workspaceFor: %v", err)
	}

	reg.drop("h")

	if c := disconnected.Load(); c != 1 {
		t.Fatalf("expected Disconnect called once, got %d", c)
	}
}

func TestDrop_DisconnectErrorLogged(t *testing.T) {
	// Disconnect errors are logged but do not panic or return error from drop.
	dropErr := errors.New("disconnect failed")
	ws := &fakeWorkspace{
		apiURL:     "http://127.0.0.1:9993",
		token:      "tok7",
		disconnect: func() error { return dropErr },
	}

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		return ws, nil
	})

	_, err := reg.workspaceFor("h", "/p")
	if err != nil {
		t.Fatalf("workspaceFor: %v", err)
	}

	// drop should not panic; it logs the error internally.
	reg.drop("h")
}

func TestCloseAll_DisconnectsAll(t *testing.T) {
	// closeAll calls Disconnect on every entry exactly once.
	var disconnectCount atomic.Int32
	ws1 := &fakeWorkspace{
		apiURL:     "http://127.0.0.1:9992",
		token:      "tok8",
		disconnect: func() error { disconnectCount.Add(1); return nil },
	}
	ws2 := &fakeWorkspace{
		apiURL:     "http://127.0.0.1:9991",
		token:      "tok9",
		disconnect: func() error { disconnectCount.Add(1); return nil },
	}

	callIdx := atomic.Int32{}
	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		n := callIdx.Add(1)
		if n == 1 {
			return ws1, nil
		}
		return ws2, nil
	})

	// Create two entries for different hosts.
	_, err := reg.workspaceFor("hostA", "/p1")
	if err != nil {
		t.Fatalf("workspaceFor hostA: %v", err)
	}
	_, err = reg.workspaceFor("hostB", "/p2")
	if err != nil {
		t.Fatalf("workspaceFor hostB: %v", err)
	}

	reg.closeAll(context.Background())

	if c := disconnectCount.Load(); c != 2 {
		t.Fatalf("expected Disconnect called twice, got %d", c)
	}
}

func TestCloseAll_SkipsAlreadyDisconnected(t *testing.T) {
	// If Disconnect was already called (via drop), closeAll does not call it again.
	var disconnectCount atomic.Int32
	ws := &fakeWorkspace{
		apiURL:     "http://127.0.0.1:9990",
		token:      "tok10",
		disconnect: func() error { disconnectCount.Add(1); return nil },
	}

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		return ws, nil
	})

	_, err := reg.workspaceFor("h", "/p")
	if err != nil {
		t.Fatalf("workspaceFor: %v", err)
	}

	// Drop first — disconnects once.
	reg.drop("h")

	// closeAll should not disconnect again (entry was removed).
	reg.closeAll(context.Background())

	if c := disconnectCount.Load(); c != 1 {
		t.Fatalf("expected Disconnect called once total, got %d", c)
	}
}

func TestMarkRegistered(t *testing.T) {
	// markRegistered returns true the first time for a (host, path) and false after.
	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		return &fakeWorkspace{apiURL: "http://127.0.0.1:9989", token: "tok11"}, nil
	})

	first := reg.markRegistered("h", "/p")
	if !first {
		t.Fatal("expected first=true")
	}

	second := reg.markRegistered("h", "/p")
	if second {
		t.Fatal("expected second=false")
	}

	// Different path on the same host — should be first.
	first2 := reg.markRegistered("h", "/q")
	if !first2 {
		t.Fatal("expected first2=true for different path")
	}

	// Different host — should be first.
	first3 := reg.markRegistered("h2", "/p")
	if !first3 {
		t.Fatal("expected first3=true for different host")
	}
}

func TestMarkRegistered_DifferentHostsIndependent(t *testing.T) {
	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		return &fakeWorkspace{apiURL: "http://127.0.0.1:9988", token: "tok12"}, nil
	})

	if !reg.markRegistered("hostA", "/p") {
		t.Fatal("expected first for hostA")
	}
	if !reg.markRegistered("hostB", "/p") {
		t.Fatal("expected first for hostB")
	}
	if reg.markRegistered("hostA", "/p") {
		t.Fatal("expected second for hostA")
	}
	if reg.markRegistered("hostB", "/p") {
		t.Fatal("expected second for hostB")
	}
}

func TestWorkspaceFor_DifferentHosts_Parallel(t *testing.T) {
	// A slow connect to host A must not block a request for host B. Host A
	// blocks INSIDE its connect on a channel; host B must return while A is
	// still blocked. Asserting only connect counts/URLs would pass even if the
	// registry held its map lock across the whole connect.
	hostAEntered := make(chan struct{}, 1)
	releaseHostA := make(chan struct{})
	var hostAConnect atomic.Int32
	var hostBConnect atomic.Int32

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		if target.Host == "hostA" {
			hostAConnect.Add(1)
			// Buffered, non-closing signal: a double-invoked connect cannot
			// panic the test with a double close.
			hostAEntered <- struct{}{}
			<-releaseHostA
			return &fakeWorkspace{apiURL: "http://127.0.0.1:9987", token: "tokA"}, nil
		}
		hostBConnect.Add(1)
		return &fakeWorkspace{apiURL: "http://127.0.0.1:9986", token: "tokB"}, nil
	})

	hostADone := make(chan error, 1)
	go func() {
		_, err := reg.workspaceFor("hostA", "/p")
		hostADone <- err
	}()
	<-hostAEntered // host A is now inside its connect

	// Host B must complete while host A is still blocked.
	type bResult struct {
		ws  remoteHostWorkspace
		err error
	}
	bCh := make(chan bResult, 1)
	go func() {
		ws, err := reg.workspaceFor("hostB", "/p")
		bCh <- bResult{ws, err}
	}()

	select {
	case r := <-bCh:
		if r.err != nil {
			t.Fatalf("hostB: %v", r.err)
		}
		if r.ws.APIURL() != "http://127.0.0.1:9986" {
			t.Fatalf("unexpected hostB API URL: %s", r.ws.APIURL())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hostB blocked behind hostA's in-flight connect")
	}

	close(releaseHostA)
	if err := <-hostADone; err != nil {
		t.Fatalf("hostA: %v", err)
	}
	if c := hostAConnect.Load(); c != 1 {
		t.Fatalf("expected hostA 1 connect, got %d", c)
	}
	if c := hostBConnect.Load(); c != 1 {
		t.Fatalf("expected hostB 1 connect, got %d", c)
	}
}

// TestDropDuringConnectDoesNotEvictReplacement is the regression guard for the
// keyed-delete race: a failing connect whose entry was dropped and replaced
// must not delete the REPLACEMENT entry (which would orphan its tunnel and
// allow a second concurrent connect for the same host).
func TestDropDuringConnectDoesNotEvictReplacement(t *testing.T) {
	reg := newTestRegistry(nil)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	reg.connect = func(target remote.Target, path string) (remoteHostWorkspace, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return nil, errors.New("first connect fails after drop")
		}
		return &fakeWorkspace{apiURL: "http://127.0.0.1:7000", token: "t2"}, nil
	}

	firstErr := make(chan error, 1)
	go func() {
		_, err := reg.workspaceFor("h", "/p")
		firstErr <- err
	}()
	<-entered

	// Drop the in-flight entry, then connect a replacement.
	reg.drop("h")
	if _, err := reg.workspaceFor("h", "/p"); err != nil {
		t.Fatalf("replacement connect: %v", err)
	}

	// Release the original (failing) connect. It must not evict the replacement.
	close(release)
	if err := <-firstErr; err == nil {
		t.Fatal("first connect returned nil error, want failure")
	}

	reg.mu.Lock()
	e := reg.byHost["h"]
	reg.mu.Unlock()
	if e == nil {
		t.Fatal("replacement entry was evicted by the superseded failed connect")
	}
	e.mu.Lock()
	connected := e.connected
	e.mu.Unlock()
	if !connected {
		t.Fatal("replacement entry lost its connected workspace")
	}
}

// TestPanicDuringConnectDoesNotPoisonHost guards the Cond-poisoning case: a
// panic inside connect must be converted to an error and the entry removed, so
// the next call retries instead of blocking forever.
func TestPanicDuringConnectDoesNotPoisonHost(t *testing.T) {
	reg := newTestRegistry(nil)
	var calls atomic.Int32
	reg.connect = func(target remote.Target, path string) (remoteHostWorkspace, error) {
		if calls.Add(1) == 1 {
			panic("boom in connect")
		}
		return &fakeWorkspace{apiURL: "http://127.0.0.1:7001", token: "t"}, nil
	}

	if _, err := reg.workspaceFor("h", "/p"); err == nil {
		t.Fatal("expected error from panicking connect, got nil")
	}

	done := make(chan error, 1)
	go func() {
		_, err := reg.workspaceFor("h", "/p")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("retry after panic: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("host poisoned by a panicking connect — later call blocked forever")
	}
}

// TestConnectTimeoutReleasesWaiters is the regression guard for the "remote SSH
// connect hangs the whole app" report: a connect that never returns used to
// leave every waiter on the entry's sync.Cond pinned forever, so each later
// request for that host (any proxied /api/remote/{host}/... call, triggered by
// a sidebar hover) hung too. With the connect bound, the waiter is released
// with an error and the entry is evicted so the next request retries.
func TestConnectTimeoutReleasesWaiters(t *testing.T) {
	block := make(chan struct{})
	var calls atomic.Int32
	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		if calls.Add(1) == 1 {
			<-block // never returns until the test releases it
		}
		return &fakeWorkspace{apiURL: "http://127.0.0.1:7002", token: "t"}, nil
	})
	reg.connectTimeout = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := reg.workspaceFor("h", "/p")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected connect timeout error, got nil")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected a timeout error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connect timeout never released the caller — waiter pinned forever")
	}

	// The timed-out entry must be evicted so a later call retries instead of
	// joining a dead in-flight connect.
	reg.mu.Lock()
	_, present := reg.byHost["h"]
	reg.mu.Unlock()
	if present {
		t.Fatal("timed-out connect left its entry in byHost; later callers would wait on it")
	}

	close(block)
}

// TestConnectTimeoutDisconnectsLateWorkspace ensures a workspace that lands
// AFTER its connect was already timed out is disconnected rather than leaked
// (the late goroutine cannot be killed).
func TestConnectTimeoutDisconnectsLateWorkspace(t *testing.T) {
	released := make(chan struct{})
	late := &fakeWorkspace{apiURL: "http://127.0.0.1:7003", token: "t"}
	disconnected := make(chan struct{})
	late.disconnect = func() error {
		close(disconnected)
		return nil
	}

	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		<-released
		return late, nil
	})
	reg.connectTimeout = 30 * time.Millisecond

	if _, err := reg.workspaceFor("h", "/p"); err == nil {
		t.Fatal("expected timeout error from never-returning connect")
	}
	// Release the late connect; its workspace must be reaped.
	close(released)
	select {
	case <-disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("late workspace from a timed-out connect was not disconnected")
	}
}

// newTestRegistry creates a remoteHostRegistry with an injectable connect
// function for testing. The real ProcessSupervisor is not needed for tests
// using fake connect; the realConnect factory errors loudly if reached.
func newTestRegistry(connectFn func(remote.Target, string) (remoteHostWorkspace, error)) *remoteHostRegistry {
	return &remoteHostRegistry{
		byHost:          make(map[string]*remoteHostEntry),
		registeredPaths: make(map[string]map[string]struct{}),
		connect:         connectFn,
		factory: func(remote.Target, string, *tool.ProcessSupervisor) (remoteHostWorkspaceConnector, error) {
			return nil, errors.New("factory not configured in this test")
		},
	}
}

// Ensure the fakes satisfy the registry interface.
var _ remoteHostWorkspace = (*fakeWorkspace)(nil)

// fakeConnector is a workspace that records whether Connect ran, so the
// realConnect construct→Connect wiring is covered without SSH.
type fakeConnector struct {
	fakeWorkspace
	connectErr error
	connected  bool
}

func (f *fakeConnector) Connect() error {
	f.connected = true
	return f.connectErr
}

// TestRealConnectCallsConnect pins the wiring the injectable connect seam
// otherwise hides: a constructed workspace has an empty API URL and token
// until Connect runs, so realConnect MUST call Connect before returning it.
func TestRealConnectCallsConnect(t *testing.T) {
	fake := &fakeConnector{}
	fake.apiURL = "http://127.0.0.1:4321"
	fake.token = "tok"

	reg := newRemoteHostRegistry(nil)
	reg.factory = func(target remote.Target, path string, sup *tool.ProcessSupervisor) (remoteHostWorkspaceConnector, error) {
		return fake, nil
	}

	ws, err := reg.realConnect(remote.Target{Kind: remote.KindSSH, Host: "h"}, "/p")
	if err != nil {
		t.Fatalf("realConnect: %v", err)
	}
	if !fake.connected {
		t.Fatal("realConnect returned a workspace without calling Connect")
	}
	if ws.APIURL() != "http://127.0.0.1:4321" {
		t.Errorf("APIURL = %q, want http://127.0.0.1:4321", ws.APIURL())
	}
}

// TestRealConnectPropagatesConnectError ensures a Connect failure surfaces to
// the caller instead of returning a half-built workspace.
func TestRealConnectPropagatesConnectError(t *testing.T) {
	connectErr := errors.New("ssh handshake refused")

	reg := newRemoteHostRegistry(nil)
	reg.factory = func(target remote.Target, path string, sup *tool.ProcessSupervisor) (remoteHostWorkspaceConnector, error) {
		return &fakeConnector{connectErr: connectErr}, nil
	}

	if _, err := reg.realConnect(remote.Target{Kind: remote.KindSSH, Host: "h"}, "/p"); !errors.Is(err, connectErr) {
		t.Fatalf("expected %v, got %v", connectErr, err)
	}
}

// TestMarkRegisteredDoesNotPoisonWorkspaceFor is the regression guard for the
// placeholder-entry deadlock: markRegistered must not create a connection entry
// that a later workspaceFor then waits on forever (no connect is in flight).
func TestMarkRegisteredDoesNotPoisonWorkspaceFor(t *testing.T) {
	reg := newTestRegistry(func(target remote.Target, path string) (remoteHostWorkspace, error) {
		return &fakeWorkspace{apiURL: "http://127.0.0.1:1", token: "t"}, nil
	})

	if !reg.markRegistered("h", "/p") {
		t.Fatal("first markRegistered = false, want true")
	}

	done := make(chan error, 1)
	go func() {
		_, err := reg.workspaceFor("h", "/p")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("workspaceFor after markRegistered: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("workspaceFor hung after markRegistered — placeholder entry deadlock")
	}
}

// TestDropWithoutEntryClearsRegisteredPaths guards the drop early-return
// asymmetry: drop must clear a host's registered path set even when no live
// entry exists, or a later reconnect would skip re-registering the project.
func TestDropWithoutEntryClearsRegisteredPaths(t *testing.T) {
	reg := newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		return &fakeWorkspace{}, nil
	})
	if !reg.markRegistered("h", "/p") {
		t.Fatal("first markRegistered = false, want true")
	}
	reg.drop("h") // no live entry for this host
	if !reg.markRegistered("h", "/p") {
		t.Fatal("markRegistered after drop = false, want true (drop must clear the path set)")
	}
}

// recordingTransport is a remote.Transport that records every Exec command and
// reports a dead pid for `kill -0` so KillServer takes its fast path. killErr,
// when set, makes the initial SIGTERM command fail so the remote-kill stage is
// observable.
type recordingTransport struct {
	mu      sync.Mutex
	calls   []string
	killErr error
}

func (r *recordingTransport) Exec(command string) (remote.ExecResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, command)
	r.mu.Unlock()
	if r.killErr != nil && strings.HasPrefix(command, "kill ") {
		return remote.ExecResult{}, r.killErr
	}
	if strings.Contains(command, "kill -0") {
		return remote.ExecResult{ExitCode: 1}, nil // dead immediately
	}
	return remote.ExecResult{}, nil
}

func (r *recordingTransport) ExecStdin(string, io.Reader) (remote.ExecResult, error) {
	return remote.ExecResult{}, nil
}
func (r *recordingTransport) ExecInteractive(string) error        { return nil }
func (r *recordingTransport) Copy(io.Reader, int64, string) error { return nil }
func (r *recordingTransport) Describe() string                    { return "recording" }

func (r *recordingTransport) hasCall(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestStatus_NeverConnected(t *testing.T) {
	reg := newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		t.Fatal("status must not connect")
		return nil, nil
	})
	st := reg.status("h")
	if st.Connected {
		t.Error("expected connected=false for a host that never connected")
	}
	if st.Version != "" {
		t.Errorf("version = %q, want empty", st.Version)
	}
	if st.LocalVersion != version.Version {
		t.Errorf("local_version = %q, want %q", st.LocalVersion, version.Version)
	}
	if st.Host != "h" {
		t.Errorf("host = %q, want h", st.Host)
	}
}

func TestStatus_ConnectedReportsVersionAndOutdated(t *testing.T) {
	ws := &fakeWorkspace{
		apiURL: "http://127.0.0.1:9",
		token:  "t",
		state:  remote.ServeState{Version: "1.0.0", Outdated: true, PID: 42},
	}
	reg := newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		return ws, nil
	})
	if _, err := reg.workspaceForPort("h", "/p", 0); err != nil {
		t.Fatalf("workspaceForPort: %v", err)
	}
	st := reg.status("h")
	if !st.Connected || st.Version != "1.0.0" || !st.Outdated || st.PID != 42 {
		t.Fatalf("status = %+v, want connected version=1.0.0 outdated=true pid=42", st)
	}
	if st.LocalVersion != version.Version {
		t.Errorf("local_version = %q, want %q", st.LocalVersion, version.Version)
	}
}

// TestRestart_KillsDropsReconnectsAndReregisters pins the restart sequence:
// kill the old pid, drop the entry, reconnect (fresh server), and re-register
// every path that was registered on the host.
func TestRestart_KillsDropsReconnectsAndReregisters(t *testing.T) {
	var mu sync.Mutex
	var registered []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/projects" {
			var body struct {
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			registered = append(registered, body.Path)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tpt := &recordingTransport{}
	oldWS := &fakeWorkspace{
		apiURL:    "http://127.0.0.1:9",
		token:     "old",
		transport: tpt,
		state:     remote.ServeState{Version: "0.0.1", Outdated: true, PID: 42},
	}
	newWS := &fakeWorkspace{
		apiURL: srv.URL,
		token:  "new",
		state:  remote.ServeState{Version: version.Version, PID: 99},
	}
	var connects atomic.Int32
	reg := newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		if connects.Add(1) == 1 {
			return oldWS, nil
		}
		return newWS, nil
	})

	// Seed a registered path so restart has something to re-register.
	reg.markRegistered("h", "/p1")
	if _, err := reg.workspaceForPort("h", "/p1", 0); err != nil {
		t.Fatalf("initial connect: %v", err)
	}

	st, err := reg.restart("h", "/p1", 0)
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if connects.Load() != 2 {
		t.Fatalf("connect calls = %d, want 2 (initial + restart)", connects.Load())
	}
	if !tpt.hasCall("kill 42") {
		t.Fatalf("old pid not killed; calls: %v", tpt.calls)
	}
	mu.Lock()
	gotRegistered := append([]string(nil), registered...)
	mu.Unlock()
	if len(gotRegistered) != 1 || gotRegistered[0] != "/p1" {
		t.Fatalf("re-registered paths = %v, want [/p1]", gotRegistered)
	}
	if !st.Connected || st.PID != 99 || st.Version != version.Version || st.Outdated {
		t.Fatalf("restart status = %+v, want connected new server", st)
	}
}

// TestRestart_KillFailureLeavesEntryDropped: a kill failure reports the
// remote-kill stage and leaves no live entry, so the next request reconnects.
func TestRestart_KillFailureLeavesEntryDropped(t *testing.T) {
	tpt := &recordingTransport{killErr: errors.New("kill: operation not permitted")}
	oldWS := &fakeWorkspace{
		apiURL:    "http://127.0.0.1:9",
		token:     "old",
		transport: tpt,
		state:     remote.ServeState{Version: "0.0.1", PID: 42},
	}
	reg := newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		return oldWS, nil
	})
	if _, err := reg.workspaceForPort("h", "/p", 0); err != nil {
		t.Fatalf("initial connect: %v", err)
	}

	_, err := reg.restart("h", "/p", 0)
	if err == nil {
		t.Fatal("expected a restart error")
	}
	var stageErr *remoteHostStageError
	if !errors.As(err, &stageErr) || stageErr.Stage != "remote-kill" {
		t.Fatalf("error %v does not carry remote-kill stage", err)
	}
	reg.mu.Lock()
	_, present := reg.byHost["h"]
	reg.mu.Unlock()
	if present {
		t.Fatal("restart failure left a live registry entry; want dropped")
	}
}
