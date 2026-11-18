package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/browsertabs"
	"github.com/u007/ocode/internal/tool"
)

// framesLivePage is the page both live tests drive. The button starts a 2000 ms
// linear move of a 60 px red box from left 20 px to left 620 px, timed with
// requestAnimationFrame and performance.now(). The status element reads DONE
// when the move ends.
const framesLivePage = `<!doctype html>
<html><head><meta charset="utf-8"><title>frames</title>
<style>
html, body { margin: 0; background: #fff; }
#go { position: absolute; left: 20px; top: 20px; }
#status { position: absolute; left: 20px; top: 100px; }
#box { position: absolute; left: 20px; top: 200px; width: 60px; height: 60px; background: #e00; }
</style></head>
<body>
<button id="go">Go</button>
<div id="status"></div>
<div id="box"></div>
<script>
document.getElementById('go').addEventListener('click', function () {
  var box = document.getElementById('box');
  var start = performance.now();
  function step() {
    var t = Math.min(1, (performance.now() - start) / 2000);
    box.style.left = (20 + 600 * t) + 'px';
    if (t < 1) { requestAnimationFrame(step); }
    else { document.getElementById('status').textContent = 'DONE'; }
  }
  requestAnimationFrame(step);
});
</script>
</body></html>`

// framesLiveTabKey is the state key the one-target fake gives the only tab.
const framesLiveTabKey = "tab:live"

var (
	// framesLiveLabelRE matches one frame label such as "+300 ms".
	framesLiveLabelRE = regexp.MustCompile(`\+(\d+) ms`)
	// framesLiveThenRE matches the then_text success line and its elapsed time.
	framesLiveThenRE = regexp.MustCompile(`Text "DONE" appeared after (\d+) ms`)
)

// framesLiveChromePath returns OCODE_CHROME_PATH, or skips the test when it is unset.
func framesLiveChromePath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("OCODE_CHROME_PATH")
	if p == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome frames test")
	}
	return p
}

// framesLiveUpstream serves framesLivePage at "/" from a loopback httptest server.
// It returns the page URL and the upstream host:port.
func framesLiveUpstream(t *testing.T) (pageURL, hostPort string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, framesLivePage)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/", strings.TrimPrefix(srv.URL, "http://")
}

// framesLiveDialer is the guarded dialer: Chrome may reach exactly the httptest
// upstream and no other address.
func framesLiveDialer(allowHostPort string) *net.Dialer {
	return &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			if address == allowHostPort {
				return nil
			}
			return fmt.Errorf("frames live test egress: %s is not the test upstream", address)
		},
	}
}

// framesLiveTarget starts headless Chrome and returns its one target. The
// manager is closed when the test ends.
func framesLiveTarget(ctx context.Context, t *testing.T, chromePath, hostPort string) *cdp.Target {
	t.Helper()
	mgr := cdp.NewManager(cdp.ManagerOptions{
		ChromePath: chromePath,
		Supervisor: tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 2 * time.Second}),
		Dialer:     framesLiveDialer(hostPort),
	})
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := mgr.Close(cctx); err != nil {
			t.Logf("closing headless Chrome: %v", err)
		}
	})
	target, _, err := mgr.OpenHeadless(ctx)
	if err != nil {
		t.Fatalf("OpenHeadless: %v", err)
	}
	return target
}

// framesLiveSurfaces is the browserSurfaces the live tests use. Every tab is the
// one real target; Open navigates it and returns a fixed state key.
type framesLiveSurfaces struct {
	target *cdp.Target
}

func (f framesLiveSurfaces) Surface(ctx context.Context, stateKey, url string) (browserSurface, string, error) {
	return cdpAgentSurface{Target: f.target}, "", nil
}

func (f framesLiveSurfaces) Open(ctx context.Context, url string) (string, error) {
	if url != "" {
		if err := f.target.Navigate(ctx, url); err != nil {
			return framesLiveTabKey, fmt.Errorf("navigate: %w", err)
		}
	}
	return framesLiveTabKey, nil
}

func (f framesLiveSurfaces) Close(ctx context.Context, stateKey string) error {
	return nil
}

