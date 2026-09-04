//go:build darwin

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSeatbeltTempScreenshotWriteEndToEnd proves the full OS path a
// screenshot save takes under a test-constructed RootSet granting only the
// runtime temp dir: a sandboxed shell command writing
// screenshot-<millis>.png directly under the temp dir succeeds,
// whether the target is spelled via the /var/... alias or the
// /private/var/... canonical path — and a write to an existing dir outside
// the granted roots still fails at the OS level.
func TestSeatbeltTempScreenshotWriteEndToEnd(t *testing.T) {
	if newWrapper().Available() == false {
		t.Skip("sandbox-exec unavailable")
	}
	tmp := os.TempDir()
	canonical, err := filepath.EvalSymlinks(filepath.Clean(tmp))
	if err != nil {
		t.Fatal(err)
	}
	roots := NewRootSet([]RootSpec{{Path: tmp, Writable: true}})
	w := newWrapper()

	write := func(target string) error {
		cmd := exec.Command("sh", "-c", "echo png-bytes > "+target+" && echo OK")
		wrapped, err := w.Wrap(cmd, roots)
		if err != nil {
			return err
		}
		_, err = wrapped.CombinedOutput()
		return err
	}

	targets := []string{
		filepath.Join(tmp, "screenshot-e2e-alias.png"),
		filepath.Join(canonical, "screenshot-e2e-canonical.png"),
	}
	for _, target := range targets {
		t.Cleanup(func() { os.Remove(target) })
		if err := write(target); err != nil {
			t.Fatalf("sandboxed write to %q failed: %v", target, err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("target %q missing after sandboxed write: %v", target, err)
		}
	}

	// Negative: a real, existing directory outside the granted roots must
	// deny. Uses a sibling of the temp dir ($TMPDIR/..), which no static
	// production root covers either (production additionally grants /var/tmp,
	// so that path would NOT be a valid negative).
	outsideDir := filepath.Join(filepath.Dir(filepath.Clean(tmp)), "sandbox-e2e-outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outsideDir) })
	outside := filepath.Join(outsideDir, "sandbox-e2e-deny.png")
	t.Cleanup(func() { os.Remove(outside) })
	if err := write(outside); err == nil {
		t.Fatalf("sandboxed write to %q succeeded, want OS denial", outside)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside target %q exists after denied write", outside)
	}
}
