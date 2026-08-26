//go:build windows

package tui

// reclaimTTYForeground is a no-op on Windows; POSIX process-group/tty
// foreground semantics (and the SIGTTOU hijack it guards against on
// Unix — see the unix build's doc comment) don't apply there.
func reclaimTTYForeground() {}
