//go:build windows

package server

import "net/http"

// HandleTerminalWS is unavailable on Windows: the pty bridge is built on
// github.com/creack/pty, which is Unix-only. The route stays registered so the
// Windows build keeps the same surface and the web UI gets an explicit status
// instead of a 404.
func (h *Handler) HandleTerminalWS(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "interactive terminal is not supported on Windows")
}
