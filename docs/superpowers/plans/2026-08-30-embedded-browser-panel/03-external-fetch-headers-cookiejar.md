# Part 03 — External-Mode Fetch, Header Surgery, Server-Side Cookie Jar

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ Backend proxy › External mode).

**Goal:** Fetch external upstreams through the hardened transport, perform request/response header surgery, isolate cookies in a server-side jar keyed by `(stateKey, upstreamOrigin)` (never forwarding site cookies to the browser), block service-worker registration, and dispatch `handleBrowse` to external vs local proxying. HTML/CSS rewriting and capture injection are called here but *provided* by Parts 04/05; Part 03 ships an identity-passthrough shim so the package compiles and its own tests (header stripping, cookie isolation, SW block, caps, nav emit) pass.

**Files:**
- Create: `internal/browse/headers.go`, `internal/browse/headers_test.go`, `internal/browse/cookiejar.go`, `internal/browse/cookiejar_test.go`, `internal/browse/external.go`, `internal/browse/external_test.go`
- Modify: `internal/browse/server.go` (replace the Part-01 `handleBrowse` stub with external/local dispatch)

**Interfaces:**
- Produces: `stripSecurityHeaders(http.Header)`, `filterRequestHeaders(*http.Request)`, `type cookieJar` + `newCookieJar()` + `(*cookieJar).Store` + `(*cookieJar).Apply`, `(*Server).handleExternal(http.ResponseWriter, *http.Request, target)`.
- Consumes: `newSafeTransport(allowPrivate bool) *http.Transport` (Part 02); `rewriteHTML(body []byte, t target, base string) ([]byte, error)` (Part 04); `rewriteCSS(body []byte, t target) []byte` (Part 04); `injectCapture(html []byte, stateKey, spaOrigin string) []byte` (Part 05). Until those parts land, Part 03 adds `//go:build`-free identity shims in `external.go` **guarded by a compile-time sentinel comment** so later parts replace them (see Step 9 note).

---

- [ ] **Step 1: Write the failing header-surgery test**

Create `internal/browse/headers_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/ -run 'TestStripSecurityHeaders|TestFilterRequestHeaders'`
Expected: FAIL — `undefined: stripSecurityHeaders` / `filterRequestHeaders` / `fixedUserAgent`.

- [ ] **Step 3: Implement `headers.go`**

Create `internal/browse/headers.go`:

```go
package browse

import "net/http"

// fixedUserAgent is sent to every upstream so the real browser/user is not
// fingerprinted through the proxy and behavior is deterministic.
const fixedUserAgent = "Mozilla/5.0 (compatible; ocode-browse/1.0)"

// responseHeadersToStrip are security headers that would break embedding or
// re-impose the upstream's now-invalid trust assumptions. The whole CSP is
// removed (partial stripping would block the injected capture script and the
// rewritten same-origin assets — see spec § External mode).
var responseHeadersToStrip = []string{
	"X-Frame-Options",
	"Content-Security-Policy",
	"Content-Security-Policy-Report-Only",
	"Strict-Transport-Security",
	"Service-Worker-Allowed",
}

// stripSecurityHeaders removes framing/SW/CSP/HSTS headers from an upstream
// response so the page can render inside the sandboxed iframe.
func stripSecurityHeaders(h http.Header) {
	for _, k := range responseHeadersToStrip {
		h.Del(k)
	}
}

// requestHeadersToDrop are hop-by-hop or leak-prone headers removed before the
// request leaves for upstream. Cookie is dropped because site cookies live in
// the server-side jar, never in the browser (see cookiejar.go).
var requestHeadersToDrop = []string{
	"Referer",
	"Cookie",
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Transfer-Encoding",
	"Upgrade",
	"Te",
	"Trailer",
}

// filterRequestHeaders sanitizes an outbound upstream request: drops the
// referer (no origin leak), pins a fixed UA, and forces an encoding we can
// decode so HTML/CSS can be rewritten.
func filterRequestHeaders(r *http.Request) {
	for _, k := range requestHeadersToDrop {
		r.Header.Del(k)
	}
	r.Header.Set("User-Agent", fixedUserAgent)
	r.Header.Set("Accept-Encoding", "gzip, identity")
	// RequestURI must be empty for client requests; caller builds a fresh URL.
	r.RequestURI = ""
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/browse/ -run 'TestStripSecurityHeaders|TestFilterRequestHeaders'`
Expected: PASS.

- [ ] **Step 5: Write the failing cookie-jar isolation test**

Create `internal/browse/cookiejar_test.go`:

```go
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
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestCookieJar`
Expected: FAIL — `undefined: newCookieJar`.

