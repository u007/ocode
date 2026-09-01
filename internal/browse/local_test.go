package browse

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestHandleLocalTLSSelfSignedLoopback(t *testing.T) {
	// httptest.NewTLSServer uses a self-signed cert signed by an unknown
	// authority. A real dev server on https://localhost:3505 (vite --https,
	// mkcert, etc.) presents the same. handleLocal must succeed — its
	// transport is allowPrivate=true with InsecureSkipVerify.
	// Loopback hosts (localhost, 127.0.0.1, ::1) are auto-allowed via
	// isLoopbackHost → insecure transport, no explicit bypass needed.
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body>tls ok</body></html>")
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "https://")
	s, cookie := newLocalTestServer(t, "tab:tls")

	r := httptest.NewRequest("GET", "/b/tab:tls/https/"+host+"/", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("TLS self-signed loopback: status = %d, body = %q (expected 200 — loopback auto-allow)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "tls ok") {
		t.Fatalf("TLS self-signed loopback: body missing upstream content: %q", body)
	}
	// Note: we don't also assert that an external (allowPrivate=false)
	// transport rejects the same self-signed cert via a live fetch to this
	// loopback TLSServer, because the SSRF dial guard blocks 127.0.0.1
	// before TLS is even attempted (which is correct — external mode never
	// fetches private upstreams). The unit test TestSafeTransportTLSVerification
	// pins that external's TLSClientConfig still has verification enabled.
}

