package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testSink records calls.
type testSink struct {
	mu     sync.Mutex
	frames []struct {
		w, h uint32
		data []byte
	}
	consoles []ConsoleEvent
	networks []NetworkEvent
	errors   []string
}

func (s *testSink) Frame(w, h uint32, jpeg []byte) {
	s.mu.Lock()
	s.frames = append(s.frames, struct {
		w, h uint32
		data []byte
	}{w, h, jpeg})
	s.mu.Unlock()
}
func (s *testSink) Console(e ConsoleEvent) {
	s.mu.Lock()
	s.consoles = append(s.consoles, e)
	s.mu.Unlock()
}
func (s *testSink) Network(e NetworkEvent) {
	s.mu.Lock()
	s.networks = append(s.networks, e)
	s.mu.Unlock()
}
func (s *testSink) Error(msg string) {
	s.mu.Lock()
	s.errors = append(s.errors, msg)
	s.mu.Unlock()
}
func (s *testSink) getErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]string, len(s.errors))
	copy(cp, s.errors)
	return cp
}
func (s *testSink) getFrames() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}
func (s *testSink) getConsoles() []ConsoleEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]ConsoleEvent, len(s.consoles))
	copy(cp, s.consoles)
	return cp
}

type navCollector struct {
	mu sync.Mutex
	v  []NavEvent
}

func (n *navCollector) emit(e NavEvent) { n.mu.Lock(); n.v = append(n.v, e); n.mu.Unlock() }
func (n *navCollector) get() []NavEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	cp := make([]NavEvent, len(n.v))
	copy(cp, n.v)
	return cp
}
func (n *navCollector) reset() { n.mu.Lock(); n.v = nil; n.mu.Unlock() }

