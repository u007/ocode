package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/browse"
)

// tabOriginTestServer builds a server with the browse origin and the tab-origin
// manager wired, persisting ports under storePath. The browse server is closed
// at cleanup, which stops every tab listener it opened.
func tabOriginTestServer(t *testing.T, storePath string) (*Server, *browse.Server) {
	t.Helper()
	bs := browse.New("", nil)
	t.Cleanup(func() { _ = bs.Close(context.Background()) })
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:9999", bs)
	s.enableTabOrigins("127.0.0.1", newTabPortStoreAt(storePath))
	return s, bs
}

// tabOriginStorePath is a fresh tab-ports.json under a temp browse dir. Only the
// path is injected, so no HOME override is needed.
func tabOriginStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "browse", "tab-ports.json")
}

// postTabOrigin sends one tab-origin request from a loopback client, unless the
// test overrides RemoteAddr and the headers itself.
func postTabOrigin(s *Server, body, host string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/browse/tab-origin", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:5555"
	r.Host = host
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	return w
}

func tabOriginBody(t *testing.T, fields map[string]string) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(b)
}

type tabOriginReply struct {
	Origin string `json:"origin"`
	Grant  string `json:"grant"`
	URL    string `json:"url"`
}

func decodeTabOrigin(t *testing.T, w *httptest.ResponseRecorder) tabOriginReply {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("tab origin: got %d want 200: %s", w.Code, w.Body.String())
	}
	var rep tabOriginReply
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode tab origin reply: %v", err)
	}
	if rep.Origin == "" || rep.Grant == "" || rep.URL == "" {
		t.Fatalf("tab origin reply incomplete: %+v", rep)
	}
	return rep
}

func originPort(t *testing.T, origin string) int {
	t.Helper()
	u, err := url.Parse(origin)
	if err != nil {
		t.Fatalf("parse origin %q: %v", origin, err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("origin %q has no port: %v", origin, err)
	}
	return p
}

func TestTabOriginReusesOriginForSameBinding(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	body := tabOriginBody(t, map[string]string{"state_key": "tab:one", "url": "http://app.example:3000/app/dashboard?view=1"})
	first := decodeTabOrigin(t, postTabOrigin(s, body, "127.0.0.1:4096"))
	second := decodeTabOrigin(t, postTabOrigin(s, body, "127.0.0.1:4096"))
	if first.Origin != second.Origin {
		t.Fatalf("same binding got two origins: %q then %q", first.Origin, second.Origin)
	}
	if !strings.HasPrefix(first.URL, first.Origin+"/app/dashboard?view=1&__grant=") {
		t.Fatalf("url = %q, want origin + upstream path/query + __grant", first.URL)
	}
}

func TestTabOriginDifferentUpstreamHostGetsDifferentOrigin(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	a := decodeTabOrigin(t, postTabOrigin(s, tabOriginBody(t, map[string]string{"state_key": "tab:one", "url": "http://a.example:3000/"}), "127.0.0.1:4096"))
	b := decodeTabOrigin(t, postTabOrigin(s, tabOriginBody(t, map[string]string{"state_key": "tab:one", "url": "http://b.example:3000/"}), "127.0.0.1:4096"))
	if a.Origin == b.Origin {
		t.Fatalf("two upstream hosts share origin %q", a.Origin)
	}
}

// The default port is part of the same origin, so one binding serves both.
func TestTabOriginDefaultPortSharesOneBinding(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	a := decodeTabOrigin(t, postTabOrigin(s, tabOriginBody(t, map[string]string{"state_key": "tab:one", "url": "https://a.example:443/x"}), "127.0.0.1:4096"))
	b := decodeTabOrigin(t, postTabOrigin(s, tabOriginBody(t, map[string]string{"state_key": "tab:one", "url": "https://a.example/y"}), "127.0.0.1:4096"))
	if a.Origin != b.Origin {
		t.Fatalf("https default port made a second binding: %q vs %q", a.Origin, b.Origin)
	}
}

func TestTabOriginSavedPortReusedAfterRestart(t *testing.T) {
	store := tabOriginStorePath(t)
	body := tabOriginBody(t, map[string]string{"state_key": "tab:restart", "url": "http://app.example:3000/"})

	s1, bs1 := tabOriginTestServer(t, store)
	first := decodeTabOrigin(t, postTabOrigin(s1, body, "127.0.0.1:4096"))
	// Shutdown: browse.Close stops the listeners; the in-memory map is dropped;
	// the persisted port stays.
	if err := bs1.Close(context.Background()); err != nil {
		t.Fatalf("close browse: %v", err)
	}
	s1.dropLiveTabOrigins()

	s2, _ := tabOriginTestServer(t, store)
	second := decodeTabOrigin(t, postTabOrigin(s2, body, "127.0.0.1:4096"))
	if second.Origin != first.Origin {
		t.Fatalf("restart changed the origin: %q then %q", first.Origin, second.Origin)
	}
}

func TestTabOriginBusySavedPortFallsBackAndPersists(t *testing.T) {
	store := tabOriginStorePath(t)
	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold port: %v", err)
	}
	t.Cleanup(func() { _ = hold.Close() })
	busy := hold.Addr().(*net.TCPAddr).Port

	seed := newTabPortStoreAt(store)
	if err := seed.Put(tabPortEntry{StateKey: "tab:busy", Scheme: "http", Host: "busy.example:3000", Port: busy, UsedAtMs: 1}); err != nil {
		t.Fatalf("seed port: %v", err)
	}

	s, _ := tabOriginTestServer(t, store)
	rep := decodeTabOrigin(t, postTabOrigin(s, tabOriginBody(t, map[string]string{"state_key": "tab:busy", "url": "http://busy.example:3000/"}), "127.0.0.1:4096"))
	got := originPort(t, rep.Origin)
	if got == busy {
		t.Fatalf("origin reuses the busy port %d", busy)
	}
	saved, err := newTabPortStoreAt(store).Get("", "tab:busy", "http", "busy.example:3000")
	if err != nil {
		t.Fatalf("read saved port: %v", err)
	}
	if saved != got {
		t.Fatalf("persisted port = %d, want the bound port %d", saved, got)
	}
}

