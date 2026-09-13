package tool

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

type computerTestDriver struct {
	pngData        []byte
	screenW        int
	screenH        int
	typeCtxErr     error
	typedText      string
	lastScreenshot bool
}

func (d *computerTestDriver) Screenshot(context.Context) ([]byte, int, int, error) {
	d.lastScreenshot = true
	return d.pngData, d.screenW, d.screenH, nil
}

func (d *computerTestDriver) Click(context.Context, int, int, MouseButton, int) error { return nil }
func (d *computerTestDriver) Move(context.Context, int, int) error                    { return nil }
func (d *computerTestDriver) Drag(context.Context, int, int, int, int) error          { return nil }
func (d *computerTestDriver) Scroll(context.Context, int, int, string, int) error     { return nil }
func (d *computerTestDriver) Type(ctx context.Context, text string) error {
	d.typeCtxErr = ctx.Err()
	d.typedText = text
	return nil
}
func (d *computerTestDriver) Key(context.Context, string) error { return nil }
func (d *computerTestDriver) Cursor(context.Context) (int, int, error) {
	return 0, 0, nil
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var b strings.Builder
	if err := png.Encode(writerFunc{write: func(p []byte) (int, error) {
		b.WriteString(string(p))
		return len(p), nil
	}}, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return []byte(b.String())
}

type writerFunc struct {
	write func([]byte) (int, error)
}

func (w writerFunc) Write(p []byte) (int, error) { return w.write(p) }

func TestComputerToolScreenshotUsesFreshDimensionsAndReturnsImage(t *testing.T) {
	driver := &computerTestDriver{pngData: testPNG(t, 320, 160), screenW: 1920, screenH: 1080}
	tool := &ComputerTool{Driver: driver}

	result, err := tool.Execute(json.RawMessage(`{"action":"screenshot"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "screen 1920x1080") {
		t.Fatalf("screenshot result = %q", result)
	}
	raw, mime, err := tool.ExecuteImage(nil)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" || len(raw) == 0 {
		t.Fatalf("image result mime=%q bytes=%d", mime, len(raw))
	}
	if _, err := png.Decode(strings.NewReader(string(raw))); err != nil {
		t.Fatalf("returned image is not PNG: %v", err)
	}
}

func TestComputerToolTypeUsesOriginalContext(t *testing.T) {
	driver := &computerTestDriver{pngData: testPNG(t, 10, 10), screenW: 10, screenH: 10}
	tool := &ComputerTool{Driver: driver}

	if _, err := tool.ExecuteCtx(context.Background(), json.RawMessage(`{"action":"type","text":"hello"}`)); err != nil {
		t.Fatal(err)
	}
	if driver.typeCtxErr != nil {
		t.Fatalf("type received canceled context: %v", driver.typeCtxErr)
	}
	if driver.typedText != "hello" {
		t.Fatalf("typed text = %q", driver.typedText)
	}
}

func TestComputerToolValidationAndUnavailableDriver(t *testing.T) {
	tool := &ComputerTool{Driver: &computerTestDriver{pngData: testPNG(t, 10, 10), screenW: 10, screenH: 10}}
	if _, err := tool.Execute(json.RawMessage(`{"action":"left_click","coordinate":[-1,2]}`)); err == nil {
		t.Fatal("expected invalid coordinate error")
	}
	if _, err := tool.Execute(json.RawMessage(`{"action":"nope"}`)); err == nil {
		t.Fatal("expected unknown action error")
	}

	unavailable := &ComputerTool{DriverErr: errors.New("unsupported platform")}
	if _, err := unavailable.Execute(json.RawMessage(`{"action":"screenshot"}`)); err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("unavailable driver error = %v", err)
	}
}

func TestBuiltinComputerToolRegistrationUsesDriverAndHonorsConfig(t *testing.T) {
	driver := &computerTestDriver{}
	cfg := &config.Config{Ocode: config.OcodeConfig{ComputerUse: config.ComputerUseConfig{Enabled: true}}}
	tools := InitBuiltinToolsWithComputerDriver(nil, cfg, nil, driver, nil)
	var found *ComputerTool
	for _, candidate := range tools {
		if computer, ok := candidate.(*ComputerTool); ok {
			found = computer
		}
	}
	if found == nil {
		t.Fatal("enabled config did not register computer tool")
	}
	if found.Driver != driver {
		t.Fatal("computer tool did not receive the platform driver")
	}

	cfg.Ocode.ComputerUse.Enabled = false
	for _, candidate := range InitBuiltinToolsWithComputerDriver(nil, cfg, nil, driver, nil) {
		if candidate.Name() == "computer" {
			t.Fatal("disabled config registered computer tool")
		}
	}
}

// recordingDriver captures the coordinates the tool hands to the driver so
// scaling and bounds behaviour can be asserted in driver space.
type recordingDriver struct {
	computerTestDriver
	clicks    [][2]int
	moves     [][2]int
	cursorX   int
	cursorY   int
	shotCount int
}

func (d *recordingDriver) Screenshot(ctx context.Context) ([]byte, int, int, error) {
	d.shotCount++
	return d.computerTestDriver.Screenshot(ctx)
}
func (d *recordingDriver) Click(_ context.Context, x, y int, _ MouseButton, _ int) error {
	d.clicks = append(d.clicks, [2]int{x, y})
	return nil
}
func (d *recordingDriver) Move(_ context.Context, x, y int) error {
	d.moves = append(d.moves, [2]int{x, y})
	return nil
}
func (d *recordingDriver) Cursor(context.Context) (int, int, error) { return d.cursorX, d.cursorY, nil }

func run(t *testing.T, tool *ComputerTool, args string) (string, error) {
	t.Helper()
	return tool.Execute(json.RawMessage(args))
}

// Retina-style: 2880x1800 pixels, 1440x900 points. Image scales to
// 1568x980; scale = 1440/1568 = 0.918.
func TestComputerToolRetinaScalingRoundTrip(t *testing.T) {
	d := &recordingDriver{computerTestDriver: computerTestDriver{pngData: testPNG(t, 2880, 1800), screenW: 1440, screenH: 900}}
	tool := &ComputerTool{Driver: d}
	out, err := run(t, tool, `{"action":"screenshot"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "screen 1440x900, image 1568x980, scale 0.92" {
		t.Fatalf("unexpected screenshot line %q", out)
	}
	raw, _, err := tool.ExecuteImage(nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(strings.NewReader(string(raw)))
	if err != nil || cfg.Width != 1568 || cfg.Height != 980 {
		t.Fatalf("image not downscaled: %dx%d err=%v", cfg.Width, cfg.Height, err)
	}
	if _, err := run(t, tool, `{"action":"left_click","coordinate":[784,490]}`); err != nil {
		t.Fatal(err)
	}
	if len(d.clicks) != 1 || d.clicks[0] != [2]int{720, 450} {
		t.Fatalf("click not mapped to points: %v", d.clicks)
	}
	d.cursorX, d.cursorY = 720, 450
	out, err = run(t, tool, `{"action":"cursor_position"}`)
	if err != nil || out != "cursor 784,490" {
		t.Fatalf("cursor round-trip: %q %v", out, err)
	}
}

// 1080p with scale > 1: 1920x1080 pixels and input space, image 1568x882.
// The far right of the image must still be clickable.
func TestComputerToolAcceptsFullImageRangeWhenScaleAboveOne(t *testing.T) {
	d := &recordingDriver{computerTestDriver: computerTestDriver{pngData: testPNG(t, 1920, 1080), screenW: 1920, screenH: 1080}}
	tool := &ComputerTool{Driver: d}
	if _, err := run(t, tool, `{"action":"screenshot"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, tool, `{"action":"left_click","coordinate":[1567,881]}`); err != nil {
		t.Fatalf("rightmost image pixel rejected: %v", err)
	}
	if d.clicks[0] != [2]int{1919, 1079} {
		t.Fatalf("expected 1919,1079 got %v", d.clicks[0])
	}
}

func TestComputerToolRejectsOutOfBounds(t *testing.T) {
	d := &recordingDriver{computerTestDriver: computerTestDriver{pngData: testPNG(t, 1920, 1080), screenW: 1920, screenH: 1080}}
	tool := &ComputerTool{Driver: d}
	if _, err := run(t, tool, `{"action":"screenshot"}`); err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{
		`{"action":"left_click","coordinate":[1568,10]}`,
		`{"action":"mouse_move","coordinate":[10,882]}`,
		`{"action":"left_click_drag","start_coordinate":[10,10],"coordinate":[2000,10]}`,
	} {
		if _, err := run(t, tool, args); err == nil || !strings.Contains(err.Error(), "out of bounds") {
			t.Errorf("%s: expected out-of-bounds error, got %v", args, err)
		}
	}
	if len(d.clicks)+len(d.moves) != 0 {
		t.Fatalf("driver called for rejected coordinates: clicks=%v moves=%v", d.clicks, d.moves)
	}
}

func TestComputerToolSeedScreenshotIsNotRetained(t *testing.T) {
	d := &recordingDriver{computerTestDriver: computerTestDriver{pngData: testPNG(t, 2880, 1800), screenW: 1440, screenH: 900}}
	tool := &ComputerTool{Driver: d}
	if _, err := run(t, tool, `{"action":"mouse_move","coordinate":[784,490]}`); err != nil {
		t.Fatal(err)
	}
	if d.shotCount != 1 || len(d.moves) != 1 || d.moves[0] != [2]int{720, 450} {
		t.Fatalf("seed screenshot/move mismatch: shots=%d moves=%v", d.shotCount, d.moves)
	}
	if _, _, err := tool.ExecuteImage(nil); err == nil {
		t.Fatal("seed screenshot must not be exposed as an image result")
	}
	// A second input action must not re-seed.
	if _, err := run(t, tool, `{"action":"mouse_move","coordinate":[1,1]}`); err != nil {
		t.Fatal(err)
	}
	if d.shotCount != 1 {
		t.Fatalf("re-seeded: shots=%d", d.shotCount)
	}
}
