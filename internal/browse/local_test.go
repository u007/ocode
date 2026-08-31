package browse

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newLocalTestServer returns a browse Server whose auth store already has a
// live session for stateKey, so requests can carry the cookie directly.
func newLocalTestServer(t *testing.T, stateKey string) (*Server, *http.Cookie) {
	t.Helper()
	s := New("apitoken", nil)
	grant := s.MintGrant(stateKey, "")
	// Local-mode tests exercise private-loopback upstreams; mark the session
	// local so the server-authoritative gate admits them.
	cookieVal, _, ok := s.auth.redeem(grant, true)
	if !ok {
		t.Fatal("could not redeem grant in test setup")
	}
	return s, &http.Cookie{Name: browseCookie, Value: cookieVal, Path: "/b/"}
}

func TestHandleLocalInjectsCaptureIntoHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><head><title>dev</title></head><body>hi</body></html>")
	}))
	defer upstream.Close()

	// upstream.Listener.Addr() is 127.0.0.1:PORT — a private literal, so t.Local.
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:local1")

	r := httptest.NewRequest("GET", "/b/tab:local1/http/"+host+"/", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "__ocode_capture.js") {
		t.Fatalf("capture script not injected into HTML: %q", body)
	}
	if !strings.Contains(body, "<title>dev</title>") {
		t.Fatalf("original HTML content lost: %q", body)
	}
}

func TestHandleLocalStreamsNonHTMLUnmodified(t *testing.T) {
	const js = "console.log('vite');\n// @vite/client HMR payload\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = io.WriteString(w, js)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:local2")

	r := httptest.NewRequest("GET", "/b/tab:local2/http/"+host+"/@vite/client", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if got := w.Body.String(); got != js {
		t.Fatalf("non-HTML body mutated:\n got %q\nwant %q", got, js)
	}
}

func TestHandleLocalBlocksServiceWorker(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = io.WriteString(w, "self.addEventListener('install',()=>{})")
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:sw")

	r := httptest.NewRequest("GET", "/b/tab:sw/http/"+host+"/sw.js", nil)
	r.Header.Set("Sec-Fetch-Dest", "serviceworker")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("service worker request: got %d want 403", w.Code)
	}
}

func TestHandleLocalEmitsLocalNavEvent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><head></head><body>x</body></html>")
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:nav")

	var got []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { got = append(got, ev) })

	r := httptest.NewRequest("GET", "/b/tab:nav/http/"+host+"/", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.AddCookie(cookie)
	s.Handler().ServeHTTP(httptest.NewRecorder(), r)

	if len(got) == 0 {
		t.Fatal("no NavEvent emitted for local document")
	}
	if got[0].Mode != "local" {
		t.Fatalf("NavEvent.Mode = %q want local", got[0].Mode)
	}
	if !strings.Contains(got[0].URL, host) {
		t.Fatalf("NavEvent.URL = %q missing upstream host", got[0].URL)
	}
}

func TestHandleLocalWebSocketPassthrough(t *testing.T) {
	// Upstream that completes a WebSocket-style 101 upgrade and echoes one line.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "expected upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("upstream ResponseWriter is not a Hijacker")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = buf.Flush()
		line, _ := buf.ReadString('\n')
		_, _ = buf.WriteString("echo:" + line)
		_ = buf.Flush()
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:ws")

	// Serve the browse server on a real listener so we can dial a raw socket.
	bln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bln.Close()
	go func() {
		if err := http.Serve(bln, s.Handler()); err != nil {
			t.Logf("browse test server closed: %v", err) // intentionally benign: test teardown
		}
	}()

	conn, err := net.Dial("tcp", bln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req := "GET /b/tab:ws/http/" + host + "/ws HTTP/1.1\r\n" +
		"Host: " + bln.Addr().String() + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n" +
		"Cookie: " + cookie.Name + "=" + cookie.Value + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	statusLine, _ := br.ReadString('\n')
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101 upgrade, got %q", statusLine)
	}
	// Drain headers.
	for {
		line, _ := br.ReadString('\n')
		if line == "\r\n" || line == "" {
			break
		}
	}
	_, _ = conn.Write([]byte("ping\n"))
	echo, _ := br.ReadString('\n')
	if !strings.HasPrefix(echo, "echo:ping") {
		t.Fatalf("websocket echo failed: got %q", echo)
	}
}

// TestHandleLocalRewritesHTMLURLs pins the live-QA fix: dev servers reference
// assets root-relative ("/pic.svg"); without the static rewrite those resolve
// against the browse origin root and 404 out of the stateless route. Local
// HTML must therefore go through rewriteHTML as well as injectCapture.
func TestHandleLocalRewritesHTMLURLs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<html><head><title>dev</title></head><body><img src="/pic.svg"><a href="/next">n</a></body></html>`)
		default:
			_, _ = io.WriteString(w, "asset")
		}
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:rw")

	r := httptest.NewRequest("GET", "/b/tab:rw/http/"+host+"/", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	body := w.Body.String()
	for _, want := range []string{"/b/tab:rw/http/" + host + "/pic.svg", "/b/tab:rw/http/" + host + "/next"} {
		if !strings.Contains(body, want) {
			t.Errorf("root-relative URL not rewritten to %q in:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "__ocode_capture.js") {
		t.Errorf("capture script missing after rewrite:\n%s", body)
	}
}

// TestHandleLocalUpgradeEmitsNoNavEvent pins the second live-QA fix: Chromium
// may omit Sec-Fetch-Dest on the WebSocket handshake, and a missing header
// used to be treated as a document navigation — the resulting nav event would
// hijack the address bar (and iframe) onto the ws route. Upgrades must never
// emit nav events.
func TestHandleLocalUpgradeEmitsNoNavEvent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Plain (non-upgrading) response to a handshake attempt: no nav events.
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "no upgrade")
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:up")

	var got []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { got = append(got, ev) })

	r := httptest.NewRequest("GET", "/b/tab:up/http/"+host+"/ws", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d want 200", w.Code)
	}
	if len(got) != 0 {
		t.Fatalf("upgrade request emitted %d nav events: %+v", len(got), got)
	}
}

func TestHandleLocalRedirectToExternalHandsOff(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://accounts.example.com/x", http.StatusFound)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:hand1")
	var got []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { got = append(got, ev) })
	r := httptest.NewRequest("GET", "/b/tab:hand1/http/"+host+"/", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d want 204, body=%q headers=%v", w.Code, w.Body.String(), w.Header())
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("Location header should be stripped, got %q", loc)
	}
	found := false
	for _, ev := range got {
		if ev.Mode == "chrome" && ev.URL == "https://accounts.example.com/x" && ev.Status == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no chrome nav event for external redirect, got %+v", got)
	}
}

func TestHandleLocalRedirectToRelativePasses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/relative", http.StatusFound)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:hand2")
	r := httptest.NewRequest("GET", "/b/tab:hand2/http/"+host+"/", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("relative redirect should pass through as 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc == "" {
		t.Fatalf("Location should be preserved for private redirect")
	}
}

func TestHandleLocalRedirectSubresourcePasses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://accounts.example.com/x", http.StatusFound)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:hand3")
	r := httptest.NewRequest("GET", "/b/tab:hand3/http/"+host+"/sub.js", nil)
	r.Header.Set("Sec-Fetch-Dest", "script")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("subresource redirect should pass through, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc == "" {
		t.Fatalf("Location should be preserved for subresource")
	}
}
