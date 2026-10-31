//go:build windows

package server

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLockServeRecord takes a non-blocking exclusive lock on the first byte of
// f. locked is false when another process already holds it. Windows releases
// the lock when the owning process exits, so a record's owner is provable
// without trusting a recyclable pid.
func tryLockServeRecord(f *os.File) (locked bool, err error) {
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return false, err
}

func unlockServeRecord(f *os.File) {
	ol := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
