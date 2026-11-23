package browse

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestInjectCaptureInsertsBootstrapAndScript(t *testing.T) {
	in := []byte(`<!doctype html><html><head><title>x</title></head><body>hi</body></html>`)
	out := string(injectCapture(in, target{StateKey: "tab:abc"}, "http://127.0.0.1:5000", ""))

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
	out := string(injectCapture(in, target{StateKey: "tab:x"}, "http://o", ""))
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

// TestCaptureBundleInspectorBlock pins the local-mode DOM inspector contract in
// capture.js. The SPA drives it entirely through these message names and the
// shared path scheme, so a rename on either side silently breaks the DOM tab
// with no compile error anywhere.
func TestCaptureBundleInspectorBlock(t *testing.T) {
	js := string(captureJS)

	for _, msg := range []string{
		"ocode:browse:inspector-tree",
		"ocode:browse:inspector-capture",
		"ocode:browse:inspector-pick-set",
		"ocode:browse:inspector-picked",
		"ocode:browse:inspector-highlight",
	} {
		if !strings.Contains(js, msg) {
			t.Errorf("capture.js lost inspector message %q", msg)
		}
	}

	// A context menu is reported, but a menu the page already cancelled, and
	// any menu while pick mode owns the click, must be left to the page/picker.
	if !strings.Contains(js, `window.addEventListener("contextmenu"`) {
		t.Error("capture.js lost the contextmenu listener")
	}
	if !strings.Contains(js, "e.defaultPrevented") {
		t.Error("capture.js must skip a contextmenu the page already handled")
	}
	if !strings.Contains(js, "if (pickOn || e.defaultPrevented) return;") {
		t.Error("capture.js contextmenu guard lost the pickOn/defaultPrevented check")
	}

	// pathOf / nodeAtPath are ONE addressing scheme shared by the tree, the
	// highlight and the picker: documentElement is "0", then childNodes indices.
	if !strings.Contains(js, "function pathOf(node)") {
		t.Error("capture.js lost pathOf()")
	}
	if !strings.Contains(js, "function nodeAtPath(path)") {
		t.Error("capture.js lost nodeAtPath()")
	}
	if !strings.Contains(js, `"0" + (segs.length ? "/" + segs.join("/") : "")`) {
		t.Error("capture.js path scheme must root at documentElement and join childNodes indices")
	}

	// The capture library is loaded lazily from a URL derived from THIS
	// script's own src, so a prefixed/remote mount resolves the sibling route.
	if !strings.Contains(js, `scriptSrc.replace(`) || !strings.Contains(js, `"/__ocode_capture_lib.js"`) {
		t.Error("capture.js must derive the lazy library URL from its own script src")
	}

	// Replies reuse their request's type. A page opened directly (parent ===
	// window) receives its own reply, so the listener must ignore payload-
	// bearing messages or it re-asks forever.
	if !strings.Contains(js, `if (d.root || "dataUrl" in d || "error" in d) return;`) {
		t.Error("capture.js must not treat an inspector reply as a new request")
	}
}

// TestServeCaptureLibBundle pins the second embedded asset: the vendored
// html-to-image UMD bundle, served as JavaScript with its provenance header
// intact (the header names the upstream project and the global it installs).
func TestServeCaptureLibBundle(t *testing.T) {
	s := New("apitoken", nil)
	r := httptest.NewRequest("GET", "/__ocode_capture_lib.js", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") &&
		!strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("content-type = %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "html-to-image") {
		t.Error("served library missing the vendored html-to-image header")
	}
	if !strings.Contains(body, "window.htmlToImage") {
		t.Error("served library missing its window.htmlToImage marker")
	}
	if !strings.Contains(string(captureLibJS), "window.htmlToImage") {
		t.Error("embedded capture_lib.js does not install window.htmlToImage")
	}
}

// jsFuncSource returns the source of `function name(...) {...}` from js, found
// by brace matching from its declaration. The capture script is one IIFE with
// no module boundary, so its pure helpers are lifted out and run under node.
// Braces inside string literals would confuse the matcher; the helpers under
// test contain none.
func jsFuncSource(t *testing.T, js, name string) string {
	t.Helper()
	start := strings.Index(js, "function "+name+"(")
	if start < 0 {
		t.Fatalf("capture.js lost function %s", name)
	}
	open := strings.Index(js[start:], "{")
	if open < 0 {
		t.Fatalf("capture.js function %s has no body", name)
	}
	depth := 0
	for i := start + open; i < len(js); i++ {
		switch js[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return js[start : i+1]
			}
		}
	}
	t.Fatalf("capture.js function %s has unbalanced braces", name)
	return ""
}

// runCaptureHelpers runs the extracted helpers plus a driver under node. The
// driver prints "ok" when every case passes and the failures otherwise. Skips
// when node is not installed, so CI without node still runs the string pins.
func runCaptureHelpers(t *testing.T, driver string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; capture helper behaviour checks skipped")
	}
	js := string(captureJS)
	src := strings.Join([]string{
		jsFuncSource(t, js, "clampRect"),
		jsFuncSource(t, js, "wantsNativeMenu"),
		driver,
	}, "\n")
	out, err := exec.Command(node, "-e", src).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("capture helper behaviour check failed (err=%v):\n%s", err, out)
	}
}

