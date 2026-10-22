package browse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mountPrefix is the path prefix a remote project's browse origin is served
// under (the browse proxy mounts it at "/api/remote/<host>/browse").
const mountPrefix = "/api/remote/james@217.216.72.49/browse"

// TestGrantRedirectStaysUnderMountPrefix pins the core remote-project bug:
// the grant-redemption redirect must stay INSIDE the mount prefix.
//
// The origin used to redirect to the bare "/b/{stateKey}/..." path. The
// browser resolves a root-relative Location against the CURRENT origin, so
// the follow-up request left the "/api/remote/<host>/browse" mount, landed on
// the proxy's LOCAL "/b/" route (this machine's origin, not the host's), and
// was rejected with a permanent 401 — the panel stayed blank and spun
// forever. The session cookie did not help: its Path was "/b/", so it was
// not sent for the redirected path either.
func TestGrantRedirectStaysUnderMountPrefix(t *testing.T) {
	s := New("apitoken", nil)
	grant := s.MintGrant("tab:abc", "")

	r := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/?__grant="+grant, nil)
	r.Header.Set(BasePathHeader, mountPrefix)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("grant nav: got %d want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, mountPrefix+"/b/") {
		t.Fatalf("redirect left the mount prefix: %q (want prefix %q)", loc, mountPrefix)
	}
	// The grant must still be stripped from the redirect.
	if strings.Contains(loc, "__grant") {
		t.Fatalf("redirect still carries the grant: %q", loc)
	}
}

// TestSessionCookieCoversTheWholeMount pins the cookie scope under a mount:
// it must cover BOTH the "/b/..." route tree AND "/__ocode_capture.js",
// which the injected script references as a ROOT-RELATIVE path.
func TestSessionCookieCoversTheWholeMount(t *testing.T) {
	s := New("apitoken", nil)
	grant := s.MintGrant("tab:abc", "")

	r := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/?__grant="+grant, nil)
	r.Header.Set(BasePathHeader, mountPrefix)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name != browseCookie {
			continue
		}
		found = true
		if c.Path != mountPrefix+"/" {
			t.Fatalf("cookie path %q does not cover the mount (%q)", c.Path, mountPrefix)
		}
	}
	if !found {
		t.Fatal("no browse session cookie set")
	}
}

// TestInjectedCaptureScriptIsPrefixed pins the injected capture script's src:
// it is fetched by a root-relative path, so under a mount it must carry the
// prefix or the browser fetches the WRONG origin's bundle.
func TestInjectedCaptureScriptIsPrefixed(t *testing.T) {
	in := []byte(`<!doctype html><html><head><title>x</title></head><body>hi</body></html>`)
	out := string(injectCapture(in, "tab:abc", "http://127.0.0.1:5000", mountPrefix))

	want := `src="` + mountPrefix + `/__ocode_capture.js"`
	if !strings.Contains(out, want) {
		t.Fatalf("capture script not prefixed with the mount: %s", out)
	}
	// The unprefixed path must NOT remain, or the browser would prefer it.
	if strings.Contains(out, `src="/__ocode_capture.js"`) {
		t.Fatalf("capture script still carries the unprefixed root path: %s", out)
	}
}

