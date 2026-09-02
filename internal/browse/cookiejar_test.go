package browse

import (
	"net/http"
	"net/url"
	"testing"
)

func respWithCookie(setCookie string) *http.Response {
	h := http.Header{}
	h.Add("Set-Cookie", setCookie)
	return &http.Response{Header: h}
}

// Cookies are host-scoped, never port-scoped (RFC 6265, same as a browser):
// two hosts under one stateKey keep separate cookies; two ports on one host
// share them.
func TestCookieJarIsolatesSitesUnderSameStateKey(t *testing.T) {
	j := newCookieJar()
	a, _ := url.Parse("http://127.0.0.1:3000/")
	b, _ := url.Parse("http://localhost:4000/")
	j.Store("tab:1", a, respWithCookie("sid=AAA; Path=/"))
	j.Store("tab:1", b, respWithCookie("sid=BBB; Path=/"))

	reqA, _ := http.NewRequest("GET", "http://127.0.0.1:3000/x", nil)
	reqA.Header.Set("Cookie", "ocode_browse=session") // browser-side, must be replaced
	j.Apply("tab:1", reqA)
	if got := reqA.Header.Get("Cookie"); got != "sid=AAA" {
		t.Errorf("site A cookie = %q, want sid=AAA", got)
	}
	reqB, _ := http.NewRequest("GET", "http://localhost:4000/x", nil)
	j.Apply("tab:1", reqB)
	if got := reqB.Header.Get("Cookie"); got != "sid=BBB" {
		t.Errorf("site B cookie = %q, want sid=BBB", got)
	}
}

func TestCookieJarIsolatesStateKeysAndClears(t *testing.T) {
	j := newCookieJar()
	a, _ := url.Parse("https://localhost:3510/")
	j.Store("tab:1", a, respWithCookie("sid=AAA; Path=/; HttpOnly"))

	other, _ := http.NewRequest("GET", "https://localhost:3510/admin", nil)
	j.Apply("tab:2", other)
	if got := other.Header.Get("Cookie"); got != "" {
		t.Errorf("stateKey leak: %q", got)
	}
	same, _ := http.NewRequest("GET", "https://localhost:3510/admin", nil)
	j.Apply("tab:1", same)
	if got := same.Header.Get("Cookie"); got != "sid=AAA" {
		t.Errorf("own cookie = %q, want sid=AAA", got)
	}
	j.Clear("tab:1")
	cleared, _ := http.NewRequest("GET", "https://localhost:3510/admin", nil)
	j.Apply("tab:1", cleared)
	if got := cleared.Header.Get("Cookie"); got != "" {
		t.Errorf("after Clear: %q", got)
	}
}

func TestCookieJarHonorsPathScope(t *testing.T) {
	j := newCookieJar()
	a, _ := url.Parse("http://127.0.0.1:3000/admin/")
	j.Store("tab:1", a, respWithCookie("adm=1; Path=/admin"))
	root, _ := http.NewRequest("GET", "http://127.0.0.1:3000/", nil)
	j.Apply("tab:1", root)
	if got := root.Header.Get("Cookie"); got != "" {
		t.Errorf("path-scoped cookie sent to /: %q", got)
	}
}
