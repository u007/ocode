package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// testSPAWebFS builds a minimal embedded-SPA filesystem: index.html plus one
// static asset, enough to exercise both the file hit and the SPA fallback.
func testSPAWebFS() fs.FS {
	fsys := fstest.MapFS{
		"index.html":          &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>spa</body></html>")},
		"assets/index-abc.js": &fstest.MapFile{Data: []byte("console.log('spa')")},
		"favicon.ico":         &fstest.MapFile{Data: []byte("icon")},
	}
	return fsys
}

// The embedded bundle must carry explicit Cache-Control: embed.FS files report
// a zero ModTime, so Go emits no Last-Modified/ETag and the browser has no
// validator — without a directive it re-downloads every hashed asset on each
// load. Hashed assets are immutable; index.html and the SPA fallback must
// revalidate so a new build is served immediately.
func TestSPAHandlerCacheHeaders(t *testing.T) {
	h := spaHandler(testSPAWebFS())

	cases := []struct {
		name       string
		path       string
		wantSubstr string
		wantAbsent string
	}{
		{"hashed asset", "/assets/index-abc.js", "immutable", "no-cache"},
		{"index", "/", "no-cache", "immutable"},
		{"spa fallback", "/session/abc", "no-cache", "immutable"},
		{"non-hashed root file", "/favicon.ico", "no-cache", "immutable"},
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

// Unknown /api/* paths must get a JSON 404, never the index.html SPA
// fallback: a 200 text/html response on an API route makes the SPA's
// fetchJSON parse HTML — in WebKit that surfaces as the cryptic
// "SyntaxError: The string did not match the expected pattern" instead of a
// readable 404, and it defeats availability probes that branch on status
// (e.g. isPortMapsAvailable). Regression test for the desktop port-maps
// panel showing the WebKit SyntaxError in local desktop mode.
func TestSPAHandlerAPINotFoundIsJSON(t *testing.T) {
	h := spaHandler(testSPAWebFS())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/desktop/portmaps", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET unknown /api path = %d, want 404", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("body = %q, want a JSON error object", rec.Body.String())
	}

	// A deep unknown API route behaves the same.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("DELETE", "/api/desktop/portmaps/4000", nil))
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown /api path = %d, want 404", rec2.Code)
	}
}

// The SPA fallback must keep working for client-side routes (no /api prefix).
func TestSPAHandlerServesIndexForClientRoutes(t *testing.T) {
	h := spaHandler(testSPAWebFS())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/session/abc123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET client route = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spa") {
		t.Fatalf("body = %q, want index.html contents", rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("GET", "/", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec2.Code)
	}
}
