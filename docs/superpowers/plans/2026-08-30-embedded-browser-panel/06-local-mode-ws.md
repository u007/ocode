# Part 06 — Local Mode: Streaming Reverse Proxy + WebSocket Passthrough + Service-Worker Block

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ Backend proxy › Local mode).

**Goal:** Serve loopback/RFC1918 upstreams (dev servers like Vite) through a **transparent streaming** reverse proxy — no body buffering, no URL rewriting — so HMR and large asset streams behave exactly as if hit directly. The only mutation is injecting the capture script into `text/html` documents. WebSocket upgrades pass through untouched (HMR sockets). Service-worker requests are refused. Every top-level document navigation emits a `NavEvent` with `Mode: "local"`.

**Why local mode is separate from external mode:** external mode (Part 03/04) buffers and rewrites every URL because arbitrary sites embed absolute cross-origin links. Local dev servers serve their own origin and rely on relative URLs plus live WebSocket reload; rewriting or buffering them breaks HMR and adds latency. So local mode is the cheap path: stream bytes, patch only HTML `<head>`.

**Files:**
- Create: `internal/browse/local.go`
- Create: `internal/browse/local_test.go`

**Interfaces:**
- Consumes: `newSafeTransport(allowPrivate bool) *http.Transport` (Part 02), `injectCapture(html []byte, stateKey, spaOrigin string) []byte` (Part 05), `(s *Server).emitNav(NavEvent)` and `target`/`NavEvent` (Part 01).
- Produces: `(s *Server) handleLocal(w http.ResponseWriter, r *http.Request, t target)` — called from the `handleBrowse` dispatch (Part 03 wired the dispatch: `if t.Local { s.handleLocal(...) } else { s.handleExternal(...) }`).

**Security note (document verbatim in the file header comment):** `handleLocal` is reachable only for a `target` whose host literal is loopback/RFC1918 (`parseTarget` sets `t.Local`, Part 01). An external page's rewritten links always carry an external host, so its subresources route to `handleExternal`, never here. A private host can therefore only be reached because a human typed/opened it in the address bar (which mints the `__grant`); subsequent subresources ride the scoped `HttpOnly` cookie. We deliberately do **not** add a per-request "user-initiated" flag — reaching a private host at all already required the user path, and the cookie is confined to `/b/`. `newSafeTransport(true)` (allowPrivate) is used **only** on this path; `handleExternal` always uses `newSafeTransport(false)`.

---

- [ ] **Step 1: Write the failing test — HTML gets the capture script injected, non-HTML streams byte-for-byte**

Create `internal/browse/local_test.go`:

```go
package browse

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newLocalTestServer returns a browse Server whose auth store already has a
// live session for stateKey, so requests can carry the cookie directly.
func newLocalTestServer(t *testing.T, stateKey string) (*Server, *http.Cookie) {
	t.Helper()
	s := New("apitoken", nil)
	grant := s.MintGrant(stateKey)
	cookieVal, _, ok := s.auth.redeem(grant)
	if !ok {
		t.Fatal("could not redeem grant in test setup")
	}
	return s, &http.Cookie{Name: browseCookie, Value: cookieVal, Path: "/b/"}
}

func TestHandleLocalInjectsCaptureIntoHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><head><title>dev</title></head><body>hi</body></html>")
	}))
	defer upstream.Close()

	// upstream.Listener.Addr() is 127.0.0.1:PORT — a private literal, so t.Local.
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:local1")

	r := httptest.NewRequest("GET", "/b/tab:local1/http/"+host+"/", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "__ocode_capture.js") {
		t.Fatalf("capture script not injected into HTML: %q", body)
	}
	if !strings.Contains(body, "<title>dev</title>") {
		t.Fatalf("original HTML content lost: %q", body)
	}
}

func TestHandleLocalStreamsNonHTMLUnmodified(t *testing.T) {
	const js = "console.log('vite');\n// @vite/client HMR payload\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = io.WriteString(w, js)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:local2")

	r := httptest.NewRequest("GET", "/b/tab:local2/http/"+host+"/@vite/client", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if got := w.Body.String(); got != js {
		t.Fatalf("non-HTML body mutated:\n got %q\nwant %q", got, js)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestHandleLocal`
Expected: FAIL — `s.handleLocal` undefined and the `handleBrowse` dispatch does not yet route local targets (returns the Part-01 stub or 404).

- [ ] **Step 3: Implement `handleLocal`**

Create `internal/browse/local.go`:

