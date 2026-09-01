package browse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGrantExchangeSetsCookieAndIsOneTime(t *testing.T) {
	s := New("apitoken", nil)
	grant := s.MintGrant("tab:abc", "")

	// First navigation carrying the grant → 302 to clean URL + Set-Cookie.
	r := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/?__grant="+grant, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("grant nav: got %d want 302", w.Code)
	}
	cookies := w.Result().Cookies()
	var sess string
	for _, c := range cookies {
		if c.Name == browseCookie {
			sess = c.Value
			if !c.HttpOnly || c.Path != "/b/" {
				t.Fatalf("cookie flags wrong: %+v", c)
			}
		}
	}
	if sess == "" {
		t.Fatal("no browse session cookie set")
	}

	// Reusing the same grant must fail (one-time).
	r2 := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/?__grant="+grant, nil)
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, r2)
	if w2.Code == http.StatusFound {
		t.Fatal("grant was reusable; must be one-time")
	}
}

func TestNoCookieNoGrantRejected(t *testing.T) {
	s := New("apitoken", nil)
	r := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d want 401", w.Code)
	}
}

// The capture script's postMessage targetOrigin must be the origin the SPA
// was actually loaded from (recorded at grant mint), not the server's bound
// listener address — "http://127.0.0.1:4096" ≠ "http://localhost:4096" and
// the browser drops every console/network message on a mismatch.
func TestGrantOriginDrivesCaptureTargetOrigin(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head></head><body>x</body></html>"))
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")

	s := New("apitoken", nil)
	s.SetSPAOrigin("http://127.0.0.1:4096")
	grant := s.MintGrant("tab:o", "http://localhost:4096")
	// Target is the private-loopback httptest upstream; the session must be
	// marked local for the gate to admit the request.
	cookieVal, _, ok := s.auth.redeem(grant, true)
	if !ok {
		t.Fatal("redeem failed")
	}
	r := httptest.NewRequest("GET", "/b/tab:o/http/"+host+"/", nil)
	r.AddCookie(&http.Cookie{Name: browseCookie, Value: cookieVal, Path: "/b/"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `spaOrigin:"http://localhost:4096"`) {
		t.Fatalf("bootstrap must carry the grant's SPA origin: %s", w.Body.String())
	}

	// Revoke forgets the per-key origin; a fresh grant with no origin falls
	// back to the configured server-wide SPA origin.
	s.Revoke("tab:o")
	if got := s.spaOriginFor("tab:o"); got != "http://127.0.0.1:4096" {
		t.Fatalf("after revoke spaOriginFor = %q", got)
	}
}
