package browse

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCookieJarIsolatesSitesUnderSameStateKey(t *testing.T) {
	j := newCookieJar()

	// Site A sets a cookie.
	respA := &http.Response{Header: http.Header{}}
	respA.Header.Add("Set-Cookie", "sid=AAA; Path=/")
	j.Store("tab:x", "https://a.example", respA)

	// Site B sets its own.
	respB := &http.Response{Header: http.Header{}}
	respB.Header.Add("Set-Cookie", "sid=BBB; Path=/")
	j.Store("tab:x", "https://b.example", respB)

	// Request to A gets only A's cookie.
	reqA := httptest.NewRequest("GET", "https://a.example/page", nil)
	j.Apply("tab:x", "https://a.example", reqA)
	if got := reqA.Header.Get("Cookie"); got != "sid=AAA" {
		t.Errorf("site A cookie = %q, want sid=AAA", got)
	}

	// Request to B never sees A's cookie.
	reqB := httptest.NewRequest("GET", "https://b.example/page", nil)
	j.Apply("tab:x", "https://b.example", reqB)
	if got := reqB.Header.Get("Cookie"); got != "sid=BBB" {
		t.Errorf("site B cookie = %q, want sid=BBB (no cross-site leak)", got)
	}
}

func TestCookieJarIsolatesStateKeys(t *testing.T) {
	j := newCookieJar()
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", "sid=ONE; Path=/")
	j.Store("tab:one", "https://a.example", resp)

	req := httptest.NewRequest("GET", "https://a.example/", nil)
	j.Apply("tab:two", "https://a.example", req)
	if req.Header.Get("Cookie") != "" {
		t.Errorf("cookie leaked across stateKey: %q", req.Header.Get("Cookie"))
	}
}
