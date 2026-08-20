//go:build windows

package lsp

import "os/exec"

// detachProcAttr is a no-op on Windows; process groups are POSIX-only here
// (mirrors internal/server's proc_windows.go).
func detachProcAttr(_ *exec.Cmd) {}
