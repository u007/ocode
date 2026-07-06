//go:build windows

package knowledge

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLockFile attempts to acquire an exclusive non-blocking lock on f using
// LockFileEx with LOCKFILE_FAIL_IMMEDIATELY.
// Returns nil on success, or an error if the lock is held.
func tryLockFile(f *os.File) error {
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&windows.Overlapped{},
	)
}

// unlockFile releases the lock on f.
func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,
		1,
		0,
		&windows.Overlapped{},
	)
}
