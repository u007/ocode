//go:build !windows

package server

import (
	"errors"
	"syscall"
)

// servePIDAlive reports whether pid names a process that currently exists.
// Signal 0 performs the existence check without delivering anything. EPERM
// still means the process exists (it belongs to another user), so only ESRCH
// is treated as gone.
func servePIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
