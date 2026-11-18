package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"
)

// solidImage is a w x h image filled with c.
func solidImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// decodePNG decodes raw, which must be a PNG.
func decodePNG(t *testing.T, raw []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("output is not a PNG: %v", err)
	}
	return img
}

// sameColour reports whether two colours are equal after alpha premultiplication.
func sameColour(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

var (
	testBlue  = color.RGBA{R: 0, G: 0, B: 255, A: 255}
	testBlack = color.RGBA{A: 255}
)

func TestComposeFrameSheet_LayoutSizes(t *testing.T) {
	cases := []struct {
		name         string
		w, h, n      int
		wantW, wantH int
	}{
		{"landscape 1 frame", 400, 200, 1, 400, 200},
		{"landscape 2 frames in one row", 400, 200, 2, 802, 200},
		{"landscape 3 frames in a 2-column grid", 400, 200, 3, 802, 402},
		{"landscape 4 frames in a 2-column grid", 400, 200, 4, 802, 402},
		{"portrait 1 frame", 200, 400, 1, 200, 400},
		{"portrait 2 frames in one row", 200, 400, 2, 402, 400},
		{"portrait 3 frames in one row", 200, 400, 3, 604, 400},
		{"portrait 4 frames in one row", 200, 400, 4, 806, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cells := make([]sheetCell, tc.n)
			for i := range cells {
				cells[i] = sheetCell{img: solidImage(tc.w, tc.h, testBlue)}
			}
			raw, err := composeFrameSheet(cells, 10000)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			b := decodePNG(t, raw).Bounds()
			if b.Dx() != tc.wantW || b.Dy() != tc.wantH {
				t.Fatalf("sheet = %dx%d, want %dx%d", b.Dx(), b.Dy(), tc.wantW, tc.wantH)
			}
		})
	}
}

func TestComposeFrameSheet_LongestSideWithinMaxSide(t *testing.T) {
	cases := []struct {
		name   string
		w, h   int
		n, max int
	}{
		{"landscape grid", 400, 200, 4, 200},
		{"portrait row", 200, 400, 3, 300},
		{"tiny budget", 400, 200, 2, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cells := make([]sheetCell, tc.n)
			for i := range cells {
				cells[i] = sheetCell{img: solidImage(tc.w, tc.h, testBlue), label: "+10 ms"}
			}
			raw, err := composeFrameSheet(cells, tc.max)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			b := decodePNG(t, raw).Bounds()
			if longest := max(b.Dx(), b.Dy()); longest > tc.max || longest < 1 {
				t.Fatalf("sheet %dx%d exceeds max side %d", b.Dx(), b.Dy(), tc.max)
			}
		})
	}
}

func TestComposeFrameSheet_ReadingOrderIsRowMajor(t *testing.T) {
	colours := []color.RGBA{
		{R: 255, A: 255},                 // first cell: top-left
		{G: 255, A: 255},                 // second: top-right
		{B: 255, A: 255},                 // third: bottom-left
		{R: 255, G: 255, B: 255, A: 255}, // fourth: bottom-right
	}
	cells := make([]sheetCell, len(colours))
	for i, c := range colours {
		cells[i] = sheetCell{img: solidImage(100, 50, c)}
	}
	raw, err := composeFrameSheet(cells, 10000)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	img := decodePNG(t, raw)
	// Cells are 100x50 with a 2 px gap: columns start at x=0 and x=102, rows at y=0 and y=52.
	for i, at := range []image.Point{{5, 5}, {107, 5}, {5, 57}, {107, 57}} {
		if !sameColour(img.At(at.X, at.Y), colours[i]) {
			t.Errorf("cell %d at %v has the wrong colour", i, at)
		}
	}
	if !sameColour(img.At(101, 5), sheetSeparator) {
		t.Error("the gap between columns is not the separator colour")
	}
}

func TestComposeFrameSheet_LabelDrawnInCorner(t *testing.T) {
	plain, err := composeFrameSheet([]sheetCell{{img: solidImage(100, 60, testBlue)}}, 1000)
	if err != nil {
		t.Fatalf("compose plain: %v", err)
	}
	labelled, err := composeFrameSheet([]sheetCell{{img: solidImage(100, 60, testBlue), label: "+512 ms"}}, 1000)
	if err != nil {
		t.Fatalf("compose labelled: %v", err)
	}
	p, l := decodePNG(t, plain), decodePNG(t, labelled)
	if !sameColour(l.At(1, 1), testBlack) {
		t.Fatal("the label's background is not solid in the top-left corner")
	}
	differ := 0
	for y := range 15 {
		for x := range 70 {
			if !sameColour(p.At(x, y), l.At(x, y)) {
				differ++
			}
		}
	}
	if differ == 0 {
		t.Fatal("the label region is the same as an unlabelled cell")
	}
	if !sameColour(p.At(99, 59), l.At(99, 59)) {
		t.Fatal("the label changed a pixel far from its corner")
	}
}

