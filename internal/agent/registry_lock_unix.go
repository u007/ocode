//go:build !windows

package agent

import (
	"os"
	"syscall"
)

// tryLockFile attempts to acquire an exclusive non-blocking lock on f.
// Returns nil on success, or an error if the lock is held by another process.
func tryLockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlockFile releases the lock on f.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
