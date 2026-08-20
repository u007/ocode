//go:build windows

package server

import "time"

// terminateProcessTree is a no-op on Windows: interactive terminals are not
// spawned there (handler_terminal.go is !windows), so there are no pty processes
// to terminate on desktop quit. The signature matches the unix variant so the
// caller is platform-agnostic.
func terminateProcessTree(pid int, grace time.Duration) {}
