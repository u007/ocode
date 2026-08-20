//go:build windows

package server

import (
	"encoding/json"
	"net/http"
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
