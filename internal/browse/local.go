package browse

// handleLocal serves loopback/RFC1918 upstreams as a streaming reverse proxy.
// It is reachable only for targets whose host literal is
// private (parseTarget sets t.Local); an external page's rewritten links
// always carry an external host and therefore route to handleExternal. A
// private host can only have been reached via a user-initiated address-bar
// navigation (which minted the __grant); subresources ride the /b/ cookie.
// We deliberately do NOT add a per-request "user-initiated" flag — reaching a
// private host at all already required the user path, and the cookie is
// confined to /b/. newSafeTransport(true) is used ONLY here; handleExternal
// always uses the guarded newSafeTransport(false).
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
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
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
			// Terminal nav event for HTML document loads only (Part 07:
			// one loading + one terminal per navigation). Gating on the
			// response type as well as the request protects against
			// header-less subresources (WS handshakes, EventSource) being
			// misclassified as documents and hijacking the address bar.
			if isHTML && isDocumentRequest(r) {
				s.emitNav(NavEvent{StateKey: t.StateKey, URL: t.Scheme + "://" + t.Host + t.Path, Status: resp.StatusCode, Mode: "local"})
			}
			if !isHTML {
				return nil // stream everything non-HTML untouched
			}
			return s.rewriteAndInjectResponse(resp, t)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.log.Printf("browse local: proxy error for %s://%s%s: %v", t.Scheme, t.Host, t.Path, err)
			if isDocumentRequest(r) {
				s.emitNav(NavEvent{StateKey: t.StateKey, URL: upstream.String() + t.Path, Status: http.StatusBadGateway, Mode: "local", Error: err.Error()})
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
func (s *Server) rewriteAndInjectResponse(resp *http.Response, t target) error {
	var reader io.Reader = resp.Body
	gzipped := strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip")
	if gzipped {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			// Malformed gzip from the dev server: stream the raw body rather
			// than kill the response. Loud, non-fatal.
			s.log.Printf("browse local: gzip reader for %s: %v", resp.Request.URL, err)
			return nil
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
		return err
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
	return nil
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