- [ ] **Step 7: Implement `cookiejar.go`**

Create `internal/browse/cookiejar.go`:

```go
package browse

import (
	"net/http"
	"sync"
)

// cookieJar holds upstream cookies entirely server-side. Site cookies are
// NEVER handed to the browser: the browser only ever holds the scoped
// ocode_browse session cookie (auth.go). Keying by (stateKey, origin) means
// two sites browsed in the same panel keep separate jars, and two panels keep
// separate jars for the same site.
type cookieJar struct {
	mu   sync.Mutex
	data map[string]map[string]*http.Cookie // key -> cookieName -> cookie
}

func newCookieJar() *cookieJar {
	return &cookieJar{data: map[string]map[string]*http.Cookie{}}
}

func jarKey(stateKey, origin string) string { return stateKey + "\x00" + origin }

// Store absorbs Set-Cookie headers from an upstream response into the jar.
func (j *cookieJar) Store(stateKey, origin string, resp *http.Response) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}
	key := jarKey(stateKey, origin)
	j.mu.Lock()
	defer j.mu.Unlock()
	m := j.data[key]
	if m == nil {
		m = map[string]*http.Cookie{}
		j.data[key] = m
	}
	for _, c := range cookies {
		// Expiry with empty value / MaxAge<0 deletes.
		if c.MaxAge < 0 || (c.Value == "" && c.MaxAge == 0 && !c.Expires.IsZero()) {
			delete(m, c.Name)
			continue
		}
		m[c.Name] = c
	}
}

// Apply attaches this (stateKey, origin)'s jar cookies to an outbound upstream
// request. Path/expiry nuance is intentionally coarse (name=value only): the
// jar is per-origin already, and finer scoping is not needed for embedding.
func (j *cookieJar) Apply(stateKey, origin string, req *http.Request) {
	key := jarKey(stateKey, origin)
	j.mu.Lock()
	m := j.data[key]
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	j.mu.Unlock()
	if len(names) == 0 {
		return
	}
	// Deterministic order for stable Cookie headers (and testability).
	sortStrings(names)
	for _, name := range names {
		j.mu.Lock()
		c := m[name]
		j.mu.Unlock()
		if c != nil {
			req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
}
```

Add a tiny local `sortStrings` helper (or use `slices.Sort` from stdlib — Go 1.26). Prefer:

```go
// at top of cookiejar.go imports: "slices"
func sortStrings(s []string) { slices.Sort(s) }
```

- [ ] **Step 8: Run to verify pass**

Run: `go test ./internal/browse/ -run TestCookieJar`
Expected: PASS.

- [ ] **Step 9: Write the failing external-fetch test**

Create `internal/browse/external_test.go`:

```go
package browse

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer builds a browse Server whose transport allows private IPs so
// httptest upstreams (127.0.0.1) are reachable in tests. Production uses
// allowPrivate=false for external mode.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	s := New("apitoken", nil)
	s.transport = newSafeTransport(true) // test-only override
	return s
}

func TestExternalStripsSecurityHeadersAndEmitsNav(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>hi</body></html>")
	}))
	defer upstream.Close()

	s := newTestServer(t)
	var gotNav NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { gotNav = ev })

	// Upstream host without scheme prefix — parseTarget builds the path form.
	host := strings.TrimPrefix(upstream.URL, "http://")
	req := httptest.NewRequest("GET", "/b/tab:x/http/"+host+"/", nil)
	w := httptest.NewRecorder()
	tgt := target{StateKey: "tab:x", Scheme: "http", Host: host, Path: "/", Local: true}
	// External path is exercised directly (dispatch tested separately); force
	// external by clearing Local for this assertion of external behavior:
	tgt.Local = false
	s.handleExternal(w, req, tgt)

	res := w.Result()
	if res.Header.Get("X-Frame-Options") != "" || res.Header.Get("Content-Security-Policy") != "" {
		t.Errorf("security headers not stripped: %+v", res.Header)
	}
	if gotNav.Mode != "proxied" || gotNav.Status != 200 {
		t.Errorf("nav event = %+v, want proxied/200", gotNav)
	}
}

func TestExternalBlocksServiceWorker(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream must not be hit for a service-worker request")
	}))
	defer upstream.Close()

	s := newTestServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	req := httptest.NewRequest("GET", "/b/tab:x/http/"+host+"/sw.js", nil)
	req.Header.Set("Sec-Fetch-Dest", "serviceworker")
	w := httptest.NewRecorder()
	s.handleExternal(w, req, target{StateKey: "tab:x", Scheme: "http", Host: host, Path: "/sw.js"})
	if w.Code != http.StatusForbidden {
		t.Errorf("SW request status = %d, want 403", w.Code)
	}
}

func TestExternalStreamsNonHTMLUnrewritten(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	s := newTestServer(t)
	host := strings.TrimPrefix(upstream.URL, "http://")
	req := httptest.NewRequest("GET", "/b/tab:x/http/"+host+"/api", nil)
	w := httptest.NewRecorder()
	s.handleExternal(w, req, target{StateKey: "tab:x", Scheme: "http", Host: host, Path: "/api"})
	if got := w.Body.String(); got != `{"ok":true}` {
		t.Errorf("body = %q, want passthrough JSON", got)
	}
}
```