func TestHandleLocalTLSLANRequiresBypass(t *testing.T) {
	// LAN private hosts (192.168.x, 10.x) must NOT auto-allow self-signed;
	// they require an explicit “Continue anyway” via AllowBypass. This test
	// uses a real self-signed TLSServer but maps a fake LAN host to it via a
	// custom DialContext so the TLS handshake actually reaches the server while
	// the Server's host classification treats it as non-loopback.
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body>lan tls ok</body></html>")
	}))
	defer upstream.Close()
	upstreamHost := strings.TrimPrefix(upstream.URL, "https://") // 127.0.0.1:port
	_, port, _ := net.SplitHostPort(upstreamHost)
	lanHost := "192.168.0.99:" + port // non-loopback private literal

	s, cookie := newLocalTestServer(t, "tab:lan")
	// Install custom transports that map lanHost -> upstreamHost for dialing.
	// Both strict and insecure variants share the same mapping.
	customDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == lanHost {
			addr = upstreamHost
		}
		return newSafeDialer(true).DialContext(ctx, network, addr)
	}
	strict := &http.Transport{
		Proxy:                 nil,
		DialContext:           customDial,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// TLSClientConfig nil => strict verification
	}
	insecure := &http.Transport{
		Proxy:                 nil,
		DialContext:           customDial,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	}
	// Inject directly, bypassing Once.
	s.localTransportVal = strict
	s.localInsecureTransportVal = insecure
	s.localTransportOnce = sync.Once{}
	s.localTransportOnce.Do(func() {})
	s.localInsecureTransportOnce = sync.Once{}
	s.localInsecureTransportOnce.Do(func() {})

	var navEvents []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { navEvents = append(navEvents, ev) })

	// 1) Without bypass, LAN self-signed must fail with TLS error + 502.
	r1 := httptest.NewRequest("GET", "/b/tab:lan/https/"+lanHost+"/", nil)
	r1.Header.Set("Sec-Fetch-Dest", "document")
	r1.AddCookie(cookie)
	w1 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w1, r1)
	if w1.Code != http.StatusBadGateway {
		t.Fatalf("LAN without bypass: status = %d, want 502, body=%q", w1.Code, w1.Body.String())
	}
	if len(navEvents) == 0 {
		t.Fatal("LAN without bypass: no NavEvent emitted")
	}
	last := navEvents[len(navEvents)-1]
	if !strings.Contains(last.Error, "TLS certificate") {
		t.Fatalf("LAN without bypass: NavEvent.Error = %q, want to contain TLS certificate", last.Error)
	}
	if last.Status != http.StatusBadGateway {
		t.Fatalf("LAN without bypass: NavEvent.Status = %d, want 502", last.Status)
	}
	// Subresource to same LAN host without bypass should also fail, but not emit document nav.
	navEvents = nil
	// 2) After explicit bypass, same LAN host must succeed.
	s.AllowBypass("tab:lan", lanHost)
	if !s.isBypassed("tab:lan", lanHost) {
		t.Fatal("AllowBypass did not record")
	}
	r2 := httptest.NewRequest("GET", "/b/tab:lan/https/"+lanHost+"/", nil)
	r2.Header.Set("Sec-Fetch-Dest", "document")
	r2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	navEvents = nil
	s.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("LAN with bypass: status = %d, want 200, body=%q", w2.Code, w2.Body.String())
	}
	if body := w2.Body.String(); !strings.Contains(body, "lan tls ok") {
		t.Fatalf("LAN with bypass: body missing upstream content: %q", body)
	}
	// 3) Revoke must clear bypass (isBypassed false) — subsequent LAN fetch fails again.
	// Note: Revoke also clears the auth session, so we re-mint a fresh session for the final check
	// to isolate the bypass-clearing behavior from the auth-clearing.
	s.Revoke("tab:lan")
	if s.isBypassed("tab:lan", lanHost) {
		t.Fatal("Revoke should clear bypass")
	}
	// Re-create session for tab:lan after revoke to test that bypass is gone (not just auth).
	grant2 := s.MintGrant("tab:lan", "")
	cookieVal2, _, ok := s.auth.redeem(grant2, true)
	if !ok {
		t.Fatal("could not redeem grant after revoke")
	}
	cookie2 := &http.Cookie{Name: browseCookie, Value: cookieVal2, Path: "/b/"}
	// Re-install custom transports (Revoke doesn't clear them, but ensure they are still mapped).
	s.localTransportVal = strict
	s.localInsecureTransportVal = insecure
	r3 := httptest.NewRequest("GET", "/b/tab:lan/https/"+lanHost+"/", nil)
	r3.Header.Set("Sec-Fetch-Dest", "document")
	r3.AddCookie(cookie2)
	w3 := httptest.NewRecorder()
	navEvents = nil
	s.SetNavPublisher(func(_ string, ev NavEvent) { navEvents = append(navEvents, ev) })
	s.Handler().ServeHTTP(w3, r3)
	if w3.Code != http.StatusBadGateway {
		t.Fatalf("LAN after revoke: status = %d, want 502 (bypass cleared)", w3.Code)
	}
}

func TestBrowseBypassStore(t *testing.T) {
	s := New("apitoken", nil)
	if s.isBypassed("tab:x", "192.168.1.1:3000") {
		t.Fatal("isBypassed should be false initially")
	}
	s.AllowBypass("tab:x", "192.168.1.1:3000")
	if !s.isBypassed("tab:x", "192.168.1.1:3000") {
		t.Fatal("AllowBypass did not set")
	}
	// Different stateKey not affected.
	if s.isBypassed("tab:y", "192.168.1.1:3000") {
		t.Fatal("bypass leaked across stateKeys")
	}
	s.Revoke("tab:x")
	if s.isBypassed("tab:x", "192.168.1.1:3000") {
		t.Fatal("Revoke should clear bypass for that stateKey")
	}
}