func TestComposeFrameSheet_LabelSkippedWhenItDoesNotFit(t *testing.T) {
	tiny := solidImage(20, 8, testBlue)
	plain, err := composeFrameSheet([]sheetCell{{img: tiny}}, 1000)
	if err != nil {
		t.Fatalf("compose plain: %v", err)
	}
	labelled, err := composeFrameSheet([]sheetCell{{img: tiny, label: "+512 ms"}}, 1000)
	if err != nil {
		t.Fatalf("compose labelled: %v", err)
	}
	if !bytes.Equal(plain, labelled) {
		t.Fatal("a label wider than its cell was drawn over the cell")
	}
}

func TestComposeFrameSheet_RefusesImpossibleInput(t *testing.T) {
	four := make([]sheetCell, 4)
	for i := range four {
		four[i] = sheetCell{img: solidImage(10, 10, testBlue)}
	}
	if _, err := composeFrameSheet(four, 3); err == nil {
		t.Error("a max side too small for the separators was accepted")
	}
	if _, err := composeFrameSheet(nil, 100); err == nil {
		t.Error("no frames was accepted")
	}
	if _, err := composeFrameSheet(four[:1], 0); err == nil {
		t.Error("a max side of 0 was accepted")
	}
}

func TestBrowserTool_FramesAndThenTextArgumentShape(t *testing.T) {
	valid := []string{
		`{"action":"click","target":"#go","frames":[10]}`,
		`{"action":"click","x":5,"y":5,"frames":[0,10,20,5000]}`,
		`{"action":"screenshot","frames":[0,1000,5000]}`,
		`{"action":"key","key":"Enter","then_text":"Saved"}`,
		`{"action":"type","text":"hi","then_text":"Sent","timeout":30}`,
		`{"action":"fill","target":"#q","value":"x","then_text":"Results"}`,
	}
	for _, args := range valid {
		var p browserArgs
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			t.Fatalf("unmarshal %s: %v", args, err)
		}
		if err := p.checkShape(); err != nil {
			t.Errorf("valid %s refused: %v", args, err)
		}
	}
	invalid := []struct{ args, want string }{
		{`{"action":"click","x":1,"y":1,"frames":[]}`, "frames needs 1 to 4"},
		{`{"action":"click","x":1,"y":1,"frames":[1,2,3,4,5]}`, "frames needs 1 to 4"},
		{`{"action":"click","x":1,"y":1,"frames":[10,10]}`, "strictly ascending"},
		{`{"action":"click","x":1,"y":1,"frames":[30,10]}`, "strictly ascending"},
		{`{"action":"click","x":1,"y":1,"frames":[-1]}`, "outside 0 to 5000"},
		{`{"action":"click","x":1,"y":1,"frames":[5001]}`, "outside 0 to 5000"},
		{`{"action":"find","target":"#a","frames":[10]}`, "does not take frames"},
		{`{"action":"wait","text":"x","frames":[10]}`, "does not take frames"},
		{`{"action":"click","x":1,"y":1,"frames":[10],"then_text":"x"}`, "cannot be combined"},
		{`{"action":"click","x":1,"y":1,"then_text":""}`, "then_text needs text"},
		{`{"action":"screenshot","then_text":"x"}`, "does not take then_text"},
		{`{"action":"scroll","direction":"down","then_text":"x"}`, "does not take then_text"},
		{`{"action":"click","x":1,"y":1,"then_text":"x","timeout":31}`, "timeout must be 1 to 30"},
		{`{"action":"click","x":1,"y":1,"then_text":"x","timeout":0}`, "timeout must be 1 to 30"},
	}
	for _, tc := range invalid {
		var p browserArgs
		if err := json.Unmarshal([]byte(tc.args), &p); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.args, err)
		}
		err := p.checkShape()
		if err == nil {
			t.Errorf("invalid %s accepted", tc.args)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q lacks %q", tc.args, err, tc.want)
		}
	}
}

func TestBrowserTool_InvalidViewsNeverReachTheSurface(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100}
	b, _ := newTestBrowserTool(t, surf, 1568)
	for _, args := range []string{
		`{"action":"click","x":1,"y":1,"frames":[10,10]}`,
		`{"action":"screenshot","then_text":"x"}`,
	} {
		if _, err := browserCall(t, b, args); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	if got := surf.got(); len(got) != 0 {
		t.Fatalf("refused views reached the surface: %v", got)
	}
}

