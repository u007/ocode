//go:build windows

package stt

import "syscall"

// workerProcAttr returns the platform process attributes for the worker.
//
// Windows has no setsid and no parent-death signal. The worker is stopped
// explicitly by stopWorkerLocked, and the idle timeout bounds how long it
// otherwise lives.
func workerProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