// TestRewrittenURLsArePrefixed pins the HTML body rewrite: every rewritten
// URL is root-relative, so it must carry the mount prefix too.
func TestRewrittenURLsArePrefixed(t *testing.T) {
	tgt := target{StateKey: "tab:abc", Scheme: "https", Host: "example.com", Path: "/"}
	in := []byte(`<html><head><link rel="stylesheet" href="/app.css"></head><body><img src="/logo.png"></body></html>`)

	out, err := rewriteHTML(in, tgt, "", mountPrefix)
	if err != nil {
		t.Fatalf("rewriteHTML: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, mountPrefix+"/b/tab:abc/https/example.com/app.css") {
		t.Fatalf("stylesheet not rewritten under the mount: %s", s)
	}
	if !strings.Contains(s, mountPrefix+"/b/tab:abc/https/example.com/logo.png") {
		t.Fatalf("image not rewritten under the mount: %s", s)
	}
	// No bare root-relative browse path may survive.
	if strings.Contains(s, `"/b/`) {
		t.Fatalf("unprefixed browse path survived the rewrite: %s", s)
	}
}

// TestMapURLUnderMount pins the URL mapper directly.
func TestMapURLUnderMount(t *testing.T) {
	tgt := target{StateKey: "tab:abc", Scheme: "https", Host: "example.com", Path: "/"}
	got := mapURL("/x.js", tgt, "", mountPrefix)
	want := mountPrefix + "/b/tab:abc/https/example.com/x.js"
	if got != want {
		t.Fatalf("mapURL = %q want %q", got, want)
	}
	// Root mount is unchanged (the local case must not regress).
	got = mapURL("/x.js", tgt, "", "")
	want = "/b/tab:abc/https/example.com/x.js"
	if got != want {
		t.Fatalf("root-mounted mapURL = %q want %q", got, want)
	}
}

func TestNormalizeBasePathRejectsHostileValues(t *testing.T) {
	bad := []string{
		"http://evil.com/x", // scheme
		"//evil.com/x",      // authority
		"/a/../b",           // traversal
		"notapath",          // no leading slash
		"/a?b",              // query
		"/a#b",              // fragment
		"/a b",              // space
		"/a\\b",             // backslash
		`/a"b`,              // double quote breaks out of src="..."
		`/a'b`,              // single quote
		"/a<b",              // angle bracket
		"/a>b",              // angle bracket
		`/x"><script>alert(1)</script>`,
		"/a\tb",  // tab
		"/a%22b", // percent-encoding is not a path-safe byte here
		"/a|b",   // pipe
	}
	for _, p := range bad {
		if got := normalizeBasePath(p); got != "" {
			t.Errorf("normalizeBasePath(%q) = %q, want %q (rejected)", p, got, "")
		}
	}
	good := map[string]string{
		"":                       "",
		"/":                      "",
		mountPrefix:              mountPrefix,
		mountPrefix + "/":        mountPrefix,
		"/api/remote/h@b/browse": "/api/remote/h@b/browse",
	}
	for in, want := range good {
		if got := normalizeBasePath(in); got != want {
			t.Errorf("normalizeBasePath(%q) = %q want %q", in, got, want)
		}
	}
}

func TestWithBasePath(t *testing.T) {
	if got := withBasePath("", "/b/x"); got != "/b/x" {
		t.Errorf("root mount = %q want /b/x", got)
	}
	if got := withBasePath(mountPrefix, "/b/x"); got != mountPrefix+"/b/x" {
		t.Errorf("prefixed = %q want %q", got, mountPrefix+"/b/x")
	}
	// A relative path is not a root-relative path: it must not be prefixed.
	if got := withBasePath(mountPrefix, "b/x"); got != "b/x" {
		t.Errorf("relative = %q want b/x", got)
	}
}

func TestCookiePath(t *testing.T) {
	if got := cookiePath(""); got != "/" {
		t.Errorf("root cookie path = %q want /", got)
	}
	if got := cookiePath(mountPrefix); got != mountPrefix+"/" {
		t.Errorf("prefixed cookie path = %q want %q", got, mountPrefix+"/")
	}
}

// TestRedirectPrefixAppliedExactlyOnce pins the redirect seam: the origin
// receives the mount-relative path (the proxy strips the prefix), so the
// grant redirect must carry the prefix exactly once, with the grant removed.
func TestRedirectPrefixAppliedExactlyOnce(t *testing.T) {
	s := New("apitoken", nil)
	grant := s.MintGrant("tab:abc", "")

	r := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/x?__grant="+grant+"&q=1", nil)
	r.Header.Set(BasePathHeader, mountPrefix)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	want := mountPrefix + "/b/tab:abc/https/example.com/x?q=1"
	if got := w.Header().Get("Location"); got != want {
		t.Fatalf("redirect = %q, want %q", got, want)
	}
	if n := strings.Count(w.Header().Get("Location"), mountPrefix); n != 1 {
		t.Fatalf("mount prefix appears %d times in redirect, want 1", n)
	}
}
