# Part 01 — Browse Server Scaffold + Auth

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ Security model, Backend proxy).

**Goal:** Stand up the separate browse-origin `http.Server`, the stateless route parser, the config endpoint on the main server that hands the SPA the browse base URL, and the grant→`HttpOnly` cookie exchange. No upstream fetching yet — a parsed target returns a stub `200` so the pipeline is testable end to end.

**Files:**
- Create: `internal/browse/server.go`, `internal/browse/route.go`, `internal/browse/route_test.go`, `internal/browse/auth.go`, `internal/browse/auth_test.go`, `internal/browse/server_test.go`
- Modify: `internal/server/server.go` (add `GET /api/browse/config` + `POST /api/browse/grant`, hold a `*browse.Server` ref), `internal/desktop/boot.go` (start the browse listener alongside the main one)

**Interfaces:**
- Produces (per INDEX contract): `browse.New`, `(*Server).Handler`, `(*Server).Listen`, `(*Server).MintGrant`, `(*Server).SetNavPublisher`, `NavEvent`, `target`, `parseTarget`.
- Consumes: nothing from other parts.

---

- [ ] **Step 1: Write the failing route-parser test**

Create `internal/browse/route_test.go`:

```go
package browse

import "testing"

func TestParseTarget(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		query   string
		want    target
		wantErr bool
	}{
		{
			name:  "external https",
			path:  "/b/tab:abc/https/example.com/foo/bar",
			query: "q=1",
			want:  target{StateKey: "tab:abc", Scheme: "https", Host: "example.com", Path: "/foo/bar", RawQuery: "q=1", Local: false},
		},
		{
			name:  "root path",
			path:  "/b/side:chat:s1/https/example.com",
			query: "",
			want:  target{StateKey: "side:chat:s1", Scheme: "https", Host: "example.com", Path: "/", RawQuery: "", Local: false},
		},
		{
			name:  "loopback host is local",
			path:  "/b/tab:x/http/127.0.0.1:5173/",
			query: "",
			want:  target{StateKey: "tab:x", Scheme: "http", Host: "127.0.0.1:5173", Path: "/", RawQuery: "", Local: true},
		},
		{name: "missing scheme", path: "/b/tab:x", wantErr: true},
		{name: "bad prefix", path: "/x/tab:x/https/example.com", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseTarget(c.path, c.query)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestParseTarget`
Expected: FAIL — `undefined: parseTarget` / `undefined: target`.

- [ ] **Step 3: Implement the route parser**

Create `internal/browse/route.go`:

```go
package browse

import (
	"fmt"
	"net/netip"
	"strings"
)

// target is the stateless parse of /b/{stateKey}/{scheme}/{host}/{path...}.
// No server-side "current upstream" state exists; every request self-describes
// its upstream, so concurrent subresources and back/forward never race.
type target struct {
	StateKey string
	Scheme   string
	Host     string // may include :port
	Path     string // always starts with "/"
	RawQuery string
	Local    bool // host resolves to loopback/RFC1918 by literal inspection
}

// parseTarget splits the browse path. Shape: /b/{stateKey}/{scheme}/{host}/{rest...}
func parseTarget(urlPath, rawQuery string) (target, error) {
	rest, ok := strings.CutPrefix(urlPath, "/b/")
	if !ok {
		return target{}, fmt.Errorf("browse: path missing /b/ prefix: %q", urlPath)
	}
	// stateKey / scheme / host / path... — stateKey and scheme contain no slash.
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 {
		return target{}, fmt.Errorf("browse: path too short: %q", urlPath)
	}
	stateKey, scheme, host := parts[0], parts[1], parts[2]
	if stateKey == "" || (scheme != "http" && scheme != "https") || host == "" {
		return target{}, fmt.Errorf("browse: malformed target: %q", urlPath)
	}
	path := "/"
	if len(parts) == 4 && parts[3] != "" {
		path = "/" + parts[3]
	}
	return target{
		StateKey: stateKey,
		Scheme:   scheme,
		Host:     host,
		Path:     path,
		RawQuery: rawQuery,
		Local:    hostIsLiteralPrivate(host),
	}, nil
}

// hostIsLiteralPrivate reports whether host is an IP literal in a private
// range (best-effort literal check for routing mode only; the authoritative
// SSRF guard runs at dial time in Part 02). Hostnames return false here.
func hostIsLiteralPrivate(host string) bool {
	h := host
	if i := strings.LastIndex(h, ":"); i != -1 && !strings.Contains(h[i+1:], "]") {
		h = h[:i]
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	return isPrivateIP(addr)
}
```

- [ ] **Step 4: Run to verify pass (defines `isPrivateIP` stub inline for now)**

Add a minimal `isPrivateIP` so the package compiles — it will be replaced/expanded in Part 02. Append to `route.go`:

