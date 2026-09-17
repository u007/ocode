package computer

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// stubPermissionRun replaces permissionRun for the duration of a test so no
// real OS permission prompt is ever fired.
func stubPermissionRun(t *testing.T, fn func(ctx context.Context, name string, args ...string) (string, error)) {
	t.Helper()
	orig := permissionRun
	permissionRun = fn
	t.Cleanup(func() { permissionRun = orig })
}

// TestRequestPermissionsNeverFails verifies the cross-platform contract: the
// call returns a report (never an error) with a platform tag and non-blank
// lines, even when every helper command fails.
func TestRequestPermissionsNeverFails(t *testing.T) {
	stubPermissionRun(t, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("boom")
	})

	rep := RequestPermissions(nil)
	if rep.Platform != runtime.GOOS {
		t.Fatalf("platform = %q, want %q", rep.Platform, runtime.GOOS)
	}
	if len(rep.Lines) == 0 {
		t.Fatal("report has no lines")
	}
	for i, line := range rep.Lines {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("line %d is blank: %q", i, rep.Lines)
		}
	}
}
