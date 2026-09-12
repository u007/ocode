//go:build darwin

package computer

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestDarwinLive_ScreenshotAndCursor(t *testing.T) {
	if os.Getenv("OCODE_COMPUTER_LIVE") != "1" {
		t.Skip("set OCODE_COMPUTER_LIVE=1 to run live driver tests")
	}
	d := &darwinDriver{r: &execRunner{sup: tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})}}
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
	d := &darwinDriver{r: &execRunner{sup: tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})}}
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