```go
// isPrivateIP is the enumerated private-range check. Part 02 tests it
// exhaustively; defined here so Part 01 compiles and routes correctly.
func isPrivateIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	// CGNAT 100.64.0.0/10.
	if ip.Is4() {
		b := ip.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
	}
	return false
}
```

Run: `go test ./internal/browse/ -run TestParseTarget`
Expected: PASS.

- [ ] **Step 5: Write the failing grant/cookie auth test**

Create `internal/browse/auth_test.go`:

```go
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
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestGrant`
Expected: FAIL — `undefined: New` / `browseCookie`.

- [ ] **Step 7: Implement server + auth**

Create `internal/browse/auth.go`:

```go
package browse

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const browseCookie = "ocode_browse"

type grantEntry struct {
	stateKey string
	expires  time.Time
}

// authStore holds one-time grants and live browse sessions. Grants are
// minted by the main server (after it has authenticated the API token) and
// redeemed exactly once for a scoped HttpOnly cookie confined to /b/.
type authStore struct {
	mu       sync.Mutex
	grants   map[string]grantEntry
	sessions map[string]string // cookie value -> stateKey
}

func newAuthStore() *authStore {
	return &authStore{grants: map[string]grantEntry{}, sessions: map[string]string{}}
}

func randToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is unrecoverable and must not be swallowed.
		panic("browse: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (a *authStore) mint(stateKey string) string {
	tok := randToken()
	a.mu.Lock()
	a.grants[tok] = grantEntry{stateKey: stateKey, expires: time.Now().Add(60 * time.Second)}
	a.mu.Unlock()
	return tok
}

// redeem consumes a grant (one-time) and returns a new session cookie value.
func (a *authStore) redeem(grant string) (cookie, stateKey string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, present := a.grants[grant]
	if !present || time.Now().After(e.expires) {
		delete(a.grants, grant)
		return "", "", false
	}
	delete(a.grants, grant)
	cookie = randToken()
	a.sessions[cookie] = e.stateKey
	return cookie, e.stateKey, true
}

func (a *authStore) sessionStateKey(cookie string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sk, ok := a.sessions[cookie]
	return sk, ok
}

func (a *authStore) revoke(stateKey string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for c, sk := range a.sessions {
		if sk == stateKey {
			delete(a.sessions, c)
		}
	}
}

// authenticate resolves the request to a stateKey, redeeming a ?__grant= on
// first navigation. Returns (stateKey, redirectURL, ok). When redirectURL is
// non-empty the caller must 302 there after setting the returned cookie.
func (a *authStore) authenticate(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if g := r.URL.Query().Get("__grant"); g != "" {
		cookieVal, stateKey, ok := a.redeem(g)
		if !ok {
			return "", "", false
		}
		http.SetCookie(w, &http.Cookie{
			Name: browseCookie, Value: cookieVal, Path: "/b/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		clean := *r.URL
		q := clean.Query()
		q.Del("__grant")
		clean.RawQuery = q.Encode()
		return stateKey, clean.String(), true
	}
	c, err := r.Cookie(browseCookie)
	if err != nil {
		return "", "", false // no cookie, no grant: reject.
	}
	sk, ok := a.sessionStateKey(c.Value)
	return sk, "", ok
}
```

Create `internal/browse/server.go`:

```go
package browse

import (
	"log"
	"net"
	"net/http"
)

// NavEvent is the server-authoritative address-bar / status update. The SPA
// renders these and NEVER page-reported URLs (spoofing defense).
type NavEvent struct {
	StateKey string `json:"state_key"`
	URL      string `json:"url"`
	Status   int    `json:"status"`
	Mode     string `json:"mode"` // "local" | "proxied"
	Error    string `json:"error,omitempty"`
}

// Server is the isolated browse origin. Proxied content is served only here,
// cross-origin to the ocode SPA, so page scripts cannot reach the SPA DOM,
// token, or /api/*.
type Server struct {
	apiToken string
	auth     *authStore
	log      *log.Logger
	mux      *http.ServeMux
	publish  func(stateKey string, ev NavEvent)
}

func New(apiToken string, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	s := &Server{apiToken: apiToken, auth: newAuthStore(), log: logger, mux: http.NewServeMux()}
	s.mux.HandleFunc("/b/", s.handleBrowse)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) MintGrant(stateKey string) string { return s.auth.mint(stateKey) }

func (s *Server) Revoke(stateKey string) { s.auth.revoke(stateKey) }

func (s *Server) SetNavPublisher(fn func(stateKey string, ev NavEvent)) { s.publish = fn }

func (s *Server) emitNav(ev NavEvent) {
	if s.publish != nil {
		s.publish(ev.StateKey, ev)
	}
}

// Listen binds addr and returns the listener plus the base URL the SPA uses.
func (s *Server) Listen(addr string) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	return ln, "http://" + ln.Addr().String(), nil
}

// handleBrowse is the single entrypoint. Part 01 authenticates and returns a
// stub 200 for a valid target; Parts 03/06 replace the stub with real
// external/local proxying.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	stateKey, redirect, ok := s.auth.authenticate(w, r)
	if !ok {
		http.Error(w, "browse: unauthorized", http.StatusUnauthorized)
		return
	}
	if redirect != "" {
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}
	t, err := parseTarget(r.URL.Path, r.URL.RawQuery)
	if err != nil {
		s.log.Printf("browse: parseTarget failed for %q: %v", r.URL.Path, err)
		http.Error(w, "browse: bad target", http.StatusBadRequest)
		return
	}
	if t.StateKey != stateKey {
		// Cookie belongs to a different panel; never cross state keys.
		http.Error(w, "browse: state key mismatch", http.StatusForbidden)
		return
	}
	// Stub until Part 03/06.
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("browse ok: " + t.Scheme + "://" + t.Host + t.Path))
}
```

