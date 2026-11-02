package browse

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// upstreamHit is one request an upstream received, as the upstream saw it.
type upstreamHit struct {
	host   string // Host header
	path   string
	query  string
	cookie string // Cookie header; "" when none was sent
	xff    string // X-Forwarded-For, which only the proxy adds
}

// foreignUpstream serves on loopback and reports each request it receives. A
// nil handle answers 200 "ok". The channel is buffered and the send never blocks.
func foreignUpstream(t *testing.T, handle http.HandlerFunc) (*httptest.Server, <-chan upstreamHit) {
	t.Helper()
	seen := make(chan upstreamHit, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- upstreamHit{host: r.Host, path: r.URL.Path, query: r.URL.RawQuery, cookie: r.Header.Get("Cookie"), xff: r.Header.Get("X-Forwarded-For")}:
		default:
		}
		if handle != nil {
			handle(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

// localhostHost names srv through "localhost" rather than 127.0.0.1. The cookie
// jar keys by host and ignores the port, so two upstreams on 127.0.0.1 would
// share cookies. Under different host names the test can tell them apart.
func localhostHost(srv *httptest.Server) string {
	return "localhost:" + strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
}

// recvSeen returns the next request an upstream recorded, or fails the test.
func recvSeen(t *testing.T, seen <-chan upstreamHit) upstreamHit {
	t.Helper()
	select {
	case got := <-seen:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("upstream received no request, want one")
		return upstreamHit{}
	}
}

// expectNoRequest fails the test if an upstream received a request.
func expectNoRequest(t *testing.T, seen <-chan upstreamHit) {
	t.Helper()
	select {
	case got := <-seen:
		t.Fatalf("upstream received %+v, want no request", got)
	default:
	}
}

// foreignSession mints a live session for stateKey and returns its cookie on
// that tab origin. local marks the session's document local, as a grant for a
// private upstream does.
func foreignSession(t *testing.T, s *Server, stateKey string, local bool) *http.Cookie {
	t.Helper()
	val, _, ok := s.auth.redeem(s.MintGrant(stateKey, ""), local)
	if !ok {
		t.Fatal("redeem grant failed in test setup")
	}
	return &http.Cookie{Name: tabCookieName(stateKey), Value: val}
}

// foreignDo sends one GET through a tab origin's handler. dest is sent as the
// Sec-Fetch-Dest header; "" omits it, which the proxy reads as a document.
func foreignDo(h http.Handler, target, dest string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if dest != "" {
		req.Header.Set("Sec-Fetch-Dest", dest)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// navLog records the nav events a server publishes. The mutex covers handlers
// that run on a listener's goroutine.
type navLog struct {
	mu  sync.Mutex
	evs []NavEvent
}

func (n *navLog) add(_ string, ev NavEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.evs = append(n.evs, ev)
}

func (n *navLog) all() []NavEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]NavEvent(nil), n.evs...)
}

// hasNav reports whether some recorded event matches want exactly.
func hasNav(evs []NavEvent, want NavEvent) bool {
	for _, ev := range evs {
		if ev == want {
			return true
		}
	}
	return false
}

// TestTabForeignSubresourceProxied: a foreign route is proxied to its own
// upstream with the request's path and query, the site cookies come from the
// jar keyed by that upstream, and the binding never sees them.
func TestTabForeignSubresourceProxied(t *testing.T) {
	bound, boundSeen := foreignUpstream(t, nil)
	foreign, foreignSeen := foreignUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s3cr3t", Path: "/", HttpOnly: true})
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok")
	})
	s := tabNewServer(t)
	const sk = "tab:fsub"
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: tabAddr(bound.URL)})
	cookie := foreignSession(t, s, sk, true)
	fhost := localhostHost(foreign)

	login := foreignDo(h, "/__ocode/b/http/"+fhost+"/login", "script", cookie)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", login.Code)
	}
	if sc := login.Header().Get("Set-Cookie"); sc != "" {
		t.Fatalf("site cookie reached the browser: %q", sc)
	}
	if got := recvSeen(t, foreignSeen); got.host != fhost || got.path != "/login" {
		t.Fatalf("foreign upstream saw host=%q path=%q, want %q /login", got.host, got.path, fhost)
	}

	app := foreignDo(h, "/__ocode/b/http/"+fhost+"/app?x=1", "script", cookie)
	if app.Code != http.StatusOK {
		t.Fatalf("app status = %d, want 200", app.Code)
	}
	got := recvSeen(t, foreignSeen)
	if got.host != fhost || got.path != "/app" || got.query != "x=1" {
		t.Fatalf("foreign upstream saw host=%q path=%q query=%q, want %q /app x=1", got.host, got.path, got.query, fhost)
	}
	if got.cookie != "sid=s3cr3t" {
		t.Fatalf("foreign request Cookie = %q, want the jar's sid=s3cr3t", got.cookie)
	}
	if got.xff == "" {
		t.Fatal("foreign request reached its upstream without the proxy (no X-Forwarded-For)")
	}

	// The binding is another host. It gets none of the foreign jar's cookies,
	// and the foreign upstream never sees the binding's paths.
	if res := foreignDo(h, "/api/me", "script", cookie); res.Code != http.StatusOK {
		t.Fatalf("binding status = %d, want 200", res.Code)
	}
	bgot := recvSeen(t, boundSeen)
	if bgot.host != tabAddr(bound.URL) || bgot.path != "/api/me" {
		t.Fatalf("binding upstream saw host=%q path=%q, want its own /api/me", bgot.host, bgot.path)
	}
	if bgot.cookie != "" {
		t.Fatalf("foreign cookie leaked to the binding upstream: %q", bgot.cookie)
	}
	expectNoRequest(t, foreignSeen)
}

