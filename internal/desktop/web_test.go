package desktop

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// Unknown /api/* paths must get a JSON 404 from the remote SPA handler too:
// a 200 text/html fallback on an API route makes the SPA's fetchJSON parse
// HTML — WebKit (desktop WKWebView) surfaces that as the cryptic
// "SyntaxError: The string did not match the expected pattern" instead of a
// readable 404, and it defeats availability probes that branch on status
// (e.g. isPortMapsAvailable). Mirrors internal/server.spaHandler's guard.
func TestRemoteSPAHandlerAPINotFoundIsJSON(t *testing.T) {
	h := remoteSPAHandler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>spa</body></html>")},
	})
	if h == nil {
		t.Fatal("remoteSPAHandler returned nil")
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/desktop/portmaps", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET unknown /api path = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("body = %q, want a JSON error object", rec.Body.String())
	}

	// Client-side routes still get the SPA fallback.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("GET", "/session/abc123", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET client route = %d, want 200", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "spa") {
		t.Fatalf("body = %q, want index.html contents", rec2.Body.String())
	}
}

// remoteSPAHandler must set the same cache policy as internal/server.spaHandler:
// embed.FS has a zero ModTime, so with no directive the webview re-downloads
// every hashed asset on each load. Hashed assets are immutable; index.html and
// the SPA fallback revalidate.
func TestRemoteSPAHandlerCacheHeaders(t *testing.T) {
	h := remoteSPAHandler(fstest.MapFS{
		"index.html":          &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>spa</body></html>")},
		"assets/index-abc.js": &fstest.MapFile{Data: []byte("console.log('spa')")},
	})

	cases := []struct {
		name       string
		path       string
		wantSubstr string
		wantAbsent string
	}{
		{"hashed asset", "/assets/index-abc.js", "immutable", "no-cache"},
		{"index", "/", "no-cache", "immutable"},
		{"spa fallback", "/session/abc", "no-cache", "immutable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tc.path, rec.Code)
			}
			cc := rec.Header().Get("Cache-Control")
			if !strings.Contains(cc, tc.wantSubstr) {
				t.Fatalf("GET %s Cache-Control = %q, want it to contain %q", tc.path, cc, tc.wantSubstr)
			}
			if strings.Contains(cc, tc.wantAbsent) {
				t.Fatalf("GET %s Cache-Control = %q, must not contain %q", tc.path, cc, tc.wantAbsent)
			}
		})
	}
}

// Compile-time sanity: the embedded-SPA filesystem type remoteSPAHandler
// expects satisfies fs.ReadFileFS (the Open call it relies on).
var _ fs.ReadFileFS = fstest.MapFS{}
