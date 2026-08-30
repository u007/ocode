package browse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInjectCaptureInsertsBootstrapAndScript(t *testing.T) {
	in := []byte(`<!doctype html><html><head><title>x</title></head><body>hi</body></html>`)
	out := string(injectCapture(in, "tab:abc", "http://127.0.0.1:5000"))

	// Bootstrap must carry the exact stateKey + spaOrigin.
	if !strings.Contains(out, `window.__OCODE_BROWSE__`) {
		t.Fatal("missing bootstrap global")
	}
	if !strings.Contains(out, `"tab:abc"`) || !strings.Contains(out, `"http://127.0.0.1:5000"`) {
		t.Fatalf("bootstrap missing stateKey/spaOrigin: %s", out)
	}
	// Script tag referencing the served capture bundle must be present.
	if !strings.Contains(out, `src="/__ocode_capture.js"`) {
		t.Fatal("missing capture script tag")
	}
	// Injection must land inside <head>, before the existing <title>.
	headIdx := strings.Index(out, "<head>")
	titleIdx := strings.Index(out, "<title>")
	bootIdx := strings.Index(out, "__OCODE_BROWSE__")
	if !(headIdx < bootIdx && bootIdx < titleIdx) {
		t.Fatalf("bootstrap not first child of head: head=%d boot=%d title=%d", headIdx, bootIdx, titleIdx)
	}
}

func TestInjectCaptureFallbackNoHead(t *testing.T) {
	// A fragment without <head> should still receive the script (before </html>
	// or appended), so capture never silently no-ops.
	in := []byte(`<html><body>hi</body></html>`)
	out := string(injectCapture(in, "tab:x", "http://o"))
	if !strings.Contains(out, `src="/__ocode_capture.js"`) {
		t.Fatalf("fallback injection missing: %s", out)
	}
}

func TestServeCaptureBundle(t *testing.T) {
	s := New("apitoken", nil)
	r := httptest.NewRequest("GET", "/__ocode_capture.js", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") &&
		!strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("content-type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "ocode:browse:console") {
		t.Fatal("served bundle missing console event type")
	}
}
