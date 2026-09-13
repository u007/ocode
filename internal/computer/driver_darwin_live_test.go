//go:build darwin

package computer

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

func TestDarwinLive_ScreenshotAndCursor(t *testing.T) {
	if os.Getenv("OCODE_COMPUTER_LIVE") != "1" {
		t.Skip("set OCODE_COMPUTER_LIVE=1 to run live driver tests")
	}
	d := liveDarwinDriver(t)
	ctx := context.Background()
	pngData, w, h, err := d.Screenshot(ctx)
	if err != nil {
		t.Fatalf("Screenshot error: %v", err)
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("expected positive dimensions got %dx%d", w, h)
	}
	_, err = png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("screenshot is not valid PNG: %v", err)
	}
	x, y, err := d.Cursor(ctx)
	if err != nil {
		t.Fatalf("Cursor error: %v", err)
	}
	if x < 0 || y < 0 {
		t.Fatalf("expected non-negative coords got %d,%d", x, y)
	}
}

func TestDarwinLive_Move(t *testing.T) {
	if os.Getenv("OCODE_COMPUTER_LIVE") != "1" {
		t.Skip("set OCODE_COMPUTER_LIVE=1 to run live driver tests")
	}
	d := liveDarwinDriver(t)
	ctx := context.Background()
	if err := d.Move(ctx, 100, 100); err != nil {
		t.Fatalf("Move error: %v", err)
	}
	x, y, err := d.Cursor(ctx)
	if err != nil {
		t.Fatalf("Cursor error: %v", err)
	}
	if x != 100 || y != 100 {
		t.Fatalf("expected (100,100) got (%d,%d)", x, y)
	}
}

func liveDarwinDriver(t *testing.T) tool.ComputerDriver {
	t.Helper()
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	d, err := newPlatformDriver(&execRunner{sup: sup}, sup)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestDarwinLive_InputIntoTextEdit drives a fresh, unsaved TextEdit document:
// click into it, type, select-all via cmd+a, scroll, drag, then read the
// document text back and close it without saving. It sends real keystrokes,
// so it is gated separately (OCODE_COMPUTER_LIVE_INPUT=1) and aborts before
// every input action unless TextEdit is the frontmost application. Requires
// Accessibility and Automation (System Events, TextEdit) grants.
func TestDarwinLive_InputIntoTextEdit(t *testing.T) {
	if os.Getenv("OCODE_COMPUTER_LIVE_INPUT") != "1" {
		t.Skip("set OCODE_COMPUTER_LIVE_INPUT=1 to run live input tests (types into TextEdit)")
	}
	ctx := context.Background()
	d := liveDarwinDriver(t)
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	r := &execRunner{sup: sup}
	jxa := func(src string) string {
		out, err := r.run(ctx, "osascript", "-l", "JavaScript", "-e", src)
		if err != nil {
			t.Fatalf("jxa %q: %v", src, err)
		}
		return strings.TrimSpace(out)
	}
	jxa(`var te = Application("TextEdit"); te.activate(); te.Document().make(); delay(1); "ok"`)
	defer jxa(`var te = Application("TextEdit"); te.documents[0].close({saving: "no"}); "closed"`)
	// Guard: never send input unless TextEdit owns the keyboard focus.
	requireFront := func(step string) {
		t.Helper()
		front := jxa(`Application("System Events").processes.whose({frontmost: true})[0].name()`)
		if front != "TextEdit" {
			t.Fatalf("%s: frontmost app is %q, not TextEdit; refusing to send input", step, front)
		}
	}
	// Window geometry from System Events: accessibility coordinates share the
	// CGEvent input space, unlike TextEdit's own bounds() on multi-display setups.
	geom := jxa(`var w = Application("System Events").processes.byName("TextEdit").windows[0]; var p = w.position(), s = w.size(); p[0] + " " + p[1] + " " + s[0] + " " + s[1]`)
	var bx, by, bw, bh int
	if _, err := fmt.Sscanf(geom, "%d %d %d %d", &bx, &by, &bw, &bh); err != nil {
		t.Fatalf("geometry %q: %v", geom, err)
	}
	cx, cy := bx+bw/2, by+bh/2
	requireFront("click")
	if err := d.Click(ctx, cx, cy, tool.MouseLeft, 1); err != nil {
		t.Fatalf("Click: %v", err)
	}
	requireFront("type")
	if err := d.Type(ctx, "ocode live ✓"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	requireFront("key Return")
	if err := d.Key(ctx, "Return"); err != nil {
		t.Fatalf("Key Return: %v", err)
	}
	requireFront("type line2")
	if err := d.Type(ctx, "line2"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := jxa(`Application("TextEdit").documents[0].text()`); got != "ocode live ✓\nline2" {
		t.Fatalf("document text = %q", got)
	}
	requireFront("key cmd+a")
	if err := d.Key(ctx, "cmd+a"); err != nil {
		t.Fatalf("Key cmd+a: %v", err)
	}
	requireFront("type replaced")
	if err := d.Type(ctx, "replaced"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := jxa(`Application("TextEdit").documents[0].text()`); got != "replaced" {
		t.Fatalf("after cmd+a: document text = %q", got)
	}
	requireFront("scroll")
	if err := d.Scroll(ctx, cx, cy, "down", 3); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	requireFront("drag")
	if err := d.Drag(ctx, cx-40, cy, cx+40, cy); err != nil {
		t.Fatalf("Drag: %v", err)
	}
	requireFront("double click")
	if err := d.Click(ctx, cx, cy, tool.MouseLeft, 2); err != nil {
		t.Fatalf("double click: %v", err)
	}
}
