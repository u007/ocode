package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/browse"
	"github.com/u007/ocode/internal/remote"
)

// TestRemoteBrowseEndToEndFollowsRedirectLikeABrowser is the end-to-end
// regression test for the remote-project blank browser panel.
//
// It wires a REAL browse origin behind the REAL remote browse proxy and then
// replays what a browser actually does:
//
//  1. GET the iframe src with a grant (the SPA's first navigation).
//  2. Follow the 302 Location, resolved against the CURRENT origin —
//     exactly as a browser resolves a root-relative Location.
//  3. GET the capture script by the path the injected <script src> carries
//     (prefixed under the mount), with the session cookie.
//
// Before the fix, step 2 left the "/api/remote/<host>/browse" mount and hit
// the proxy's LOCAL "/b/" route, so the origin rejected it with 401 and the
// panel stayed blank and spun forever. Step 3 failed for the same reason:
// the session cookie was scoped to "/b/" and was therefore never sent for
// the root-relative capture-script path.
//
// A real browser is not simulated on purpose: the failure mode IS the
// redirect resolution, so the test resolves the Location exactly as a
// browser does (current origin + Location) and asserts the whole path works.
func TestRemoteBrowseEndToEndFollowsRedirectLikeABrowser(t *testing.T) {
	const host = "user@realhost"
	const mount = "/api/remote/" + host + "/browse"

	// The REAL browse origin, serving the host's content.
	origin := browse.New("apitoken", nil)
	originLn, originBase, err := origin.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("origin listen: %v", err)
	}
	originHS := &http.Server{Handler: origin.Handler()}
	go func() { _ = originHS.Serve(originLn) }()
	defer originHS.Close()

	ws := &browseProxyTestWorkspace{
		fakeTestWorkspace: fakeTestWorkspace{state: remote.ServeState{BrowsePort: 1}},
		browseURL:         originBase,
	}
	h := newTestProxyHandler(t, host, "/home/user/project", ws)
	connectTestHost(t, h, host, "/home/user/project")
	s := &Server{handler: h, rl: newRateLimiter()}

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer proxyLn.Close()
	proxyPort := proxyLn.Addr().(*net.TCPAddr).Port
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/api/remote/{host}/browse/{rest...}", s.browseProxyGate(func(w http.ResponseWriter, r *http.Request) {
		s.handleRemoteBrowseProxy(w, r, r.PathValue("host"))
	}))
	// The proxy ALSO serves the LOCAL "/b/" route, exactly like the real
	// second listener. The remote flow must never reach it.
	proxyMux.Handle("/b/", s.browseProxyGate(func(w http.ResponseWriter, r *http.Request) {
		s.browseProxy.ServeHTTP(w, r)
	}))
	proxyHS := &http.Server{Handler: proxyMux}
	go func() { _ = proxyHS.Serve(proxyLn) }()
	defer proxyHS.Close()

	proxyOrigin := "http://127.0.0.1:" + strconv.Itoa(proxyPort)

	// The SPA mints the grant on the HOST's origin (the remote API proxy
	// forwards POST /api/browse/grant to the host's server).
	grant := origin.MintGrant("tab:e2e", proxyOrigin)

	// Step 1: the iframe's first navigation, carrying the grant.
	// The redirect must NOT be auto-followed: the test has to read the
	// Location and the Set-Cookie the redemption produced.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	first := proxyOrigin + mount + "/b/tab:e2e/https/example.com/?__grant=" + grant
	resp, err := noRedirect.Get(first)
	if err != nil {
		t.Fatalf("step 1 get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("step 1: status = %d, want 302 (grant redemption)", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("step 1: no Location header")
	}
	// The redirect must stay inside the mount prefix.
	if !strings.HasPrefix(loc, mount+"/b/") {
		t.Fatalf("step 1: Location %q left the mount %q", loc, mount)
	}
	// The session cookie must cover the whole mount (so it is also sent for
	// the root-relative capture-script path).
	var sessionCookie string
	for _, c := range resp.Cookies() {
		if c.Name == "ocode_browse" {
			sessionCookie = c.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("step 1: no session cookie set")
	}

	// Step 2: the browser follows the Location, resolved against the CURRENT
	// origin (a root-relative Location keeps the origin and takes the path).
	followed := proxyOrigin + loc
	req, _ := http.NewRequest("GET", followed, nil)
	req.AddCookie(&http.Cookie{Name: "ocode_browse", Value: sessionCookie})
	noRedirect2 := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp2, err := noRedirect2.Do(req)
	if err != nil {
		t.Fatalf("step 2 get: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode == http.StatusUnauthorized || resp2.StatusCode == http.StatusForbidden {
		t.Fatalf("step 2: status = %d — the redirect left the mount and hit the wrong origin", resp2.StatusCode)
	}
	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("step 2: status = %d, want the document to load", resp2.StatusCode)
	}

	// Step 3: the capture script, fetched by the path the injected
	// <script src> carries. Under a mount that path is PREFIXED — the
	// browser resolves it against the current origin, so it must stay
	// inside the mount and reach the host's origin.
	// injected <script src> does) with the session cookie.
	req3, _ := http.NewRequest("GET", proxyOrigin+mount+"/__ocode_capture.js", nil)
	req3.AddCookie(&http.Cookie{Name: "ocode_browse", Value: sessionCookie})
	noRedirect3 := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp3, err := noRedirect3.Do(req3)
	if err != nil {
		t.Fatalf("step 3 get: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("step 3: capture script status = %d, want 200", resp3.StatusCode)
	}
}

// TestRemoteBrowseProxyLocalRouteIsNotUsedByRemoteFlow asserts the remote
// flow never falls through to the LOCAL "/b/" route — the proxy's local
// route serves THIS machine's origin, which is a different machine from the
// remote host's.
func TestRemoteBrowseProxyLocalRouteIsNotUsedByRemoteFlow(t *testing.T) {
	const host = "user@realhost"
	const mount = "/api/remote/" + host + "/browse"

	origin := browse.New("apitoken", nil)
	originLn, originBase, err := origin.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("origin listen: %v", err)
	}
	originHS := &http.Server{Handler: origin.Handler()}
	go func() { _ = originHS.Serve(originLn) }()
	defer originHS.Close()

	ws := &browseProxyTestWorkspace{
		fakeTestWorkspace: fakeTestWorkspace{state: remote.ServeState{BrowsePort: 1}},
		browseURL:         originBase,
	}
	h := newTestProxyHandler(t, host, "/home/user/project", ws)
	connectTestHost(t, h, host, "/home/user/project")
	s := &Server{handler: h, rl: newRateLimiter()}

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer proxyLn.Close()
	proxyPort := proxyLn.Addr().(*net.TCPAddr).Port
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/api/remote/{host}/browse/{rest...}", s.browseProxyGate(func(w http.ResponseWriter, r *http.Request) {
		s.handleRemoteBrowseProxy(w, r, r.PathValue("host"))
	}))
	// The local route is a trap: if the remote flow ever lands here, it has
	// silently switched to this machine's origin.
	localHit := false
	proxyMux.Handle("/b/", s.browseProxyGate(func(w http.ResponseWriter, r *http.Request) {
		localHit = true
		w.WriteHeader(http.StatusTeapot)
	}))
	proxyHS := &http.Server{Handler: proxyMux}
	go func() { _ = proxyHS.Serve(proxyLn) }()
	defer proxyHS.Close()

	proxyOrigin := "http://127.0.0.1:" + strconv.Itoa(proxyPort)
	grant := origin.MintGrant("tab:trap", proxyOrigin)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(proxyOrigin + mount + "/b/tab:trap/https/example.com/?__grant=" + grant)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("no Location header")
	}
	// Follow the redirect the way a browser does.
	req, _ := http.NewRequest("GET", proxyOrigin+loc, nil)
	noRedirect2 := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp2, err := noRedirect2.Do(req)
	if err != nil {
		t.Fatalf("follow get: %v", err)
	}
	defer resp2.Body.Close()
	if localHit {
		t.Fatalf("the remote flow fell through to the LOCAL /b/ route (Location %q)", loc)
	}
	if resp2.StatusCode == http.StatusTeapot {
		t.Fatalf("the remote flow hit the local-route trap (Location %q)", loc)
	}
}

// unused import guard: httptest is used by the sibling proxy tests.
var _ = httptest.NewRequest
