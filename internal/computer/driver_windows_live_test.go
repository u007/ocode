//go:build windows

package computer

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestWindowsLive_ScreenshotAndCursor(t *testing.T) {
	if os.Getenv("OCODE_COMPUTER_LIVE") != "1" {
		t.Skip("set OCODE_COMPUTER_LIVE=1 to run live driver tests")
	}
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	d, err := newWindowsDriver(&execRunner{sup: sup}, sup)
	if err != nil {
		t.Fatalf("newWindowsDriver error: %v", err)
	}
	ctx := context.Background()
	pngData, w, h, err := d.Screenshot(ctx)
	if err != nil {
		t.Fatalf("Screenshot error: %v", err)
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("expected positive dimensions got %dx%d", w, h)
	}
	if _, err := png.Decode(bytes.NewReader(pngData)); err != nil {
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
func TestWindowsLive_Move(t *testing.T) {
	if os.Getenv("OCODE_COMPUTER_LIVE") != "1" {
		t.Skip("set OCODE_COMPUTER_LIVE=1 to run live driver tests")
	}
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	d, err := newWindowsDriver(&execRunner{sup: sup}, sup)
	if err != nil {
		t.Fatalf("newWindowsDriver error: %v", err)
	}
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
