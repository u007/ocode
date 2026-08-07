// Package filelock provides advisory cross-process file locking with a
// bounded retry loop, used by anything that needs to serialize writes to a
// shared file or directory across processes (and goroutines). It is the
// single implementation; higher-level helpers (e.g. the knowledge bundle's
// WithBundleLock) delegate to it rather than duplicating the flock logic.
package filelock

import (
	"fmt"
	"log/slog"
	"os"
	"time"
)

const (
	// RetryInterval is how long to wait between lock acquisition attempts.
	RetryInterval = 100 * time.Millisecond

	// Timeout is the maximum time to wait for the lock.
	Timeout = 10 * time.Second
)

// WithFileLock acquires an exclusive advisory lock on lockPath, executes fn,
// and releases the lock on return.
//
// The lock file is created if it does not exist. The lock is implemented using
// platform-specific file locking (flock on Unix, LockFileEx on Windows). If the
// lock cannot be acquired within Timeout, an error is returned. The lock is
// released when fn returns, even if fn panics.
func WithFileLock(lockPath string, fn func() error) (err error) {
	// Open (or create) the lock file.
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("filelock: open lock file %s: %w", lockPath, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("filelock: close lock file %s: %w", lockPath, closeErr)
		}
	}()

	// Try to acquire the exclusive lock with a bounded wait.
	deadline := time.Now().Add(Timeout)
	for {
		lockErr := tryLockFile(f)
		if lockErr == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("filelock: timeout waiting for lock on %s", lockPath)
		}
		time.Sleep(RetryInterval)
	}

	// Ensure the lock is released when fn returns.
	defer func() {
		if unlockErr := unlockFile(f); unlockErr != nil {
			slog.Error("filelock: failed to unlock lock file", "path", lockPath, "err", unlockErr)
		}
	}()

	return fn()
}
