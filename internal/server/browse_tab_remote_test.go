package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse"
	"github.com/u007/ocode/internal/remote"
)

// remoteTabToken is the host's bearer token. The proxy injects it on the grant call,
// and it must never reach the browser, so every reply is scanned for it.
const remoteTabToken = "remote-token-must-not-leak"

// remoteTabHost is the saved project host the forwarded tabs open for.
const remoteTabHost = "user@realhost"

// remoteTabSPAOrigin is the SPA page's origin, as the browser sends it on POST.
const remoteTabSPAOrigin = "http://localhost:3510"

// remoteTabFixture is one remote host for the forwarded tab flow: a browse origin
// standing in for the host's, the host's API server (which mints and revokes grants
// on that origin, and checks the bearer token), and the upstream the host proxies.
type remoteTabFixture struct {
	s      *Server
	rb     *browse.Server
	ws     *browseProxyTestWorkspace
	upHost string

	mu      sync.Mutex
	revokes []string
}

func newRemoteTabFixture(t *testing.T, upstream http.Handler) *remoteTabFixture {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	f := &remoteTabFixture{upHost: strings.TrimPrefix(up.URL, "http://")}
	f.rb = browse.New("remote-browse-token", nil)
	t.Cleanup(func() { _ = f.rb.Close(context.Background()) })
	remoteBrowse := httptest.NewServer(f.rb.Handler())
	t.Cleanup(remoteBrowse.Close)
	api := httptest.NewServer(f.apiMux())
	t.Cleanup(api.Close)
	f.ws = &browseProxyTestWorkspace{
		fakeTestWorkspace: fakeTestWorkspace{apiURL: api.URL, token: remoteTabToken, state: remote.ServeState{BrowsePort: 1}},
		browseURL:         remoteBrowse.URL,
	}
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	s.handler = newTestProxyHandler(t, remoteTabHost, "/home/user/project", f.ws)
	connectTestHost(t, s.handler, remoteTabHost, "/home/user/project")
	f.s = s
	return f
}

// apiMux is the host's API as the proxy reaches it: grant and revoke on the host's
// browse origin, behind the bearer token.
func (f *remoteTabFixture) apiMux() http.Handler {
	mux := http.NewServeMux()
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+remoteTabToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/browse/grant", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		var req struct {
			StateKey string `json:"state_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"grant": f.rb.MintGrant(req.StateKey, r.Header.Get("Origin"))})
	})
	mux.HandleFunc("POST /api/browse/revoke", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		var req struct {
			StateKey string `json:"state_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.rb.Revoke(req.StateKey)
		f.mu.Lock()
		f.revokes = append(f.revokes, req.StateKey)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// revoked lists the state keys the host was asked to revoke, in order.
func (f *remoteTabFixture) revoked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revokes...)
}

// openTab asks this machine for a forwarded tab of stateKey onto upstreamURL on the
// host, the way the SPA does, and returns the reply.
func (f *remoteTabFixture) openTab(t *testing.T, stateKey, upstreamURL string) tabOriginReply {
	t.Helper()
	body := tabOriginBody(t, map[string]string{"state_key": stateKey, "url": upstreamURL})
	r := httptest.NewRequest(http.MethodPost, "/api/browse/tab-origin?host="+url.QueryEscape(remoteTabHost), strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:5555"
	r.Host = "127.0.0.1:4096"
	r.Header.Set("Origin", remoteTabSPAOrigin)
	w := httptest.NewRecorder()
	f.s.mux.ServeHTTP(w, r)
	return decodeTabOrigin(t, w)
}

// remoteTabNoRedirect is a client that returns redirects as replies.
func remoteTabNoRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// remoteTabPage is the upstream's app page: root-relative links and one foreign link.
const remoteTabPage = `<!doctype html><html><head><title>app</title></head><body><a href="/next">n</a><a href="%s">f</a><script type="module" src="/src/main.js"></script></body></html>`

// remoteTabUpstream serves the app page at /app/dashboard, and a 404 elsewhere.
func remoteTabUpstream(foreignHost string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/dashboard" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, remoteTabPage, "http://"+foreignHost+"/x")
	})
}