```go
package browse

// handleLocal serves loopback/RFC1918 upstreams as a transparent streaming
// reverse proxy. It is reachable only for targets whose host literal is
// private (parseTarget sets t.Local); an external page's rewritten links
// always carry an external host and therefore route to handleExternal. A
// private host can only have been reached via a user-initiated address-bar
// navigation (which minted the __grant); subresources ride the /b/ cookie.
// newSafeTransport(true) is used ONLY here.

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// localTransport is built once with allowPrivate=true. Reused across requests.
func (s *Server) localTransport() *http.Transport {
	s.localTransportOnce.Do(func() {
		s.localTransportVal = newSafeTransport(true)
	})
	return s.localTransportVal
}

func (s *Server) handleLocal(w http.ResponseWriter, r *http.Request, t target) {
	// Service workers are never allowed on the browse origin: a site-controlled
	// worker would persist across restarts scoped to /b/.
	if isServiceWorkerRequest(r) {
		http.Error(w, "browse: service workers are blocked", http.StatusForbidden)
		return
	}

	upstream := &url.URL{Scheme: t.Scheme, Host: t.Host}

	proxy := &httputil.ReverseProxy{
		Transport: s.localTransport(),
		Director: func(req *http.Request) {
			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.URL.Path = t.Path
			req.URL.RawQuery = t.RawQuery
			req.Host = upstream.Host
			// Never forward the browse session cookie upstream.
			req.Header.Del("Cookie")
			// Strip the grant marker if it somehow survived.
			req.Header.Del("X-Ocode-Grant")
		},
		ModifyResponse: func(resp *http.Response) error {
			// Never let the dev server install a service worker via header.
			resp.Header.Del("Service-Worker-Allowed")
			ct := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "text/html") {
				return nil // stream everything non-HTML untouched
			}
			return s.injectCaptureIntoResponse(resp, t.StateKey)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.Printf("browse local: proxy error for %s://%s%s: %v", t.Scheme, t.Host, t.Path, err)
			s.emitNav(NavEvent{StateKey: t.StateKey, URL: upstream.String() + t.Path, Mode: "local", Error: err.Error()})
			http.Error(w, "browse: upstream unreachable", http.StatusBadGateway)
		},
	}

	// Emit the authoritative nav event for top-level document loads only.
	// (Subresources have Sec-Fetch-Dest != "document"; treat missing header as
	// a document request so plain navigations still emit.)
	if isDocumentRequest(r) {
		s.emitNav(NavEvent{
			StateKey: t.StateKey,
			URL:      t.Scheme + "://" + t.Host + t.Path,
			Status:   0, // filled by the SPA on load; 0 = navigating
			Mode:     "local",
		})
	}

	proxy.ServeHTTP(w, r)
}

// injectCaptureIntoResponse reads the (possibly gzipped) HTML body, injects the
// capture script, and rewrites the body + Content-Length/Content-Encoding.
// Only HTML documents reach here, so buffering is bounded by page size.
func (s *Server) injectCaptureIntoResponse(resp *http.Response, stateKey string) error {
	var reader io.ReadCloser = resp.Body
	gzipped := strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip")
	if gzipped {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			// Not actually gzip / corrupt: log and pass through unmodified.
			s.log.Printf("browse local: gzip reader for HTML failed, passing through: %v", err)
			return nil
		}
		reader = gr
	}
	raw, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return err
	}
	injected := injectCapture(raw, stateKey, s.spaOrigin())

	resp.Body = io.NopCloser(bytes.NewReader(injected))
	// We always emit identity-encoded HTML after injection.
	resp.Header.Del("Content-Encoding")
	resp.Header.Set("Content-Length", itoa(len(injected)))
	resp.ContentLength = int64(len(injected))
	return nil
}
```

- [ ] **Step 4: Add the small shared helpers this part relies on**

These helpers are used by both local and external modes. If Part 03 already added identical helpers, keep one copy (DRY) — grep first; only add the ones missing. Append to `internal/browse/local.go` (or move to `server.go` if Part 03 owns them):

```go
import "strconv"

func itoa(n int) string { return strconv.Itoa(n) }

func isServiceWorkerRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Sec-Fetch-Dest"), "serviceworker") ||
		r.Header.Get("Service-Worker") != ""
}

func isDocumentRequest(r *http.Request) bool {
	d := r.Header.Get("Sec-Fetch-Dest")
	return d == "" || strings.EqualFold(d, "document") || strings.EqualFold(d, "iframe")
}
```

Add the transport-cache fields to the `Server` struct in `server.go` (Part 01 owns the struct; add these fields):

```go
	localTransportOnce sync.Once
	localTransportVal  *http.Transport
```

