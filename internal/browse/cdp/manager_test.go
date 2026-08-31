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

// --- Task 4 tests ---

func TestManager_Navigate(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	var navs []NavEvent
	m := newTestManager(t, stub, func(e NavEvent) { navs = append(navs, e) }, nil)
	defer m.Close(context.Background())
	ctx := context.Background()
	tgt, err := m.Attach(ctx, "k1", &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	navs = nil
	if err := tgt.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if !containsCall(stub.Calls(), "Page.navigate") {
		t.Error("expected Page.navigate")
	}
	// Immediate nav Status 0
	found := false
	for _, n := range navs {
		if n.URL == "https://example.com/" && n.Status == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected immediate nav Status 0, got %v", navs)
	}
	// Simulate responseReceived for main document
	navs = nil
	// Need main frame id; use target's mainFrameID or inject with any frame
	mm := tgt
	mm.mu.Lock()
	mf := mm.mainFrameID
	mm.mu.Unlock()
	if mf == "" {
		mf = "frame-main"
		// set it so handler matches
		mm.mu.Lock()
		mm.mainFrameID = mf
		mm.mu.Unlock()
	}
	stub.InjectEvent(tgt.sessionID, "Network.responseReceived", map[string]any{
		"requestId": "r1", "type": "Document", "frameId": mf,
		"response": map[string]any{"url": "https://example.com/", "status": 200},
	})
	time.Sleep(30 * time.Millisecond)
	found = false
	for _, n := range navs {
		if n.URL == "https://example.com/" && n.Status == 200 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected nav 200 after responseReceived, got %v", navs)
	}
}

func TestManager_LoadingFailed(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	var navs []NavEvent
	m := newTestManager(t, stub, func(e NavEvent) { navs = append(navs, e) }, nil)
	defer m.Close(context.Background())
	ctx := context.Background()
	tgt, _ := m.Attach(ctx, "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	// set main frame
	tgt.mu.Lock()
	tgt.mainFrameID = "mf1"
	tgt.mu.Unlock()
	navs = nil
	stub.InjectEvent(tgt.sessionID, "Network.loadingFailed", map[string]any{
		"requestId": "r1", "type": "Document", "frameId": "mf1", "errorText": "net::ERR_NAME_NOT_RESOLVED",
	})
	time.Sleep(30 * time.Millisecond)
	if len(navs) == 0 || navs[0].Error == "" {
		t.Fatalf("expected nav error, got %v", navs)
	}
	if !containsStr(navs[0].Error, "ERR_NAME_NOT_RESOLVED") {
		t.Errorf("error %q", navs[0].Error)
	}
	// Tunnel error maps to not reachable
	navs = nil
	stub.InjectEvent(tgt.sessionID, "Network.loadingFailed", map[string]any{
		"requestId": "r2", "type": "Document", "frameId": "mf1", "errorText": "net::ERR_TUNNEL_CONNECTION_FAILED",
	})
	time.Sleep(30 * time.Millisecond)
	if len(navs) == 0 || !containsStr(navs[0].Error, "not reachable from Chrome mode") {
		t.Errorf("expected tunnel mapping, got %v", navs)
	}
}

func TestManager_NavigateBadScheme(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	before := len(stub.Calls())
	for _, url := range []string{"file:///etc/passwd", "javascript:1", "data:text/html,hi", "chrome://settings"} {
		if err := tgt.Navigate(context.Background(), url); err == nil {
			t.Errorf("expected ErrBadScheme for %q", url)
		}
	}
	if len(stub.Calls()) != before {
		t.Error("bad scheme should not call CDP")
	}
}

func TestManager_FrameNavigatedBadScheme(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	var navs []NavEvent
	m := newTestManager(t, stub, func(e NavEvent) { navs = append(navs, e) }, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	tgt.mu.Lock()
	tgt.mainFrameID = "mf1"
	tgt.mu.Unlock()
	stub.InjectEvent(tgt.sessionID, "Page.frameNavigated", map[string]any{
		"frame": map[string]any{"id": "mf1", "url": "file:///etc/passwd"},
	})
	time.Sleep(40 * time.Millisecond)
	if !containsCall(stub.Calls(), "Page.navigate") {
		t.Error("expected navigate to about:blank")
	}
	// Check that nav error emitted
	found := false
	for _, n := range navs {
		if n.Error != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected nav error for bad scheme")
	}
}

func TestManager_NavigatedWithinDocument(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	var navs []NavEvent
	m := newTestManager(t, stub, func(e NavEvent) { navs = append(navs, e) }, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	tgt.mu.Lock()
	tgt.mainFrameID = "mf1"
	tgt.mu.Unlock()
	stub.InjectEvent(tgt.sessionID, "Page.navigatedWithinDocument", map[string]any{
		"frameId": "mf1", "url": "https://example.com/#hash",
	})
	time.Sleep(30 * time.Millisecond)
	found := false
	for _, n := range navs {
		if n.URL == "https://example.com/#hash" && n.Status == 200 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected navigatedWithinDocument nav, got %v", navs)
	}
}

func TestManager_BackForwardReload(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	// Our stub for getNavigationHistory returns empty; Back should not error
	if err := tgt.Back(context.Background()); err != nil {
		t.Fatalf("Back: %v", err)
	}
	if err := tgt.Forward(context.Background()); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if err := tgt.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	// Check calls
	if !containsCall(stub.Calls(), "Page.getNavigationHistory") {
		t.Error("missing getNavigationHistory")
	}
	if !containsCall(stub.Calls(), "Page.reload") {
		t.Error("missing reload")
	}
}

func containsStr(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
