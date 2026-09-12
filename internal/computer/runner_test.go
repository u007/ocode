package computer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

func newTestSupervisor(t *testing.T) *tool.ProcessSupervisor {
	t.Helper()
	return tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
}

// TestRunner_CapturesStdout verifies execRunner.run captures
// stdout and registers a process record with kind computer.
func TestRunner_CapturesStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a shell")
	}
	sup := newTestSupervisor(t)
	r := &execRunner{sup: sup}
	out, err := r.run(context.Background(), "echo", "hi")
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if out != "hi\n" {
		t.Fatalf("expected %q got %q", "hi\n", out)
	}
	snap := sup.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 record got %d", len(snap))
	}
	if snap[0].Kind != tool.ProcessKindComputer {
		t.Fatalf("expected kind %q got %q", tool.ProcessKindComputer, snap[0].Kind)
	}
}

// TestRunner_NonZeroExitIncludesStderr verifies that a non-zero
// exit code returns an error containing stderr output.
func TestRunner_NonZeroExitIncludesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a shell")
	}
	sup := newTestSupervisor(t)
	r := &execRunner{sup: sup}
	_, err := r.run(context.Background(), "sh", "-c", "echo bad >&2; exit 3")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Fatalf("error should contain %q: %v", "bad", err)
	}
}

// TestRunner_MissingBinary verifies that a missing binary
// returns an error wrapping exec.ErrNotFound.
func TestRunner_MissingBinary(t *testing.T) {
	sup := newTestSupervisor(t)
	r := &execRunner{sup: sup}
	_, err := r.run(context.Background(), "ocode-definitely-missing-binary")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRunner_Timeout verifies that a long-running command
// with a short context returns a context error quickly.
func TestRunner_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep not portable")
	}
	sup := newTestSupervisor(t)
	r := &execRunner{sup: sup}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := r.run(ctx, "sleep", "5")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context error, got %v", err)
	}
}

// TestNew_NilSupervisor verifies that New returns an error
// when given a nil supervisor.
func TestNew_NilSupervisor(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "supervisor") {
		t.Fatalf("expected supervisor error, got %v", err)
	}
}

// TestTempPNGPathUnderTempDir verifies that tempPNGPath
// returns a path under the temp directory.
func TestTempPNGPathUnderTempDir(t *testing.T) {
	path, err := tempPNGPath()
	if err != nil {
		t.Fatalf("tempPNGPath error: %v", err)
	}
	defer os.Remove(path)
	dir := filepath.Dir(path)
	if filepath.Clean(dir) != filepath.Clean(os.TempDir()) {
		t.Fatalf("expected %q got %q", os.TempDir(), dir)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Fatalf("expected .png suffix got %q", path)
	}
	if !strings.Contains(filepath.Base(path), "ocode-computer") {
		t.Fatalf("expected prefix ocode-computer in %q", path)
	}
}
