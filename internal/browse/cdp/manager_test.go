package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"
)

// testSink records calls.
type testSink struct {
	frames []struct{ w, h uint32; data []byte }
	consoles []ConsoleEvent
	networks []NetworkEvent
	errors   []string
}

func (s *testSink) Frame(w, h uint32, jpeg []byte) { s.frames = append(s.frames, struct{ w, h uint32; data []byte }{w, h, jpeg}) }
func (s *testSink) Console(e ConsoleEvent)       { s.consoles = append(s.consoles, e) }
func (s *testSink) Network(e NetworkEvent)       { s.networks = append(s.networks, e) }
func (s *testSink) Error(msg string)             { s.errors = append(s.errors, msg) }

func newTestManager(t *testing.T, stub *stubChrome, emit func(NavEvent), lg *log.Logger) *Manager {
	t.Helper()
	m := NewManager(ManagerOptions{EmitNav: emit, Log: lg})
	exited := make(chan int)
	m.SetLauncher(func(ctx context.Context) (*Conn, <-chan int, func(), error) {
		return stub.Conn(), exited, func() {}, nil
	})
	return m
}

func TestManager_LazyLaunch(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	var navs []NavEvent
	lg := log.New(&bytes.Buffer{}, "", 0)
	m := newTestManager(t, stub, func(e NavEvent) { navs = append(navs, e) }, lg)
	defer m.Close(context.Background())

	// NewManager should not have launched (no calls yet)
	if len(stub.Calls()) != 0 {
		t.Fatalf("expected no calls before Attach, got %d", len(stub.Calls()))
	}

	sink := &testSink{}
	ctx := context.Background()
	_, err := m.Attach(ctx, "k1", sink)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	// Wait for async calls to be processed
	time.Sleep(50 * time.Millisecond)
	calls := stub.Calls()
	has := func(method string) bool {
		for _, c := range calls {
			if c.Method == method {
				return true
			}
		}
		return false
	}
	if !has("Target.createBrowserContext") {
		t.Error("missing createBrowserContext")
	}
	if !has("Target.createTarget") {
		t.Error("missing createTarget")
	}
	if !has("Target.attachToTarget") {
		t.Error("missing attachToTarget")
	}
	if !has("Target.setAutoAttach") {
		t.Error("missing setAutoAttach")
	}
	if !has("Page.enable") {
		t.Error("missing Page.enable")
	}
	// Check proxyServer param
	for _, c := range calls {
		if c.Method == "Target.createBrowserContext" {
			var p map[string]string
			_ = json.Unmarshal(c.Params, &p)
			if p["proxyServer"] == "" {
				t.Error("proxyServer empty")
			}
			if p["proxyBypassList"] != "<-loopback>" {
				t.Errorf("proxyBypassList %q", p["proxyBypassList"])
			}
		}
	}
}

func TestManager_SecondAttachSameKeyReplacesSink(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	ctx := context.Background()
	sink1 := &testSink{}
	_, err := m.Attach(ctx, "k1", sink1)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	before := len(stub.CallsFor("Target.createBrowserContext"))
	sink2 := &testSink{}
	_, err = m.Attach(ctx, "k1", sink2)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	after := len(stub.CallsFor("Target.createBrowserContext"))
	if after != before {
		t.Errorf("second Attach same key should not create new context: before %d after %d", before, after)
	}
	// Old sink should not receive frames; new sink should
	// Inject a frame for the session
	calls := stub.CallsFor("Target.attachToTarget")
	if len(calls) == 0 {
		t.Fatal("no attach")
	}
	// Find session via stub seq — we can inject on any session, but target's session is sess-<n>
	// Retrieve manager target's session
	m.mu.Lock()
	tgt := m.targets["k1"]
	m.mu.Unlock()
	if tgt == nil {
		t.Fatal("missing target")
	}
	stub.InjectEvent(tgt.sessionID, "Page.screencastFrame", map[string]any{
		"data": "aGVsbG8=", "sessionId": 1, "metadata": map[string]any{"deviceWidth": 10, "deviceHeight": 20},
	})
	time.Sleep(30 * time.Millisecond)
	if len(sink1.frames) != 0 {
		t.Error("old sink should not receive frames")
	}
	if len(sink2.frames) != 1 {
		t.Errorf("new sink frames %d want 1", len(sink2.frames))
	}
}

func TestManager_TwoKeysTwoContexts(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	ctx := context.Background()
	_, _ = m.Attach(ctx, "k1", &testSink{})
	_, _ = m.Attach(ctx, "k2", &testSink{})
	time.Sleep(40 * time.Millisecond)
	if n := len(stub.CallsFor("Target.createBrowserContext")); n != 2 {
		t.Errorf("expected 2 contexts, got %d", n)
	}
}

func TestManager_Revoke(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	ctx := context.Background()
	_, _ = m.Attach(ctx, "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	m.Revoke("k1")
	time.Sleep(30 * time.Millisecond)
	if !containsCall(stub.Calls(), "Target.closeTarget") {
		t.Error("Revoke should call closeTarget")
	}
	if !containsCall(stub.Calls(), "Target.disposeBrowserContext") {
		t.Error("Revoke should call disposeBrowserContext")
	}
	// revoke unknown no-op
	m.Revoke("unknown")
}

func TestManager_FindChromeFailure(t *testing.T) {
	var buf bytes.Buffer
	lg := log.New(&buf, "", 0)
	var navs []NavEvent
	m := NewManager(ManagerOptions{EmitNav: func(e NavEvent) { navs = append(navs, e) }, Log: lg})
	m.SetLauncher(func(ctx context.Context) (*Conn, <-chan int, func(), error) {
		return nil, nil, nil, ErrChromeNotFound
	})
	ctx := context.Background()
	_, err := m.Attach(ctx, "k1", &testSink{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !containsNavError(navs, "k1") {
		t.Error("expected EmitNav for k1")
	}
	// Second attach should not log second line
	_, _ = m.Attach(ctx, "k2", &testSink{})
	lines := bytes.Count(buf.Bytes(), []byte("\n"))
	if lines != 1 {
		t.Errorf("expected 1 log line, got %d: %q", lines, buf.String())
	}
}

func containsCall(calls []stubCall, method string) bool {
	for _, c := range calls {
		if c.Method == method {
			return true
		}
	}
	return false
}
func containsNavError(navs []NavEvent, key string) bool {
	for _, n := range navs {
		if n.StateKey == key && n.Error != "" {
			return true
		}
	}
	return false
}