And a `spaOrigin()` accessor on `Server` (the SPA origin the capture script must post to). It is configured when the main server calls `EnableBrowse`; store it on the browse `Server`:

```go
// spaOrigin returns the ocode SPA origin (e.g. http://127.0.0.1:PORT) that the
// capture script targets with postMessage. Set via SetSPAOrigin at boot.
func (s *Server) spaOrigin() string { return s.spaOriginVal }

func (s *Server) SetSPAOrigin(origin string) { s.spaOriginVal = origin }
```

(Add `spaOriginVal string` to the struct. In `internal/desktop/boot.go` / the shared browse-start helper from Part 01, call `bs.SetSPAOrigin(handle.URL)` after the main server URL is known.)

- [ ] **Step 5: Run to verify the two tests pass**

Run: `go test ./internal/browse/ -run TestHandleLocal`
Expected: PASS.

- [ ] **Step 6: Write the failing service-worker-block test**

Append to `internal/browse/local_test.go`:

```go
func TestHandleLocalBlocksServiceWorker(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = io.WriteString(w, "self.addEventListener('install',()=>{})")
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:sw")

	r := httptest.NewRequest("GET", "/b/tab:sw/http/"+host+"/sw.js", nil)
	r.Header.Set("Sec-Fetch-Dest", "serviceworker")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("service worker request: got %d want 403", w.Code)
	}
}
```

- [ ] **Step 7: Run to verify it passes**

Run: `go test ./internal/browse/ -run TestHandleLocalBlocksServiceWorker`
Expected: PASS (the guard is already in `handleLocal`).

- [ ] **Step 8: Write the failing NavEvent test**

Append to `internal/browse/local_test.go`:

```go
func TestHandleLocalEmitsLocalNavEvent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><head></head><body>x</body></html>")
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:nav")

	var got []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { got = append(got, ev) })

	r := httptest.NewRequest("GET", "/b/tab:nav/http/"+host+"/", nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.AddCookie(cookie)
	s.Handler().ServeHTTP(httptest.NewRecorder(), r)

	if len(got) == 0 {
		t.Fatal("no NavEvent emitted for local document")
	}
	if got[0].Mode != "local" {
		t.Fatalf("NavEvent.Mode = %q want local", got[0].Mode)
	}
	if !strings.Contains(got[0].URL, host) {
		t.Fatalf("NavEvent.URL = %q missing upstream host", got[0].URL)
	}
}
```

- [ ] **Step 9: Run to verify it passes**

Run: `go test ./internal/browse/ -run TestHandleLocalEmitsLocalNavEvent`
Expected: PASS.

- [ ] **Step 10: Write the failing WebSocket-passthrough test**

Go's `httputil.ReverseProxy` performs WebSocket passthrough automatically when the upstream responds `101 Switching Protocols` and the transport supports it — no extra hijack code needed. Verify with a raw upgrade handshake against an httptest upstream (no external ws library, so hand-roll the 101). Append to `internal/browse/local_test.go`:

```go
import (
	"bufio"
	"net"
)

func TestHandleLocalWebSocketPassthrough(t *testing.T) {
	// Upstream that completes a WebSocket-style 101 upgrade and echoes one line.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	s, cookie := newLocalTestServer(t, "tab:ws")

	// Serve the browse server on a real listener so we can dial a raw socket.
	bln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bln.Close()
	go func() { _ = http.Serve(bln, s.Handler()) }()

	conn, err := net.Dial("tcp", bln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req := "GET /b/tab:ws/http/" + host + "/ws HTTP/1.1\r\n" +
		"Host: " + bln.Addr().String() + "\r\n" +
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
	// Drain headers.
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
```

- [ ] **Step 11: Run to verify it passes**

Run: `go test ./internal/browse/ -run TestHandleLocalWebSocketPassthrough`
Expected: PASS. If it fails because the WS request routes through `injectCaptureIntoResponse`, confirm `ModifyResponse` returns early for non-HTML (a 101 has no `text/html` Content-Type) — the guard already handles this; a 101 response never reaches `ModifyResponse` in Go's proxy because upgrades bypass it. If Go's version requires it, ensure `handleLocal` does not short-circuit upgrade requests before `proxy.ServeHTTP`.

- [ ] **Step 12: Run the full package test suite**

Run: `go test ./internal/browse/`
Expected: PASS (all local + earlier-part tests green).

- [ ] **Step 13: Commit**

```bash
git add internal/browse/local.go internal/browse/local_test.go internal/browse/server.go
git commit -m "feat(browse): local-mode streaming proxy with WS passthrough, capture injection, SW block"
```