func TestBrowserTool_ProducesImageTruthTable(t *testing.T) {
	b := newBrowserTool(testProjectKey, 1568, nil, nil)
	cases := []struct {
		args string
		want bool
	}{
		{`{"action":"screenshot"}`, true},
		{`{"action":"text"}`, false},
		{`{"action":"click","x":1,"y":1}`, false},
		{`{"action":"click","x":1,"y":1,"frames":[10]}`, true},
		{`{"action":"screenshot","frames":[10,20]}`, true},
		{`{"action":"key","key":"Enter","then_text":"x"}`, true},
		{`{"action":"key","key":"Enter"}`, false},
	}
	for _, tc := range cases {
		if got := b.ProducesImage(json.RawMessage(tc.args)); got != tc.want {
			t.Errorf("ProducesImage(%s) = %t, want %t", tc.args, got, tc.want)
		}
	}
}

func TestBrowserDeadlineFramesAndThenText(t *testing.T) {
	thirty := 30
	text := "x"
	cases := []struct {
		name string
		p    browserArgs
		want time.Duration
	}{
		{"plain click", browserArgs{Action: "click"}, browserActionTimeout},
		{"frames to 2500 ms", browserArgs{Action: "click", Frames: []int{500, 2500}}, browserActionTimeout + 2500*time.Millisecond + browserWaitMargin},
		{"screenshot burst to 5000 ms", browserArgs{Action: "screenshot", Frames: []int{5000}}, browserActionTimeout + 5*time.Second + browserWaitMargin},
		{"then_text with default wait", browserArgs{Action: "click", ThenText: &text}, browserActionTimeout + 10*time.Second + browserWaitMargin},
		{"then_text with 30 s wait", browserArgs{Action: "type", ThenText: &text, Timeout: &thirty}, browserActionTimeout + 30*time.Second + browserWaitMargin},
	}
	for _, tc := range cases {
		if got := browserDeadline(tc.p); got != tc.want {
			t.Errorf("%s: deadline = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestBrowserTool_DefinitionDescribesFramesAndThenText(t *testing.T) {
	b := newBrowserTool(testProjectKey, 1568, nil, nil)
	def := b.Definition()
	desc, _ := def["description"].(string)
	const sentence = "To see what an action causes, add frames (screenshots at ms offsets after it, as one contact sheet) or then_text (wait for text, then screenshot) to click, key, type or fill; frames also works on screenshot."
	if !strings.Contains(desc, sentence) {
		t.Errorf("description lacks the frames and then_text sentence")
	}
	props, _ := def["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	for _, name := range []string{"frames", "then_text"} {
		if _, ok := props[name]; !ok {
			t.Errorf("schema lacks property %s", name)
		}
	}
	timeout, _ := props["timeout"].(map[string]interface{})
	if d, _ := timeout["description"].(string); !strings.Contains(d, "then_text") {
		t.Errorf("timeout description does not cover then_text: %q", d)
	}
}

// sheetOf decodes the image ExecuteImage returns for the last call.
func sheetOf(t *testing.T, b *browserTool) image.Rectangle {
	t.Helper()
	raw, mime, err := b.ExecuteImage(nil)
	if err != nil {
		t.Fatalf("ExecuteImage: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q", mime)
	}
	return decodePNG(t, raw).Bounds()
}

// frameLabelTimes reads the actual times from the reading-order line of a result.
func frameLabelTimes(t *testing.T, out string) []int64 {
	t.Helper()
	const lead = "in reading order: "
	start := strings.Index(out, lead)
	if start < 0 {
		t.Fatalf("no frame list in %q", out)
	}
	rest := out[start+len(lead):]
	end := strings.Index(rest, ". This image")
	if end < 0 {
		t.Fatalf("frame list is not followed by the view-only note in %q", out)
	}
	var times []int64
	for _, part := range strings.Split(rest[:end], ", ") {
		var ms int64
		if _, err := fmt.Sscanf(part, "+%d ms", &ms); err != nil {
			t.Fatalf("label %q: %v", part, err)
		}
		times = append(times, ms)
	}
	return times
}

func TestBrowserTool_ClickWithFramesTakesTimedScreenshotsInOrder(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100}
	b, _ := newTestBrowserTool(t, surf, 1568)
	out, err := browserCall(t, b, `{"action":"click","target":"#go","frames":[10,30]}`)
	if err != nil {
		t.Fatalf("click with frames: %v", err)
	}
	if !strings.HasPrefix(out, "clicked #go on tab b1.") {
		t.Errorf("action line missing: %q", out)
	}
	want := []string{"clickElement frame= target=#go", "screenshot", "screenshot"}
	if got := surf.got(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	times := frameLabelTimes(t, out)
	if len(times) != 2 || times[0] < 10 || times[1] < 30 || times[0] > times[1] {
		t.Fatalf("actual times %v do not honour offsets 10 and 30 in order", times)
	}
	if got := sheetOf(t, b); got.Dx() != 402 || got.Dy() != 100 {
		t.Fatalf("sheet = %v, want 402x100", got)
	}
	b.mu.Lock()
	shotHeld := b.shot != nil
	b.mu.Unlock()
	if shotHeld {
		t.Fatal("a frames call left b.shot set, so a coordinate click would work on a view-only sheet")
	}
	if _, err := browserCall(t, b, `{"action":"click","x":1,"y":1}`); err == nil || !strings.Contains(err.Error(), "needs a screenshot") {
		t.Fatalf("coordinate click after a frames call: err = %v, want the needs-a-screenshot error", err)
	}
}

func TestBrowserTool_ScreenshotWithFramesTakesNoNormalShot(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100}
	b, _ := newTestBrowserTool(t, surf, 1568)
	out, err := browserCall(t, b, `{"action":"screenshot","frames":[10,30,50]}`)
	if err != nil {
		t.Fatalf("screenshot with frames: %v", err)
	}
	if !strings.HasPrefix(out, "burst of tab b1: 200x100 px sheet frame\n") {
		t.Errorf("action line missing: %q", out)
	}
	want := []string{"screenshot", "screenshot", "screenshot"}
	if got := surf.got(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if n := len(frameLabelTimes(t, out)); n != 3 {
		t.Fatalf("%d frames listed, want 3", n)
	}
	if got := sheetOf(t, b); got.Dx() != 402 || got.Dy() != 202 {
		t.Fatalf("sheet = %v, want 402x202 (three cells in a 2-column grid)", got)
	}
}

func TestBrowserTool_FramesPartialFailureKeepsActionAndNamesMissing(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100, shotFailFrom: 2}
	b, _ := newTestBrowserTool(t, surf, 1568)
	out, err := browserCall(t, b, `{"action":"screenshot","frames":[10,30,60]}`)
	if err != nil {
		t.Fatalf("a partial burst failed the call: %v", err)
	}
	if !strings.Contains(out, "Frames at +30 ms, +60 ms could not be captured: capture failed.") {
		t.Errorf("missing offsets not named: %q", out)
	}
	if n := len(frameLabelTimes(t, out)); n != 1 {
		t.Fatalf("%d frames listed, want the 1 captured", n)
	}
	if got := sheetOf(t, b); got.Dx() != 200 || got.Dy() != 100 {
		t.Fatalf("sheet = %v, want the single captured frame 200x100", got)
	}
}

func TestBrowserTool_FramesTotalFailureYieldsNoImage(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100, shotFailFrom: 1}
	b, _ := newTestBrowserTool(t, surf, 1568)
	out, err := browserCall(t, b, `{"action":"click","target":"#go","frames":[10]}`)
	if err != nil {
		t.Fatalf("the action succeeded, so the call must: %v", err)
	}
	if !strings.Contains(out, "No frame could be captured: capture failed.") {
		t.Errorf("total failure not reported: %q", out)
	}
	if _, _, err := b.ExecuteImage(nil); err == nil {
		t.Fatal("ExecuteImage returned an image after no frame was captured")
	}
	b.mu.Lock()
	shotHeld := b.shot != nil
	b.mu.Unlock()
	if shotHeld {
		t.Fatal("a failed burst left b.shot set")
	}
}

func TestBrowserTool_SheetIsNotReusedByLaterCalls(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100}
	b, _ := newTestBrowserTool(t, surf, 1568)
	if _, err := browserCall(t, b, `{"action":"click","target":"#go","frames":[10,30]}`); err != nil {
		t.Fatalf("click with frames: %v", err)
	}
	if got := sheetOf(t, b); got.Dx() != 402 {
		t.Fatalf("sheet = %v, want the two-frame sheet", got)
	}
	if _, err := browserCall(t, b, `{"action":"key","key":"Enter"}`); err != nil {
		t.Fatalf("key: %v", err)
	}
	if _, _, err := b.ExecuteImage(nil); err == nil {
		t.Fatal("a later key call attached the old sheet")
	}
	if _, err := browserCall(t, b, `{"action":"screenshot"}`); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if got := sheetOf(t, b); got.Dx() != 200 || got.Dy() != 100 {
		t.Fatalf("after a screenshot ExecuteImage returned %v, want the plain 200x100 screenshot", got)
	}
}

