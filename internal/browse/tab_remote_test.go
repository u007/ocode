package browse

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fwdRootPage is an app page with root-relative links, a root-relative script,
// and one absolute link to the foreign upstream.
const fwdRootPage = `<!doctype html><html><head><title>app</title></head><body><a href="/next">n</a><a href="%s">f</a><script type="module" src="/src/main.js"></script></body></html>`

// fwdNoRedirect is a client that returns redirects as replies, so a test sees the
// Location and Set-Cookie of each step.
func fwdNoRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// fwdUpstream serves fwdRootPage at /app/dashboard, with a foreign link to foreignHost.
func fwdUpstream(t *testing.T, foreignHost string) string {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/dashboard" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, fwdRootPage, "http://"+foreignHost+"/x")
	}))
	t.Cleanup(up.Close)
	return strings.TrimPrefix(up.URL, "http://")
}

// fwdForeign serves a plain body on loopback, as a second upstream.
func fwdForeign(t *testing.T) string {
	t.Helper()
	f := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "pong from foreign "+r.URL.Path)
	}))
	t.Cleanup(f.Close)
	return strings.TrimPrefix(f.URL, "http://")
}

// fwdRemote is a browse origin standing in for a remote host's, served over HTTP.
func fwdRemote(t *testing.T) (*Server, string) {
	t.Helper()
	rb := New("remote-token", nil)
	t.Cleanup(func() { _ = rb.Close(context.Background()) })
	srv := httptest.NewServer(rb.Handler())
	t.Cleanup(srv.Close)
	return rb, srv.URL
}

// fwdCookie returns the Set-Cookie of name from resp, or fails.
func fwdCookie(t *testing.T, resp *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no Set-Cookie %q in reply (headers %v)", name, resp.Header)
	return nil
}

// fwdRedeem sends the grant to the tab origin at base and returns the per-tab
// session cookie the reply set, after checking the reply is the root redirect.
func fwdRedeem(t *testing.T, base, path, grant, stateKey string) *http.Cookie {
	t.Helper()
	resp, err := fwdNoRedirect().Get(base + path + "?__grant=" + grant)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redeem status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != path {
		t.Fatalf("redeem Location = %q, want root path %q", loc, path)
	}
	c := fwdCookie(t, resp, tabCookieName(stateKey))
	if c.Path != "/" {
		t.Fatalf("session cookie Path = %q, want /", c.Path)
	}
	return c
}

