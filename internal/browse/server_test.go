package browse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// navAndServe authenticates via a minted grant and returns a request context
// carrying the resulting session cookie, mimicking the SPA's first navigation
// followed by ordinary browsing traffic.
func navAndServe(t *testing.T, s *Server, stateKey, path string) *httptest.ResponseRecorder {
	t.Helper()
	grant := s.MintGrant(stateKey)
	r := httptest.NewRequest("GET", path+"?__grant="+grant, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("grant nav to %s: got %d want 302", path, w.Code)
	}
	var cookie string
	for _, c := range w.Result().Cookies() {
		if c.Name == browseCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie after grant redeem")
	}
	r2 := httptest.NewRequest("GET", path, nil)
	r2.AddCookie(&http.Cookie{Name: browseCookie, Value: cookie})
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, r2)
	return w2
}

// TestAuthenticatedNavigationProxiesExternal replaces the Part-01 stub
// assertion: an authenticated navigation must reach handleBrowse's real
// dispatch. The loopback httptest upstream parses as Local, so this exercises
// the Part-06 local branch end to end through the mux: transparent streaming
// (body intact, upstream headers NOT surgically stripped — that is external
// mode's contract, covered by external_test.go against handleExternal with an
// allowPrivate transport override).
func TestAuthenticatedNavigationProxiesExternal(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/foo" {
			t.Errorf("upstream saw path %q, want /foo", r.URL.Path)
		}
		// Local mode must never forward the browse session cookie upstream.
		if c := r.Header.Get("Cookie"); strings.Contains(c, browseCookie) {
			t.Errorf("browse session cookie leaked upstream: %q", c)
		}
		w.Header().Set("X-Frame-Options", "DENY") // local mode passes it through untouched
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("browse ok: proxied"))
	}))
	defer upstream.Close()

	s := New("apitoken", nil)
	s.transport = newSafeTransport(true) // loopback upstream reachable in tests
	host := strings.TrimPrefix(upstream.URL, "http://")
	w := navAndServe(t, s, "tab:abc", "/b/tab:abc/http/"+host+"/foo")
	if w.Code != http.StatusOK {
		t.Fatalf("proxied nav: got %d want 200", w.Code)
	}
	if body := w.Body.String(); !strings.HasPrefix(body, "browse ok: proxied") {
		t.Fatalf("unexpected proxied body %q", body)
	}
}

func TestStateKeyMismatchForbidden(t *testing.T) {
	s := New("apitoken", nil)
	grant := s.MintGrant("tab:mine")
	r := httptest.NewRequest("GET", "/b/tab:mine/https/example.com/?__grant="+grant, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var cookie string
	for _, c := range w.Result().Cookies() {
		if c.Name == browseCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie after grant redeem")
	}
	// Same cookie, but the path claims a different panel's state key.
	r2 := httptest.NewRequest("GET", "/b/tab:other/https/example.com/", nil)
	r2.AddCookie(&http.Cookie{Name: browseCookie, Value: cookie})
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("cross state key: got %d want 403", w2.Code)
	}
}

func TestMalformedTargetAfterAuthIs400(t *testing.T) {
	s := New("apitoken", nil)
	grant := s.MintGrant("tab:abc")
	r := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/?__grant="+grant, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var cookie string
	for _, c := range w.Result().Cookies() {
		if c.Name == browseCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie after grant redeem")
	}
	// Valid session, but the target carries a scheme we refuse.
	r2 := httptest.NewRequest("GET", "/b/tab:abc/ftp/example.com/", nil)
	r2.AddCookie(&http.Cookie{Name: browseCookie, Value: cookie})
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("malformed target: got %d want 400", w2.Code)
	}
}

func TestRevokeKillsSessionCookie(t *testing.T) {
	s := New("apitoken", nil)
	// Mint+redeem once to obtain a live session cookie.
	grant := s.MintGrant("tab:abc")
	r := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/?__grant="+grant, nil)
	rw := httptest.NewRecorder()
	s.Handler().ServeHTTP(rw, r)
	var cookie string
	for _, c := range rw.Result().Cookies() {
		if c.Name == browseCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie after grant redeem")
	}
	// Sanity: the cookie authenticates before revocation.
	r2 := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/", nil)
	r2.AddCookie(&http.Cookie{Name: browseCookie, Value: cookie})
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("pre-revoke nav: got %d want 200", w2.Code)
	}
	// After Revoke, the same cookie must no longer be accepted.
	s.Revoke("tab:abc")
	r3 := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/", nil)
	r3.AddCookie(&http.Cookie{Name: browseCookie, Value: cookie})
	w3 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w3, r3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("post-revoke nav: got %d want 401", w3.Code)
	}
}

func TestListenReturnsListenerAndBaseURL(t *testing.T) {
	s := New("apitoken", nil)
	ln, base, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	if !strings.HasPrefix(base, "http://127.0.0.1:") || base == "http://127.0.0.1:0" {
		t.Fatalf("unexpected base URL %q", base)
	}
}
