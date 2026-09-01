package browse

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/u007/ocode/internal/browse/cdp"
)

// fakeTarget records calls for assertions.
type fakeTarget struct {
	mu        sync.Mutex
	navigates []string
	backs     int
	forwards  int
	reloads   int
	resizes   []struct {
		w, h int
		dpr  float64
	}
	mouses       []cdp.MouseEvent
	keys         []cdp.KeyEvent
	detachCalled bool
}

func (f *fakeTarget) Navigate(_ context.Context, url string) error {
	f.mu.Lock()
	f.navigates = append(f.navigates, url)
	f.mu.Unlock()
	return nil
}
func (f *fakeTarget) Back(_ context.Context) error { f.mu.Lock(); f.backs++; f.mu.Unlock(); return nil }
func (f *fakeTarget) Forward(_ context.Context) error {
	f.mu.Lock()
	f.forwards++
	f.mu.Unlock()
	return nil
}
func (f *fakeTarget) Reload(_ context.Context) error {
	f.mu.Lock()
	f.reloads++
	f.mu.Unlock()
	return nil
}
func (f *fakeTarget) Resize(_ context.Context, w, h int, dpr float64) error {
	f.mu.Lock()
	f.resizes = append(f.resizes, struct {
		w, h int
		dpr  float64
	}{w, h, dpr})
	f.mu.Unlock()
	return nil
}
func (f *fakeTarget) Mouse(_ context.Context, ev cdp.MouseEvent) error {
	f.mu.Lock()
	f.mouses = append(f.mouses, ev)
	f.mu.Unlock()
	return nil
}
func (f *fakeTarget) Key(_ context.Context, ev cdp.KeyEvent) error {
	f.mu.Lock()
	f.keys = append(f.keys, ev)
	f.mu.Unlock()
	return nil
}
func (f *fakeTarget) Detach() { f.mu.Lock(); f.detachCalled = true; f.mu.Unlock() }

// fakeManager implements chromeManager without Chrome.
type fakeManager struct {
	mu          sync.Mutex
	attachCalls []string
	sinks       map[string]cdp.FrameSink
	targets     map[string]*fakeTarget
	attachErr   error
	revoked     []string
	closeCalled bool
}

func newFakeManager() *fakeManager {
	return &fakeManager{sinks: make(map[string]cdp.FrameSink), targets: make(map[string]*fakeTarget)}
}

func (m *fakeManager) Attach(_ context.Context, stateKey string, sink cdp.FrameSink) (chromeTarget, error) {
	if m.attachErr != nil {
		return nil, m.attachErr
	}
	m.mu.Lock()
	m.attachCalls = append(m.attachCalls, stateKey)
	m.sinks[stateKey] = sink
	t := &fakeTarget{}
	m.targets[stateKey] = t
	m.mu.Unlock()
	return t, nil
}
func (m *fakeManager) Revoke(stateKey string) {
	m.mu.Lock()
	m.revoked = append(m.revoked, stateKey)
	m.mu.Unlock()
}
func (m *fakeManager) Close(_ context.Context) error {
	m.mu.Lock()
	m.closeCalled = true
	m.mu.Unlock()
	return nil
}

func (m *fakeManager) sinkFor(key string) cdp.FrameSink {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sinks[key]
}
func (m *fakeManager) targetFor(key string) *fakeTarget {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.targets[key]
}

func newBrowseWithFake(t *testing.T, spaOrigin string) (*Server, *fakeManager, *httptest.Server) {
	t.Helper()
	s := New("apitoken", nil)
	s.SetSPAOrigin(spaOrigin)
	fake := newFakeManager()
	s.SetCDPManager(fake)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, fake, ts
}

func wsURL(ts *httptest.Server, stateKey, grant string) string {
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/b/" + stateKey + "/__cdp?__grant=" + grant
	return u
}

func TestCDP_NoGrant401(t *testing.T) {
	_, _, ts := newBrowseWithFake(t, "http://example.com")
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/b/tab:x/__cdp"
	dialer := websocket.Dialer{}
	_, resp, err := dialer.Dial(u, http.Header{"Origin": []string{"http://example.com"}})
	if err == nil {
		t.Fatal("expected dial error for missing grant")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got resp %v err %v", resp, err)
	}
}

func TestCDP_UsedGrant401(t *testing.T) {
	s, _, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	u := wsURL(ts, "tab:x", grant)
	dialer := websocket.Dialer{}
	header := http.Header{"Origin": []string{"http://example.com"}}
	conn, _, err := dialer.Dial(u, header)
	if err != nil {
		t.Fatalf("first dial failed: %v", err)
	}
	_ = conn.Close()
	// second use of same grant should 401
	_, resp, err2 := dialer.Dial(u, header)
	if err2 == nil {
		t.Fatal("second dial with used grant should fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second dial want 401, got %v", resp)
	}
}