// TestCaptureBundleClampRect pins the crop clamp that keeps the screenshot's
// surrounding context inside the rendered page at all four edges. A rect that
// hangs off an edge must keep only the part that exists, and the outline
// offset inside the crop is then element origin minus the clamped origin.
func TestCaptureBundleClampRect(t *testing.T) {
	runCaptureHelpers(t, `
var fails = [];
function eq(name, got, want) {
  var g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) fails.push(name + ": got " + g + " want " + w);
}
// 200x100 rendered page image.
eq("inside", clampRect({x: 10, y: 10, w: 50, h: 40}, 200, 100), {x: 10, y: 10, w: 50, h: 40});
eq("top-left corner", clampRect({x: -16, y: -16, w: 82, h: 72}, 200, 100), {x: 0, y: 0, w: 66, h: 56});
eq("left edge only", clampRect({x: -16, y: 20, w: 40, h: 10}, 200, 100), {x: 0, y: 20, w: 24, h: 10});
eq("top edge only", clampRect({x: 20, y: -16, w: 40, h: 30}, 200, 100), {x: 20, y: 0, w: 40, h: 14});
eq("bottom-right corner", clampRect({x: 180, y: 80, w: 50, h: 40}, 200, 100), {x: 180, y: 80, w: 20, h: 20});
eq("right edge only", clampRect({x: 180, y: 20, w: 50, h: 10}, 200, 100), {x: 180, y: 20, w: 20, h: 10});
eq("bottom edge only", clampRect({x: 20, y: 80, w: 40, h: 40}, 200, 100), {x: 20, y: 80, w: 40, h: 20});
eq("whole page", clampRect({x: -5, y: -5, w: 300, h: 300}, 200, 100), {x: 0, y: 0, w: 200, h: 100});
eq("fractional edges round", clampRect({x: 9.6, y: 0.4, w: 20.2, h: 5}, 200, 100), {x: 10, y: 0, w: 20, h: 5});
eq("off right edge", clampRect({x: 250, y: 10, w: 10, h: 10}, 200, 100).w, 0);
eq("off bottom edge", clampRect({x: 10, y: 120, w: 10, h: 10}, 200, 100).h, 0);
if (fails.length) { console.log(fails.join("\n")); process.exit(1); }
console.log("ok");
`)
}

// TestCaptureBundleContextMenuPassThrough pins the four conditions under which
// the page keeps the native context menu: not embedded, Shift held, an editable
// target, or a text selection. The listener must decide before preventDefault.
func TestCaptureBundleContextMenuPassThrough(t *testing.T) {
	js := string(captureJS)
	start := strings.Index(js, `window.addEventListener("contextmenu"`)
	end := strings.Index(js, `"ocode:browse:contextmenu"`)
	if start < 0 || end < start {
		t.Fatal("capture.js lost the contextmenu listener body")
	}
	body := js[start:end]
	gate := strings.Index(body, "wantsNativeMenu(window.parent !== window")
	prevent := strings.Index(body, "e.preventDefault()")
	if gate < 0 {
		t.Error("contextmenu listener must consult wantsNativeMenu for the embedded/Shift/editable/selection pass-through")
	}
	if prevent < 0 || gate > prevent {
		t.Error("contextmenu pass-through must return before e.preventDefault()")
	}
	if !strings.Contains(body, "if (pickOn || e.defaultPrevented) return;") {
		t.Error("contextmenu listener lost the existing pickOn/defaultPrevented early return")
	}

	runCaptureHelpers(t, `
var fails = [];
function eq(name, got, want) {
  var g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) fails.push(name + ": got " + g + " want " + w);
}
eq("embedded, plain right-click shows replacement menu", wantsNativeMenu(true, false, false, ""), false);
eq("not embedded keeps native menu", wantsNativeMenu(false, false, false, ""), true);
eq("shift keeps native menu", wantsNativeMenu(true, true, false, ""), true);
eq("editable target keeps native menu", wantsNativeMenu(true, false, true, ""), true);
eq("text selection keeps native menu", wantsNativeMenu(true, false, false, "copy me"), true);
eq("collapsed selection is empty", wantsNativeMenu(true, false, false, ""), false);
if (fails.length) { console.log(fails.join("\n")); process.exit(1); }
console.log("ok");
`)
}

// TestCaptureBundleFullContextCrop pins the full-document screenshot branch:
// the crop grows the element by the context margin (real page pixels), while
// the outline stays on the element itself.
func TestCaptureBundleFullContextCrop(t *testing.T) {
	js := string(captureJS)
	if !strings.Contains(js, "{ x: ex - pad, y: ey - pad, w: sw + pad * 2, h: sh + pad * 2 }") {
		t.Error("full-document capture must expand the source crop by pad on each side")
	}
	if !strings.Contains(js, "{ x: ex, y: ey, w: sw, h: sh }, 0)") {
		t.Error("full-document capture must keep the outline on the element with no white margin")
	}
}

// TestCaptureLibRouteRejectsNonGet pins the route registration itself: the SPA
// fetches the bundle with GET, and a non-GET request must not serve it (the
// root-relative catch-all answers other verbs, so only "not the bundle" is a
// stable assertion here).
func TestCaptureLibRouteRejectsNonGet(t *testing.T) {
	s := New("apitoken", nil)
	post := httptest.NewRequest("POST", "/__ocode_capture_lib.js", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, post)
	if w.Code == http.StatusOK {
		t.Fatalf("POST served the capture library (status 200); route is not GET-only")
	}
	if strings.Contains(w.Body.String(), "window.htmlToImage") {
		t.Fatal("POST response carried the capture library body")
	}
}
