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
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>spa</body></html>")},
	}
	return fsys
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