**Shim note:** Parts 04/05 provide `rewriteHTML`, `rewriteCSS`, `injectCapture`. So Part 03's tests do not assert rewritten HTML content — only header stripping, nav emit, SW block, and non-HTML passthrough. To compile before Parts 04/05 land, add identity shims at the bottom of `external.go` wrapped in a clearly marked block titled `// PROVIDED BY PARTS 04/05 — remove these shims when implementing those parts`. When Part 04/05 are implemented, delete the shim block; the real functions in `rewrite.go`/`capture.go` take over.

- [ ] **Step 10: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestExternal`
Expected: FAIL — `undefined: (*Server).handleExternal` / `s.transport` / shims.

- [ ] **Step 11: Implement `external.go`**

Create `internal/browse/external.go`:

```go
package browse

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxRewriteBytes caps buffered HTML/CSS. Larger responses stream unrewritten
// (rewriting requires holding the whole body in memory).
const maxRewriteBytes = 10 << 20 // 10 MiB

const upstreamTimeout = 30 * time.Second

// upstreamOrigin returns scheme://host for jar keying and nav reporting.
func upstreamOrigin(t target) string { return t.Scheme + "://" + t.Host }

// handleExternal proxies an external (non-private) upstream: header surgery,
// server-side cookie jar, service-worker block, gzip decode, and HTML/CSS
// rewrite + capture injection for document responses.
func (s *Server) handleExternal(w http.ResponseWriter, r *http.Request, t target) {
	// Block service-worker registration before any upstream contact: a SW
	// scoped to /b/ on the browse origin would persist site-controlled code.
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Dest"), "serviceworker") {
		http.Error(w, "browse: service workers are blocked", http.StatusForbidden)
		return
	}

	origin := upstreamOrigin(t)
	upURL := &url.URL{Scheme: t.Scheme, Host: t.Host, Path: t.Path, RawQuery: t.RawQuery}

	body := r.Body // streamed through for POST/PUT/etc.
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upURL.String(), body)
	if err != nil {
		s.log.Printf("browse: build upstream request %s: %v", upURL, err)
		s.failNav(w, t, "bad upstream request")
		return
	}
	// Copy safe request headers, then sanitize.
	copyHeader(req.Header, r.Header)
	filterRequestHeaders(req)
	s.jar.Apply(t.StateKey, origin, req)

	client := &http.Client{
		Transport:     s.transport,
		Timeout:       upstreamTimeout,
		CheckRedirect: capRedirects(10), // SSRF re-checked per hop in the dialer.
	}
	resp, err := client.Do(req)
	if err != nil {
		s.log.Printf("browse: upstream fetch %s failed: %v", upURL, err)
		s.failNav(w, t, classifyFetchError(err))
		return
	}
	defer resp.Body.Close()

	s.jar.Store(t.StateKey, origin, resp)

	// Decode gzip so we can inspect/rewrite; drop the header afterwards.
	reader := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, gerr := gzip.NewReader(resp.Body)
		if gerr != nil {
			s.log.Printf("browse: gzip reader for %s: %v", upURL, gerr)
			s.failNav(w, t, "decode error")
			return
		}
		defer gz.Close()
		reader = gz
		resp.Header.Del("Content-Encoding")
	}

	stripSecurityHeaders(resp.Header)
	resp.Header.Del("Content-Length") // recomputed by the writer / chunked

	ct := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(ct, "text/html")
	isCSS := strings.Contains(ct, "text/css")

	// Emit the authoritative nav event for the top-level document only.
	if isHTML && r.Header.Get("Sec-Fetch-Dest") != "image" {
		s.emitNav(NavEvent{StateKey: t.StateKey, URL: origin + t.Path, Status: resp.StatusCode, Mode: "proxied"})
	}

	if !isHTML && !isCSS {
		// Stream everything else untouched (media, JSON, downloads).
		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, reader); err != nil {
			s.log.Printf("browse: stream copy %s: %v", upURL, err)
		}
		return
	}

	// Buffer up to the cap for rewriting.
	limited := io.LimitReader(reader, maxRewriteBytes+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		s.log.Printf("browse: read body %s: %v", upURL, err)
		s.failNav(w, t, "read error")
		return
	}
	if len(buf) > maxRewriteBytes {
		// Too large to rewrite: stream what we have + the rest, unrewritten.
		s.log.Printf("browse: %s exceeds rewrite cap, streaming unrewritten", upURL)
		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(buf)
		_, _ = io.Copy(w, reader)
		return
	}

	var out []byte
	if isHTML {
		rewritten, rerr := rewriteHTML(buf, t, s.spaOrigin)
		if rerr != nil {
			s.log.Printf("browse: rewriteHTML %s: %v", upURL, rerr)
			rewritten = buf // fail open to raw HTML rather than blank page
		}
		out = injectCapture(rewritten, t.StateKey, s.spaOrigin)
	} else {
		out = rewriteCSS(buf, t)
	}

	copyHeader(w.Header(), resp.Header)
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(out); err != nil {
		s.log.Printf("browse: write rewritten %s: %v", upURL, err)
	}
}

