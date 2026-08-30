package browse

// handleLocal serves loopback/RFC1918 upstreams as a transparent streaming
// reverse proxy. It is reachable only for targets whose host literal is
// private (parseTarget sets t.Local); an external page's rewritten links
// always carry an external host and therefore route to handleExternal. A
// private host can only have been reached via a user-initiated address-bar
// navigation (which minted the __grant); subresources ride the /b/ cookie.
// We deliberately do NOT add a per-request "user-initiated" flag — reaching a
// private host at all already required the user path, and the cookie is
// confined to /b/. newSafeTransport(true) is used ONLY here; handleExternal
// always uses the guarded newSafeTransport(false).

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
			s.emitNav(NavEvent{StateKey: t.StateKey, URL: upstream.String() + t.Path, Status: http.StatusBadGateway, Mode: "local", Error: err.Error()})
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
	injected := injectCapture(raw, stateKey, s.spaOrigin)

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

func isDocumentRequest(r *http.Request) bool {
	d := r.Header.Get("Sec-Fetch-Dest")
	return d == "" || strings.EqualFold(d, "document") || strings.EqualFold(d, "iframe")
}
