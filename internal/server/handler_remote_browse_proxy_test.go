package server

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

type browseProxyTestWorkspace struct {
	fakeTestWorkspace
	browseURL string
}

func (w *browseProxyTestWorkspace) BrowseURL() string { return w.browseURL }

// TestRemoteBrowseProxyStripsPrefix verifies that the proxy strips the
// /api/remote/{host}/browse prefix before forwarding to the upstream.
func TestRemoteBrowseProxyStripsPrefix(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/b/tab:x/https/example.com/" {
			t.Errorf("upstream path = %q, want /b/tab:x/https/example.com/", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ws := &browseProxyTestWorkspace{
		fakeTestWorkspace: fakeTestWorkspace{state: remote.ServeState{BrowsePort: 1}},
		browseURL:         upstream.URL,
	}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	connectTestHost(t, h, "user@realhost", "/home/user/project")
	s := &Server{handler: h, rl: newRateLimiter()}

	// Create a second listener for the proxy
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer proxyLn.Close()
	proxyPort := proxyLn.Addr().(*net.TCPAddr).Port
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/api/remote/{host}/browse/{rest...}", s.browseProxyGate(func(w http.ResponseWriter, r *http.Request) {
		s.handleRemoteBrowseProxy(w, r, r.PathValue("host"))
	}))
	proxyHS := &http.Server{Handler: proxyMux}
	go func() { _ = proxyHS.Serve(proxyLn) }()
	defer proxyHS.Close()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/remote/user@realhost/browse/b/tab:x/https/example.com/?__grant=abc", proxyPort)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body %q", resp.StatusCode, resp.Body)
	}
}

// TestRemoteBrowseProxyCookiePathRewrite verifies Set-Cookie Path rewriting.
func TestRemoteBrowseProxyCookiePathRewrite(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "ocode_browse=abc123; Path=/b/; HttpOnly")
		w.Header().Add("Set-Cookie", "other=val; Path=/")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ws := &browseProxyTestWorkspace{
		fakeTestWorkspace: fakeTestWorkspace{state: remote.ServeState{BrowsePort: 1}},
		browseURL:         upstream.URL,
	}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	connectTestHost(t, h, "user@realhost", "/home/user/project")
	s := &Server{handler: h, rl: newRateLimiter()}

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer proxyLn.Close()
	proxyPort := proxyLn.Addr().(*net.TCPAddr).Port
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/api/remote/{host}/browse/{rest...}", s.browseProxyGate(func(w http.ResponseWriter, r *http.Request) {
		s.handleRemoteBrowseProxy(w, r, r.PathValue("host"))
	}))
	proxyHS := &http.Server{Handler: proxyMux}
	go func() { _ = proxyHS.Serve(proxyLn) }()
	defer proxyHS.Close()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/remote/user@realhost/browse/b/tab:x/https/example.com/?__grant=abc", proxyPort)
	req, _ := http.NewRequest("GET", url, nil)
	req.AddCookie(&http.Cookie{Name: "ocode_browse", Value: "sess"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	cookies := resp.Header.Values("Set-Cookie")
	found := false
	for _, c := range cookies {
		if strings.Contains(c, "ocode_browse") {
			if !strings.Contains(c, "Path=/api/remote/user@realhost/browse/b/") {
				t.Errorf("cookie path not rewritten: %q", c)
			}
			found = true
		}
	}
	if !found {
		t.Error("ocode_browse cookie not found in response")
	}
}

// TestRemoteBrowseProxyUnknownHost verifies host validation.
func TestRemoteBrowseProxyUnknownHost(t *testing.T) {
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", &browseProxyTestWorkspace{})
	s := &Server{handler: h, rl: newRateLimiter()}

	r := httptest.NewRequest("GET", "/api/remote/evil@elsewhere/browse/b/tab:x/https/example.com/", nil)
	w := httptest.NewRecorder()
	s.handleRemoteBrowseProxy(w, r, "evil@elsewhere")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body %q, want 403", w.Code, w.Body.String())
	}
}

// TestRemoteBrowseProxyNoBrowseOrigin verifies the no-origin error.
func TestRemoteBrowseProxyNoBrowseOrigin(t *testing.T) {
	ws := &browseProxyTestWorkspace{
		fakeTestWorkspace: fakeTestWorkspace{state: remote.ServeState{BrowsePort: 0}},
		browseURL:         "",
	}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	connectTestHost(t, h, "user@realhost", "/home/user/project")
	s := &Server{handler: h, rl: newRateLimiter()}

	r := httptest.NewRequest("GET", "/api/remote/user@realhost/browse/b/tab:x/https/example.com/", nil)
	w := httptest.NewRecorder()
	s.handleRemoteBrowseProxy(w, r, "user@realhost")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body %q, want 502", w.Code, w.Body.String())
	}
}

// TestRemoteBrowseProxyPreservesUpstreamAuth verifies that an upstream 401
// (no valid browse grant/cookie) is passed through unchanged.
func TestRemoteBrowseProxyPreservesUpstreamAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "browse: unauthorized", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	ws := &browseProxyTestWorkspace{
		fakeTestWorkspace: fakeTestWorkspace{state: remote.ServeState{BrowsePort: 1}},
		browseURL:         upstream.URL,
	}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	connectTestHost(t, h, "user@realhost", "/home/user/project")
	s := &Server{handler: h, rl: newRateLimiter()}

	r := httptest.NewRequest("GET", "/api/remote/user@realhost/browse/b/tab:x/https/example.com/", nil)
	w := httptest.NewRecorder()
	s.handleRemoteBrowseProxy(w, r, "user@realhost")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body %q, want 401 passthrough", w.Code, w.Body.String())
	}
}

// connectTestHost brings the host up the way the authenticated
// /api/browse/config call does; the unauthenticated proxy only reuses it.
func connectTestHost(t *testing.T, h *Handler, host, path string) {
	t.Helper()
	if _, err := h.remoteHosts.workspaceForPort(host, path, 0); err != nil {
		t.Fatalf("connect %s: %v", host, err)
	}
}

// The proxy route is outside authMiddleware, so it must refuse to dial an
// unconnected host rather than start an SSH connect for any caller.
func TestRemoteBrowseProxyDoesNotConnectUnconnectedHost(t *testing.T) {
	dialed := false
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", &browseProxyTestWorkspace{})
	h.remoteHosts = newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		dialed = true
		return &browseProxyTestWorkspace{}, nil
	})
	s := &Server{handler: h, rl: newRateLimiter()}

	r := httptest.NewRequest("GET", "/api/remote/user@realhost/browse/b/tab:x/https/example.com/?__grant=forged", nil)
	w := httptest.NewRecorder()
	s.handleRemoteBrowseProxy(w, r, "user@realhost")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if dialed {
		t.Fatal("unauthenticated proxy request triggered a remote connect")
	}
}

// A request carrying a forged grant passes the structural pre-filter but is
// rejected downstream; that 401 must count toward the per-IP lockout.
func TestBrowseProxyGateCountsDownstreamAuthFailures(t *testing.T) {
	s := &Server{rl: newRateLimiter()}
	gate := s.browseProxyGate(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	var last int
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest("GET", "/b/x?__grant=forged", nil)
		r.RemoteAddr = "203.0.113.9:1234"
		w := httptest.NewRecorder()
		gate(w, r)
		last = w.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after repeated forged grants status = %d, want 429", last)
	}
}