// remoteTabForeign is a second upstream, reached only through the reserved route.
func remoteTabForeign(t *testing.T) string {
	t.Helper()
	f := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "pong from foreign "+r.URL.Path)
	}))
	t.Cleanup(f.Close)
	return strings.TrimPrefix(f.URL, "http://")
}

// remoteTabGet makes one document request on the forwarded tab, and returns the
// reply with its body read.
func remoteTabGet(t *testing.T, target string, cookies ...*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	return remoteTabDo(t, target, "", cookies...)
}

// remoteTabSubresource makes one subresource request on the forwarded tab. A browser
// marks a subresource with Sec-Fetch-Dest, and without it the browse origin takes the
// request for a document.
func remoteTabSubresource(t *testing.T, target string, cookies ...*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	return remoteTabDo(t, target, "empty", cookies...)
}

func remoteTabDo(t *testing.T, target, secFetchDest string, cookies ...*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", target, err)
	}
	if secFetchDest != "" {
		req.Header.Set("Sec-Fetch-Dest", secFetchDest)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := remoteTabNoRedirect().Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	return resp, body
}

// remoteTabSessionCookie redeems the tab's grant and returns the session cookie the
// host set through the forwarding tab. The redirect must be root-relative and the
// cookie must sit at Path=/, with nothing rewritten onto the SPA's prefix.
func remoteTabSessionCookie(t *testing.T, rep tabOriginReply, stateKey, wantLocation string) *http.Cookie {
	t.Helper()
	resp, _ := remoteTabGet(t, rep.URL)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redeem status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != wantLocation {
		t.Fatalf("redeem Location = %q, want %q", loc, wantLocation)
	}
	for _, sc := range resp.Header.Values("Set-Cookie") {
		if strings.Contains(sc, "/api/remote/") || strings.Contains(sc, "Path=/b/") {
			t.Fatalf("Set-Cookie carries a prefix path: %q", sc)
		}
	}
	for _, c := range resp.Cookies() {
		if c.Name == browse.TabCookieName(stateKey) {
			if c.Path != "/" {
				t.Fatalf("session cookie Path = %q, want /", c.Path)
			}
			return c
		}
	}
	t.Fatalf("no session cookie %q in redeem reply", browse.TabCookieName(stateKey))
	return nil
}