// TestTabForeignDocumentHandsOff: a top-level document on a foreign route is
// never fetched. The proxy emits a nav event for it and answers 204, and the SPA
// navigates. A public document hands off as chrome and leaves the session's
// local mode alone.
func TestTabForeignDocumentHandsOff(t *testing.T) {
	bound, _ := foreignUpstream(t, nil)
	foreign, foreignSeen := foreignUpstream(t, nil)
	s := tabNewServer(t)
	var navs navLog
	s.SetNavPublisher(navs.add)
	const sk = "tab:fdoc"
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: tabAddr(bound.URL)})
	cookie := foreignSession(t, s, sk, true)
	fhost := localhostHost(foreign)

	rec := foreignDo(h, "/__ocode/b/http/"+fhost+"/app?x=1", "document", cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("private document status = %d, want 204", rec.Code)
	}
	expectNoRequest(t, foreignSeen)
	want := NavEvent{StateKey: sk, URL: "http://" + fhost + "/app?x=1", Status: 0, Mode: "local"}
	if evs := navs.all(); !hasNav(evs, want) {
		t.Fatalf("nav events %+v lack %+v", evs, want)
	}

	rec = foreignDo(h, "/__ocode/b/https/example.test:443/a", "document", cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("public document status = %d, want 204", rec.Code)
	}
	wantPublic := NavEvent{StateKey: sk, URL: "https://example.test:443/a", Status: 0, Mode: "chrome"}
	if evs := navs.all(); !hasNav(evs, wantPublic) {
		t.Fatalf("nav events %+v lack %+v", evs, wantPublic)
	}
	if !s.auth.sessionLocalDoc(cookie.Value) {
		t.Fatal("a foreign public document moved the session out of local mode; the binding's next private request would be refused")
	}
}

// TestTabForeignLocalGateRefusesPublicTab: a tab whose session is not local
// cannot reach a private host through the reserved route, whether the request
// is a subresource or a document. The refusal is the local-mode gate's 403, and
// no handoff is emitted.
func TestTabForeignLocalGateRefusesPublicTab(t *testing.T) {
	foreign, foreignSeen := foreignUpstream(t, nil)
	s := tabNewServer(t)
	var navs navLog
	s.SetNavPublisher(navs.add)
	const sk = "tab:pubgate"
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: "public.example:80"})
	cookie := foreignSession(t, s, sk, false)
	fhost := tabAddr(foreign.URL)

	for _, dest := range []string{"script", "document"} {
		rec := foreignDo(h, "/__ocode/b/http/"+fhost+"/secret", dest, cookie)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("dest=%s status = %d, want 403 from the local gate", dest, rec.Code)
		}
	}
	expectNoRequest(t, foreignSeen)
	for _, ev := range navs.all() {
		if ev.Mode == "chrome" {
			t.Fatalf("refused private request produced a chrome handoff: %+v", ev)
		}
	}
	denied := NavEvent{StateKey: sk, URL: "http://" + fhost + "/secret", Status: http.StatusForbidden, Mode: "local", Error: "local navigation requires user action"}
	if !hasNav(navs.all(), denied) {
		t.Fatalf("nav events %+v lack the gate's refusal %+v", navs.all(), denied)
	}
}