func TestTabOriginNonLocalClientIsConflict(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	body := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": "http://app.example:3000/"})

	r := httptest.NewRequest(http.MethodPost, "/api/browse/tab-origin", strings.NewReader(body))
	r.RemoteAddr = "192.168.1.20:5555"
	r.Host = "192.168.1.20:4096"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("LAN client: got %d want 409: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "tab origin unavailable: ") {
		t.Fatalf("409 body lacks reason: %s", w.Body.String())
	}
}

// A tunnelled client arrives from loopback, but a proxy header marks it as
// proxied, and the shared origin is the right answer for it.
func TestTabOriginProxiedLoopbackClientIsConflict(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	body := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": "http://app.example:3000/"})
	r := httptest.NewRequest(http.MethodPost, "/api/browse/tab-origin", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:5555"
	r.Host = "127.0.0.1:4096"
	r.Header.Set("X-Forwarded-For", "100.64.0.9")
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("proxied client: got %d want 409", w.Code)
	}
}

// A remote host is served through a forwarding tab only when it is a saved project.
// A host that is not saved is refused by both spellings of the host parameter, and
// no listener is opened for it.
func TestTabOriginUnsavedRemoteHostIsForbidden(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	bodyField := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": "http://app.example:3000/", "host": "aimsai2"})
	if w := postTabOrigin(s, bodyField, "127.0.0.1:4096"); w.Code != http.StatusForbidden {
		t.Fatalf("body host: got %d want 403: %s", w.Code, w.Body.String())
	}
	plain := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": "http://app.example:3000/"})
	r := httptest.NewRequest(http.MethodPost, "/api/browse/tab-origin?host=aimsai2", strings.NewReader(plain))
	r.RemoteAddr = "127.0.0.1:5555"
	r.Host = "127.0.0.1:4096"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("query host: got %d want 403: %s", w.Code, w.Body.String())
	}
	s.tabOrigins.mu.Lock()
	live := len(s.tabOrigins.live)
	s.tabOrigins.mu.Unlock()
	if live != 0 {
		t.Fatalf("a listener was opened for an unsaved host: %d live", live)
	}
}

