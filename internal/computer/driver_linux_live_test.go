//go:build linux

package computer

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// newLiveLinuxDriver builds a driver over real processes for the live
// smoke tests, which are skipped unless OCODE_COMPUTER_LIVE=1.
func newLiveLinuxDriver(t *testing.T) tool.ComputerDriver {
	t.Helper()
	if os.Getenv("OCODE_COMPUTER_LIVE") != "1" {
		t.Skip("set OCODE_COMPUTER_LIVE=1 to run live driver tests")
	}
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	d, err := newLinuxDriver(&execRunner{sup: sup}, sup)
	if err != nil {
		t.Fatalf("newLinuxDriver error: %v", err)
	}
	return d
}

// TestLinuxLive_Screenshot captures the primary display and checks the
// bytes decode as PNG with positive reported dimensions.
func TestLinuxLive_Screenshot(t *testing.T) {
	d := newLiveLinuxDriver(t)
	pngData, w, h, err := d.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot error: %v", err)
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("expected positive dimensions got %dx%d", w, h)
	}
	if _, err := png.Decode(bytes.NewReader(pngData)); err != nil {
		t.Fatalf("screenshot is not valid PNG: %v", err)
	}
}

// TestLinuxLive_MoveCursorRoundTrip moves the pointer and reads it back.
// It performs no clicks and types nothing. Cursor is unsupported on
// Wayland, so the round-trip assertion is X11-only.
func TestLinuxLive_MoveCursorRoundTrip(t *testing.T) {
	d := newLiveLinuxDriver(t)
	ctx := context.Background()
	if linuxBackend(os.Getenv) == "wayland" {
		t.Skip("cursor_position is unsupported on wayland")
	}
	const wantX, wantY = 100, 100
	if err := d.Move(ctx, wantX, wantY); err != nil {
		t.Fatalf("Move error: %v", err)
	}
	x, y, err := d.Cursor(ctx)
	if err != nil {
		t.Fatalf("Cursor error: %v", err)
	}
	if x != wantX || y != wantY {
		t.Fatalf("cursor round-trip: got %d,%d want %d,%d", x, y, wantX, wantY)
	}
}
