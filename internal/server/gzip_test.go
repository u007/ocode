package server

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// hijackableRecorder is an httptest.ResponseRecorder that also advertises
// http.Hijacker, so gzipResponseWriter.Hijack delegation can be asserted.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func serveGzip(t *testing.T, acceptEncoding, path string, handler http.HandlerFunc) *http.Response {
	t.Helper()
	h := gzipMiddleware(handler)
	req := httptest.NewRequest("GET", path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func TestGzipMiddlewareCompressesJSON(t *testing.T) {
	body := map[string]string{"payload": strings.Repeat("x", 5000)}
	res := serveGzip(t, "gzip", "/api/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	})
	if got := res.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if !strings.Contains(res.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", res.Header.Get("Vary"))
	}
	gr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	raw, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decompressed body is not JSON: %v", err)
	}
	if decoded["payload"] != body["payload"] {
		t.Fatal("decompressed body does not match")
	}
}

func TestGzipMiddlewareSkipsWithoutAcceptEncoding(t *testing.T) {
	res := serveGzip(t, "", "/api/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"a": strings.Repeat("x", 5000)})
	})
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
}

func TestGzipMiddlewareSkipsNonAPIPaths(t *testing.T) {
	res := serveGzip(t, "gzip", "/assets/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(strings.Repeat("var a=1;", 2000)))
	})
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("static path compressed: Content-Encoding = %q", got)
	}
}

func TestGzipMiddlewareSkipsEventStreamButKeepsFlusher(t *testing.T) {
	res := serveGzip(t, "gzip", "/api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("handler lost http.Flusher through gzip middleware")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: hello\n\n")
		flusher.Flush()
	})
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("SSE response compressed: Content-Encoding = %q", got)
	}
	raw, _ := io.ReadAll(res.Body)
	if string(raw) != "data: hello\n\n" {
		t.Fatalf("SSE body altered: %q", string(raw))
	}
}

func TestGzipMiddlewareSkipsSmallBodies(t *testing.T) {
	res := serveGzip(t, "gzip", "/api/config/model", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "17")
		_, _ = io.WriteString(w, `{"model":"gpt-4o"}`)
	})
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("tiny body compressed: Content-Encoding = %q", got)
	}
}

func TestGzipMiddlewareSkipsUpgrade(t *testing.T) {
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A WebSocket upgrade must never be compressed: gorilla/websocket writes
		// a 101 and hijacks the connection, which a gzip envelope would corrupt.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	req := httptest.NewRequest("GET", "/api/terminal/ws", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Result().Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("upgrade response compressed: %q", got)
	}
}

func TestGzipResponseWriterHijackDelegates(t *testing.T) {
	hr := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	gw := &gzipResponseWriter{ResponseWriter: hr}
	if _, _, err := gw.Hijack(); err != nil {
		t.Fatalf("Hijack returned error: %v", err)
	}
	if !hr.hijacked {
		t.Fatal("Hijack did not delegate to the underlying writer")
	}
	// A writer without Hijacker support must surface ErrNotSupported.
	gw2 := &gzipResponseWriter{ResponseWriter: httptest.NewRecorder()}
	if _, _, err := gw2.Hijack(); err != http.ErrNotSupported {
		t.Fatalf("Hijack on plain recorder err = %v, want ErrNotSupported", err)
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":                    false,
		"identity":            false,
		"gzip":                true,
		"gzip, deflate, br":   true,
		"deflate":             false,
		"*":                   true,
		"gzip;q=0":            false,
		"gzip;q=0.5, deflate": true,
		"br;q=1, gzip;q=0":    false,
	}
	for header, want := range cases {
		if got := acceptsGzip(header); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestIsCompressibleContentType(t *testing.T) {
	cases := map[string]bool{
		"application/json":                true,
		"application/json; charset=utf-8": true,
		"text/event-stream":               false,
		"text/html":                       true,
		"application/javascript":          true,
		"application/xml":                 true,
		"application/octet-stream":        false,
		"image/png":                       false,
		"":                                false,
	}
	for ct, want := range cases {
		if got := isCompressibleContentType(ct); got != want {
			t.Errorf("isCompressibleContentType(%q) = %v, want %v", ct, got, want)
		}
	}
}

// TestGzipMiddlewareOverRealServer exercises negotiation through the real
// net/http stack (chunked writes + automatic client decompression).
func TestGzipMiddlewareOverRealServer(t *testing.T) {
	payload := strings.Repeat("abcdefgh", 2000)
	srv := httptest.NewServer(gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"payload": payload})
	})))
	defer srv.Close()

	// No explicit Accept-Encoding: the default Transport advertises gzip and
	// transparently decompresses, so a correct body proves end-to-end gzip.
	res, err := srv.Client().Get(srv.URL + "/api/models")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("body is not decompressed JSON: %v (first bytes %q)", err, string(raw[:min(len(raw), 40)]))
	}
	if decoded["payload"] != payload {
		t.Fatal("round-tripped payload mismatch")
	}
}