func (s *testSink) getNetworks() []NetworkEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]NetworkEvent, len(s.networks))
	copy(cp, s.networks)
	return cp
}

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
	var nc navCollector
	lg := log.New(&bytes.Buffer{}, "", 0)
	m := newTestManager(t, stub, nc.emit, lg)
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
	sink1.mu.Lock()
	f1 := len(sink1.frames)
	sink1.mu.Unlock()
	if f1 != 0 {
		t.Error("old sink should not receive frames")
	}
	sink2.mu.Lock()
	f2 := len(sink2.frames)
	sink2.mu.Unlock()
	if f2 != 1 {
		t.Errorf("new sink frames %d want 1", f2)
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
	var nc navCollector
	m := NewManager(ManagerOptions{EmitNav: nc.emit, Log: lg})
	m.SetLauncher(func(ctx context.Context) (*Conn, <-chan int, func(), error) {
		return nil, nil, nil, ErrChromeNotFound
	})
	ctx := context.Background()
	_, err := m.Attach(ctx, "k1", &testSink{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !containsNavError(nc.get(), "k1") {
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
	var nc navCollector
	m := newTestManager(t, stub, nc.emit, nil)
	defer m.Close(context.Background())
	ctx := context.Background()
	tgt, err := m.Attach(ctx, "k1", &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	nc.reset()
	if err := tgt.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if !containsCall(stub.Calls(), "Page.navigate") {
		t.Error("expected Page.navigate")
	}
	// Immediate nav Status 0
	found := false
	for _, n := range nc.get() {
		if n.URL == "https://example.com/" && n.Status == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected immediate nav Status 0, got %v", nc.get())
	}
	// Simulate responseReceived for main document
	nc.reset()
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
	for _, n := range nc.get() {
		if n.URL == "https://example.com/" && n.Status == 200 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected nav 200 after responseReceived, got %v", nc.get())
	}
}

func TestManager_LoadingFailed(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	var nc navCollector
	m := newTestManager(t, stub, nc.emit, nil)
	defer m.Close(context.Background())
	ctx := context.Background()
	tgt, _ := m.Attach(ctx, "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	// set main frame
	tgt.mu.Lock()
	tgt.mainFrameID = "mf1"
	tgt.mu.Unlock()
	nc.reset()
	stub.InjectEvent(tgt.sessionID, "Network.loadingFailed", map[string]any{
		"requestId": "r1", "type": "Document", "frameId": "mf1", "errorText": "net::ERR_NAME_NOT_RESOLVED",
	})
	time.Sleep(30 * time.Millisecond)
	if len(nc.get()) == 0 || nc.get()[0].Error == "" {
		t.Fatalf("expected nav error, got %v", nc.get())
	}
	if !containsStr(nc.get()[0].Error, "ERR_NAME_NOT_RESOLVED") {
		t.Errorf("error %q", nc.get()[0].Error)
	}
	// Tunnel error maps to not reachable
	nc.reset()
	stub.InjectEvent(tgt.sessionID, "Network.loadingFailed", map[string]any{
		"requestId": "r2", "type": "Document", "frameId": "mf1", "errorText": "net::ERR_TUNNEL_CONNECTION_FAILED",
	})
	time.Sleep(30 * time.Millisecond)
	if len(nc.get()) == 0 || !containsStr(nc.get()[0].Error, "not reachable from Chrome mode") {
		t.Errorf("expected tunnel mapping, got %v", nc.get())
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
	var nc navCollector
	m := newTestManager(t, stub, nc.emit, nil)
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
	for _, n := range nc.get() {
		if n.Error != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected nav error for bad scheme")
	}
}

// Chrome renders its internal navigation-error page at
// chrome-error://chromewebdata/ when a load fails (proxy/TLS/DNS error). That
// must NOT be treated as an unsupported user scheme: the real error is already
// emitted via Network.loadingFailed, and blanking the page would hide it.
// Covers both event orderings (loadingFailed before/after frameNavigated)
// since CDP does not guarantee their relative arrival order.
func TestManager_FrameNavigatedChromeError(t *testing.T) {
	tryOrder := func(t *testing.T, loadingFailedFirst bool) {
		t.Helper()
		stub := newStubChrome()
		defer stub.Close()
		var nc navCollector
		m := newTestManager(t, stub, nc.emit, nil)
		defer m.Close(context.Background())
		tgt, _ := m.Attach(context.Background(), "k1", &testSink{})
		time.Sleep(30 * time.Millisecond)
		tgt.mu.Lock()
		tgt.mainFrameID = "mf1"
		tgt.mu.Unlock()
		if err := tgt.Navigate(context.Background(), "https://yahoo.com"); err != nil {
			t.Fatalf("Navigate: %v", err)
		}
		navBefore := len(stub.CallsFor("Page.navigate"))

		loadingFailed := func() {
			stub.InjectEvent(tgt.sessionID, "Network.loadingFailed", map[string]any{
				"requestId": "r1", "type": "Document", "frameId": "mf1", "errorText": "net::ERR_PROXY_CONNECTION_FAILED",
			})
		}
		frameNavigated := func() {
			stub.InjectEvent(tgt.sessionID, "Page.frameNavigated", map[string]any{
				"frame": map[string]any{"id": "mf1", "url": "chrome-error://chromewebdata/"},
			})
		}
		if loadingFailedFirst {
			loadingFailed()
			frameNavigated()
		} else {
			frameNavigated()
			loadingFailed()
		}
		time.Sleep(40 * time.Millisecond)

		if got := len(stub.CallsFor("Page.navigate")); got != navBefore {
			t.Errorf("chrome-error page must not be blanked via Page.navigate: got %d calls, want %d", got, navBefore)
		}
		for _, n := range nc.get() {
			if containsStr(n.Error, "unsupported URL scheme") {
				t.Errorf("chrome-error page must not emit unsupported URL scheme, got error %q", n.Error)
			}
		}
		// The genuine loadingFailed error must still be observable.
		found := false
		for _, n := range nc.get() {
			if containsStr(n.Error, "ERR_PROXY_CONNECTION_FAILED") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected real loadingFailed error to survive, got %v", nc.get())
		}
	}

	t.Run("loadingFailed_first", func(t *testing.T) { tryOrder(t, true) })
	t.Run("frameNavigated_first", func(t *testing.T) { tryOrder(t, false) })
}

func TestManager_NavigatedWithinDocument(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	var nc navCollector
	m := newTestManager(t, stub, nc.emit, nil)
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
	for _, n := range nc.get() {
		if n.URL == "https://example.com/#hash" && n.Status == 200 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected navigatedWithinDocument nav, got %v", nc.get())
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

// --- Task 5 tests ---

func TestManager_ScreencastStart(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	_, _ = m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(40 * time.Millisecond)
	calls := stub.Calls()
	// Find order of stop then start
	var stopIdx, startIdx = -1, -1
	for i, c := range calls {
		if c.Method == "Page.stopScreencast" && stopIdx == -1 {
			stopIdx = i
		}
		if c.Method == "Page.startScreencast" && startIdx == -1 {
			startIdx = i
		}
	}
	if stopIdx == -1 || startIdx == -1 {
		t.Fatalf("missing screencast calls: stop %d start %d", stopIdx, startIdx)
	}
	if stopIdx > startIdx {
		t.Error("stop should come before start")
	}
	// Check defaults
	for _, c := range calls {
		if c.Method == "Page.startScreencast" {
			var p map[string]any
			_ = json.Unmarshal(c.Params, &p)
			if p["format"] != "jpeg" {
				t.Errorf("format %v", p["format"])
			}
			if p["quality"] != float64(70) {
				t.Errorf("quality %v", p["quality"])
			}
			if p["maxWidth"] != float64(1280) || p["maxHeight"] != float64(800) {
				t.Errorf("max dims %v %v", p["maxWidth"], p["maxHeight"])
			}
		}
	}
}

func TestManager_ScreencastFrame(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	sink := &testSink{}
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", sink)
	time.Sleep(30 * time.Millisecond)
	// clear calls after attach
	stub.callsMu.Lock()
	stub.calls = nil
	stub.callsMu.Unlock()
	stub.InjectEvent(tgt.sessionID, "Page.screencastFrame", map[string]any{
		"data": "aGVsbG8=", "sessionId": 7, "metadata": map[string]any{"deviceWidth": 1280, "deviceHeight": 800},
	})
	time.Sleep(40 * time.Millisecond)
	sink.mu.Lock()
	f := append([]struct {
		w, h uint32
		data []byte
	}(nil), sink.frames...)
	sink.mu.Unlock()
	if len(f) != 1 {
		t.Fatalf("frames %d", len(f))
	}
	if f[0].w != 1280 || f[0].h != 800 {
		t.Errorf("dims %d %d", f[0].w, f[0].h)
	}
	// Ack should have been sent after sink
	found := false
	for _, c := range stub.Calls() {
		if c.Method == "Page.screencastFrameAck" {
			var p map[string]int
			_ = json.Unmarshal(c.Params, &p)
			if p["sessionId"] == 7 {
				found = true
			}
		}
	}
	if !found {
		t.Error("missing screencastFrameAck")
	}
}

func TestManager_Resize(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	// clear
	stub.callsMu.Lock()
	stub.calls = nil
	stub.callsMu.Unlock()
	if err := tgt.Resize(context.Background(), 1000, 600, 2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if !containsCall(stub.Calls(), "Emulation.setDeviceMetricsOverride") {
		t.Error("missing setDeviceMetricsOverride")
	}
	// check params
	for _, c := range stub.Calls() {
		if c.Method == "Emulation.setDeviceMetricsOverride" {
			var p map[string]any
			_ = json.Unmarshal(c.Params, &p)
			if p["width"] != float64(1000) || p["height"] != float64(600) || p["deviceScaleFactor"] != float64(2) {
				t.Errorf("metrics %v", p)
			}
		}
		if c.Method == "Page.startScreencast" {
			var p map[string]any
			_ = json.Unmarshal(c.Params, &p)
			if p["maxWidth"] != float64(2000) || p["maxHeight"] != float64(1200) {
				t.Errorf("resize screencast dims %v", p)
			}
		}
	}
}

func TestManager_MouseKey(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	stub.callsMu.Lock()
	stub.calls = nil
	stub.callsMu.Unlock()
	_ = tgt.Mouse(context.Background(), MouseEvent{Kind: "down", X: 10, Y: 20, Button: "left", ClickCount: 1})
	time.Sleep(20 * time.Millisecond)
	if !containsCall(stub.Calls(), "Input.dispatchMouseEvent") {
		t.Error("missing mouse")
	}
	for _, c := range stub.Calls() {
		if c.Method == "Input.dispatchMouseEvent" {
			var p map[string]any
			_ = json.Unmarshal(c.Params, &p)
			if p["type"] != "mousePressed" {
				t.Errorf("type %v", p["type"])
			}
		}
	}
	stub.callsMu.Lock()
	stub.calls = nil
	stub.callsMu.Unlock()
	_ = tgt.Key(context.Background(), KeyEvent{Kind: "down", Key: "a", Code: "KeyA", Text: "a"})
	time.Sleep(20 * time.Millisecond)
	found := false
	for _, c := range stub.Calls() {
		if c.Method == "Input.dispatchKeyEvent" {
			var p map[string]any
			_ = json.Unmarshal(c.Params, &p)
			if p["type"] == "keyDown" {
				found = true
			}
		}
	}
	if !found {
		t.Error("missing keyDown")
	}
}

func TestManager_Detach(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	sink := &testSink{}
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", sink)
	time.Sleep(30 * time.Millisecond)
	tgt.Detach()
	time.Sleep(20 * time.Millisecond)
	if !containsCall(stub.Calls(), "Page.stopScreencast") {
		t.Error("detach should stop screencast")
	}
	// After detach, frames not delivered
	stub.InjectEvent(tgt.sessionID, "Page.screencastFrame", map[string]any{
		"data": "aGVsbG8=", "sessionId": 1, "metadata": map[string]any{"deviceWidth": 10, "deviceHeight": 10},
	})
	time.Sleep(20 * time.Millisecond)
	sink.mu.Lock()
	fc := len(sink.frames)
	sink.mu.Unlock()
	if fc != 0 {
		t.Errorf("frames after detach %d", fc)
	}
}

// --- Task 6 tests ---

func TestManager_Console(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	sink := &testSink{}
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", sink)
	time.Sleep(30 * time.Millisecond)
	stub.InjectEvent(tgt.sessionID, "Runtime.consoleAPICalled", map[string]any{
		"type": "warning", "args": []any{map[string]any{"value": "x"}, map[string]any{"description": "Object"}}, "timestamp": 1234,
	})
	time.Sleep(30 * time.Millisecond)
	sink.mu.Lock()
	cc := append([]ConsoleEvent(nil), sink.consoles...)
	sink.mu.Unlock()
	if len(cc) != 1 || cc[0].Level != "warn" {
		t.Fatalf("console %v", cc)
	}
	if len(cc[0].Args) != 2 || cc[0].Args[0] != "x" {
		t.Errorf("args %v", cc[0].Args)
	}
	// exception
	stub.InjectEvent(tgt.sessionID, "Runtime.exceptionThrown", map[string]any{
		"exceptionDetails": map[string]any{"text": "oops", "url": "https://x.com/app.js", "lineNumber": 10},
		"timestamp":        1235,
	})
	time.Sleep(30 * time.Millisecond)
	sink.mu.Lock()
	cc2 := append([]ConsoleEvent(nil), sink.consoles...)
	sink.mu.Unlock()
	if len(cc2) != 2 || cc2[1].Level != "error" {
		t.Fatalf("exception %v", cc2)
	}
	if !containsStr(cc2[1].Args[0], "oops") {
		t.Errorf("exception text %v", sink.consoles[1].Args)
	}
}

func TestManager_NetworkTelemetry(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	sink := &testSink{}
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", sink)
	time.Sleep(30 * time.Millisecond)
	stub.InjectEvent(tgt.sessionID, "Network.requestWillBeSent", map[string]any{
		"requestId": "r1", "request": map[string]any{"url": "https://example.com/a", "method": "GET"},
	})
	time.Sleep(20 * time.Millisecond)
	stub.InjectEvent(tgt.sessionID, "Network.responseReceived", map[string]any{
		"requestId": "r1", "type": "Other", "frameId": "f1",
		"response": map[string]any{"url": "https://example.com/a", "status": 200},
	})
	time.Sleep(30 * time.Millisecond)
	sink.mu.Lock()
	nn := append([]NetworkEvent(nil), sink.networks...)
	sink.mu.Unlock()
	if len(nn) < 2 {
		t.Fatalf("networks %d", len(nn))
	}
	found := false
	for _, n := range nn {
		if n.URL == "https://example.com/a" && n.Status == 200 {
			if n.DurationMs < 0 {
				t.Error("duration negative")
			}
			found = true
		}
	}
	if !found {
		t.Error("missing network 200")
	}
	// loadingFailed tunnel → blocked
	stub.InjectEvent(tgt.sessionID, "Network.loadingFailed", map[string]any{
		"requestId": "r2", "type": "Other", "errorText": "net::ERR_TUNNEL_CONNECTION_FAILED",
	})
	time.Sleep(30 * time.Millisecond)
	sink.mu.Lock()
	nn2 := append([]NetworkEvent(nil), sink.networks...)
	sink.mu.Unlock()
	found = false
	for _, n := range nn2 {
		if n.Blocked == "private address" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected blocked private address, got %v", nn2)
	}
}

func TestManager_AutoAttach(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())
	_, _ = m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(40 * time.Millisecond)
	// Inject attachedToTarget waitingForDebugger
	stub.InjectEvent("", "Target.attachedToTarget", map[string]any{
		"sessionId": "W1", "waitingForDebugger": true,
	})
	time.Sleep(40 * time.Millisecond)
	if !containsCall(stub.Calls(), "Runtime.runIfWaitingForDebugger") {
		t.Error("missing runIfWaitingForDebugger")
	}
}

func TestManager_TargetCrashed(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	var nc navCollector
	sink := &testSink{}
	m := newTestManager(t, stub, nc.emit, nil)
	defer m.Close(context.Background())
	tgt, _ := m.Attach(context.Background(), "k1", sink)
	time.Sleep(30 * time.Millisecond)
	stub.InjectEvent("", "Target.targetCrashed", map[string]any{
		"targetId": tgt.targetID, "status": "crashed",
	})
	time.Sleep(40 * time.Millisecond)
	sink.mu.Lock()
	errCopy := append([]string(nil), sink.errors...)
	sink.mu.Unlock()
	if len(errCopy) == 0 || errCopy[0] != "target crashed" {
		t.Errorf("sink errors %v", errCopy)
	}
	ng := nc.get()
	if len(ng) == 0 || ng[len(ng)-1].Error != "target crashed" {
		t.Errorf("navs %v", ng)
	}
	// Next attach for same key should create fresh target
	before := len(stub.CallsFor("Target.createBrowserContext"))
	_, _ = m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	after := len(stub.CallsFor("Target.createBrowserContext"))
	if after != before+1 {
		t.Errorf("expected fresh context after crash: before %d after %d", before, after)
	}
}

func TestManager_ChromeExit(t *testing.T) {
	stub1 := newStubChrome()
	stub2 := newStubChrome()
	var launchCount int
	var currentStub *stubChrome = stub1
	m := NewManager(ManagerOptions{EmitNav: func(e NavEvent) {}, Log: log.New(&bytes.Buffer{}, "", 0)})
	exited := make(chan int)
	m.SetLauncher(func(ctx context.Context) (*Conn, <-chan int, func(), error) {
		launchCount++
		if launchCount == 1 {
			currentStub = stub1
		} else {
			currentStub = stub2
			exited = make(chan int)
		}
		return currentStub.Conn(), exited, func() {}, nil
	})
	// Attach two keys via first stub
	sink1 := &testSink{}
	sink2 := &testSink{}
	_, _ = m.Attach(context.Background(), "k1", sink1)
	_, _ = m.Attach(context.Background(), "k2", sink2)
	time.Sleep(40 * time.Millisecond)
	// Simulate exit by closing conn and firing exited
	_ = stub1.Conn().Close()
	close(exited)
	time.Sleep(60 * time.Millisecond)
	sink1.mu.Lock()
	e1 := append([]string(nil), sink1.errors...)
	sink1.mu.Unlock()
	if len(e1) == 0 || e1[0] != "chrome exited" {
		t.Errorf("sink1 %v", e1)
	}
	sink2.mu.Lock()
	e2 := append([]string(nil), sink2.errors...)
	sink2.mu.Unlock()
	if len(e2) == 0 {
		t.Errorf("sink2 %v", e2)
	}
	// Next attach should relaunch (launcher called twice)
	_, _ = m.Attach(context.Background(), "k3", &testSink{})
	time.Sleep(30 * time.Millisecond)
	if launchCount != 2 {
		t.Errorf("launchCount %d want 2", launchCount)
	}
	_ = m.Close(context.Background())
	stub1.Close()
	stub2.Close()
}

func TestManager_IdleReaper(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := NewManager(ManagerOptions{IdleTimeout: 50 * time.Millisecond, Log: log.New(&bytes.Buffer{}, "", 0)})
	exited := make(chan int)
	var cleanupCalled atomic.Bool
	m.SetLauncher(func(ctx context.Context) (*Conn, <-chan int, func(), error) {
		return stub.Conn(), exited, func() { cleanupCalled.Store(true) }, nil
	})
	_, _ = m.Attach(context.Background(), "k1", &testSink{})
	time.Sleep(30 * time.Millisecond)
	m.Revoke("k1")
	time.Sleep(120 * time.Millisecond)
	if !cleanupCalled.Load() {
		t.Error("expected cleanup after idle timeout")
	}
	// Next attach should relaunch
	stub2 := newStubChrome()
	defer stub2.Close()
	exited2 := make(chan int)
	m.SetLauncher(func(ctx context.Context) (*Conn, <-chan int, func(), error) {
		return stub2.Conn(), exited2, func() {}, nil
	})
	_, err := m.Attach(context.Background(), "k2", &testSink{})
	if err != nil {
		t.Fatalf("relaunch after idle: %v", err)
	}
	_ = m.Close(context.Background())
}


// A policy 403 from the egress proxy carries X-Ocode-Blocked (readable by
// CDP even though CORS hides it from page JS for cross-origin fetches with
// ACAO:*): the network drawer row must surface it as Blocked.
func TestNetworkRowMarksProxyBlockedResponses(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := newTestManager(t, stub, nil, nil)
	defer m.Close(context.Background())

	sink := &testSink{}
	tgt, err := m.Attach(context.Background(), "tab:blk", sink)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}

	stub.InjectEvent(tgt.sessionID, "Network.requestWillBeSent", map[string]any{
		"requestId": "R1", "request": map[string]any{"url": "http://10.0.0.1/", "method": "GET"},
	})
	stub.InjectEvent(tgt.sessionID, "Network.responseReceived", map[string]any{
		"requestId": "R1", "type": "Fetch",
		"response": map[string]any{
			"url": "http://10.0.0.1/", "status": 403,
			"headers": map[string]any{"X-Ocode-Blocked": "private address"},
		},
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		var got *NetworkEvent
		for _, n := range sink.getNetworks() {
			if n.URL == "http://10.0.0.1/" && n.Status == 403 {
				nn := n
				got = &nn
				break
			}
		}
		if got != nil {
			if got.Blocked != "private address" {
				t.Fatalf("Blocked = %q, want %q; ev=%+v", got.Blocked, "private address", *got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no 403 network row for http://10.0.0.1/; rows=%+v", sink.getNetworks())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
