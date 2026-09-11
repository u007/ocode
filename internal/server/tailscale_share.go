package server

import (
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"

	"github.com/u007/ocode/internal/tailscale"
)

// desktopTailscalePath is the stable --set-path mount for the headless/
// desktop server share. A fixed path (instead of the server root) keeps
// cleanup scoped: Shutdown removes only this mount instead of resetting the
// global serve/funnel config that TUI /rc sessions share on the same node.
// The public URL becomes https://<tailnet-host>/desktop, and the SPA base
// path logic (client.ts _basePath) already resolves /desktop/session/<id>.
const desktopTailscalePath = "desktop"

// tailscaleShare caches one tailscale exposure per server process. The first
// GET /api/tailscale-url starts serve/funnel; later opens reuse the cached
// URL instead of spawning a process per dialog open (which would overwrite
// routes and leak background processes).
type tailscaleShare struct {
	mu      sync.Mutex
	url     string
	hint    string
	started bool
	proc    *exec.Cmd
	path    string
}

// ensure starts the exposure once for the given port and returns the cached
// result on later calls.
func (t *tailscaleShare) ensure(port int) (url, hint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return t.url, t.hint
	}
	t.started = true
	u, proc, hint := tailscale.StartExpose(port, desktopTailscalePath)
	t.url = u
	t.hint = hint
	t.proc = proc
	if u != "" {
		t.path = tailscale.SanitizePath(desktopTailscalePath)
	}
	return t.url, t.hint
}

// cleanup kills the background process and removes only our --set-path mount.
// It never calls a global reset, which would tear down TUI /rc sessions.
func (t *tailscaleShare) cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.proc != nil && t.proc.Process != nil {
		_ = t.proc.Process.Kill()
		t.proc = nil
	}
	if t.path != "" {
		tailscale.RemoveSetPath(t.path)
		t.path = ""
	}
	t.url = ""
	t.hint = ""
	t.started = false
}

// serverPort parses the bound port from the server address.
func serverPort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return p
}

// handleGetTailscaleURL reports the cached (or newly started) tailscale share
// URL for this server. Always 200: availability is in the body so the dialog
// can fall back to the LAN URL instead of treating "no tailnet" as an error.
func (s *Server) handleGetTailscaleURL(w http.ResponseWriter, r *http.Request) {
	if s.tsShare == nil {
		writeJSON(w, http.StatusOK, map[string]any{"url": "", "available": false})
		return
	}
	port := serverPort(s.Addr())
	if port == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"url": "", "available": false})
		return
	}
	u, hint := s.tsShare.ensure(port)
	writeJSON(w, http.StatusOK, map[string]any{
		"url":       u,
		"available": u != "",
		"hint":      hint,
	})
}
