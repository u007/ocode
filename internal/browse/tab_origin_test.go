package browse

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// tabSeen is one request the recording upstream received.
type tabSeen struct {
	path string
	host string
}

// tabUpstream serves a plain-text 200 on loopback and reports each request it
// receives. The channel is buffered and the send never blocks.
func tabUpstream(t *testing.T) (*httptest.Server, <-chan tabSeen) {
	t.Helper()
	seen := make(chan tabSeen, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- tabSeen{path: r.URL.Path, host: r.Host}:
		default:
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

// tabNewServer returns a Server whose tab listeners are closed at test end.
func tabNewServer(t *testing.T) *Server {
	t.Helper()
	s := New("tok", nil)
	t.Cleanup(func() {
		if err := s.Close(context.Background()); err != nil {
			t.Errorf("close browse server: %v", err)
		}
	})
	return s
}

// tabNoRedirect returns a client that reports 3xx responses instead of
// following them, so the grant redemption's Set-Cookie can be inspected.
func tabNoRedirect() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// tabRedeem mints a grant for stateKey, navigates the tab listener at base to
// path with it, and returns the session cookie the tab set.
func tabRedeem(t *testing.T, s *Server, base, stateKey, path string) *http.Cookie {
	t.Helper()
	grant := s.MintGrant(stateKey, "")
	resp, err := tabNoRedirect().Get(base + path + "?__grant=" + grant)
	if err != nil {
		t.Fatalf("redeem GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redeem status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != path {
		t.Fatalf("redeem Location = %q, want %q (grant must be stripped, path kept)", loc, path)
	}
	want := tabCookieName(stateKey)
	for _, c := range resp.Cookies() {
		if c.Name == want {
			if c.Path != "/" {
				t.Fatalf("session cookie Path = %q, want %q", c.Path, "/")
			}
			return c
		}
	}
	t.Fatalf("redeem set no cookie named %q", want)
	return nil
}

// tabGet performs one GET against base+path, optionally with a cookie.
func tabGet(t *testing.T, base, path string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := tabNoRedirect().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// tabAddr strips the scheme from a tab base URL, leaving host:port.
func tabAddr(base string) string {
	return strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
}

func TestTabOriginBindingIsImmutable(t *testing.T) {
	upstream, seen := tabUpstream(t)
	s := tabNewServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	_, base, err := s.ListenTab("tab:bind", "http", host, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab: %v", err)
	}
	cookie := tabRedeem(t, s, base, "tab:bind", "/app/dashboard")

	// A path naming another upstream must still reach only the bound host, at
	// the literal path: the path selects a resource, never the upstream.
	resp := tabGet(t, base, "/b/tab:bind/http/evil.example/secret", cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := <-seen
	if got.path != "/b/tab:bind/http/evil.example/secret" {
		t.Fatalf("upstream path = %q, want the literal request path", got.path)
	}
	if got.host != host {
		t.Fatalf("upstream Host = %q, want bound host %q", got.host, host)
	}
}

func TestTabOriginCookieNamePerTab(t *testing.T) {
	a, b := tabCookieName("tab:a"), tabCookieName("tab:b")
	if a == b {
		t.Fatalf("two tabs share cookie name %q", a)
	}
	for _, name := range []string{a, b} {
		if !strings.HasPrefix(name, "ocode_browse_") || len(name) != len("ocode_browse_")+16 {
			t.Fatalf("cookie name %q, want ocode_browse_ plus 16 hex chars", name)
		}
		if strings.ContainsAny(name, ":/ ") {
			t.Fatalf("cookie name %q contains a character illegal in cookie names", name)
		}
	}

	upstream, _ := tabUpstream(t)
	s := tabNewServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	_, baseA, err := s.ListenTab("tab:a", "http", host, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab a: %v", err)
	}
	_, baseB, err := s.ListenTab("tab:b", "http", host, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab b: %v", err)
	}
	cookieA := tabRedeem(t, s, baseA, "tab:a", "/")
	cookieB := tabRedeem(t, s, baseB, "tab:b", "/")
	if cookieA.Value == cookieB.Value {
		t.Fatal("two tabs issued the same session value")
	}
}

func TestTabOriginStateKeyMismatchForbidden(t *testing.T) {
	upstream, _ := tabUpstream(t)
	s := tabNewServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	_, base, err := s.ListenTab("tab:bound", "http", host, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab: %v", err)
	}

	// Cookie tossing: a live session for another panel, presented under this
	// tab's cookie name, must be refused by the target-binding check.
	sessionVal, _, ok := s.auth.redeem(s.MintGrant("tab:other", ""), true)
	if !ok {
		t.Fatal("redeem other-panel grant failed")
	}
	tossed := &http.Cookie{Name: tabCookieName("tab:bound"), Value: sessionVal}
	if resp := tabGet(t, base, "/app", tossed); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("tossed cookie status = %d, want 403", resp.StatusCode)
	}

	// A grant minted for another panel is refused before any cookie is set.
	resp := tabGet(t, base, "/app?__grant="+s.MintGrant("tab:other", ""))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign grant status = %d, want 403", resp.StatusCode)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatalf("foreign grant set cookies %v, want none", resp.Cookies())
	}
}

func TestTabOriginUnauthenticatedUnauthorized(t *testing.T) {
	upstream, _ := tabUpstream(t)
	s := tabNewServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	_, base, err := s.ListenTab("tab:anon", "http", host, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab: %v", err)
	}
	if resp := tabGet(t, base, "/app"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-cookie status = %d, want 401", resp.StatusCode)
	}
	// The capture bundle stays unauthenticated, as in prefix mode.
	if resp := tabGet(t, base, captureScriptPath); resp.StatusCode != http.StatusOK {
		t.Fatalf("capture script status = %d, want 200", resp.StatusCode)
	}
}

func TestTabOriginDuplicateBindingRefused(t *testing.T) {
	s := tabNewServer(t)
	if _, _, err := s.ListenTab("tab:dup", "http", "A.Test:80", "127.0.0.1:0", nil); err != nil {
		t.Fatalf("first ListenTab: %v", err)
	}
	if _, _, err := s.ListenTab("tab:dup", "http", "A.Test:80", "127.0.0.1:0", nil); err == nil {
		t.Fatal("second ListenTab for the same binding succeeded, want error")
	}
	// The host is case-insensitive, so a differently cased host is the same binding.
	if _, _, err := s.ListenTab("tab:dup", "http", "a.test:80", "127.0.0.1:0", nil); err == nil {
		t.Fatal("case-variant binding succeeded, want error")
	}
}

func TestTabOriginCapPerStateKey(t *testing.T) {
	s := tabNewServer(t)
	for i := 1; i <= maxTabListenersPerState; i++ {
		host := "h" + string(rune('0'+i)) + ".test:80"
		if _, _, err := s.ListenTab("tab:cap", "http", host, "127.0.0.1:0", nil); err != nil {
			t.Fatalf("listener %d: %v", i, err)
		}
	}
	if _, _, err := s.ListenTab("tab:cap", "http", "over.test:80", "127.0.0.1:0", nil); err == nil {
		t.Fatalf("listener %d accepted, want cap error", maxTabListenersPerState+1)
	}
	// The cap is per stateKey: another panel still gets a listener.
	if _, _, err := s.ListenTab("tab:other-cap", "http", "over.test:80", "127.0.0.1:0", nil); err != nil {
		t.Fatalf("other stateKey ListenTab: %v", err)
	}
}

func TestTabOriginRevokeClosesListeners(t *testing.T) {
	s := tabNewServer(t)
	_, revoked, err := s.ListenTab("tab:x", "http", "a.test:80", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab x1: %v", err)
	}
	_, revokedTwo, err := s.ListenTab("tab:x", "http", "b.test:80", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab x2: %v", err)
	}
	_, kept, err := s.ListenTab("tab:y", "http", "a.test:80", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab y: %v", err)
	}

	s.Revoke("tab:x")
	for _, base := range []string{revoked, revokedTwo} {
		if c, err := net.DialTimeout("tcp", tabAddr(base), time.Second); err == nil {
			_ = c.Close()
			t.Fatalf("revoked listener %s still accepts connections", base)
		}
	}
	if c, err := net.DialTimeout("tcp", tabAddr(kept), time.Second); err != nil {
		t.Fatalf("other panel's listener closed by Revoke: %v", err)
	} else {
		_ = c.Close()
	}
	if _, _, err := s.ListenTab("tab:x", "http", "a.test:80", "127.0.0.1:0", nil); err != nil {
		t.Fatalf("binding not reusable after Revoke: %v", err)
	}
}

func TestTabOriginCloseClosesAll(t *testing.T) {
	s := New("tok", nil)
	_, base, err := s.ListenTab("tab:all", "http", "a.test:80", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if c, err := net.DialTimeout("tcp", tabAddr(base), time.Second); err == nil {
		_ = c.Close()
		t.Fatal("listener still accepts connections after Close")
	}
}

func TestTabOriginCallerClosedListenerFreesBinding(t *testing.T) {
	s := tabNewServer(t)
	ln, _, err := s.ListenTab("tab:self", "http", "a.test:80", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close caller listener: %v", err)
	}
	// Serve notices the closed listener on its own goroutine, so poll.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, _, err := s.ListenTab("tab:self", "http", "a.test:80", "127.0.0.1:0", nil); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("binding still held after its listener was closed by the caller")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRootModeRewriteEmitsPathForSameUpstream(t *testing.T) {
	tgt := target{StateKey: "tab:r", Scheme: "http", Host: "127.0.0.1:3510", Path: "/app/dashboard", Local: true, RootMounted: true}
	in := []byte(`<!doctype html><html><head></head><body>` +
		`<a href="http://127.0.0.1:3510/x?y=1">same</a>` +
		`<img src="/img.png">` +
		`<script src="https://other.test/a.js"></script>` +
		`</body></html>`)
	out, err := rewriteHTML(in, tgt, "", "")
	if err != nil {
		t.Fatalf("rewriteHTML: %v", err)
	}
	s := string(out)
	for _, want := range []string{`href="/x?y=1"`, `src="/img.png"`, `src="/__ocode/b/https/other.test/a.js"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("rewritten HTML missing %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "/b/tab:r/") {
		t.Fatalf("root-mounted rewrite emitted a prefix-mode /b/ route:\n%s", s)
	}

	cap := string(injectCapture([]byte(`<html><head></head></html>`), tgt, "http://127.0.0.1:4096", ""))
	if !strings.Contains(cap, `rootMode:true,scheme:"http",host:"127.0.0.1:3510"`) {
		t.Fatalf("root-mode payload missing binding: %s", cap)
	}
}
