package browse

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStripSecurityHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'self'")
	h.Set("Content-Security-Policy-Report-Only", "default-src 'none'")
	h.Set("Strict-Transport-Security", "max-age=63072000")
	h.Set("Service-Worker-Allowed", "/")
	h.Set("Content-Type", "text/html") // must survive

	stripSecurityHeaders(h)

	for _, k := range []string{
		"X-Frame-Options", "Content-Security-Policy",
		"Content-Security-Policy-Report-Only", "Strict-Transport-Security",
		"Service-Worker-Allowed",
	} {
		if h.Get(k) != "" {
			t.Errorf("%s was not stripped: %q", k, h.Get(k))
		}
	}
	if h.Get("Content-Type") != "text/html" {
		t.Errorf("Content-Type was clobbered: %q", h.Get("Content-Type"))
	}
}

func TestFilterRequestHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "https://example.com/", nil)
	r.Header.Set("Referer", "http://127.0.0.1:9999/secret")
	r.Header.Set("User-Agent", "leak/1.0")
	r.Header.Set("Accept-Encoding", "br, zstd")
	r.Header.Set("Cookie", "sess=abc") // browser-side cookie must not leak upstream

	filterRequestHeaders(r)

	if r.Header.Get("Referer") != "" {
		t.Errorf("Referer not stripped: %q", r.Header.Get("Referer"))
	}
	if r.Header.Get("User-Agent") != fixedUserAgent {
		t.Errorf("User-Agent = %q, want fixed", r.Header.Get("User-Agent"))
	}
	if r.Header.Get("Accept-Encoding") != "gzip, identity" {
		t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
	}
	if r.Header.Get("Cookie") != "" {
		t.Errorf("browser Cookie leaked upstream: %q", r.Header.Get("Cookie"))
	}
}