// TestTabForeignPublicSubresourceUsesSSRFSafeTransport: a public foreign host is
// dialed through the SSRF-safe transport, never the private-address transports.
func TestTabForeignPublicSubresourceUsesSSRFSafeTransport(t *testing.T) {
	s := tabNewServer(t)
	pub := target{StateKey: "tab:pubt", Scheme: "https", Host: "cdn.example:443", Path: "/a.js", RootMounted: true, Foreign: true, BindScheme: "http", BindHost: "public.example:80"}
	if got := s.chooseLocalTransport(pub); got != s.externalTransport() {
		t.Fatal("public foreign subresource does not use the SSRF-safe external transport")
	}
}

// TestTabForeignBadTargetRefused: a foreign route whose scheme or host is not
// acceptable is refused with 400 and the binding is not dialed. The session is
// authenticated, so the refusal comes from the target check.
func TestTabForeignBadTargetRefused(t *testing.T) {
	bound, boundSeen := foreignUpstream(t, nil)
	s := tabNewServer(t)
	const sk = "tab:bad"
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: tabAddr(bound.URL)})
	cookie := foreignSession(t, s, sk, true)

	for _, p := range []string{
		"/__ocode/b/",
		"/__ocode/b/ftp/localhost:1/x",
		"/__ocode/b/javascript/x/y",
		"/__ocode/b/http/",
		"/__ocode/b/https//x",
		"/__ocode/b/http/user@localhost:1/x",
		"/__ocode/b/http/localhost:/x",
		"/__ocode/b/http/a%20b/x",
	} {
		if rec := foreignDo(h, p, "script", cookie); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", p, rec.Code)
		}
	}
	expectNoRequest(t, boundSeen)
}

// TestTabPathsOutsideReservedRouteStayUpstream: only "/__ocode/b/" is reserved.
// Paths that merely start with "/__ocode" are the binding's own paths.
func TestTabPathsOutsideReservedRouteStayUpstream(t *testing.T) {
	bound, boundSeen := foreignUpstream(t, nil)
	s := tabNewServer(t)
	const sk = "tab:edge"
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: tabAddr(bound.URL)})
	cookie := foreignSession(t, s, sk, true)

	for _, p := range []string{"/__ocode", "/__ocode/", "/__ocode/b", "/__ocode_x/app", "/__ocode/bogus/x"} {
		if rec := foreignDo(h, p, "script", cookie); rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", p, rec.Code)
		}
		if got := recvSeen(t, boundSeen); got.path != p {
			t.Fatalf("binding upstream path = %q, want the literal %q", got.path, p)
		}
	}
}

// TestTabForeignGrantRefused: a grant on a foreign route is refused before it is
// redeemed, so it neither enters local mode for the foreign host nor is consumed.
func TestTabForeignGrantRefused(t *testing.T) {
	bound, _ := foreignUpstream(t, nil)
	foreign, foreignSeen := foreignUpstream(t, nil)
	s := tabNewServer(t)
	const sk = "tab:grant"
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: tabAddr(bound.URL)})
	grant := s.MintGrant(sk, "")

	rec := foreignDo(h, "/__ocode/b/http/"+localhostHost(foreign)+"/x?__grant="+grant, "document")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("grant on foreign route status = %d, want 400", rec.Code)
	}
	if _, live := s.auth.grantStateKey(grant); !live {
		t.Fatal("grant on foreign route was consumed; it must be left unredeemed")
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatalf("grant on foreign route set cookies %v", rec.Result().Cookies())
	}
	expectNoRequest(t, foreignSeen)
}

// TestTabForeignRedirects: a redirect from the binding or from a foreign route is
// rewritten so the browser stays on the proxy. Same-binding redirects become a
// path. Foreign subresource redirects go to the reserved route. A foreign document
// redirect is handed off to the SPA as a nav event.
func TestTabForeignRedirects(t *testing.T) {
	foreign, _ := foreignUpstream(t, nil)
	fhost := localhostHost(foreign)
	bound, _ := foreignUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "http://"+fhost+"/landing", http.StatusFound)
		case "/go":
			http.Redirect(w, r, "/login", http.StatusFound)
		case "/sub":
			http.Redirect(w, r, "http://"+fhost+"/x?y=1", http.StatusFound)
		case "/cdn":
			http.Redirect(w, r, "https://cdn.test/a.js", http.StatusFound)
		}
	})
	s := tabNewServer(t)
	var navs navLog
	s.SetNavPublisher(navs.add)
	const sk = "tab:redir"
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: tabAddr(bound.URL)})
	cookie := foreignSession(t, s, sk, true)

	cases := []struct {
		name, path, dest string
		wantCode         int
		wantLocation     string
	}{
		{"same binding document", "/go", "document", http.StatusFound, "/login"},
		{"same binding subresource", "/go", "script", http.StatusFound, "/login"},
		{"foreign document handed off", "/start", "document", http.StatusNoContent, ""},
		{"foreign subresource to reserved route", "/sub", "script", http.StatusFound, "/__ocode/b/http/" + fhost + "/x?y=1"},
		{"public subresource to reserved route", "/cdn", "script", http.StatusFound, "/__ocode/b/https/cdn.test/a.js"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := foreignDo(h, tc.path, tc.dest, cookie)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if loc := rec.Header().Get("Location"); loc != tc.wantLocation {
				t.Fatalf("Location = %q, want %q", loc, tc.wantLocation)
			}
		})
	}
	want := NavEvent{StateKey: sk, URL: "http://" + fhost + "/landing", Status: 0, Mode: "local"}
	if !hasNav(navs.all(), want) {
		t.Fatalf("nav events %+v lack the document handoff %+v", navs.all(), want)
	}
}

