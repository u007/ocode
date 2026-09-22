//go:build windows

package shell

import "context"

// Session is the Windows stub of the persistent interactive shell. Windows has
// no pty in this codebase, so NewSession always fails and the server keeps its
// one-shot `cmd /C` fallback. The type and methods exist so callers can be
// written platform-neutrally.
type Session struct{}

// NewSession reports that persistent shells are unsupported on this platform.
func NewSession(SessionOptions) (*Session, error) {
	return nil, ErrUnsupported
}

// Run always fails: a stub session can never be constructed.
func (*Session) Run(context.Context, string) (Result, string, error) {
	return Result{ExitCode: 1, Err: ErrUnsupported}, "", ErrUnsupported
}

// Close is a no-op on the stub.
func (*Session) Close() error { return nil }

// Alive reports false: the stub never represents a live shell.
func (*Session) Alive() bool { return false }