// framesLiveTool builds the browser tool for an empty project, with the same
// tab store fake the existing browser tool tests use.
func framesLiveTool(t *testing.T, target *cdp.Target) *browserTool {
	t.Helper()
	store, err := browsertabs.NewStoreAt(filepath.Join(t.TempDir(), "browser-tabs.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	return newBrowserTool("", 1568, store, framesLiveSurfaces{target: target})
}

// framesLiveArgs marshals one tool call's arguments.
func framesLiveArgs(t *testing.T, args map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return raw
}

// framesLiveReadingOrderLine returns the frame list of a frames result: the text
// between "Frames after the action, in reading order: " and the next period.
func framesLiveReadingOrderLine(t *testing.T, out string) string {
	t.Helper()
	const marker = "Frames after the action, in reading order: "
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("result has no frame list:\n%s", out)
	}
	rest := out[i+len(marker):]
	if j := strings.Index(rest, "."); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// framesLiveRedMeanX returns the mean x of the strongly red pixels in r, relative
// to r's left edge, and how many such pixels there are.
func framesLiveRedMeanX(img image.Image, r image.Rectangle) (float64, int) {
	var sum float64
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			pr, pg, pb, _ := img.At(x, y).RGBA()
			R, G, B := pr>>8, pg>>8, pb>>8
			if R > 180 && G < 80 && B < 80 {
				sum += float64(x - r.Min.X)
				n++
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

// framesLiveCellRect returns frame i's cell in a W x H contact sheet of n frames
// of a landscape viewport. It recomputes composeFrameSheet's layout from the
// sheet size, not from a helper: sheetLayout gives the grid, the cells are equal
// in size, and browserSheetSep pixels separate them, so the canvas is
// cols*cellW + (cols-1)*browserSheetSep wide (and likewise for height).
func framesLiveCellRect(t *testing.T, W, H, n, i int) image.Rectangle {
	t.Helper()
	cols, rows := sheetLayout(n, false)
	sepW, sepH := (cols-1)*browserSheetSep, (rows-1)*browserSheetSep
	if (W-sepW)%cols != 0 || (H-sepH)%rows != 0 {
		t.Fatalf("sheet %dx%d does not match a %dx%d grid with %d px separators", W, H, cols, rows, browserSheetSep)
	}
	cellW, cellH := (W-sepW)/cols, (H-sepH)/rows
	if cellW <= cellH {
		t.Fatalf("cell %dx%d is not landscape, so the landscape layout assumption does not hold", cellW, cellH)
	}
	x := (i % cols) * (cellW + browserSheetSep)
	y := (i / cols) * (cellH + browserSheetSep)
	return image.Rect(x, y, x+cellW, y+cellH)
}

// TestBrowserFramesLiveChrome drives click with frames against real headless
// Chrome and checks the contact sheet shows the box moving.
func TestBrowserFramesLiveChrome(t *testing.T) {
	chromePath := framesLiveChromePath(t)
	pageURL, hostPort := framesLiveUpstream(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	target := framesLiveTarget(ctx, t, chromePath, hostPort)
	b := framesLiveTool(t, target)

	if _, err := b.ExecuteCtx(ctx, framesLiveArgs(t, map[string]any{"action": "open", "url": pageURL})); err != nil {
		t.Fatalf("open: %v", err)
	}

	offsets := []int{300, 1000, 1900}
	clickArgs := framesLiveArgs(t, map[string]any{"action": "click", "target": "#go", "frames": offsets})
	out, err := b.ExecuteCtx(ctx, clickArgs)
	if err != nil {
		t.Fatalf("click with frames: %v", err)
	}
	t.Logf("click result:\n%s", out)

	// Each label is the time the tool requested the capture, measured from the
	// start of the burst. It is the actual time the tool reports for the frame.
	matches := framesLiveLabelRE.FindAllStringSubmatch(framesLiveReadingOrderLine(t, out), -1)
	if len(matches) != len(offsets) {
		t.Fatalf("result lists %d frame times, want %d", len(matches), len(offsets))
	}
	for i, m := range matches {
		actual, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("frame %d label %q: %v", i+1, m[1], err)
		}
		delta := actual - offsets[i]
		t.Logf("frame %d: offset +%d ms, actual +%d ms, delta %+d ms", i+1, offsets[i], actual, delta)
		if delta < -400 || delta > 400 {
			t.Errorf("frame %d actual +%d ms is more than 400 ms from offset +%d ms", i+1, actual, offsets[i])
		}
	}

	raw, mime, err := b.ExecuteImage(clickArgs)
	if err != nil {
		t.Fatalf("ExecuteImage: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("ExecuteImage mime = %q, want image/png", mime)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("contact sheet is not a PNG: %v", err)
	}
	if outPath := os.Getenv("OCODE_FRAMES_OUT"); outPath != "" {
		if err := os.WriteFile(outPath, raw, 0o644); err != nil {
			t.Fatalf("write contact sheet to %s: %v", outPath, err)
		}
		t.Logf("wrote contact sheet to %s", outPath)
	}

	bounds := img.Bounds()
	t.Logf("contact sheet %dx%d", bounds.Dx(), bounds.Dy())
	var xs [3]float64
	for i := range xs {
		r := framesLiveCellRect(t, bounds.Dx(), bounds.Dy(), len(offsets), i)
		mean, n := framesLiveRedMeanX(img, r)
		t.Logf("cell %d rect %v: %d red pixels, mean x %.1f px from the cell's left edge", i+1, r, n, mean)
		if n == 0 {
			t.Fatalf("no red box found in cell %d (rect %v)", i+1, r)
		}
		xs[i] = mean
	}
	if !(xs[0] < xs[1] && xs[1] < xs[2]) {
		t.Errorf("box x does not strictly increase across frames: %.1f, %.1f, %.1f", xs[0], xs[1], xs[2])
	}

	// The sheet is view-only: a coordinate click after the frames must be refused.
	_, err = b.ExecuteCtx(ctx, framesLiveArgs(t, map[string]any{"action": "click", "x": 10, "y": 10}))
	if err == nil || !strings.Contains(err.Error(), "needs a screenshot") {
		t.Fatalf("coordinate click after frames: err = %v, want the needs-a-screenshot refusal", err)
	}
	t.Logf("coordinate click after frames refused: %v", err)
}

// TestBrowserThenTextLiveChrome drives click with then_text against real headless
// Chrome: the text wait, the normal screenshot, and the timed-out case.
func TestBrowserThenTextLiveChrome(t *testing.T) {
	chromePath := framesLiveChromePath(t)
	pageURL, hostPort := framesLiveUpstream(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	target := framesLiveTarget(ctx, t, chromePath, hostPort)
	b := framesLiveTool(t, target)

	if _, err := b.ExecuteCtx(ctx, framesLiveArgs(t, map[string]any{"action": "open", "url": pageURL})); err != nil {
		t.Fatalf("open: %v", err)
	}

	// Case 1: the text appears when the move ends.
	clickArgs := framesLiveArgs(t, map[string]any{"action": "click", "target": "#go", "then_text": "DONE", "timeout": 5})
	out, err := b.ExecuteCtx(ctx, clickArgs)
	if err != nil {
		t.Fatalf("click with then_text DONE: %v", err)
	}
	t.Logf("then_text DONE result:\n%s", out)
	m := framesLiveThenRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("result does not say DONE appeared:\n%s", out)
	}
	elapsed, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("elapsed %q: %v", m[1], err)
	}
	t.Logf("then_text elapsed: %d ms", elapsed)
	if elapsed < 1500 || elapsed > 4000 {
		t.Errorf("then_text elapsed %d ms is outside 1500 to 4000 ms", elapsed)
	}

	raw, mime, err := b.ExecuteImage(clickArgs)
	if err != nil {
		t.Fatalf("ExecuteImage: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("ExecuteImage mime = %q, want image/png", mime)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("screenshot is not a PNG: %v", err)
	}
	bounds := img.Bounds()
	mean, n := framesLiveRedMeanX(img, bounds)
	if n == 0 {
		t.Fatalf("no red box in the screenshot after then_text")
	}
	// The box ends at left 620 px, so its centre is near 650 px. On the 1280 px
	// viewport this run gets that is about 51% of the width, not the far right.
	// The start is at about 6%. A threshold of 45% proves the box reached the
	// end of its travel and is clear of both the start and mid-travel (about 28%).
	ratio := mean / float64(bounds.Dx())
	t.Logf("screenshot %dx%d: %d red pixels, mean x %.1f (%.1f%% of width; end of travel ~51%%, start ~6%%)", bounds.Dx(), bounds.Dy(), n, mean, ratio*100)
	if ratio <= 0.45 {
		t.Errorf("red box mean x is %.1f%% of the width, want more than 45%% (box at the end of its travel)", ratio*100)
	}

	// A coordinate click right after must not need a new screenshot.
	if _, err := b.ExecuteCtx(ctx, framesLiveArgs(t, map[string]any{"action": "click", "x": 5, "y": 5})); err != nil {
		t.Fatalf("coordinate click after then_text: %v", err)
	}

	// Case 2: a fresh load, and text that never appears within 1 s.
	if _, err := b.ExecuteCtx(ctx, framesLiveArgs(t, map[string]any{"action": "open", "url": pageURL})); err != nil {
		t.Fatalf("reload open: %v", err)
	}
	// Precondition: the reloaded page shows the box at its start, at the left.
	pre := framesLiveArgs(t, map[string]any{"action": "screenshot"})
	if _, err := b.ExecuteCtx(ctx, pre); err != nil {
		t.Fatalf("screenshot after reload: %v", err)
	}
	preRaw, _, err := b.ExecuteImage(pre)
	if err != nil {
		t.Fatalf("ExecuteImage after reload: %v", err)
	}
	preImg, err := png.Decode(bytes.NewReader(preRaw))
	if err != nil {
		t.Fatalf("pre-click screenshot is not a PNG: %v", err)
	}
	preMean, preN := framesLiveRedMeanX(preImg, preImg.Bounds())
	t.Logf("fresh page: %d red pixels, mean x %.1f px", preN, preMean)
	if preN == 0 || preMean/float64(preImg.Bounds().Dx()) > 0.2 {
		t.Fatalf("reloaded page is not fresh: red box mean x %.1f px of width %d", preMean, preImg.Bounds().Dx())
	}

	neverArgs := framesLiveArgs(t, map[string]any{"action": "click", "target": "#go", "then_text": "NEVER", "timeout": 1})
	out, err = b.ExecuteCtx(ctx, neverArgs)
	if err != nil {
		t.Fatalf("click with then_text NEVER: %v", err)
	}
	t.Logf("then_text NEVER result:\n%s", out)
	if !strings.Contains(out, "did not appear within 1 s") {
		t.Errorf("result does not say the text did not appear within 1 s")
	}
	raw, mime, err = b.ExecuteImage(neverArgs)
	if err != nil {
		t.Fatalf("ExecuteImage after timed-out wait: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("ExecuteImage mime = %q, want image/png", mime)
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("screenshot after timed-out wait is not a PNG: %v", err)
	}
}
