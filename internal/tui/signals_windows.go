//go:build windows

package tui

import "os"

// terminationSignals is the set watchProgramSignals asks the runtime to
// deliver. Windows has no SIGTERM/SIGHUP/SIGCONT, so only os.Interrupt
// (CTRL_C_EVENT / CTRL_BREAK_EVENT) is available — but it is deliberately
// kept: it is what drives the graceful cleanup request, and without it a
// Ctrl+C would hard-kill the process, skipping bubbletea's tty restore and
// leaving the alt-screen plus mouse tracking enabled in the user's shell.
// The Unix-only signals have no job-control equivalent here, so there is
// nothing to log for a stop/resume cycle.
func terminationSignals() []os.Signal { return []os.Signal{os.Interrupt} }

// isResumeSignal is always false on Windows: there is no SIGCONT.
func isResumeSignal(os.Signal) bool { return false }