- [ ] **Step 8: Run to verify pass**

Run: `go test ./internal/browse/`
Expected: PASS (route + auth tests).

- [ ] **Step 9: Write the failing wiring test on the main server**

Create `internal/browse/server_test.go` addition — but the config/grant endpoints live on the main server. Add to `internal/server/server_browse_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowseConfigEndpoint(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil) // no-auth loopback form
	s.EnableBrowse("http://127.0.0.1:54321")
	r := httptest.NewRequest("GET", "/api/browse/config", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var body struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.BaseURL != "http://127.0.0.1:54321" {
		t.Fatalf("base_url = %q", body.BaseURL)
	}
}
```

(Confirm the `New` signature against `internal/server/server.go` — adjust arg list to the real constructor; the point is a `New` + `EnableBrowse` + mux round-trip.)

- [ ] **Step 10: Run to verify it fails**

Run: `go test ./internal/server/ -run TestBrowseConfig`
Expected: FAIL — `EnableBrowse` undefined.

- [ ] **Step 11: Wire the main server**

In `internal/server/server.go`: add a `browse *browse.Server` field and a `browseBase string` to `Server`; add:

```go
// EnableBrowse records the browse-origin base URL and registers the SPA-facing
// config + grant endpoints. Called from the desktop/CLI boot after the browse
// listener is up.
func (s *Server) EnableBrowse(baseURL string, bs *browse.Server) {
	s.browseBase = baseURL
	s.browse = bs
	s.mux.HandleFunc("GET /api/browse/config", s.authMiddleware(s.handleBrowseConfig))
	s.mux.HandleFunc("POST /api/browse/grant", s.authMiddleware(s.handleBrowseGrant))
}

func (s *Server) handleBrowseConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"base_url": s.browseBase})
}

func (s *Server) handleBrowseGrant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StateKey string `json:"state_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.StateKey == "" {
		s.log.Printf("browse grant: bad request: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	grant := s.browse.MintGrant(req.StateKey)
	writeJSON(w, map[string]string{"grant": grant})
}
```

Adjust `EnableBrowse` signature to take `*browse.Server` (the test above passes only a URL — update the test to pass a real `browse.New("", nil)` or split into two: config uses base only). Use the project's existing JSON helper (`writeJSON` or equivalent — grep for the real name and match it).

- [ ] **Step 12: Run to verify pass**

Run: `go test ./internal/server/ -run TestBrowse ./internal/browse/`
Expected: PASS.

- [ ] **Step 13: Boot the browse listener in the desktop shell**

In `internal/desktop/boot.go`, inside `StartServer` after the main server is constructed and before `Serve`:

```go
	// Browse origin: a second loopback listener, isolated from the SPA origin.
	bs := browse.New(token, log.Default())
	bln, bBase, err := bs.Listen("127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("desktop: browse listen: %w", err)
	}
	srv.EnableBrowse(bBase, bs)
	go func() {
		if err := http.Serve(bln, bs.Handler()); err != nil {
			log.Printf("desktop: browse serve error: %v", err)
		}
	}()
```

Add the `browse` import. (The CLI web-server entrypoint that also calls `server.New` gets the same three lines — grep for other `server.New(` call sites and wire each, or add a shared helper `server.StartBrowse(srv, token)` and call it from both. Prefer the helper to stay DRY.)

- [ ] **Step 14: Build + full package test**

Run: `go build ./... && go test ./internal/browse/ ./internal/server/`
Expected: build OK, tests PASS.

- [ ] **Step 15: Commit**

```bash
git add internal/browse/ internal/server/server.go internal/server/server_browse_test.go internal/desktop/boot.go
git commit -m "feat(browse): browse-origin server scaffold, stateless routing, grant/cookie auth"
```
