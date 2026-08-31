package browse

import (
	"compress/gzip"
	"errors"
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
	if isServiceWorkerRequest(r) {
		http.Error(w, "browse: service workers are blocked", http.StatusForbidden)
		return
	}

	origin := upstreamOrigin(t)
	upURL := &url.URL{Scheme: t.Scheme, Host: t.Host, Path: t.Path, RawQuery: t.RawQuery}

	// Loading nav event for top-level document navigations only, before any
	// upstream round-trip, so the SPA shows "navigating" even if the fetch
	// later fails (Part 07: exactly one loading + one terminal per nav).
	if isDocumentRequest(r) {
		s.emitNav(NavEvent{StateKey: t.StateKey, URL: origin + t.Path, Status: 0, Mode: "proxied"})
	}

	body := r.Body // streamed through for POST/PUT/etc.
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upURL.String(), body)
	if err != nil {
		s.log.Printf("browse: build upstream request %s: %v", upURL, err)
		s.failNav(w, r, t, "bad upstream request")
		return
	}
	// Copy safe request headers, then sanitize.
	copyHeader(req.Header, r.Header)
	filterRequestHeaders(req)
	s.jar.Apply(t.StateKey, origin, req)

	client := &http.Client{
		Transport:     s.transport,
		Timeout:       upstreamTimeout,
		CheckRedirect: capRedirects(maxRedirects), // SSRF re-checked per hop in the dialer.
	}
	resp, err := client.Do(req)
	if err != nil {
		s.log.Printf("browse: upstream fetch %s failed: %v", upURL, err)
		s.failNav(w, r, t, classifyFetchError(err))
		return
	}
	defer resp.Body.Close()

	s.jar.Store(t.StateKey, origin, resp)
	// Site cookies live in the server-side jar only (spec § External mode:
	// "never forwarded to the browser"). Forwarding Set-Cookie would let
	// page JS on the browse origin read/keep upstream cookies.
	resp.Header.Del("Set-Cookie")

	// Decode gzip so we can inspect/rewrite; drop the header afterwards.
	reader := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, gerr := gzip.NewReader(resp.Body)
		if gerr != nil {
			s.log.Printf("browse: gzip reader for %s: %v", upURL, gerr)
			s.failNav(w, r, t, "decode error")
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

	// Emit the authoritative terminal nav event for the top-level document
	// only (Part 07 guard: never per-subresource, never for image/script/
	// style/font/fetch dests). Emitted for any Content-Type so a download or
	// JSON doc still closes the loading event.
	if isDocumentRequest(r) {
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
		s.failNav(w, r, t, "read error")
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
		// base is the resolution base for relative URLs: "" means "resolve
		// against the document target" (the rewriteHTML contract). Passing
		// spaOrigin here (an earlier draft) would route root-relative asset
		// URLs at the SPA server instead of the upstream — live QA caught it.
		rewritten, rerr := rewriteHTML(buf, t, "")
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

// failNav emits the terminal nav event for a failed navigation — but only
// for top-level document requests (Part 07 guard): a failed subresource must
// not rewrite the address bar. The HTTP error response is sent either way.
func (s *Server) failNav(w http.ResponseWriter, r *http.Request, t target, reason string) {
	if isDocumentRequest(r) {
		s.emitNav(NavEvent{StateKey: t.StateKey, URL: upstreamOrigin(t) + t.Path, Status: http.StatusBadGateway, Mode: "proxied", Error: reason})
	}
	http.Error(w, "browse: "+reason, http.StatusBadGateway)
}

// classifyFetchError maps an upstream client error to a short nav-event
// reason. The SSRF branch uses errors.Is against the dialer's sentinel
// (part 02) because net.Dial wraps Control errors in *net.OpError — a
// substring match on the wrapper text would be brittle.
func classifyFetchError(err error) string {
	switch {
	case errors.Is(err, errPrivateAddr):
		return "blocked: private address"
	default:
		msg := err.Error()
		switch {
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

// (Part 05) injectCapture now lives in capture.go; (Part 04) rewriteHTML /
// rewriteCSS live in rewrite.go; (Part 06) handleLocal lives in local.go.
