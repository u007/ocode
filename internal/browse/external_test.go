package browse

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer builds a browse Server whose transport allows private IPs so
// httptest upstreams (127.0.0.1) are reachable in tests. Production uses
// allowPrivate=false for external mode.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	s := New("apitoken", nil)
	s.transport = newSafeTransport(true) // test-only override
	return s
}

func TestExternalStripsSecurityHeadersAndEmitsNav(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>hi</body></html>")
	}))
	defer upstream.Close()

	s := newTestServer(t)
	var gotNav NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { gotNav = ev })

	// Upstream host without scheme prefix — parseTarget builds the path form.
	host := strings.TrimPrefix(upstream.URL, "http://")
	req := httptest.NewRequest("GET", "/b/tab:x/http/"+host+"/", nil)
	w := httptest.NewRecorder()
	tgt := target{StateKey: "tab:x", Scheme: "http", Host: host, Path: "/", Local: true}
	// External path is exercised directly (dispatch tested separately); force
	// external by clearing Local for this assertion of external behavior:
	tgt.Local = false
	s.handleExternal(w, req, tgt)

	res := w.Result()
	if res.Header.Get("X-Frame-Options") != "" || res.Header.Get("Content-Security-Policy") != "" {
		t.Errorf("security headers not stripped: %+v", res.Header)
	}
	if gotNav.Mode != "proxied" || gotNav.Status != 200 {
		t.Errorf("nav event = %+v, want proxied/200", gotNav)
	}
}

func TestExternalBlocksServiceWorker(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream must not be hit for a service-worker request")
	}))
	defer upstream.Close()

	s := newTestServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	req := httptest.NewRequest("GET", "/b/tab:x/http/"+host+"/sw.js", nil)
	req.Header.Set("Sec-Fetch-Dest", "serviceworker")
	w := httptest.NewRecorder()
	s.handleExternal(w, req, target{StateKey: "tab:x", Scheme: "http", Host: host, Path: "/sw.js"})
	if w.Code != http.StatusForbidden {
		t.Errorf("SW request status = %d, want 403", w.Code)
	}
}

func TestExternalStreamsNonHTMLUnrewritten(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	s := newTestServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	req := httptest.NewRequest("GET", "/b/tab:x/http/"+host+"/api", nil)
	w := httptest.NewRecorder()
	s.handleExternal(w, req, target{StateKey: "tab:x", Scheme: "http", Host: host, Path: "/api"})
	if got := w.Body.String(); got != `{"ok":true}` {
		t.Errorf("body = %q, want passthrough JSON", got)
	}
}

// TestExternalCookiesStayInServerJar pins the spec invariant (§ External mode,
// Cookies): upstream Set-Cookie is absorbed into the server-side jar and is
// NEVER re-emitted to the browser; the next request for the same
// (stateKey, origin) carries the cookie upstream from the jar.
func TestExternalCookiesStayInServerJar(t *testing.T) {
	var gotUpstreamCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Add("Set-Cookie", "session=XYZ; Path=/")
		} else {
			gotUpstreamCookie = r.Header.Get("Cookie")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()

	s := newTestServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	tgt := target{StateKey: "tab:jar", Scheme: "http", Host: host, Path: "/login"}

	w1 := httptest.NewRecorder()
	s.handleExternal(w1, httptest.NewRequest("GET", "/b/tab:jar/http/"+host+"/login", nil), tgt)
	for _, c := range w1.Result().Cookies() {
		if c.Name == "session" {
			t.Errorf("upstream cookie leaked to browser: %v", c)
		}
	}

	tgt.Path = "/page"
	w2 := httptest.NewRecorder()
	s.handleExternal(w2, httptest.NewRequest("GET", "/b/tab:jar/http/"+host+"/page", nil), tgt)
	if gotUpstreamCookie != "session=XYZ" {
		t.Errorf("upstream saw Cookie %q, want session=XYZ (jar apply)", gotUpstreamCookie)
	}
}

// TestExternalSSRFBlockedClassifiesNav uses the PRODUCTION default transport
// (allowPrivate=false, set by New) to prove the dialer guard trips on a
// loopback upstream and the failure nav error classifies via
// errors.Is(err, errPrivateAddr) — not a fragile substring of the wrapped text.
func TestExternalSSRFBlockedClassifiesNav(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream must not be reachable through the guarded transport")
	}))
	defer upstream.Close()

	s := New("apitoken", nil) // default transport: private IPs blocked
	var gotNav NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { gotNav = ev })

	host := strings.TrimPrefix(upstream.URL, "http://")
	w := httptest.NewRecorder()
	s.handleExternal(w, httptest.NewRequest("GET", "/b/tab:x/http/"+host+"/", nil),
		target{StateKey: "tab:x", Scheme: "http", Host: host, Path: "/"})

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
	if gotNav.Error != "blocked: private address" {
		t.Errorf("nav error = %q, want %q", gotNav.Error, "blocked: private address")
	}
}
