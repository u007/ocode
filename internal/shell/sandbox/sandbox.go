// Package sandbox confines agent shell commands to a write-integrity boundary:
// filesystem writes outside the classified allowed roots fail at the OS level,
// while reads, exec, and network egress stay open. It is not a confidentiality
// boundary — a sandboxed command can still read secrets and send them out.
package sandbox

import (
	"os/exec"
	"runtime"
)

// Wrapper is a per-GOOS sandbox backend. Wrap rewrites cmd so the wrapped
// invocation confines filesystem writes to roots.WritableRoots (default-deny
// writes outside them, fail-closed); Available reports whether the backend can
// actually confine right now (binary present / kernel ABI ok). A backend that
// is Supported() (see below) but not Available() must cause the caller to fail
// the command, never to run it unconfined.
type Wrapper interface {
	// Wrap returns a (possibly rewritten) command that confines writes to the
	// given RootSet, or an error when confinement cannot be established — the
	// caller must treat that error as fail-closed (do not run the plain cmd).
	Wrap(cmd *exec.Cmd, roots RootSet) (*exec.Cmd, error)
	// Available reports whether this backend can confine right now.
	Available() bool
}

// Supported reports whether the current GOOS has a real confinement backend at
// compile time: darwin (Seatbelt) and linux (Landlock/bwrap) yes, everything
// else (including windows) no. Supported-but-unavailable must fail closed;
// !Supported degrades to normal prompting.
func Supported() bool {
	switch runtime.GOOS {
	case "darwin", "linux":
		return true
	default:
		return false
	}
}

// New returns the sandbox backend for the current GOOS. On platforms without a
// real backend (windows, others) it returns the no-op, which the permission
// layer treats as "degrade to normal".
func New() Wrapper { return newWrapper() }