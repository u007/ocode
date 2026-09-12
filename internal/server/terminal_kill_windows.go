//go:build windows

package server

import "time"

// terminateProcessTree is a no-op on Windows: interactive terminals are not
// spawned there (handler_terminal.go is !windows), so there are no pty processes
// to terminate on desktop quit. The signature matches the unix variant so the
// caller is platform-agnostic.
func terminateProcessTree(pid int, grace time.Duration) {}

// terminateProcessTreeSignal mirrors the unix split: no pty processes exist on
// Windows, so signaling is a no-op. Present so shutdownGracefully compiles on
// every platform.
func terminateProcessTreeSignal(pid int) {}

// terminateProcessTreeKill mirrors the unix split: escalation is a no-op on
// Windows for the same reason.
func terminateProcessTreeKill(pid int) {}