func TestCDP_WrongOrigin403(t *testing.T) {
	s, _, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	u := wsURL(ts, "tab:x", grant)
	dialer := websocket.Dialer{}
	_, resp, err := dialer.Dial(u, http.Header{"Origin": []string{"http://evil.com"}})
	if err == nil {
		t.Fatal("expected dial error for wrong origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %v err %v", resp, err)
	}
}

func TestCDP_ValidUpgrade101AndAttach(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	u := wsURL(ts, "tab:x", grant)
	dialer := websocket.Dialer{}
	header := http.Header{"Origin": []string{"http://example.com"}}
	conn, resp, err := dialer.Dial(u, header)
	if err != nil {
		t.Fatalf("dial failed: %v resp=%v", err, resp)
	}
	defer conn.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("want 101, got %d", resp.StatusCode)
	}
	// Give handler time to Attach
	time.Sleep(50 * time.Millisecond)
	fake.mu.Lock()
	if len(fake.attachCalls) != 1 || fake.attachCalls[0] != "tab:x" {
		t.Fatalf("Attach calls = %v, want [tab:x]", fake.attachCalls)
	}
	fake.mu.Unlock()
}

func TestCDP_SinkFrameBinary(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	u := wsURL(ts, "tab:x", grant)
	conn, _, err := (&websocket.Dialer{}).Dial(u, http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)
	sink := fake.sinkFor("tab:x")
	if sink == nil {
		t.Fatal("no sink after attach")
	}
	jpeg := []byte{0xFF, 0xD8, 0xFF}
	sink.Frame(2, 3, jpeg)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if mt != websocket.BinaryMessage {
		t.Fatalf("message type = %d, want Binary", mt)
	}
	if len(data) < 8 {
		t.Fatalf("binary too short: %d", len(data))
	}
	w := binary.BigEndian.Uint32(data[0:4])
	h := binary.BigEndian.Uint32(data[4:8])
	if w != 2 || h != 3 {
		t.Fatalf("header w=%d h=%d want 2,3", w, h)
	}
	if string(data[8:]) != string(jpeg) {
		t.Fatalf("jpeg mismatch")
	}
}

func TestCDP_SinkConsoleNetwork(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, "tab:x", grant), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)
	sink := fake.sinkFor("tab:x")
	sink.Console(cdp.ConsoleEvent{Level: "log", Args: []string{"hi"}, TS: 123})
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read console: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	if got["t"] != "console" || got["level"] != "log" {
		t.Fatalf("console frame = %s", string(data))
	}
	sink.Network(cdp.NetworkEvent{Method: "GET", URL: "https://example.com/", Status: 200, DurationMs: 10, TS: 456})
	_, data, _ = conn.ReadMessage()
	_ = json.Unmarshal(data, &got)
	if got["t"] != "network" || got["method"] != "GET" {
		t.Fatalf("network frame = %s", string(data))
	}
}

func TestCDP_SinkErrorCloses1011(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, "tab:x", grant), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)
	sink := fake.sinkFor("tab:x")
	sink.Error("boom")
	// First message is error JSON
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read error json: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	if got["t"] != "error" || got["message"] != "boom" {
		t.Fatalf("error frame = %s", string(data))
	}
	// Next should be close 1011
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected close after error")
	}
	if ce, ok := err.(*websocket.CloseError); ok {
		if ce.Code != 1011 {
			t.Fatalf("close code = %d want 1011", ce.Code)
		}
	}
}