// A body host and a query host that name different hosts are a bad request.
func TestTabOriginConflictingHostsAreBadRequest(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	body := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": "http://app.example:3000/", "host": "aimsai2"})
	r := httptest.NewRequest(http.MethodPost, "/api/browse/tab-origin?host=other", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:5555"
	r.Host = "127.0.0.1:4096"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("conflicting hosts: got %d want 400", w.Code)
	}
}

func TestTabOriginRemoteServerIsConflict(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	s.remoteMode = true
	body := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": "http://app.example:3000/"})
	// authMiddleware demands a token on a remote server, so the gate is driven
	// directly: a remote server's own loopback client must still get 409.
	r := httptest.NewRequest(http.MethodPost, "/api/browse/tab-origin", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:5555"
	r.Host = "127.0.0.1:4096"
	w := httptest.NewRecorder()
	s.handleBrowseTabOrigin(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("remote-mode server: got %d want 409", w.Code)
	}
}

func TestTabOriginBadURLIsBadRequest(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	cases := map[string]string{
		"empty":        "",
		"ftp scheme":   "ftp://x.example/",
		"javascript":   "javascript:alert(1)",
		"credentials":  "http://user:pw@x.example/",
		"no host":      "http:///path",
		"empty port":   "http://x.example:/",
		"relative url": "not a url",
	}
	for name, u := range cases {
		body := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": u})
		if w := postTabOrigin(s, body, "127.0.0.1:4096"); w.Code != http.StatusBadRequest {
			t.Fatalf("%s (%q): got %d want 400", name, u, w.Code)
		}
	}
	if w := postTabOrigin(s, tabOriginBody(t, map[string]string{"url": "http://x.example/"}), "127.0.0.1:4096"); w.Code != http.StatusBadRequest {
		t.Fatalf("missing state_key: got %d want 400", w.Code)
	}
}

func TestTabOriginUnwiredIsUnavailable(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	body := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": "http://app.example:3000/"})
	if w := postTabOrigin(s, body, "127.0.0.1:4096"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no browse origin: got %d want 503", w.Code)
	}
}

// The origin must match the host the SPA loaded, so a page on localhost frames a
// same-site origin. A 127.0.0.1 origin would be cross-site and drop the cookie.
func TestTabOriginKeepsSameSiteLoopbackHost(t *testing.T) {
	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	body := tabOriginBody(t, map[string]string{"state_key": "tab:x", "url": "http://app.example:3000/"})
	rep := decodeTabOrigin(t, postTabOrigin(s, body, "localhost:4096"))
	u, err := url.Parse(rep.Origin)
	if err != nil {
		t.Fatalf("parse origin: %v", err)
	}
	if u.Hostname() != "localhost" {
		t.Fatalf("origin host = %q, want localhost (same site as the SPA)", u.Hostname())
	}
}

// The grant in the reply redeems on the tab origin, sets the per-tab session,
// and the upstream path is served there unchanged. The grant is one-time.
func TestTabOriginGrantRedeemsToUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "upstream path=%s query=%s", r.URL.Path, r.URL.RawQuery)
	}))
	t.Cleanup(upstream.Close)

	s, _ := tabOriginTestServer(t, tabOriginStorePath(t))
	body := tabOriginBody(t, map[string]string{"state_key": "tab:grant", "url": upstream.URL + "/app/dashboard?view=1"})
	rep := decodeTabOrigin(t, postTabOrigin(s, body, "127.0.0.1:4096"))

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	resp, err := client.Get(rep.URL)
	if err != nil {
		t.Fatalf("GET tab url: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redeem: got %d want 200: %s", resp.StatusCode, got)
	}
	want := "upstream path=/app/dashboard query=view=1"
	if string(got) != want {
		t.Fatalf("upstream body = %q, want %q", got, want)
	}

	// Grants are one-time: a second use from a client with no cookie is refused.
	fresh, err := http.Get(rep.URL)
	if err != nil {
		t.Fatalf("GET replayed grant: %v", err)
	}
	fresh.Body.Close()
	if fresh.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed grant: got %d want 401", fresh.StatusCode)
	}
}