func TestBrowserTool_FramesCancelledMidBurstReturnsContextError(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100}
	b, _ := newTestBrowserTool(t, surf, 1568)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"click","target":"#go","frames":[5000]}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	for _, c := range surf.got() {
		if c == "screenshot" {
			t.Fatal("a frame was captured after the stop")
		}
	}
	if _, _, err := b.ExecuteImage(nil); err == nil {
		t.Fatal("a cancelled burst left an image to attach")
	}
}

func TestBrowserTool_ThenTextPresentWaitsScreenshotsAndAllowsCoordinateClick(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100}
	b, _ := newTestBrowserTool(t, surf, 1568)
	out, err := browserCall(t, b, `{"action":"click","target":"#go","then_text":"TRY YOUR LUCK"}`)
	if err != nil {
		t.Fatalf("click with then_text: %v", err)
	}
	want := []string{
		"clickElement frame= target=#go",
		"wait frame= selector= text=TRY YOUR LUCK gone=false timeout=10s",
		"screenshot",
	}
	if got := surf.got(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if !strings.Contains(out, `Text "TRY YOUR LUCK" appeared after`) {
		t.Errorf("appearance not reported: %q", out)
	}
	if !strings.Contains(out, "screenshot of tab b1: 200x100 px image") {
		t.Errorf("normal screenshot line missing: %q", out)
	}
	if _, err := browserCall(t, b, `{"action":"click","x":10,"y":10}`); err != nil {
		t.Fatalf("coordinate click after then_text: %v", err)
	}
}