// TestRootModeMapURLForeign pins the root-mounted rewrite: the binding is a path,
// and every other upstream is a reserved-route path with the foreign upstream's
// own resolution. Resolution runs against the foreign document, while the
// same-upstream check runs against the binding.
func TestRootModeMapURLForeign(t *testing.T) {
	tgt := target{StateKey: "tab:rw", Scheme: "http", Host: "localhost:9000", Path: "/app/", Local: true, RootMounted: true, Foreign: true, BindScheme: "http", BindHost: "127.0.0.1:8000"}
	cases := []struct {
		name, raw, want string
	}{
		{"foreign absolute keeps query and fragment", "http://localhost:9000/x?y=1#f", "/__ocode/b/http/localhost:9000/x?y=1#f"},
		{"binding absolute is a path", "http://127.0.0.1:8000/own?z=2", "/own?z=2"},
		{"root-relative resolves on the foreign upstream", "/x", "/__ocode/b/http/localhost:9000/x"},
		{"relative resolves against the foreign document", "img.png", "/__ocode/b/http/localhost:9000/app/img.png"},
		{"other scheme and host", "https://cdn.test/s.css", "/__ocode/b/https/cdn.test/s.css"},
		{"host is lowercased", "HTTPS://CDN.Test:443/a", "/__ocode/b/https/cdn.test:443/a"},
		{"untouched scheme", "data:image/png;base64,AA", "data:image/png;base64,AA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapURL(tc.raw, tgt, "", ""); got != tc.want {
				t.Fatalf("mapURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	out, err := rewriteHTML([]byte(`<html><head></head><body><img src="/a.png"><a href="http://127.0.0.1:8000/own">x</a></body></html>`), tgt, "", "")
	if err != nil {
		t.Fatalf("rewriteHTML: %v", err)
	}
	if s := string(out); !strings.Contains(s, `src="/__ocode/b/http/localhost:9000/a.png"`) || !strings.Contains(s, `href="/own"`) {
		t.Fatalf("foreign HTML rewrite wrong:\n%s", s)
	}
}

// TestTabForeignRefererIsBindingOrigin: a foreign upstream gets the binding's
// origin as its Referer, never the tab origin's URL or path. That is what
// strict-origin-when-cross-origin sends for a real cross-site request from the
// binding. An https binding sending to an http upstream is a downgrade, so no
// Referer goes out, and a request without one gets none.
func TestTabForeignRefererIsBindingOrigin(t *testing.T) {
	const tabPage = "http://127.0.0.1:3700/app/dashboard"
	const bind = "127.0.0.1:9" // never dialed: only foreign routes are requested
	cases := []struct {
		name, bindScheme, referer string
		wantSet                   bool
	}{
		{"tab URL becomes the binding origin", "http", tabPage, true},
		{"https binding to http foreign is a downgrade", "https", tabPage, false},
		{"no Referer stays absent", "http", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refs := make(chan string, 1)
			foreign, _ := foreignUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				select {
				case refs <- r.Header.Get("Referer"):
				default:
				}
				_, _ = io.WriteString(w, "ok")
			})
			s := tabNewServer(t)
			const sk = "tab:fref"
			h := s.tabHandler(tabKey{stateKey: sk, scheme: tc.bindScheme, host: bind})
			req := httptest.NewRequest(http.MethodGet, "/__ocode/b/http/"+localhostHost(foreign)+"/app", nil)
			req.Header.Set("Sec-Fetch-Dest", "script")
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			req.AddCookie(foreignSession(t, s, sk, true))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var got string
			select {
			case got = <-refs:
			case <-time.After(2 * time.Second):
				t.Fatal("foreign upstream received no request")
			}
			want := ""
			if tc.wantSet {
				want = tc.bindScheme + "://" + bind + "/"
			}
			if got != want {
				t.Fatalf("foreign Referer = %q, want %q", got, want)
			}
		})
	}
}

