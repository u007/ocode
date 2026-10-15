package server

import (
	"fmt"
	"log"
	"net/http"
)

// handleRemoteBrowseConfig answers /api/browse/config?host=<remote> with the
// browse origin that `ocode serve --remote` runs on that host. The workspace
// tunnel forwards the same port number to this machine (see
// remote.StartTunnel), so the base is the local end of that tunnel
// (RemoteWorkspace.BrowseURL): a page on the host's `localhost` is then
// reached from the host, not from here.
//
// Admission matches the path-less remote proxy: the host must have a saved
// project. A failed connect or a remote without a browse origin is an error,
// never a fallback to this machine's own browse origin — that would dial
// localhost on the wrong machine.
func (s *Server) handleRemoteBrowseConfig(w http.ResponseWriter, r *http.Request, host string) {
	h := s.handler
	if h.remoteHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "remote connections not available")
		return
	}
	project, ok := h.firstSavedProjectForHost(host)
	if !ok {
		writeError(w, http.StatusForbidden, "unknown host")
		return
	}
	ws, err := h.remoteHosts.workspaceForPort(host, project.Path, project.RemotePort)
	if err != nil {
		log.Printf("remote browse config: connect host %s failed: %v", host, err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("remote connect failed: %v", err))
		return
	}
	tunnelBase := ws.BrowseURL()
	if tunnelBase == "" {
		log.Printf("remote browse config: host %s reports no browse origin (browse port 0)", host)
		writeError(w, http.StatusBadGateway, "remote server has no browse origin")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url":    sameSiteBrowseBase(tunnelBase, r.Host),
		"remote_mode": true,
	})
}
