package knowledge

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	// lockFileName is the name of the lock file created in the bundle root.
	lockFileName = ".okf.lock"

	// lockRetryInterval is how long to wait between lock acquisition attempts.
	lockRetryInterval = 100 * time.Millisecond

	// lockTimeout is the maximum time to wait for the lock.
	lockTimeout = 10 * time.Second
)

// WithBundleLock acquires an exclusive advisory lock on <root>/.okf.lock,
// executes fn, and releases the lock on return.
//
// The lock file is created if it does not exist. The lock is implemented using
// platform-specific file locking (flock on Unix, LockFileEx on Windows). If the
// lock cannot be acquired within ~10 seconds, an error is returned. The lock is
// released when fn returns, even if fn panics.
func WithBundleLock(root string, fn func() error) (err error) {
	lockPath := filepath.Join(root, lockFileName)

	// Open (or create) the lock file.
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("knowledge: open lock file %s: %w", lockPath, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("knowledge: close lock file %s: %w", lockPath, closeErr)
		}
	}()

	// Try to acquire the exclusive lock with a bounded wait.
	deadline := time.Now().Add(lockTimeout)
	for {
		lockErr := tryLockFile(f)
		if lockErr == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("knowledge: timeout waiting for lock on %s", lockPath)
		}
		time.Sleep(lockRetryInterval)
	}

	// Ensure the lock is released when fn returns.
	defer func() {
		if unlockErr := unlockFile(f); unlockErr != nil {
			slog.Error("knowledge: failed to unlock lock file", "path", lockPath, "err", unlockErr)
		}
	}()

	return fn()
}