// TestTabBindingRefererForwardedVerbatim: a binding-upstream request keeps the
// Referer it arrived with. Only foreign routes are trimmed.
func TestTabBindingRefererForwardedVerbatim(t *testing.T) {
	const tabPage = "http://127.0.0.1:3700/app/dashboard"
	refs := make(chan string, 1)
	bound, _ := foreignUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case refs <- r.Header.Get("Referer"):
		default:
		}
		_, _ = io.WriteString(w, "ok")
	})
	s := tabNewServer(t)
	const sk = "tab:bref"
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: tabAddr(bound.URL)})
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Sec-Fetch-Dest", "script")
	req.Header.Set("Referer", tabPage)
	req.AddCookie(foreignSession(t, s, sk, true))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	select {
	case got := <-refs:
		if got != tabPage {
			t.Fatalf("binding Referer = %q, want %q verbatim", got, tabPage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("binding upstream received no request")
	}
}

// TestTabSelfRouteIsTheBinding: a reserved-route URL that names this listener is
// the binding at the route's path, so a static self-link is served without
// dialing the listener (which carries no cookie of its own). A document on that
// route is redirected to the binding's own path, so its location.pathname stays
// the upstream path. A route naming another host still reaches that host.
func TestTabSelfRouteIsTheBinding(t *testing.T) {
	bound, boundSeen := foreignUpstream(t, nil)
	s := tabNewServer(t)
	const sk = "tab:self"
	const self = "127.0.0.1:4711" // the listener's own address; nothing is listening there
	h := s.tabHandler(tabKey{stateKey: sk, scheme: "http", host: tabAddr(bound.URL)})
	cookie := foreignSession(t, s, sk, true)
	do := func(path, dest string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = self
		req.Header.Set("Sec-Fetch-Dest", dest)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	sub := do("/__ocode/b/http/"+self+"/x?y=1", "image")
	if sub.Code != http.StatusOK {
		t.Fatalf("self-route subresource status = %d, want 200", sub.Code)
	}
	if got := recvSeen(t, boundSeen); got.host != tabAddr(bound.URL) || got.path != "/x" || got.query != "y=1" {
		t.Fatalf("binding saw host=%q path=%q query=%q, want its own /x y=1", got.host, got.path, got.query)
	}

	doc := do("/__ocode/b/http/"+self+"/x?y=1", "document")
	if doc.Code != http.StatusTemporaryRedirect || doc.Header().Get("Location") != "/x?y=1" {
		t.Fatalf("self-route document = %d Location %q, want 307 to /x?y=1", doc.Code, doc.Header().Get("Location"))
	}
	expectNoRequest(t, boundSeen)

	foreign, foreignSeen := foreignUpstream(t, nil)
	if other := do("/__ocode/b/http/"+localhostHost(foreign)+"/z", "script"); other.Code != http.StatusOK {
		t.Fatalf("other-host status = %d, want 200", other.Code)
	}
	if got := recvSeen(t, foreignSeen); got.path != "/z" {
		t.Fatalf("other host saw path %q, want /z", got.path)
	}
}

// TestTabForeignWebSocketPassthrough: an upgrade on a foreign route is proxied
// to the foreign upstream, which is the only way a foreign socket can reach the page.
func TestTabForeignWebSocketPassthrough(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "expected upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("upstream ResponseWriter is not a Hijacker")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = buf.Flush()
		line, _ := buf.ReadString('\n')
		_, _ = buf.WriteString("echo:" + line)
		_ = buf.Flush()
	}))
	t.Cleanup(foreign.Close)
	fhost := localhostHost(foreign)

	s := tabNewServer(t)
	const sk = "tab:fws"
	// The binding is never dialed by this test, so its address is only a key.
	_, base, err := s.ListenTab(sk, "http", "127.0.0.1:9", "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("ListenTab: %v", err)
	}
	cookie := foreignSession(t, s, sk, true)
	addr := tabAddr(base)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := "GET /__ocode/b/http/" + fhost + "/ws HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n" +
		"Cookie: " + cookie.Name + "=" + cookie.Value + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	statusLine, _ := br.ReadString('\n')
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101 upgrade, got %q", statusLine)
	}
	for {
		line, _ := br.ReadString('\n')
		if line == "\r\n" || line == "" {
			break
		}
	}
	_, _ = conn.Write([]byte("ping\n"))
	echo, _ := br.ReadString('\n')
	if !strings.HasPrefix(echo, "echo:ping") {
		t.Fatalf("websocket echo failed: got %q", echo)
	}
}
