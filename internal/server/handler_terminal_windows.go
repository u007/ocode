//go:build windows

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// HandleTerminalWS is unavailable on Windows: the pty bridge is built on
// github.com/creack/pty, which is Unix-only. The route stays registered so the
// Windows build keeps the same surface and the web UI gets an explicit status
// instead of a 404.
func (h *Handler) HandleTerminalWS(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "interactive terminal is not supported on Windows")
}

// HandleTerminalProcesses mirrors the Unix stub: no pty processes exist.
func (h *Handler) HandleTerminalProcesses(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode([]any{})
}

// HandleTerminalKill mirrors the Unix stub: there is no shell to kill.
func (h *Handler) HandleTerminalKill(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "interactive terminal is not supported on Windows")
}

// HandleTerminalList mirrors the Unix inventory endpoint: no pty sessions
// exist on Windows, so the route is present but unimplemented.
func (h *Handler) HandleTerminalList(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "interactive terminal is not supported on Windows")
}

// HandleTerminalHistory mirrors the other terminal stubs so the history route
// remains present in the Windows build even though no pty log is created.
func (h *Handler) HandleTerminalHistory(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "interactive terminal is not supported on Windows")
}

// terminalSession is only ever constructed by the Unix pty bridge; the type
// exists here so terminalSessionTable compiles on Windows.
type terminalSession struct {
	resumable bool
	project   string
	startedAt time.Time
	title     string
}

func (s *terminalSession) hasExited() bool { return true }
func (s *terminalSession) kill()           {}

// snapshot is never reached on Windows (no pty sessions exist) but must exist
// for terminalSessionTable.listForProject to compile.
func (s *terminalSession) snapshot() terminalListEntry {
	return terminalListEntry{}
}

// shutdownGracefully is a no-op on Windows: no pty processes exist (see
// terminal_kill_windows.go), so there is nothing to signal or drain. Present
// so Handler.shutdownTerminals compiles on every platform.
func (s *terminalSession) shutdownGracefully(_ context.Context) {}
