package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/projects"
)

// remoteLifecycleProject resolves the {host} path value of a remote lifecycle
// request to the first saved project on that host. The saved project supplies
// the path and remote port that connect/restart pass to the registry. As with
// the proxy, only hosts present in the saved project store are allowed.
func (h *Handler) remoteLifecycleProject(r *http.Request) (projects.Project, int, string) {
	host := r.PathValue("host")
	if host == "" {
		return projects.Project{}, http.StatusBadRequest, "missing host in URL path"
	}
	p, ok := h.firstSavedProjectForHost(host)
	if !ok {
		return projects.Project{}, http.StatusForbidden, "unknown host"
	}
	return p, 0, ""
}

// requireRemoteHosts guards the lifecycle endpoints when the registry is not
// wired (e.g. a Handler built without a Server).
func (h *Handler) requireRemoteHosts(w http.ResponseWriter) bool {
	if h.remoteHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "remote connections not available")
		return false
	}
	return true
}

// writeRemoteLifecycleError writes the 502 body for a failed lifecycle action,
// including the stage that failed. A remoteHostStageError carries the stage;
// anything else uses fallbackStage.
func writeRemoteLifecycleError(w http.ResponseWriter, err error, fallbackStage string) {
	stage := fallbackStage
	var stageErr *remoteHostStageError
	if errors.As(err, &stageErr) && stageErr.Stage != "" {
		stage = stageErr.Stage
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{
		"error": err.Error(),
		"stage": stage,
	})
}

// HandleRemoteStatus reports what the local registry knows about a remote host
// without connecting. A host that has never connected reports
// connected=false.
func (h *Handler) HandleRemoteStatus(w http.ResponseWriter, r *http.Request) {
	p, status, message := h.remoteLifecycleProject(r)
	if status != 0 {
		writeError(w, status, message)
		return
	}
	if !h.requireRemoteHosts(w) {
		return
	}
	writeJSON(w, http.StatusOK, h.remoteHosts.status(p.Host, p.RemotePort))
}

// HandleRemoteConnect brings a remote host up (discover-or-start the server,
// open the tunnel, register the project) and returns its status.
func (h *Handler) HandleRemoteConnect(w http.ResponseWriter, r *http.Request) {
	p, status, message := h.remoteLifecycleProject(r)
	if status != 0 {
		writeError(w, status, message)
		return
	}
	if !h.requireRemoteHosts(w) {
		return
	}
	st, err := h.remoteHosts.connectHost(p.Host, p.Path, p.RemotePort)
	if err != nil {
		log.Printf("remote lifecycle: connect host %s failed: %v", p.Host, err)
		writeRemoteLifecycleError(w, err, "remote-connect")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// HandleRemoteRestart kills the host's remote server, reconnects at the local
// version, and re-registers the host's saved projects. It is unguarded: it
// kills running turns and terminals.
func (h *Handler) HandleRemoteRestart(w http.ResponseWriter, r *http.Request) {
	p, status, message := h.remoteLifecycleProject(r)
	if status != 0 {
		writeError(w, status, message)
		return
	}
	if !h.requireRemoteHosts(w) {
		return
	}
	st, err := h.remoteHosts.restart(p.Host, p.Path, p.RemotePort)
	if err != nil {
		log.Printf("remote lifecycle: restart host %s failed: %v", p.Host, err)
		writeRemoteLifecycleError(w, err, "remote-restart")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// HandleRemoteSync re-pushes the local auth-profile store and global config
// files to an already-connected remote host. force=true bypasses the
// per-host skip-if-unchanged cache.
func (h *Handler) HandleRemoteSync(w http.ResponseWriter, r *http.Request) {
	p, status, message := h.remoteLifecycleProject(r)
	if status != 0 {
		writeError(w, status, message)
		return
	}
	if !h.requireRemoteHosts(w) {
		return
	}
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	if err := h.remoteHosts.SyncHost(p.Host, p.RemotePort, force); err != nil {
		log.Printf("remote sync: %s failed: %v", p.Host, err)
		writeRemoteLifecycleError(w, err, "remote-sync")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "synced"})
}

// SyncConnectedRemoteHosts fans out a cache-aware sync to every connected
// remote host in a background goroutine. Best-effort; errors are logged.
// Safe to call from config/auth save paths; it never blocks the caller.
//
// Runs are single-flight: a call made while a sync is in progress marks one
// follow-up run (so the latest save is still pushed) instead of starting an
// overlapping sync, however fast saves arrive.
func (h *Handler) SyncConnectedRemoteHosts() {
	if h.remoteHosts == nil {
		return
	}
	h.remoteSyncMu.Lock()
	if h.remoteSyncRunning {
		h.remoteSyncPending = true
		h.remoteSyncMu.Unlock()
		return
	}
	h.remoteSyncRunning = true
	h.remoteSyncMu.Unlock()
	crashguard.Go(func() {
		for {
			h.remoteHosts.SyncConnectedHosts()
			h.remoteSyncMu.Lock()
			if !h.remoteSyncPending {
				h.remoteSyncRunning = false
				h.remoteSyncMu.Unlock()
				return
			}
			h.remoteSyncPending = false
			h.remoteSyncMu.Unlock()
		}
	})
}
