//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package stt

import "syscall"

// workerProcAttr returns the platform process attributes for the worker.
//
// The child must not inherit the controlling terminal: the TUI runs in the
// alt-screen and a child that opens /dev/tty can steal the foreground process
// group and corrupt the whole shell session. Setsid gives the child its own
// session with no controlling terminal.
//
// There is no parent-death signal on these platforms (Go's syscall package
// only exposes Pdeathsig on Linux), so an abruptly killed ocode can leave the
// worker behind. The idle timeout and the explicit stop on shutdown bound the
// damage, and the worker holds no user data.
func workerProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
