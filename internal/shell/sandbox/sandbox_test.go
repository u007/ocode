package sandbox

import (
	"os/exec"
	"runtime"
	"testing"
)

// TestNoopWrapPassthrough locks the no-op contract: Wrap returns the cmd
// unchanged with nil error, and Available() is false (Windows and pre-backend
// builds degrade to normal prompting — never a fake confinement claim).
func TestNoopWrapPassthrough(t *testing.T) {
	w := newNoop()
	cmd := exec.Command("true")
	out, err := w.Wrap(cmd, RootSet{})
	if err != nil {
		t.Fatalf("noop Wrap error: %v", err)
	}
	if out != cmd {
		t.Fatal("noop Wrap must return the same *exec.Cmd pointer")
	}
	if w.Available() {
		t.Fatal("noop Available() = true, want false")
	}
}

// TestSupportedMatchesGOOS locks the compile-time support table: darwin/linux
// have real backends, everything else does not.
func TestSupportedMatchesGOOS(t *testing.T) {
	want := runtime.GOOS == "darwin" || runtime.GOOS == "linux"
	if got := Supported(); got != want {
		t.Fatalf("Supported() = %v on %s, want %v", got, runtime.GOOS, want)
	}
}

// TestNewReturnsWrapper locks the factory contract: New() never returns nil on
// any platform (no-op fallback keeps every target compiling).
func TestNewReturnsWrapper(t *testing.T) {
	if w := New(); w == nil {
		t.Fatal("New() returned nil")
	}
}
