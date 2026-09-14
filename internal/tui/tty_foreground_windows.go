//go:build windows

package tui

// reclaimTTYForeground is a no-op on Windows; POSIX process-group/tty
// foreground semantics (and the SIGTTOU hijack it guards against on
// Unix — see the unix build's doc comment) don't apply there.
func reclaimTTYForeground() {}

// ttyForegroundPgrp has no Windows equivalent (no POSIX job control).
func ttyForegroundPgrp() (self, fg int, err error) { return 0, 0, nil }
