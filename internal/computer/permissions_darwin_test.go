//go:build darwin

package computer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// stubDarwinPermissions installs a permissionRun stub that answers the three
// macOS permission helpers according to ax/sr/auto, recording every `open`
// target. sr=false makes the screencapture probe fail; there is no way to make
// it succeed while reporting "denied" because macOS returns a wallpaper-only
// image without an error.
func stubDarwinPermissions(t *testing.T, ax, sr, auto bool, opened *[]string) {
	t.Helper()
	stubPermissionRun(t, func(_ context.Context, name string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case name == "open":
			*opened = append(*opened, joined)
			return "", nil
		case name == "screencapture":
			if sr {
				return "", nil
			}
			return "", errors.New("capture probe failed")
		case name == "osascript" && strings.Contains(joined, "AXIsProcessTrustedWithOptions"):
			return boolString(ax), nil
		case name == "osascript" && strings.Contains(joined, "System Events"):
			if auto {
				return "Finder", nil
			}
			return "", errors.New("automation not authorized")
		}
		t.Fatalf("unexpected command: %s %s", name, joined)
		return "", nil
	})
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestPlatformRequestPermissionsAllGranted(t *testing.T) {
	var opened []string
	stubDarwinPermissions(t, true, true, true, &opened)

	rep := RequestPermissions(context.Background())
	if !rep.Granted {
		t.Fatalf("Granted = false, want true; lines = %v", rep.Lines)
	}
	if len(opened) != 0 {
		t.Fatalf("opened settings panes %v, want none", opened)
	}
	if !slices.ContainsFunc(rep.Lines, func(l string) bool {
		return strings.Contains(l, "All detectable permissions are in place")
	}) {
		t.Fatalf("missing all-granted line: %v", rep.Lines)
	}
}

func TestPlatformRequestPermissionsMissingAccessibilityOnly(t *testing.T) {
	var opened []string
	stubDarwinPermissions(t, false, true, true, &opened)

	rep := RequestPermissions(context.Background())
	if rep.Granted {
		t.Fatal("Granted = true, want false")
	}
	if !slices.Equal(opened, []string{accessibilityPane}) {
		t.Fatalf("opened = %v, want [%s]", opened, accessibilityPane)
	}
}

func TestPlatformRequestPermissionsFailedScreenRecordingProbe(t *testing.T) {
	var opened []string
	stubDarwinPermissions(t, true, false, true, &opened)

	rep := RequestPermissions(context.Background())
	if rep.Granted {
		t.Fatal("Granted = true, want false")
	}
	if !slices.Equal(opened, []string{screenRecordingPane}) {
		t.Fatalf("opened = %v, want [%s]", opened, screenRecordingPane)
	}
}

func TestPlatformRequestPermissionsMissingAutomationOnly(t *testing.T) {
	var opened []string
	stubDarwinPermissions(t, true, true, false, &opened)

	rep := RequestPermissions(context.Background())
	if rep.Granted {
		t.Fatal("Granted = true, want false")
	}
	if !slices.Equal(opened, []string{automationPane}) {
		t.Fatalf("opened = %v, want [%s]", opened, automationPane)
	}
}

func TestPlatformRequestPermissionsMultipleMissingOpensRoot(t *testing.T) {
	var opened []string
	stubDarwinPermissions(t, false, false, true, &opened)

	rep := RequestPermissions(context.Background())
	if rep.Granted {
		t.Fatal("Granted = true, want false")
	}
	if !slices.Equal(opened, []string{privacyRoot}) {
		t.Fatalf("opened = %v, want [%s]", opened, privacyRoot)
	}
}

func TestPlatformRequestPermissionsReportsOpenFailure(t *testing.T) {
	stubPermissionRun(t, func(_ context.Context, name string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case name == "open":
			return "", errors.New("open failed")
		case name == "screencapture":
			return "", nil
		case strings.Contains(joined, "AXIsProcessTrustedWithOptions"):
			return "false", nil
		default:
			return "Finder", nil
		}
	})

	rep := RequestPermissions(context.Background())
	if !slices.ContainsFunc(rep.Lines, func(l string) bool {
		return strings.Contains(l, "Open System Settings")
	}) {
		t.Fatalf("missing manual-open fallback line: %v", rep.Lines)
	}
}
