package browse

import (
	"io"
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
	grant := s.MintGrant(stateKey, "")
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

// TestAuthenticatedNavigationProxiesExternal verifies the chrome hand-off:
// an authenticated document navigation to a non-private host no longer proxies
// — it emits a chrome nav event and answers 204 with no upstream fetch.
func TestAuthenticatedNavigationProxiesExternal(t *testing.T) {
	s := New("apitoken", nil)
	var got NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { got = ev })
	w := navAndServe(t, s, "tab:abc", "/b/tab:abc/https/example.com/")
	if w.Code != http.StatusNoContent {
		t.Fatalf("chrome hand-off: got %d want 204, body %q", w.Code, w.Body.String())
	}
	if got.Mode != "chrome" || got.URL != "https://example.com/" || got.Status != 0 {
		t.Fatalf("nav event = %+v want chrome https://example.com/ 0", got)
	}
	if got.StateKey != "tab:abc" {
		t.Fatalf("nav stateKey = %q want tab:abc", got.StateKey)
	}
}

func TestAuthenticatedNavigationProxiesExternalLocalStillProxies(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/foo" {
			t.Errorf("upstream saw path %q, want /foo", r.URL.Path)
		}
		if c := r.Header.Get("Cookie"); strings.Contains(c, browseCookie) {
			t.Errorf("browse session cookie leaked upstream: %q", c)
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("browse ok: local"))
	}))
	defer upstream.Close()

	s := New("apitoken", nil)
	host := strings.TrimPrefix(upstream.URL, "http://")
	w := navAndServe(t, s, "tab:abc", "/b/tab:abc/http/"+host+"/foo")
	if w.Code != http.StatusOK {
		t.Fatalf("local nav: got %d want 200", w.Code)
	}
	if body := w.Body.String(); !strings.HasPrefix(body, "browse ok: local") {
		t.Fatalf("unexpected local body %q", body)
	}
}

func TestStateKeyMismatchForbidden(t *testing.T) {
	s := New("apitoken", nil)
	grant := s.MintGrant("tab:mine", "")
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
	grant := s.MintGrant("tab:abc", "")
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
	grant := s.MintGrant("tab:abc", "")
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
	// Sanity: the cookie authenticates before revocation (chrome hand-off 204).
	r2 := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/", nil)
	r2.Header.Set("Sec-Fetch-Dest", "document")
	r2.AddCookie(&http.Cookie{Name: browseCookie, Value: cookie})
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("pre-revoke nav: got %d want 204", w2.Code)
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

func TestLocalModeSessionStateGate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "local")
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	path := "/b/tab:local/http/" + host + "/"

	s := New("apitoken", nil)

	// req factory — bound to s for the mux dispatch.
	get := func(cookieVal string, dest string, destHeader *http.Header) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", dest, nil)
		req.AddCookie(&http.Cookie{Name: browseCookie, Value: cookieVal})
		for k, vs := range *destHeader {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		return w
	}

	// 1) A session that has NEVER been in local mode cannot load a local
	// document — even with NO Referer at all. The gate is session state, not
	// Referer, so a suppressed/missing referer cannot widen the surface.
	grant := s.MintGrant("tab:local", "")
	cookie, _, ok := s.auth.redeem(grant, false) // session starts external
	if !ok {
		t.Fatal("redeem failed")
	}
	if w := get(cookie, path, &http.Header{}); w.Code != http.StatusForbidden {
		t.Fatalf("external-mode local doc (no referer): got %d want 403", w.Code)
	}
	// … and the same applies to local SUBRESOURCES from an external page
	// (e.g. <img src="/b/…/127.0.0.1:PORT/x.png">): they must not probe local.
	if w := get(cookie, path, &http.Header{"Sec-Fetch-Dest": {"image"}}); w.Code != http.StatusForbidden {
		t.Fatalf("external-mode local subresource: got %d want 403", w.Code)
	}

	// 2) Grant flow into local mode: mint → 302 (cookie set) → follow-up with
	// that cookie is admitted and the session is now local.
	grant = s.MintGrant("tab:local", "")
	localCookie := s.sessionAfterGrant(t, path, grant)
	if w := get(localCookie, path, &http.Header{}); w.Code != http.StatusOK {
		t.Fatalf("local doc after local grant: got %d body %q", w.Code, w.Body.String())
	}

	// 3) local->local still works (dev-server navigation must not stall):
	// the session is already local, so a follow-up local doc passes. (Note:
	// path ends in "/", so "next" must not be prefix-slash — a // would trip
	// ServeMux's path-clean 307.)
	if w := get(localCookie, path+"next", &http.Header{}); w.Code != http.StatusOK {
		t.Fatalf("local->local document: got %d want 200", w.Code)
	}

	// 4) Loading an EXTERNAL document flips the session back to external mode:
	// the next local request is refused again until a fresh local grant. We
	// drive the flip through the exact call handleBrowse performs on external
	// document dispatch (a real external upstream would require network, which
	// unit tests must avoid); the local-mode gate then refuses as expected.
	s.auth.setLocalDoc(localCookie, false)
	if w := get(localCookie, path, &http.Header{}); w.Code != http.StatusForbidden {
		t.Fatalf("local doc after external nav: got %d want 403", w.Code)
	}
}

// sessionAfterGrant mints a fresh grant for path, performs the grant->302
// exchange through the mux, and returns the session cookie the 302 installs.
func (s *Server) sessionAfterGrant(t *testing.T, path, grant string) string {
	t.Helper()
	first := httptest.NewRequest("GET", path+"?__grant="+grant, nil)
	firstW := httptest.NewRecorder()
	s.Handler().ServeHTTP(firstW, first)
	if firstW.Code != http.StatusFound {
		t.Fatalf("grant nav to %s: got %d want 302", path, firstW.Code)
	}
	for _, c := range firstW.Result().Cookies() {
		if c.Name == browseCookie {
			return c.Value
		}
	}
	t.Fatalf("grant nav to %s set no session cookie", path)
	return ""
}

// portOf extracts the port from a host:port string; the test loopback host
// always has one (httptest uses 127.0.0.1:random).
func portOf(hostport string) string {
	if i := strings.LastIndex(hostport, ":"); i != -1 {
		return hostport[i+1:]
	}
	return ""
}
