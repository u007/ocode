package browse

// handleLocal serves loopback/RFC1918 upstreams as a streaming reverse proxy.
// It is reachable only for targets whose host literal is
// private (parseTarget sets t.Local); an external page's rewritten links
// always carry a non-private host and therefore route to Chrome via CDP. A
// private host can only have been reached via a user-initiated address-bar
// navigation (which minted the __grant); subresources ride the /b/ cookie.
// We deliberately do NOT add a per-request "user-initiated" flag — reaching a
// private host at all already required the user path, and the cookie is
// confined to /b/. This transport uses allowPrivate=true and is the only
// place private upstreams are dialed.
//
// Deviation from the spec's "only transformation: inject the capture script"
// (found by live QA 2026-08-31): HTML responses are ALSO URL-rewritten via
// rewriteHTML, because dev servers (Vite et al.) reference nearly all of
// their assets root-relative ("/src/main.js", "/@vite/client"); without the
// static rewrite those requests hit the browse origin root, drop out of the
// stateless route, and 404. Non-HTML bodies (JS/CSS/images/WS) still stream
// byte-for-byte untouched.

import (
	"bytes"
	"compress/gzip"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// localTransport returns the strict-TLS transport for private upstreams.
// TLS verification stays on; loopback auto-bypass and explicit user bypasses
// use localInsecureTransport.
func (s *Server) localTransport() *http.Transport {
	s.localTransportOnce.Do(func() {
		s.localTransportVal = newSafeTransport(true)
	})
	return s.localTransportVal
}

// localInsecureTransport returns the allowPrivate transport that skips TLS
// verification. Used only for loopback auto-allow (localhost, 127.0.0.1, ::1)
// and for hosts the user has explicitly bypassed via POST /b/{stateKey}/__bypass.
func (s *Server) localInsecureTransport() *http.Transport {
	s.localInsecureTransportOnce.Do(func() {
		s.localInsecureTransportVal = newSafeTransportInsecure(true)
	})
	return s.localInsecureTransportVal
}

// chooseLocalTransport picks the right transport for t. Loopback hosts are
// auto-allowed (self-signed dev certs just work); other private hosts require
// an explicit bypass. Subresources inherit the document's bypass (same stateKey+host).
func (s *Server) chooseLocalTransport(t target) *http.Transport {
	if t.Scheme != "https" {
		return s.localTransport()
	}
	if isLoopbackHost(t.Host) {
		return s.localInsecureTransport()
	}
	if s.isBypassed(t.StateKey, t.Host) {
		return s.localInsecureTransport()
	}
	return s.localTransport()
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
		Transport: s.chooseLocalTransport(t),
		Director: func(req *http.Request) {
			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.URL.Path = t.Path
			req.URL.RawQuery = t.RawQuery
			req.Host = upstream.Host
			// Site cookies come from the per-stateKey server-side jar; this
			// also drops the browser's Cookie header (the browse session
			// cookie must never go upstream). See cookiejar.go.
			s.cookies.Apply(t.StateKey, req)
			// Strip the grant marker if it somehow survived.
			req.Header.Del("X-Ocode-Grant")
		},
		ModifyResponse: func(resp *http.Response) error {
			// Capture upstream cookies into the stateKey's jar and keep them
			// out of the browser: the browse origin is shared by every
			// proxied host, so a browser-stored cookie would leak across
			// hosts and tabs. Runs first so redirects (login flows) keep
			// their session cookie too.
			s.cookies.Store(t.StateKey, resp.Request.URL, resp)
			resp.Header.Del("Set-Cookie")
			// Local → Chrome hand-off: if a document request received a 3xx
			// whose Location resolves to a non-private host, do not follow it
			// inside the iframe (it would die on X-Frame-Options). Instead
			// replace with 204 and emit a chrome nav event. The SPA will switch
			// viewport and the chrome target will navigate there.
			if isDocumentRequest(r) && resp.StatusCode >= 300 && resp.StatusCode < 400 {
				loc := resp.Header.Get("Location")
				if loc != "" {
					if parsed, err := url.Parse(loc); err == nil {
						base := &url.URL{Scheme: t.Scheme, Host: t.Host, Path: t.Path}
						resolved := base.ResolveReference(parsed)
						if resolved.Host != "" && !hostIsLiteralPrivate(resolved.Host) {
							// Hand off to chrome.
							s.emitNav(NavEvent{StateKey: t.StateKey, URL: resolved.String(), Status: 0, Mode: "chrome"})
							// Drain and close original body.
							if resp.Body != nil {
								_, _ = io.Copy(io.Discard, resp.Body)
								_ = resp.Body.Close()
							}
							resp.Body = io.NopCloser(bytes.NewReader(nil))
							resp.StatusCode = http.StatusNoContent
							resp.Status = http.StatusText(http.StatusNoContent)
							resp.Header.Del("Location")
							resp.Header.Del("Content-Length")
							resp.Header.Del("Content-Type")
							resp.Header.Del("Content-Encoding")
							resp.ContentLength = 0
							return nil
						}
					}
				}
			}
			// Never let the dev server install a service worker via header.
			resp.Header.Del("Service-Worker-Allowed")
			ct := resp.Header.Get("Content-Type")
			isHTML := strings.HasPrefix(ct, "text/html")
			if !isHTML {
				return nil // stream everything non-HTML untouched
			}
			// Terminal nav event for HTML document loads only (Part 07:
			// one loading + one terminal per navigation). Gating on the
			// response type as well as the request protects against
			// header-less subresources (WS handshakes, EventSource) being
			// misclassified as documents and hijacking the address bar.
			// The terminal payload is decided after the body is read
			// (dev-server HTML replaces the normal local event with an
			// error-tagged one), so rewriteAndInjectResponse returns it.
			term, err := s.rewriteAndInjectResponse(resp, t, isDocumentRequest(r))
			if err != nil {
				return err
			}
			if term != nil {
				s.emitNav(*term)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.log.Printf("browse local: proxy error for %s://%s%s: %v", t.Scheme, t.Host, t.Path, err)
			// For TLS verification failures on private hosts, surface a stable
			// string the SPA can match to show a “Continue anyway” interstitial.
			errStr := err.Error()
			if cf := classifyFetchError(err); cf == "TLS certificate not trusted" || cf == "TLS certificate verification failed" {
				errStr = cf + " — " + err.Error()
			}
			if isDocumentRequest(r) {
				s.emitNav(NavEvent{StateKey: t.StateKey, URL: upstream.String() + t.Path, Status: http.StatusBadGateway, Mode: "local", Error: errStr})
			}
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

	// Per-stateKey upstream concurrency cap (spec § External mode limits,
	// extended to local mode — same upstream resource). ReverseProxy.ServeHTTP
	// blocks for the whole exchange, including a hijacked WebSocket tunnel
	// (handleUpgradeResponse waits for both copy directions to finish), so the
	// slot is held for the connection's full lifetime and a long-lived WS
	// counts against the 32 — exactly what the cap is meant to bound.
	release, cerr := s.conns.acquire(r.Context(), t.StateKey)
	if cerr != nil {
		s.log.Printf("browse local: upstream cap hit for %s://%s%s: %v", t.Scheme, t.Host, t.Path, cerr)
		s.failBusy(w, r, t, "local")
		return
	}
	defer release()

	proxy.ServeHTTP(w, r)
}

// rewriteAndInjectResponse reads the (possibly gzipped) HTML body, rewrites
// its URLs into the stateless route (root-relative dev-server assets stay on
// the route), injects the capture script, and rewrites the body +
// Content-Length/Content-Encoding. Only HTML documents reach here, so
// buffering is bounded by page size.
//
// It returns the terminal NavEvent for the response (nil when no terminal
// event applies); the caller emits it. Top-level documents whose body looks
// like a Vite/TanStack dev server get a self-contained notice instead (see
// devServerHTMLRe) — such pages cannot render through the proxy — and the
// returned event carries DevServerNoticeError so the SPA can offer the
// Chrome-mode fallback.
func (s *Server) rewriteAndInjectResponse(resp *http.Response, t target, isDoc bool) (*NavEvent, error) {
	var reader io.Reader = resp.Body
	gzipped := strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip")
	if gzipped {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			// Malformed gzip from the dev server: stream the raw body rather
			// than kill the response. Loud, non-fatal.
			s.log.Printf("browse local: gzip reader for %s: %v", resp.Request.URL, err)
			return localTerminalNav(t, resp.StatusCode), nil
		}
		reader = gr
	}
	raw, err := io.ReadAll(reader)
	if cerr, ok := reader.(io.Closer); ok {
		if err := cerr.Close(); err != nil {
			log.Printf("browse local: close response reader: %v", err)
		}
	}
	if err != nil {
		return nil, err
	}
	if isDoc && isDevServerHTML(raw) {
		// Vite/TanStack dev servers serve JS modules whose static imports are
		// resolved by the BROWSER against the module URL's origin (root-absolute
		// "/@fs/...", "/@id/virtual:...", "/node_modules/.vite/..."). Behind the
		// /b/ proxy that origin is the browse server, so those imports 404 and
		// the whole module graph dies — the page renders blank. Neither the
		// server-side HTML rewrite nor capture.js can reach native module
		// resolution, so the page is unsalvageable in local mode. Serve a
		// self-contained notice instead of a blank document and tag the nav
		// event so the SPA can offer Chrome (CDP) mode, which renders with a
		// real browser.
		notice := devServerNotice(t)
		resp.Body = io.NopCloser(bytes.NewReader(notice))
		// Identity-encoded, like every other transformed HTML response.
		resp.Header.Del("Content-Encoding")
		resp.Header.Set("Content-Length", strconv.Itoa(len(notice)))
		resp.ContentLength = int64(len(notice))
		term := localTerminalNav(t, resp.StatusCode)
		term.Error = DevServerNoticeError
		return term, nil
	}
	// base "" = resolve relative URLs against the document target (see the
	// rewriteHTML contract); the SPA origin is only for capture injection.
	injected, rerr := rewriteHTML(raw, t, "")
	if rerr != nil {
		s.log.Printf("browse local: rewriteHTML for %s: %v — serving unrewritten", resp.Request.URL, rerr)
		injected = raw // fail open to raw HTML rather than blank page
	}
	injected = injectCapture(injected, t.StateKey, s.spaOriginFor(t.StateKey))

	resp.Body = io.NopCloser(bytes.NewReader(injected))
	// We always emit identity-encoded HTML after injection.
	resp.Header.Del("Content-Encoding")
	resp.Header.Set("Content-Length", strconv.Itoa(len(injected)))
	resp.ContentLength = int64(len(injected))
	return localTerminalNav(t, resp.StatusCode), nil
}

// devServerTagRe matches the bracketed tags in an HTML document. Dev-server
// markers are only significant inside TAG ATTRIBUTES (script src / link href /
// modulepreload); text nodes — e.g. a documentation page displaying "/@fs/"
// as prose — must never trip the detector (see isDevServerHTML).
var devServerTagRe = regexp.MustCompile(`<[^>]+>`)

// devServerMarkerRe matches markers that, inside a tag, only appear in
// development-mode HTML emitted by Vite / TanStack Start servers. Kept
// deliberately conservative: a false NEGATIVE (an undetected dev server)
// leaves today's blank-page behavior unchanged, but a false positive would
// replace a page that actually renders. Bare-Vite pages typically carry
// "/src/main.tsx" (no marker) and are not detected — acceptable, fail-open.
var devServerMarkerRe = regexp.MustCompile(`/@id/virtual:|/@fs/|/@vite/client|/node_modules/\.vite/|tanstack-start-dev-client-entry|@tanstack-start/`)

// isDevServerHTML reports whether raw looks like a Vite/TanStack dev-server
// document. Markers are matched against the TAG TEXT ONLY (attributes), so
// prose or code samples displaying marker-like strings never trigger the
// notice.
func isDevServerHTML(raw []byte) bool {
	var sb strings.Builder
	for _, tag := range devServerTagRe.FindAll(raw, -1) {
		sb.Write(tag)
		sb.WriteByte('\n')
	}
	return devServerMarkerRe.MatchString(sb.String())
}

// DevServerNoticeError tags the nav event for dev-server HTML. The SPA
// matches the "dev-server-module-graph:" prefix to offer the Chrome-mode
// fallback (see BrowserPanel).
const DevServerNoticeError = "dev-server-module-graph: this dev server's module graph can't be served by the embedded proxy — open externally or switch to Chrome mode"

// localTerminalNav is the plain terminal nav event for a served document.
// The query string is preserved so the address bar and any Chrome-mode
// hand-off navigate to the exact URL the user asked for.
func localTerminalNav(t target, status int) *NavEvent {
	u := t.Scheme + "://" + t.Host + t.Path
	if t.RawQuery != "" {
		u += "?" + t.RawQuery
	}
	return &NavEvent{StateKey: t.StateKey, URL: u, Status: status, Mode: "local"}
}

// devServerNotice builds the self-contained replacement document for
// unsupported dev-server pages. No external assets, no scripts, no popups —
// it must render inside the sandboxed iframe; the actionable affordances
// (open external / Chrome mode) live in the SPA chrome, driven by the
// DevServerNoticeError nav event.
func devServerNotice(t target) []byte {
	u := t.Scheme + "://" + t.Host + t.Path
	if t.RawQuery != "" {
		u += "?" + t.RawQuery
	}
	esc := html.EscapeString(u)
	return []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<title>Dev server not supported in embedded browser</title>
<style>
  body{font-family:system-ui,-apple-system,sans-serif;background:#141414;color:#e8e8e8;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;padding:16px}
  .card{max-width:560px;padding:24px 28px;border:1px solid #3a3a3a;border-radius:10px;background:#1e1e1e}
  h1{font-size:16px;margin:0 0 10px;color:#fff}
  p{font-size:13.5px;line-height:1.55;color:#c9c9c9;margin:8px 0}
  code{color:#dfb3ff;word-break:break-all}
  b{color:#fff}
</style></head>
<body><div class="card">
<h1>Development server not supported in the embedded browser</h1>
<p>This page (<code>` + esc + `</code>) is served by a Vite/TanStack development server. Its JavaScript loads through a module graph the browser resolves against the server origin — the embedded proxy cannot serve that graph, so the page would render blank here.</p>
<p>Use <b>Open external</b> in the address bar, or switch this browser to <b>Chrome mode</b> to render the page with a real browser.</p>
</div></body></html>`)
}

// --- shared request-classification helpers (local + external modes) ---

func isServiceWorkerRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Sec-Fetch-Dest"), "serviceworker") ||
		r.Header.Get("Service-Worker") != ""
}

// isUpgradeRequest reports whether this is a protocol-upgrade attempt
// (WebSocket handshake et al.). Upgrades are never top-level navigations —
// and Chromium may omit Sec-Fetch-Dest on them, which would otherwise make
// isDocumentRequest misclassify the handshake as a navigation and hijack the
// address bar / iframe onto the websocket route.
func isUpgradeRequest(r *http.Request) bool {
	return r.Header.Get("Sec-WebSocket-Version") != "" ||
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func isDocumentRequest(r *http.Request) bool {
	if isUpgradeRequest(r) {
		return false
	}
	d := r.Header.Get("Sec-Fetch-Dest")
	return d == "" || strings.EqualFold(d, "document") || strings.EqualFold(d, "iframe")
}
