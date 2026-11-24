package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// newAgentTestTarget returns a Target wired to a stub Chrome. Only the fields
// the agent operations read (conn, sessionID) are set, as manager_test does.
func newAgentTestTarget(t *testing.T) (*Target, *stubChrome) {
	t.Helper()
	stub := newStubChrome()
	t.Cleanup(stub.Close)
	return &Target{conn: stub.Conn(), sessionID: "sess-agent"}, stub
}

func TestTargetScreenshotReturnsPNGAndCSSViewport(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	png := []byte("\x89PNG fake bytes")
	stub.SetResult("Runtime.evaluate", map[string]any{
		"result": map[string]any{"value": map[string]any{"w": 1280, "h": 800}},
	})
	stub.SetResult("Page.captureScreenshot", map[string]any{
		"data": base64.StdEncoding.EncodeToString(png),
	})

	got, w, h, err := tgt.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if string(got) != string(png) {
		t.Errorf("png bytes = %q, want %q", got, png)
	}
	if w != 1280 || h != 800 {
		t.Errorf("css viewport = %dx%d, want 1280x800", w, h)
	}
	shots := stub.CallsFor("Page.captureScreenshot")
	if len(shots) != 1 {
		t.Fatalf("captureScreenshot calls = %d, want 1", len(shots))
	}
	if shots[0].SessionID != "sess-agent" {
		t.Errorf("captureScreenshot session = %q, want sess-agent", shots[0].SessionID)
	}
	var params map[string]any
	if err := json.Unmarshal(shots[0].Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params["format"] != "png" {
		t.Errorf("format = %v, want png", params["format"])
	}
}

func TestTargetScreenshotRejectsBadBase64(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("Runtime.evaluate", map[string]any{"result": map[string]any{"value": map[string]any{"w": 1, "h": 1}}})
	stub.SetResult("Page.captureScreenshot", map[string]any{"data": "%%%not-base64"})

	if _, _, _, err := tgt.Screenshot(context.Background()); err == nil {
		t.Fatal("Screenshot accepted undecodable data")
	}
}

func TestTargetPageTextTruncatesInPage(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("Runtime.evaluate", map[string]any{
		"result": map[string]any{"value": map[string]any{"text": "hello", "total": pageTextMaxChars + 1}},
	})

	text, truncated, err := tgt.PageText(context.Background())
	if err != nil {
		t.Fatalf("PageText: %v", err)
	}
	if text != "hello" || !truncated {
		t.Errorf("PageText = (%q, %v), want (hello, true)", text, truncated)
	}
	evals := stub.CallsFor("Runtime.evaluate")
	if len(evals) != 1 {
		t.Fatalf("Runtime.evaluate calls = %d, want 1", len(evals))
	}
	var params struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(evals[0].Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if !strings.Contains(params.Expression, "slice(0, 20000)") {
		t.Errorf("expression does not slice in the page: %s", params.Expression)
	}
}

func TestTargetPageTextNotTruncatedWhenShort(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("Runtime.evaluate", map[string]any{
		"result": map[string]any{"value": map[string]any{"text": "short", "total": 5}},
	})

	text, truncated, err := tgt.PageText(context.Background())
	if err != nil {
		t.Fatalf("PageText: %v", err)
	}
	if text != "short" || truncated {
		t.Errorf("PageText = (%q, %v), want (short, false)", text, truncated)
	}
}

func TestTargetPageTextSurfacesEvaluationException(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	stub.SetResult("Runtime.evaluate", map[string]any{
		"exceptionDetails": map[string]any{"text": "boom"},
	})

	if _, _, err := tgt.PageText(context.Background()); err == nil {
		t.Fatal("PageText swallowed an evaluation exception")
	}
}

func TestTargetClickMovesThenPressesAndReleases(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	if err := tgt.Click(context.Background(), 10.5, 20); err != nil {
		t.Fatalf("Click: %v", err)
	}
	calls := stub.CallsFor("Input.dispatchMouseEvent")
	if len(calls) != 3 {
		t.Fatalf("dispatchMouseEvent calls = %d, want 3", len(calls))
	}
	var types []string
	for _, c := range calls {
		var p struct {
			Type   string  `json:"type"`
			X      float64 `json:"x"`
			Y      float64 `json:"y"`
			Button string  `json:"button"`
		}
		if err := json.Unmarshal(c.Params, &p); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if p.X != 10.5 || p.Y != 20 {
			t.Errorf("%s at (%v,%v), want (10.5,20)", p.Type, p.X, p.Y)
		}
		if p.Type == "mouseMoved" && p.Button != "none" {
			t.Errorf("mouseMoved button = %q, want \"none\" as the panel sends", p.Button)
		}
		types = append(types, p.Type)
	}
	want := []string{"mouseMoved", "mousePressed", "mouseReleased"}
	if !reflect.DeepEqual(types, want) {
		t.Errorf("event order = %v, want %v", types, want)
	}
}

func TestTargetScrollPageDirections(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	ctx := context.Background()
	if err := tgt.ScrollPage(ctx, -1); err != nil {
		t.Fatalf("ScrollPage(-1): %v", err)
	}
	evals := stub.CallsFor("Runtime.evaluate")
	if len(evals) != 1 {
		t.Fatalf("Runtime.evaluate calls = %d, want 1", len(evals))
	}
	var p struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(evals[0].Params, &p); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if !strings.Contains(p.Expression, "scrollBy(0, -1 * window.innerHeight") {
		t.Errorf("expression = %s, want an upward scrollBy", p.Expression)
	}

	if err := tgt.ScrollPage(ctx, 2); err == nil {
		t.Error("ScrollPage accepted direction 2")
	}
	if n := len(stub.CallsFor("Runtime.evaluate")); n != 1 {
		t.Errorf("invalid direction reached Chrome: %d evaluate calls", n)
	}
}

func TestTargetPressKeyEnterSendsDownThenUp(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	if err := tgt.PressKey(context.Background(), "Enter"); err != nil {
		t.Fatalf("PressKey(Enter): %v", err)
	}
	calls := stub.CallsFor("Input.dispatchKeyEvent")
	if len(calls) != 2 {
		t.Fatalf("dispatchKeyEvent calls = %d, want 2", len(calls))
	}
	var down, up struct {
		Type string `json:"type"`
		Code string `json:"code"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(calls[0].Params, &down); err != nil {
		t.Fatalf("decode down: %v", err)
	}
	if err := json.Unmarshal(calls[1].Params, &up); err != nil {
		t.Fatalf("decode up: %v", err)
	}
	if down.Type != "keyDown" || down.Code != "Enter" || down.Text != "\r" {
		t.Errorf("down = %+v, want keyDown Enter with CR text", down)
	}
	if up.Type != "keyUp" || up.Code != "Enter" || up.Text != "" {
		t.Errorf("up = %+v, want keyUp Enter without text", up)
	}
}

func TestTargetPressKeyRejectsUnknownName(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	err := tgt.PressKey(context.Background(), "ctrl+s")
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("PressKey(ctrl+s) error = %v, want ErrUnknownKey", err)
	}
	if n := len(stub.CallsFor("Input.dispatchKeyEvent")); n != 0 {
		t.Errorf("unknown key reached Chrome: %d key events", n)
	}
}

func TestNamedKeysSortedAndComplete(t *testing.T) {
	names := NamedKeys()
	if len(names) != len(namedKeys) {
		t.Fatalf("NamedKeys len = %d, want %d", len(names), len(namedKeys))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("NamedKeys not sorted: %v", names)
	}
}

func TestManagerLookupTargetNeverCreatesOrReplaces(t *testing.T) {
	m := NewManager(ManagerOptions{})
	attached := &Target{stateKey: "tab:a"}
	m.targets["tab:a"] = attached

	got, ok := m.LookupTarget("tab:a")
	if !ok || got != attached {
		t.Fatalf("LookupTarget(tab:a) = (%p, %v), want the attached target", got, ok)
	}
	if _, ok := m.LookupTarget("tab:missing"); ok {
		t.Fatal("LookupTarget reported a target for an unattached tab")
	}
	if len(m.targets) != 1 {
		t.Fatalf("LookupTarget changed the target map: %d entries", len(m.targets))
	}
}

// TestTargetInputScaleTracksAppliedZoom pins the factor the socket applies to
// the SPA's canvas pixels: a viewport apply lays the page out at viewport/zoom,
// so a visual point maps to layout point/zoom. It stays 1 until a viewport is
// actually applied, because no device metrics have changed yet.
func TestTargetInputScaleTracksAppliedZoom(t *testing.T) {
	tgt, _ := newAgentTestTarget(t)
	ctx := context.Background()
	if got := tgt.InputScale(); got != 1 {
		t.Fatalf("InputScale before resize = %v, want 1", got)
	}
	if err := tgt.Resize(ctx, 1000, 600, 1); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// Resized at 100% is the normal no-zoom state: the apply commits zoom 1, so
	// this must still read as 1.
	if got := tgt.InputScale(); got != 1 {
		t.Fatalf("InputScale after resize without zoom = %v, want 1", got)
	}
	if err := tgt.SetZoom(ctx, 2); err != nil {
		t.Fatalf("SetZoom: %v", err)
	}
	if got := tgt.InputScale(); got != 0.5 {
		t.Fatalf("InputScale at zoom 2 = %v, want 0.5", got)
	}
	// A zoom that arrives before any viewport is inert: no metrics applied.
	unsized, _ := newAgentTestTarget(t)
	if err := unsized.SetZoom(ctx, 2); err != nil {
		t.Fatalf("SetZoom: %v", err)
	}
	if got := unsized.InputScale(); got != 1 {
		t.Fatalf("InputScale after unsized SetZoom = %v, want 1", got)
	}
}

// TestTargetClickKeepsLayoutCoordinates pins that Target.Click/Mouse take
// layout CSS pixels (DOM quads, element clicks). Only the SPA socket converts
// canvas pixels, so this must stay unscaled even at zoom != 1.
func TestTargetClickKeepsLayoutCoordinates(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	ctx := context.Background()
	if err := tgt.Resize(ctx, 1000, 600, 1); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := tgt.SetZoom(ctx, 2); err != nil {
		t.Fatalf("SetZoom: %v", err)
	}
	if err := tgt.Click(ctx, 100, 50); err != nil {
		t.Fatalf("Click: %v", err)
	}
	calls := stub.CallsFor("Input.dispatchMouseEvent")
	if len(calls) != 3 {
		t.Fatalf("click dispatched %d mouse events, want 3", len(calls))
	}
	for _, c := range calls {
		var p struct {
			Type string  `json:"type"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
		}
		if err := json.Unmarshal(c.Params, &p); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if p.X != 100 || p.Y != 50 {
			t.Errorf("%s at (%v,%v), want (100,50): layout pixels are not rescaled", p.Type, p.X, p.Y)
		}
	}
}

// TestTargetSetZoomKeepsAppliedFactorOnMetricsFailure pins publish-after-apply:
// a failed Emulation.setDeviceMetricsOverride must not leave InputScale
// reporting a zoom the page never received.
func TestTargetSetZoomKeepsAppliedFactorOnMetricsFailure(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	ctx := context.Background()
	if err := tgt.Resize(ctx, 1000, 600, 1); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := tgt.SetZoom(ctx, 2); err != nil {
		t.Fatalf("SetZoom: %v", err)
	}
	stub.SetFailure("Emulation.setDeviceMetricsOverride", "Target closed")
	if err := tgt.SetZoom(ctx, 3); err == nil {
		t.Fatal("SetZoom swallowed the metrics failure")
	}
	if got := tgt.InputScale(); got != 0.5 {
		t.Fatalf("InputScale after a failed zoom = %v, want the applied 0.5", got)
	}
}

// TestTargetSetZoomSkipsRedundantApply pins the no-op guard: the SPA re-sends
// its zoom on every socket open, so a factor the layout already has must not
// re-apply metrics or restart the screencast.
func TestTargetSetZoomSkipsRedundantApply(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	tgt.sink = &testSink{}
	ctx := context.Background()
	if err := tgt.Resize(ctx, 1000, 600, 1); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := tgt.SetZoom(ctx, 2); err != nil {
		t.Fatalf("SetZoom: %v", err)
	}
	metrics := len(stub.CallsFor("Emulation.setDeviceMetricsOverride"))
	starts := len(stub.CallsFor("Page.startScreencast"))
	if err := tgt.SetZoom(ctx, 2); err != nil {
		t.Fatalf("repeated SetZoom: %v", err)
	}
	if got := len(stub.CallsFor("Emulation.setDeviceMetricsOverride")); got != metrics {
		t.Fatalf("metrics calls = %d, want %d (an unchanged factor must not reapply)", got, metrics)
	}
	if got := len(stub.CallsFor("Page.startScreencast")); got != starts {
		t.Fatalf("startScreencast calls = %d, want %d (an unchanged factor must not restart)", got, starts)
	}
}

// TestTargetInputScaleStaysOneForUnsizedZoomThenFailedResize pins the applied
// latch rather than a zero zoom: an unsized SetZoom stores the factor, and a
// resize whose metrics then fail must still report scale 1 because the page is
// laid out at 100%.
func TestTargetInputScaleStaysOneForUnsizedZoomThenFailedResize(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	ctx := context.Background()
	if err := tgt.SetZoom(ctx, 2); err != nil {
		t.Fatalf("SetZoom: %v", err)
	}
	stub.SetFailure("Emulation.setDeviceMetricsOverride", "Target closed")
	if err := tgt.Resize(ctx, 1000, 600, 1); err == nil {
		t.Fatal("Resize swallowed the metrics failure")
	}
	if got := tgt.InputScale(); got != 1 {
		t.Fatalf("InputScale = %v, want 1 while no metrics were applied", got)
	}
}

// TestTargetSetZoomCommitsFactorBeforeScreencastRestart pins that a screencast
// restart failure after accepted device metrics does not desync input scaling:
// the page IS laid out at the new zoom, so InputScale must report it even though
// SetZoom returned an error.
func TestTargetSetZoomCommitsFactorBeforeScreencastRestart(t *testing.T) {
	tgt, stub := newAgentTestTarget(t)
	tgt.sink = &testSink{}
	ctx := context.Background()
	if err := tgt.Resize(ctx, 1000, 600, 1); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := tgt.SetZoom(ctx, 2); err != nil {
		t.Fatalf("SetZoom: %v", err)
	}
	stub.SetFailure("Page.startScreencast", "renderer busy")
	if err := tgt.SetZoom(ctx, 4); err == nil {
		t.Fatal("SetZoom swallowed the screencast failure")
	}
	if got := tgt.InputScale(); got != 0.25 {
		t.Fatalf("InputScale after metrics applied at zoom 4 = %v, want 0.25", got)
	}
}