// TestRemoteTabForwardsRootMountedPageThroughHost is the end-to-end path of a saved
// remote project: the tab origin is opened, the grant is redeemed on the host, the
// page is served root-mounted, a foreign upstream is reached through the reserved
// route, and the host's token appears in no reply.
func TestRemoteTabForwardsRootMountedPageThroughHost(t *testing.T) {
	foreignHost := remoteTabForeign(t)
	f := newRemoteTabFixture(t, remoteTabUpstream(foreignHost))
	const sk = "tab:remote"
	rep := f.openTab(t, sk, "http://"+f.upHost+"/app/dashboard?view=1")
	if !strings.HasPrefix(rep.URL, rep.Origin+"/app/dashboard?view=1&__grant=") {
		t.Fatalf("url = %q, want origin + upstream path, query and __grant", rep.URL)
	}
	if strings.Contains(rep.URL, remoteTabToken) || rep.Grant == remoteTabToken {
		t.Fatal("the host token reached the browser reply")
	}

	cookie := remoteTabSessionCookie(t, rep, sk, "/app/dashboard?view=1")
	seen := []string{rep.URL}

	doc, body := remoteTabGet(t, rep.Origin+"/app/dashboard?view=1", cookie)
	seen = append(seen, string(body))
	if doc.StatusCode != http.StatusOK {
		t.Fatalf("document status = %d body %q", doc.StatusCode, body)
	}
	for _, want := range []string{`rootMode:true`, `src="/__ocode_capture.js"`, `href="/next"`, `src="/src/main.js"`, `href="/__ocode/b/http/` + foreignHost + `/x"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("root-mounted document lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(string(body), "/b/"+sk) {
		t.Errorf("document carries the prefix route:\n%s", body)
	}

	capture, capBody := remoteTabGet(t, rep.Origin+"/__ocode_capture.js")
	seen = append(seen, string(capBody))
	if capture.StatusCode != http.StatusOK {
		t.Fatalf("capture bundle status = %d", capture.StatusCode)
	}

	foreign, fbody := remoteTabSubresource(t, rep.Origin+"/__ocode/b/http/"+foreignHost+"/api/ping", cookie)
	seen = append(seen, string(fbody))
	if foreign.StatusCode != http.StatusOK || string(fbody) != "pong from foreign /api/ping" {
		t.Fatalf("foreign through the host: status %d body %q", foreign.StatusCode, fbody)
	}

	for _, reply := range seen {
		if strings.Contains(reply, remoteTabToken) {
			t.Fatalf("the host token appears in a reply: %q", reply)
		}
	}
}

// TestRemoteTabOldHostFailsClosed: a host that did not answer in root mode is never
// relayed. The tab fails with a clear error that names the restart.
func TestRemoteTabOldHostFailsClosed(t *testing.T) {
	f := newRemoteTabFixture(t, remoteTabUpstream("unused.example"))
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>PREFIX-SHAPED-CONTENT</html>")
	}))
	t.Cleanup(old.Close)
	f.ws.browseURL = old.URL
	rep := f.openTab(t, "tab:old", "http://"+f.upHost+"/app/dashboard")
	resp, body := remoteTabGet(t, rep.URL)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d body %q, want 502", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "restart the host") {
		t.Fatalf("502 body %q does not name the restart", body)
	}
	if strings.Contains(string(body), "PREFIX-SHAPED") {
		t.Fatalf("content from a host without the marker was relayed: %q", body)
	}
}

// TestRemoteTabConnectingIsServiceUnavailable: while the host's connect runs, a
// forwarded request is 503 with Retry-After, never 502 and never a dial.
func TestRemoteTabConnectingIsServiceUnavailable(t *testing.T) {
	f := newRemoteTabFixture(t, remoteTabUpstream("unused.example"))
	rep := f.openTab(t, "tab:connecting", "http://"+f.upHost+"/app/dashboard")
	reg := f.s.handler.remoteHosts
	reg.drop(remoteTabHost, 0)

	release := make(chan struct{})
	var connects atomic.Int32
	reg.connect = func(remote.Target, string) (remoteHostWorkspace, error) {
		connects.Add(1)
		<-release
		return nil, errors.New("late")
	}
	go func() { _, _ = reg.workspaceForPort(remoteTabHost, "/home/user/project", 0) }()
	for connects.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	defer close(release)

	resp, body := remoteTabGet(t, rep.URL)
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") != "2" {
		t.Fatalf("status = %d Retry-After %q body %q, want 503 with Retry-After 2", resp.StatusCode, resp.Header.Get("Retry-After"), body)
	}
}

// TestRemoteTabPortKeyedByProjectHost: a local tab and a remote tab on the same
// upstream and state key get two listeners and two saved ports. Neither overwrites
// the other's entry.
func TestRemoteTabPortKeyedByProjectHost(t *testing.T) {
	f := newRemoteTabFixture(t, remoteTabUpstream("unused.example"))
	const sk = "tab:both"
	upstream := "http://" + f.upHost + "/app/dashboard"
	remoteRep := f.openTab(t, sk, upstream)

	local := decodeTabOrigin(t, postTabOrigin(f.s, tabOriginBody(t, map[string]string{"state_key": sk, "url": upstream}), "127.0.0.1:4096"))
	if originPort(t, local.Origin) == originPort(t, remoteRep.Origin) {
		t.Fatalf("local and remote tabs share port %d", originPort(t, local.Origin))
	}
	store := newTabPortStoreAt(f.s.tabOrigins.store.path)
	localPort, err := store.Get("", sk, "http", f.upHost)
	if err != nil || localPort != originPort(t, local.Origin) {
		t.Fatalf("local saved port = %d (%v), want %d", localPort, err, originPort(t, local.Origin))
	}
	remotePort, err := store.Get(remoteTabHost, sk, "http", f.upHost)
	if err != nil || remotePort != originPort(t, remoteRep.Origin) {
		t.Fatalf("remote saved port = %d (%v), want %d", remotePort, err, originPort(t, remoteRep.Origin))
	}
}

// TestRemoteTabRevokeClosesForwardersAndHostSession: a local revoke of a panel with a
// forwarded tab closes the forwarder, forgets its saved port, and asks the host to
// revoke its own session. The SPA's own revoke of a remote panel closes the
// forwarder too.
func TestRemoteTabRevokeClosesForwardersAndHostSession(t *testing.T) {
	f := newRemoteTabFixture(t, remoteTabUpstream("unused.example"))
	const sk = "tab:revoke"
	rep := f.openTab(t, sk, "http://"+f.upHost+"/app/dashboard")
	remoteTabSessionCookie(t, rep, sk, "/app/dashboard")

	revoke := httptest.NewRequest(http.MethodPost, "/api/browse/revoke", strings.NewReader(`{"state_key":"`+sk+`"}`))
	revoke.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	f.s.mux.ServeHTTP(w, revoke)
	if w.Code != http.StatusNoContent {
		t.Fatalf("local revoke: got %d want 204: %s", w.Code, w.Body.String())
	}
	if got := f.revoked(); len(got) != 1 || got[0] != sk {
		t.Fatalf("host revokes = %v, want [%s]", got, sk)
	}
	if _, err := http.Get(rep.Origin + "/app/dashboard"); err == nil {
		t.Fatal("forwarder still accepts connections after local revoke")
	}
	if p, _ := newTabPortStoreAt(f.s.tabOrigins.store.path).Get(remoteTabHost, sk, "http", f.upHost); p != 0 {
		t.Fatalf("persisted remote port survives revoke: %d", p)
	}

	rep = f.openTab(t, sk, "http://"+f.upHost+"/app/dashboard")
	spa := httptest.NewRequest(http.MethodPost, "/api/remote/"+remoteTabHost+"/api/browse/revoke", strings.NewReader(`{"state_key":"`+sk+`"}`))
	spa.SetPathValue("host", remoteTabHost)
	spa.SetPathValue("rest", "browse/revoke")
	spaW := httptest.NewRecorder()
	f.s.handleRemoteAPI(spaW, spa)
	if spaW.Code != http.StatusNoContent {
		t.Fatalf("remote revoke via SPA route: got %d want 204: %s", spaW.Code, spaW.Body.String())
	}
	if _, err := http.Get(rep.Origin + "/app/dashboard"); err == nil {
		t.Fatal("forwarder still accepts connections after the SPA's remote revoke")
	}
	if got := f.revoked(); len(got) != 2 {
		t.Fatalf("host revokes after SPA revoke = %v, want the SPA's call forwarded", got)
	}
}

// TestRemoteTabRevokeNeedsStateKey: a remote revoke that names no state key is refused
// before anything is forwarded to the host.
func TestRemoteTabRevokeNeedsStateKey(t *testing.T) {
	f := newRemoteTabFixture(t, remoteTabUpstream("unused.example"))
	spa := httptest.NewRequest(http.MethodPost, "/api/remote/"+remoteTabHost+"/api/browse/revoke", bytes.NewReader([]byte(`{}`)))
	spa.SetPathValue("host", remoteTabHost)
	spa.SetPathValue("rest", "browse/revoke")
	w := httptest.NewRecorder()
	f.s.handleRemoteAPI(w, spa)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("revoke without state_key: got %d want 400", w.Code)
	}
	if len(f.revoked()) != 0 {
		t.Fatalf("host was asked to revoke: %v", f.revoked())
	}
}
