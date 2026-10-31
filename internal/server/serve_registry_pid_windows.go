//go:build windows

package server

import (
	"errors"

	"golang.org/x/sys/windows"
)

// servePIDAlive reports whether pid names a process that currently exists.
// OpenProcess fails with ERROR_INVALID_PARAMETER for a pid that is gone;
// ERROR_ACCESS_DENIED means the process exists but is not openable, which still
// counts as alive.
func servePIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	_ = windows.CloseHandle(h)
	return true
}