func TestHandleLocalDevServerNoticeReplacesDocument(t *testing.T) {
	const devHTML = `<html><head><link rel="modulepreload" href="/@id/virtual:tanstack-start-dev-client-entry"/>` +
		`<script type="module" async="" src="/@id/virtual:tanstack-start-dev-client-entry"></script></head>` +
		`<body><div>the dev app shell</div></body></html>`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, devHTML)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:dev1")
	var got []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { got = append(got, ev) })

	r := httptest.NewRequest("GET", "/b/tab:dev1/http/"+host+"/admin", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "the dev app shell") {
		t.Fatalf("dev-server HTML was not replaced by the notice: %q", body)
	}
	if !strings.Contains(body, "Development server not supported") {
		t.Fatalf("notice text missing from body: %q", body)
	}
	if strings.Contains(body, "__ocode_capture.js") {
		t.Fatalf("notice must not inject the capture script: %q", body)
	}
	// Terminal nav event carries the error token the SPA matches on.
	foundErr := false
	for _, ev := range got {
		if ev.Mode == "local" && strings.HasPrefix(ev.Error, "dev-server-module-graph") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatalf("no error-tagged local nav event, got %+v", got)
	}
}

func TestHandleLocalDevServerMarkerSubresourceNotReplaced(t *testing.T) {
	// A TRUE subresource (script/image/…, not an iframe — iframes count as
	// document requests) with dev-server markers must keep the normal
	// rewrite+capture path, not be replaced by the notice: only document
	// loads get the notice, and only they drive the address bar.
	const devHTML = `<html><head><script src="/@id/virtual:tanstack-start-dev-client-entry"></script></head><body>subresource content</body></html>`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, devHTML)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:dev2")

	r := httptest.NewRequest("GET", "/b/tab:dev2/http/"+host+"/sub", nil)
	r.Header.Set("Sec-Fetch-Dest", "script")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "subresource content") {
		t.Fatalf("subresource HTML should pass through untouched: %q", body)
	}
	if !strings.Contains(body, "__ocode_capture.js") {
		t.Fatalf("subresource HTML should still get capture injection: %q", body)
	}
}

func TestHandleLocalNormalHTMLGetsNoNotice(t *testing.T) {
	// Pages without dev-server markers must never be replaced by the notice.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>plain page</body></html>")
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:dev3")

	r := httptest.NewRequest("GET", "/b/tab:dev3/http/"+host+"/", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	body := w.Body.String()
	if strings.Contains(body, "Development server not supported") {
		t.Fatalf("normal HTML wrongly replaced by notice: %q", body)
	}
	if !strings.Contains(body, "plain page") {
		t.Fatalf("normal HTML content lost: %q", body)
	}
	if !strings.Contains(body, "__ocode_capture.js") {
		t.Fatalf("normal HTML should get capture injection: %q", body)
	}
}

func TestHandleLocalMarkerAsTextDoesNotTriggerNotice(t *testing.T) {
	// Markers appearing in TEXT NODES (prose/code samples) must not trip the
	// detector — only tag attributes are significant.
	const prose = `<html><body><pre>Vite dev paths like /@fs/ and /@id/virtual: and /node_modules/.vite/ are documented here</pre></body></html>`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, prose)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:dev4")

	r := httptest.NewRequest("GET", "/b/tab:dev4/http/"+host+"/", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	body := w.Body.String()
	if strings.Contains(body, "Development server not supported") {
		t.Fatalf("marker as text wrongly triggered the notice: %q", body)
	}
	if !strings.Contains(body, "are documented here") {
		t.Fatalf("prose content lost: %q", body)
	}
	if !strings.Contains(body, "__ocode_capture.js") {
		t.Fatalf("normal HTML should still get capture injection: %q", body)
	}
}

func TestHandleLocalTerminalNavPreservesQuery(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>x</body></html>")
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:dev5")
	var navs []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { navs = append(navs, ev) })

	r := httptest.NewRequest("GET", "/b/tab:dev5/http/"+host+"/admin?foo=bar&baz=1", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	found := false
	for _, ev := range navs {
		if ev.Mode == "local" && ev.Status == http.StatusOK && strings.Contains(ev.URL, "?foo=bar&baz=1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("terminal nav event lost the query string, got %+v", navs)
	}
}
