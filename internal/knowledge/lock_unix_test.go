//go:build !windows

package knowledge

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestWithBundleLockBoundedWait lives here because it takes an exclusive flock
// on the lock file via syscall.Flock, which does not exist on Windows. Keeping
// it in the untagged lock_test.go broke the Windows build of this package's
// tests (and with it every test in the package).
func TestWithBundleLockBoundedWait(t *testing.T) {
	td := t.TempDir()

	// Acquire the lock externally on the lock file (not just create the file).
	lockPath := filepath.Join(td, ".okf.lock")
	f, err := os.OpenFile(lockPath, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Acquire an exclusive flock on the external fd.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("failed to acquire external lock: %v", err)
	}

	// With the lock held externally, WithBundleLock should time out.
	err = WithBundleLock(td, func() error {
		t.Error("fn should not be executed when lock is held")
		return nil
	})
	if err == nil {
		t.Fatal("expected timeout error when lock is held externally")
	}
	// Release the external lock so cleanup doesn't hang.
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