func TestBrowserTool_ThenTextTimeoutStillSucceedsAndScreenshots(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100, waitBlocks: true}
	b, _ := newTestBrowserTool(t, surf, 1568)
	out, err := browserCall(t, b, `{"action":"click","target":"#go","then_text":"Never","timeout":1}`)
	if err != nil {
		t.Fatalf("a timed-out wait failed the call: %v", err)
	}
	if !strings.Contains(out, `Text "Never" did not appear within 1 s.`) {
		t.Errorf("timeout not reported: %q", out)
	}
	if !strings.Contains(out, "screenshot of tab b1:") {
		t.Errorf("screenshot not taken after the timeout: %q", out)
	}
	got := surf.got()
	if len(got) < 2 || got[len(got)-1] != "screenshot" || !strings.Contains(got[len(got)-2], "timeout=1s") {
		t.Errorf("calls = %v, want a 1 s wait then a screenshot", got)
	}
}

func TestBrowserTool_ThenTextWaitErrorIsReportedNotFatal(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100, targetErr: errors.New("cdp: frame detached")}
	b, _ := newTestBrowserTool(t, surf, 1568)
	if _, err := browserCall(t, b, `{"action":"screenshot"}`); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	out, err := browserCall(t, b, `{"action":"click","x":10,"y":10,"then_text":"Saved"}`)
	if err != nil {
		t.Fatalf("the click happened, so the call must succeed: %v", err)
	}
	if !strings.Contains(out, `Text "Saved" could not be checked: cdp: frame detached.`) {
		t.Errorf("wait error not reported: %q", out)
	}
	if !strings.Contains(out, "screenshot of tab b1:") {
		t.Errorf("screenshot not taken: %q", out)
	}
}

func TestBrowserTool_ThenTextScreenshotFailureKeepsCallSuccessful(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100, shotFailFrom: 1}
	b, _ := newTestBrowserTool(t, surf, 1568)
	out, err := browserCall(t, b, `{"action":"click","target":"#go","then_text":"Saved"}`)
	if err != nil {
		t.Fatalf("the action succeeded, so the call must: %v", err)
	}
	if !strings.Contains(out, "No screenshot could be taken: browser: screenshot of tab b1: capture failed.") {
		t.Errorf("screenshot failure not reported: %q", out)
	}
	if _, _, err := b.ExecuteImage(nil); err == nil {
		t.Fatal("ExecuteImage returned an image after the screenshot failed")
	}
}

func TestBrowserTool_ThenTextCancelledWaitReturnsContextError(t *testing.T) {
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100, waitBlocks: true, waitStarted: make(chan struct{})}
	b, _ := newTestBrowserTool(t, surf, 1568)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-surf.waitStarted
		cancel()
	}()
	_, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"click","target":"#go","then_text":"Never"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := surf.got(); len(got) == 0 || got[0] != "clickElement frame= target=#go" {
		t.Fatalf("the action did not run before the wait: %v", got)
	}
}