// TestRootModeHostServesRootMountedPage: a host that receives a root-mode request
// answers as a ListenTab origin would. Same-upstream links are plain paths, foreign
// links take the reserved route, the redirect is root-relative, the capture
// bootstrap carries the binding, and every reply carries the marker.
func TestRootModeHostServesRootMountedPage(t *testing.T) {
	foreignHost := fwdForeign(t)
	upHost := fwdUpstream(t, foreignHost)
	rb, _ := fwdRemote(t)
	const sk = "tab:root"
	grant := rb.MintGrant(sk, "http://localhost:3510")

	redeem := httptest.NewRequest(http.MethodGet, "/b/"+sk+"/http/"+upHost+"/app/dashboard?__grant="+grant+"&view=1", nil)
	redeem.Header.Set(RootModeHeader, "http://"+upHost)
	w := httptest.NewRecorder()
	rb.Handler().ServeHTTP(w, redeem)
	if w.Code != http.StatusFound {
		t.Fatalf("redeem status = %d body %q, want 302", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/app/dashboard?view=1" {
		t.Fatalf("redeem Location = %q, want root path with the grant removed", loc)
	}
	if w.Header().Get(RootModeHeader) != RootModeMarker {
		t.Fatalf("redirect lacks the root-mode marker")
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == tabCookieName(sk) {
			cookie = c
		}
	}
	if cookie == nil || cookie.Path != "/" {
		t.Fatalf("session cookie = %+v, want %q at Path=/", cookie, tabCookieName(sk))
	}

	doc := httptest.NewRequest(http.MethodGet, "/b/"+sk+"/http/"+upHost+"/app/dashboard", nil)
	doc.Header.Set(RootModeHeader, "http://"+upHost)
	doc.AddCookie(cookie)
	w = httptest.NewRecorder()
	rb.Handler().ServeHTTP(w, doc)
	if w.Code != http.StatusOK {
		t.Fatalf("document status = %d body %q", w.Code, w.Body.String())
	}
	if w.Header().Get(RootModeHeader) != RootModeMarker {
		t.Fatalf("document lacks the root-mode marker")
	}
	body := w.Body.String()
	for _, want := range []string{
		`rootMode:true`,
		`src="/__ocode_capture.js"`,
		`href="/next"`,
		`src="/src/main.js"`,
		`href="/__ocode/b/http/` + foreignHost + `/x"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("root-mode document lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/b/"+sk) {
		t.Errorf("root-mode document carries the prefix route:\n%s", body)
	}
}

// TestRootModeMarkerOnEveryReply: a refused request still answers with the marker,
// so the sender can tell root-mode refusals from a host that ignored the request.
func TestRootModeMarkerOnEveryReply(t *testing.T) {
	rb, _ := fwdRemote(t)
	unauth := httptest.NewRequest(http.MethodGet, "/b/tab:m/http/127.0.0.1:1/", nil)
	unauth.Header.Set(RootModeHeader, "http://127.0.0.1:1")
	w := httptest.NewRecorder()
	rb.Handler().ServeHTTP(w, unauth)
	if w.Code != http.StatusUnauthorized || w.Header().Get(RootModeHeader) != RootModeMarker {
		t.Fatalf("unauthorized: status %d marker %q", w.Code, w.Header().Get(RootModeHeader))
	}

	badBinding := httptest.NewRequest(http.MethodGet, "/b/tab:m/http/127.0.0.1:1/", nil)
	badBinding.Header.Set(RootModeHeader, "http://127.0.0.1:1/some/path")
	w = httptest.NewRecorder()
	rb.Handler().ServeHTTP(w, badBinding)
	if w.Code != http.StatusBadRequest || w.Header().Get(RootModeHeader) != RootModeMarker {
		t.Fatalf("bad binding: status %d marker %q", w.Code, w.Header().Get(RootModeHeader))
	}
}

// TestRootModeBindingIsNotThePath: the binding is the header, so a foreign route on
// the host's /b/ path is not mistaken for the binding. The grant is redeemed on the
// binding (a grant on a foreign route is refused), then the foreign path is read.
func TestRootModeBindingIsNotThePath(t *testing.T) {
	foreignHost := fwdForeign(t)
	upHost := fwdUpstream(t, foreignHost)
	rb, _ := fwdRemote(t)
	const sk = "tab:bind"
	grant := rb.MintGrant(sk, "http://localhost:3510")
	redeem := httptest.NewRequest(http.MethodGet, "/b/"+sk+"/http/"+upHost+"/app/dashboard?__grant="+grant, nil)
	redeem.Header.Set(RootModeHeader, "http://"+upHost)
	w := httptest.NewRecorder()
	rb.Handler().ServeHTTP(w, redeem)
	cookie := fwdCookie(t, w.Result(), tabCookieName(sk))

	foreignReq := httptest.NewRequest(http.MethodGet, "/b/"+sk+"/http/"+foreignHost+"/api/ping", nil)
	foreignReq.Header.Set(RootModeHeader, "http://"+upHost)
	foreignReq.Header.Set("Sec-Fetch-Dest", "empty")
	foreignReq.AddCookie(cookie)
	w = httptest.NewRecorder()
	rb.Handler().ServeHTTP(w, foreignReq)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pong from foreign /api/ping") {
		t.Fatalf("foreign upstream through the host: status %d body %q", w.Code, w.Body.String())
	}
}

// TestForwardTabServesRootMountedThroughTunnel: a forwarded tab redeems the grant
// on the host and serves the host's root-mounted page at the tab's own root.
func TestForwardTabServesRootMountedThroughTunnel(t *testing.T) {
	foreignHost := fwdForeign(t)
	upHost := fwdUpstream(t, foreignHost)
	rb, remoteURL := fwdRemote(t)
	s := New("local-token", nil)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tunnel := func(w http.ResponseWriter, r *http.Request) (string, bool) { return remoteURL, true }
	_, base, err := s.ListenForwardTab("proj.example", "tab:fwd", "http", upHost, "127.0.0.1:0", nil, tunnel)
	if err != nil {
		t.Fatalf("listen forward tab: %v", err)
	}
	grant := rb.MintGrant("tab:fwd", base)
	cookie := fwdRedeem(t, base, "/app/dashboard", grant, "tab:fwd")

	req, _ := http.NewRequest(http.MethodGet, base+"/app/dashboard", nil)
	req.AddCookie(cookie)
	resp, err := fwdNoRedirect().Do(req)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("document status = %d body %q", resp.StatusCode, body)
	}
	if resp.Header.Get(RootModeHeader) != RootModeMarker {
		t.Fatalf("forwarded reply lacks the marker")
	}
	if !strings.Contains(string(body), `href="/next"`) || !strings.Contains(string(body), "rootMode:true") {
		t.Fatalf("forwarded document is not root-mounted:\n%s", body)
	}
	if !strings.Contains(string(body), `href="/__ocode/b/http/`+foreignHost+`/x"`) {
		t.Fatalf("foreign link not on the reserved route:\n%s", body)
	}
}

// TestForwardTabMissingMarkerFailsClosed: a reply without the marker is an older
// host that ignored root mode. Its content is never relayed.
func TestForwardTabMissingMarkerFailsClosed(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>PREFIX-SHAPED-CONTENT</html>")
	}))
	t.Cleanup(old.Close)
	s := New("local-token", nil)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tunnel := func(w http.ResponseWriter, r *http.Request) (string, bool) { return old.URL, true }
	_, base, err := s.ListenForwardTab("proj.example", "tab:old", "http", "app.example:3000", "127.0.0.1:0", nil, tunnel)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	resp, err := fwdNoRedirect().Get(base + "/app/dashboard")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d body %q, want 502", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "too old") {
		t.Fatalf("502 body %q does not name the cause", body)
	}
	if strings.Contains(string(body), "PREFIX-SHAPED") {
		t.Fatalf("content of a host without the marker was relayed: %q", body)
	}
}

// TestForwardTabTunnelRefusalWritesNoForward: when the tunnel cannot resolve, the
// resolver's own response is what the client gets, and nothing is forwarded.
func TestForwardTabTunnelRefusalWritesNoForward(t *testing.T) {
	s := New("local-token", nil)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tunnel := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "remote connecting", http.StatusServiceUnavailable)
		return "", false
	}
	_, base, err := s.ListenForwardTab("proj.example", "tab:busy", "http", "app.example:3000", "127.0.0.1:0", nil, tunnel)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	resp, err := fwdNoRedirect().Get(base + "/app/dashboard")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") != "2" {
		t.Fatalf("status = %d Retry-After %q, want 503 with Retry-After", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

// TestForwardTabSendsOnlyTheTabCookie: the browser's other cookies belong to the
// SPA's origin and never reach the host.
func TestForwardTabSendsOnlyTheTabCookie(t *testing.T) {
	const sk = "tab:cookie"
	var seen string
	rec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Cookie")
		w.Header().Set(RootModeHeader, RootModeMarker)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(rec.Close)
	s := New("local-token", nil)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tunnel := func(w http.ResponseWriter, r *http.Request) (string, bool) { return rec.URL, true }
	_, base, err := s.ListenForwardTab("proj.example", sk, "http", "app.example:3000", "127.0.0.1:0", nil, tunnel)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/api/data", nil)
	req.AddCookie(&http.Cookie{Name: "ocode_session", Value: "spa-secret"})
	req.AddCookie(&http.Cookie{Name: tabCookieName(sk), Value: "tab-session"})
	resp, err := fwdNoRedirect().Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if seen != tabCookieName(sk)+"=tab-session" {
		t.Fatalf("host received Cookie %q, want only the tab session", seen)
	}
}

// TestForwardTabRefusesGrantOnForeignRoute: a grant on the reserved route is refused
// before any tunnel is resolved.
func TestForwardTabRefusesGrantOnForeignRoute(t *testing.T) {
	s := New("local-token", nil)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tunnel := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		t.Errorf("tunnel resolved for a refused grant on %s", r.URL.Path)
		return "", false
	}
	_, base, err := s.ListenForwardTab("proj.example", "tab:g", "http", "app.example:3000", "127.0.0.1:0", nil, tunnel)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	resp, err := fwdNoRedirect().Get(base + "/__ocode/b/http/other.example/x?__grant=anything")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestForwardTabForeignRouteThroughTunnel: a request on the reserved route reaches
// the foreign upstream through the host, with the foreign host and path.
func TestForwardTabForeignRouteThroughTunnel(t *testing.T) {
	foreignHost := fwdForeign(t)
	upHost := fwdUpstream(t, foreignHost)
	rb, remoteURL := fwdRemote(t)
	s := New("local-token", nil)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tunnel := func(w http.ResponseWriter, r *http.Request) (string, bool) { return remoteURL, true }
	_, base, err := s.ListenForwardTab("proj.example", "tab:foreign", "http", upHost, "127.0.0.1:0", nil, tunnel)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grant := rb.MintGrant("tab:foreign", base)
	cookie := fwdRedeem(t, base, "/app/dashboard", grant, "tab:foreign")

	req, _ := http.NewRequest(http.MethodGet, base+"/__ocode/b/http/"+foreignHost+"/api/ping", nil)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.AddCookie(cookie)
	resp, err := fwdNoRedirect().Do(req)
	if err != nil {
		t.Fatalf("foreign: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "pong from foreign /api/ping" {
		t.Fatalf("foreign through tunnel: status %d body %q", resp.StatusCode, body)
	}
}

// TestForwardTabWebSocketSubprotocolPassesThrough: the 101 reply reaches the browser
// with the subprotocol the upstream selected, and nothing added.
func TestForwardTabWebSocketSubprotocolPassesThrough(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.NotFound(w, r)
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Protocol: vite-hmr\r\n\r\n")
		_ = buf.Flush()
	}))
	t.Cleanup(up.Close)
	upHost := strings.TrimPrefix(up.URL, "http://")
	rb, remoteURL := fwdRemote(t)
	s := New("local-token", nil)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	tunnel := func(w http.ResponseWriter, r *http.Request) (string, bool) { return remoteURL, true }
	_, base, err := s.ListenForwardTab("proj.example", "tab:ws", "http", upHost, "127.0.0.1:0", nil, tunnel)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grant := rb.MintGrant("tab:ws", base)
	cookie := fwdRedeem(t, base, "/hmr", grant, "tab:ws")

	addr := strings.TrimPrefix(base, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	req := "GET /hmr HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Protocol: vite-hmr, other\r\nCookie: " + cookie.Name + "=" + cookie.Value + "\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read upgrade reply: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	if got := resp.Header.Get("Sec-WebSocket-Protocol"); got != "vite-hmr" {
		t.Fatalf("Sec-WebSocket-Protocol = %q, want the upstream's selection vite-hmr", got)
	}
}
