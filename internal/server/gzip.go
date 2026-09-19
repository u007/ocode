package server

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// gzipMinSize is the smallest known body (via Content-Length) worth
// compressing. Below this the gzip framing overhead can exceed the savings.
const gzipMinSize = 1024

var gzipWriterPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

// gzipResponseWriter must keep exposing the optional interfaces handlers rely
// on (SSE flush, terminal WebSocket hijack, http.ResponseController unwrap).
var (
	_ http.Flusher                              = (*gzipResponseWriter)(nil)
	_ http.Hijacker                             = (*gzipResponseWriter)(nil)
	_ interface{ Unwrap() http.ResponseWriter } = (*gzipResponseWriter)(nil)
)

// gzipMiddleware negotiates gzip for /api responses when the client advertises
// it. Only /api paths are compressed: the SPA's static assets are not the
// target (and Range/conditional semantics for files stay untouched). It
// deliberately skips WebSocket upgrades (gorilla/websocket hijacks the
// connection) and leaves non-compressible or already-encoded responses alone.
//
// The ResponseWriter implements http.Flusher and http.Hijacker by delegation so
// streaming handlers and the terminal WebSocket keep working; SSE responses
// (text/event-stream) are never compressed and their flushes pass straight
// through.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.Method == http.MethodHead ||
			r.Header.Get("Upgrade") != "" ||
			!acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.Close()
		next.ServeHTTP(gw, r)
	})
}

// gzipResponseWriter lazily decides whether to compress on the first
// WriteHeader, based on the response's status, Content-Type and Content-Length.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz       *gzip.Writer
	compress bool
	wroteHdr bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHdr {
		return
	}
	w.wroteHdr = true
	if shouldGzip(status, w.Header()) {
		w.compress = true
		h := w.Header()
		h.Set("Content-Encoding", "gzip")
		// The compressed length is unknown until the body is streamed; let the
		// server use chunked encoding instead of a stale identity length.
		h.Del("Content-Length")
		addVary(h, "Accept-Encoding")
	}
	w.ResponseWriter.WriteHeader(status)
	if w.compress {
		w.gz = gzipWriterPool.Get().(*gzip.Writer)
		w.gz.Reset(w.ResponseWriter)
	}
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHdr {
		w.WriteHeader(http.StatusOK)
	}
	if w.compress && w.gz != nil {
		return w.gz.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

// Flush implements http.Flusher. For a compressed response the gzip stream is
// flushed first so buffered bytes reach the client; the underlying writer is
// always flushed when it supports it.
func (w *gzipResponseWriter) Flush() {
	if w.compress && w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker so WebSocket upgrades (terminal) keep
// working. Compression is disabled once a connection is hijacked.
func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	w.compress = false
	return h.Hijack()
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Close finishes a compressed body and returns the writer to the pool. Safe to
// call more than once and when no compression happened.
func (w *gzipResponseWriter) Close() {
	if w.gz == nil {
		return
	}
	_ = w.gz.Close()
	gzipWriterPool.Put(w.gz)
	w.gz = nil
}

// shouldGzip reports whether a response with the given status and headers is
// worth compressing. Unknown content types are left alone rather than sniffed.
func shouldGzip(status int, h http.Header) bool {
	if status < 200 || status == http.StatusNoContent || status == http.StatusNotModified || status == http.StatusPartialContent {
		return false
	}
	if h.Get("Content-Encoding") != "" {
		return false
	}
	if h.Get("Content-Range") != "" {
		return false
	}
	ct := h.Get("Content-Type")
	if !isCompressibleContentType(ct) {
		return false
	}
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n < gzipMinSize {
			return false
		}
	}
	return true
}

// isCompressibleContentType reports whether a Content-Type benefits from gzip.
// text/event-stream is explicitly excluded even though it starts with "text/".
func isCompressibleContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch {
	case ct == "":
		return false
	case ct == "text/event-stream":
		return false
	case strings.HasPrefix(ct, "text/"):
		return true
	case ct == "application/json" || strings.HasSuffix(ct, "+json"):
		return true
	case ct == "application/javascript" || ct == "application/x-javascript":
		return true
	case ct == "application/xml" || strings.HasSuffix(ct, "+xml"):
		return true
	case ct == "application/x-ndjson" || ct == "application/wasm":
		return true
	}
	return false
}

// acceptsGzip parses an Accept-Encoding header for a usable gzip or * token.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		enc := strings.TrimSpace(fields[0])
		if !strings.EqualFold(enc, "gzip") && enc != "*" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if v, ok := strings.CutPrefix(f, "q="); ok {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = parsed
				}
			}
		}
		return q > 0
	}
	return false
}

// addVary appends a value to a Vary header without duplicating it.
func addVary(h http.Header, value string) {
	for _, existing := range h.Values("Vary") {
		for _, v := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(v), value) {
				return
			}
		}
	}
	h.Add("Vary", value)
}
