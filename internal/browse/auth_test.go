package browse

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGrantExchangeSetsCookieAndIsOneTime(t *testing.T) {
	s := New("apitoken", nil)
	grant := s.MintGrant("tab:abc")

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