func (s *Server) failNav(w http.ResponseWriter, t target, reason string) {
	s.emitNav(NavEvent{StateKey: t.StateKey, URL: upstreamOrigin(t) + t.Path, Mode: "proxied", Error: reason})
	http.Error(w, "browse: "+reason, http.StatusBadGateway)
}

func classifyFetchError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "blocked by SSRF guard"):
		return "blocked: private address"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "no such host"):
		return "dns error"
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	default:
		return "fetch error"
	}
}

func capRedirects(max int) func(*http.Request, []*http.Request) error {
	return func(_ *http.Request, via []*http.Request) error {
		if len(via) >= max {
			return http.ErrUseLastResponse
		}
		return nil
	}
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// ----------------------------------------------------------------------------
// PROVIDED BY PARTS 04/05 — remove these shims when implementing those parts.
// Identity passthrough so Part 03 compiles and its tests (headers/cookies/SW/
// caps/nav) pass before the rewrite engine and capture script exist.
// ----------------------------------------------------------------------------

func rewriteHTML(body []byte, _ target, _ string) ([]byte, error) { return body, nil }
func rewriteCSS(body []byte, _ target) []byte                     { return body }
func injectCapture(html []byte, _ string, _ string) []byte        { return html }
```

- [ ] **Step 12: Add the required `Server` fields**

In `internal/browse/server.go`, extend the `Server` struct and `New`:

```go
// add to Server struct:
	transport *http.Transport
	jar       *cookieJar
	spaOrigin string // set via EnableBrowse wiring; used for postMessage target

// in New(...), after constructing s:
	s.transport = newSafeTransport(false) // external mode: private IPs blocked
	s.jar = newCookieJar()
```

Add `func (s *Server) SetSPAOrigin(o string) { s.spaOrigin = o }` and call it from the main-server `EnableBrowse` wiring (Part 01) passing the SPA base URL, so `injectCapture` can set an exact `postMessage` target origin (Part 05).

- [ ] **Step 13: Replace the `handleBrowse` dispatch stub**

In `internal/browse/server.go`, replace the Part-01 stub body (the `// Stub until Part 03/06.` block) with:

```go
	if t.Local {
		s.handleLocal(w, r, t) // provided by Part 06
		return
	}
	s.handleExternal(w, r, t)
```

**Shim note:** `handleLocal` is provided by Part 06. To compile now, add a temporary method in `external.go`'s shim block:

```go
// PROVIDED BY PART 06 — remove when implementing local mode.
func (s *Server) handleLocal(w http.ResponseWriter, r *http.Request, t target) {
	s.handleExternal(w, r, t) // temporary: treat local like external
}
```

- [ ] **Step 14: Run to verify pass**

Run: `go test ./internal/browse/ -run TestExternal`
Expected: PASS.

- [ ] **Step 15: Full package build + test**

Run: `go build ./... && go test ./internal/browse/`
Expected: build OK, all tests PASS.

- [ ] **Step 16: Commit**

```bash
git add internal/browse/headers.go internal/browse/headers_test.go \
        internal/browse/cookiejar.go internal/browse/cookiejar_test.go \
        internal/browse/external.go internal/browse/external_test.go \
        internal/browse/server.go
git commit -m "feat(browse): external-mode fetch, header surgery, server-side cookie jar, SW block"
```

**Note for later parts:** the shim block in `external.go` (`rewriteHTML`, `rewriteCSS`, `injectCapture`, `handleLocal`) MUST be deleted as Parts 04/05/06 land — leaving them causes duplicate-definition compile errors, which is the intended forcing function.