func TestTabOriginStoreMergeKeepsOtherKeys(t *testing.T) {
	store := tabOriginStorePath(t)
	storeA := newTabPortStoreAt(store)
	storeB := newTabPortStoreAt(store)
	if err := storeA.Put(tabPortEntry{StateKey: "tab:a", Scheme: "http", Host: "a.example:1", Port: 1001, UsedAtMs: 1}); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := storeB.Put(tabPortEntry{StateKey: "tab:b", Scheme: "http", Host: "b.example:2", Port: 1002, UsedAtMs: 2}); err != nil {
		t.Fatalf("put b: %v", err)
	}
	if err := storeB.DeleteState("tab:b"); err != nil {
		t.Fatalf("delete b: %v", err)
	}
	reader := newTabPortStoreAt(store)
	if p, err := reader.Get("", "tab:a", "http", "a.example:1"); err != nil || p != 1001 {
		t.Fatalf("a after other-process writes: port=%d err=%v, want 1001", p, err)
	}
	if p, err := reader.Get("", "tab:b", "http", "b.example:2"); err != nil || p != 0 {
		t.Fatalf("b after delete: port=%d err=%v, want 0", p, err)
	}
}

func TestTabOriginStoreEvictsLeastRecentlyUsed(t *testing.T) {
	store := newTabPortStoreAt(tabOriginStorePath(t))
	for i := 0; i <= maxPersistedTabPorts; i++ {
		e := tabPortEntry{StateKey: fmt.Sprintf("tab:%d", i), Scheme: "http", Host: "a.example:1", Port: 2000 + i, UsedAtMs: int64(i + 1)}
		if err := store.Put(e); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	m, err := store.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(m) != maxPersistedTabPorts {
		t.Fatalf("entries = %d, want %d", len(m), maxPersistedTabPorts)
	}
	if p, _ := store.Get("", "tab:0", "http", "a.example:1"); p != 0 {
		t.Fatalf("least recent entry survived with port %d", p)
	}
	if p, _ := store.Get("", fmt.Sprintf("tab:%d", maxPersistedTabPorts), "http", "a.example:1"); p == 0 {
		t.Fatal("most recent entry was evicted")
	}
}

func TestTabOriginRevokeDropsPersistedPort(t *testing.T) {
	store := tabOriginStorePath(t)
	s, _ := tabOriginTestServer(t, store)
	body := tabOriginBody(t, map[string]string{"state_key": "tab:rev", "url": "http://app.example:3000/"})
	rep := decodeTabOrigin(t, postTabOrigin(s, body, "127.0.0.1:4096"))
	if p := originPort(t, rep.Origin); p == 0 {
		t.Fatal("no port bound")
	}

	revoke := httptest.NewRequest(http.MethodPost, "/api/browse/revoke", strings.NewReader(`{"state_key":"tab:rev"}`))
	revoke.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, revoke)
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke: got %d want 204", w.Code)
	}
	if p, err := newTabPortStoreAt(store).Get("", "tab:rev", "http", "app.example:3000"); err != nil || p != 0 {
		t.Fatalf("persisted port after revoke: port=%d err=%v, want 0", p, err)
	}
	s.tabOrigins.mu.Lock()
	live := len(s.tabOrigins.live)
	s.tabOrigins.mu.Unlock()
	if live != 0 {
		t.Fatalf("live bindings after revoke = %d, want 0", live)
	}
}
