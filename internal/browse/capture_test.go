package browse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInjectCaptureInsertsBootstrapAndScript(t *testing.T) {
	in := []byte(`<!doctype html><html><head><title>x</title></head><body>hi</body></html>`)
	out := string(injectCapture(in, "tab:abc", "http://127.0.0.1:5000", ""))

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
	out := string(injectCapture(in, "tab:x", "http://o", ""))
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

// TestCaptureBundleRerouteRules pins the two reroute invariants that live QA
// (2026-08-31) proved easy to regress: root-relative "/x" URLs must be
// re-prefixed with the 3-segment route base (origin-root would drop out of
// the stateless route), and the WebSocket wrapper must map ws:/wss: onto the
// http/https scheme segment — parseTarget rejects "/b/{key}/ws/..." outright.
func TestCaptureBundleRerouteRules(t *testing.T) {
	js := string(captureJS)
	if !strings.Contains(js, `location.origin + routeBase() + raw`) {
		t.Error("capture.js lost the root-relative re-prefix in reroute()")
	}
	// The route base is derived RELATIVE to the mount prefix (a remote
	// project's origin is mounted at "/api/remote/<host>/browse"), so the
	// "/b/..." anchor must be matched against the path with the prefix
	// stripped — an absolute "^/b/" anchor would never match under a
	// prefixed mount and would silently break every re-prefixed URL.
	if !strings.Contains(js, `location.pathname.slice(mountBase().length)`) {
		t.Error("capture.js must derive the route base relative to the mount prefix")
	}
	if !strings.Contains(js, `function mountBase()`) {
		t.Error("capture.js lost the mountBase() prefix derivation")
	}
	if !strings.Contains(js, `mountBase() + m[1]`) {
		t.Error("capture.js must re-apply the mount prefix to the route base")
	}
	if !strings.Contains(js, `u.protocol === "wss:" ? "https" : "http"`) {
		t.Error("capture.js WebSocket wrapper must map ws/wss to http/https route schemes")
	}
}

// TestCaptureBundleRuntimeURLHooks pins the runtime-injected <script>/<link>
// reroute (webpack/Next.js lazy chunks). Lazy chunk URLs never appear in the
// initial HTML, so the Part 04 server rewrite cannot see them and they would
// otherwise resolve against the browse origin ROOT and 404 (ChunkLoadError →
// blank SPA). The hooks must stay wired through reroute().
func TestCaptureBundleRuntimeURLHooks(t *testing.T) {
	js := string(captureJS)
	if !strings.Contains(js, `hookURLProp(HTMLScriptElement.prototype, "src")`) {
		t.Error("capture.js lost the script.src runtime reroute hook")
	}
	if !strings.Contains(js, `hookURLProp(HTMLLinkElement.prototype, "href")`) {
		t.Error("capture.js lost the link.href runtime reroute hook")
	}
	if !strings.Contains(js, `desc.set.call(this, reroute(v))`) {
		t.Error("capture.js runtime URL hooks must pipe through reroute()")
	}
	if !strings.Contains(js, `Element.prototype.setAttribute`) {
		t.Error("capture.js lost the setAttribute(src/href) reroute path")
	}
}
