//go:build linux

package stt

import "syscall"

// workerProcAttr returns the platform process attributes for the worker.
//
// The child must not inherit the controlling terminal: the TUI runs in the
// alt-screen and a child that opens /dev/tty can steal the foreground process
// group and corrupt the whole shell session. Setsid gives the child its own
// session with no controlling terminal.
//
// Pdeathsig makes the kernel kill the worker when ocode exits. Without it a
// SIGKILLed ocode orphans a multi-gigabyte Python process holding the model.
func workerProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGKILL}
}
