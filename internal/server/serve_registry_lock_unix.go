//go:build !windows

package server

import (
	"errors"
	"os"
	"syscall"
)

// tryLockServeRecord takes a non-blocking exclusive advisory lock on f.
// locked is false when another process already holds it. The kernel releases
// the lock when the owner exits, which is what makes a record's owner provable
// without trusting a recyclable pid.
func tryLockServeRecord(f *os.File) (locked bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

func unlockServeRecord(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