func TestCDP_ClientNavResizeMouseKey(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, "tab:x", grant), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)
	target := fake.targetFor("tab:x")
	if target == nil {
		t.Fatal("no target")
	}
	// nav
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"nav","url":"https://example.com/foo"}`))
	time.Sleep(50 * time.Millisecond)
	target.mu.Lock()
	if len(target.navigates) != 1 || target.navigates[0] != "https://example.com/foo" {
		t.Fatalf("navigates = %v", target.navigates)
	}
	target.mu.Unlock()
	// resize
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"resize","w":800,"h":600,"dpr":2}`))
	time.Sleep(50 * time.Millisecond)
	target.mu.Lock()
	if len(target.resizes) != 1 || target.resizes[0].w != 800 || target.resizes[0].h != 600 || target.resizes[0].dpr != 2 {
		t.Fatalf("resizes = %v", target.resizes)
	}
	target.mu.Unlock()
	// back/forward/reload
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"back"}`))
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"forward"}`))
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"reload"}`))
	time.Sleep(50 * time.Millisecond)
	target.mu.Lock()
	if target.backs != 1 || target.forwards != 1 || target.reloads != 1 {
		t.Fatalf("backs=%d forwards=%d reloads=%d", target.backs, target.forwards, target.reloads)
	}
	target.mu.Unlock()
	// mouse
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"mouse","kind":"down","x":10,"y":20,"button":"left","clickCount":1,"modifiers":2}`))
	time.Sleep(50 * time.Millisecond)
	target.mu.Lock()
	if len(target.mouses) != 1 || target.mouses[0].Kind != "down" || target.mouses[0].X != 10 {
		t.Fatalf("mouses = %+v", target.mouses)
	}
	target.mu.Unlock()
	// key
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"key","kind":"down","key":"a","code":"KeyA","text":"a","modifiers":0}`))
	time.Sleep(50 * time.Millisecond)
	target.mu.Lock()
	if len(target.keys) != 1 || target.keys[0].Key != "a" {
		t.Fatalf("keys = %+v", target.keys)
	}
	target.mu.Unlock()
}

func TestCDP_MalformedJSONIgnored(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, "tab:x", grant), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`not json`))
	time.Sleep(50 * time.Millisecond)
	// socket should still be open: send valid nav
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"nav","url":"https://example.com/ok"}`))
	time.Sleep(50 * time.Millisecond)
	target := fake.targetFor("tab:x")
	target.mu.Lock()
	if len(target.navigates) != 1 {
		t.Fatalf("after malformed, navigates = %v, want 1", target.navigates)
	}
	target.mu.Unlock()
}

func TestCDP_ClientCloseDetachesNotRevoke(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	grant := s.MintGrant("tab:x", "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, "tab:x", grant), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	target := fake.targetFor("tab:x")
	_ = conn.Close()
	time.Sleep(100 * time.Millisecond)
	target.mu.Lock()
	detached := target.detachCalled
	target.mu.Unlock()
	if !detached {
		t.Fatal("Detach not called on client close")
	}
	fake.mu.Lock()
	if len(fake.revoked) != 0 {
		t.Fatalf("Revoke called unexpectedly: %v", fake.revoked)
	}
	fake.mu.Unlock()
}

func TestCDP_SecondSocketReplacesFirst(t *testing.T) {
	s, _, ts := newBrowseWithFake(t, "http://example.com")
	grant1 := s.MintGrant("tab:x", "http://example.com")
	grant2 := s.MintGrant("tab:x", "http://example.com")
	dialer := websocket.Dialer{}
	c1, _, err := dialer.Dial(wsURL(ts, "tab:x", grant1), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial1: %v", err)
	}
	defer c1.Close()
	time.Sleep(50 * time.Millisecond)
	c2, _, err := dialer.Dial(wsURL(ts, "tab:x", grant2), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer c2.Close()
	// c1 should receive replaced error then close
	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := c1.ReadMessage()
	if err != nil {
		t.Fatalf("c1 read replaced error: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	if got["t"] != "error" || got["message"] != "replaced" {
		t.Fatalf("c1 replaced frame = %s", string(data))
	}
	_, _, err = c1.ReadMessage()
	if err == nil {
		t.Fatal("c1 should be closed after replaced")
	}
}

func TestCDP_AttachChromeNotFound(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	fake.attachErr = cdp.ErrChromeNotFound
	grant := s.MintGrant("tab:x", "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, "tab:x", grant), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	if got["t"] != "error" {
		t.Fatalf("want error t, got %s", string(data))
	}
	msg, _ := got["message"].(string)
	if !strings.Contains(msg, "chrome not found") {
		t.Fatalf("message = %q want contains chrome not found", msg)
	}
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected close after chrome not found")
	}
	if ce, ok := err.(*websocket.CloseError); ok && ce.Code != 1011 {
		t.Fatalf("close code %d want 1011", ce.Code)
	}
}

func TestCDP_AttachUnsupportedPlatform(t *testing.T) {
	s, fake, ts := newBrowseWithFake(t, "http://example.com")
	fake.attachErr = cdp.ErrUnsupportedPlatform
	grant := s.MintGrant("tab:x", "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, "tab:x", grant), http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	msg, _ := got["message"].(string)
	if msg != "Chrome mode is not supported on Windows yet" {
		t.Fatalf("message = %q want Chrome mode not supported", msg)
	}
}
