package server

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/umlinfo"
)

type umlResponse struct {
	State string         `json:"state"` // ready | indexing | error
	Index *umlinfo.Index `json:"index,omitempty"`
	Error string         `json:"error,omitempty"`
}

type umlStatusResponse struct {
	State    string `json:"state"` // indexing | error | idle
	Error    string `json:"error,omitempty"`
	Building bool   `json:"building"`
}

// HandleUml serves the AST symbol index behind the UML session sub-tab.
//
//   - project is resolved against allowedProjectRoots() (containment),
//     never against h.workDir or os.Getwd().
//   - ?host= is refused with 400 in v1: remote projects are indexed by the
//     host's own server, which is where the source of truth lives. The
//     proxy path may carry ?host= later; this handler never routes remote.
//   - A fresh fingerprint returns 200 with the index. Otherwise the client
//     gets 202 and polls; the build runs in a background goroutine with a
//     30s deadline. The request never pins the HTTP connection on the
//     build (the WKWebView connection budget is finite).
func (h *Handler) HandleUml(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("host") != "" {
		writeJSON(w, http.StatusBadRequest, umlResponse{State: "error", Error: "remote projects are not indexed by this server yet; open the UML tab on the host serving the project"})
		return
	}
	project := r.URL.Query().Get("project")
	if project == "" {
		writeJSON(w, http.StatusBadRequest, umlResponse{State: "error", Error: "project is required"})
		return
	}
	abs, err := filepath.Abs(project)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, umlResponse{State: "error", Error: "invalid project path"})
		return
	}
	if !h.projectRootAllowed(abs) {
		writeJSON(w, http.StatusForbidden, umlResponse{State: "error", Error: "project is outside the allowed roots for this server"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	fp, err := umlinfo.Fingerprint(ctx, abs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, umlResponse{State: "error", Error: "failed to fingerprint the project: " + err.Error()})
		return
	}
	if idx, ok := h.umlIndexer.Get(abs, fp); ok {
		writeJSON(w, http.StatusOK, umlResponse{State: "ready", Index: idx})
		return
	}
	if err, ok := h.umlIndexer.LastError(abs); ok && !h.umlIndexer.Building(abs) {
		writeJSON(w, http.StatusInternalServerError, umlResponse{State: "error", Error: err.Error()})
		return
	}
	h.umlIndexer.StartBuild(abs, fp)
	w.Header().Set("Retry-After", "1")
	writeJSON(w, http.StatusAccepted, umlResponse{State: "indexing"})
}

// HandleUmlStatus reports build progress without triggering one, so a
// client that only needs "is it done yet" does not kick work off.
func (h *Handler) HandleUmlStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("host") != "" {
		writeJSON(w, http.StatusBadRequest, umlStatusResponse{State: "error", Error: "remote not supported"})
		return
	}
	project := r.URL.Query().Get("project")
	if project == "" {
		writeJSON(w, http.StatusBadRequest, umlStatusResponse{State: "error", Error: "project is required"})
		return
	}
	abs, err := filepath.Abs(project)
	if err != nil || !h.projectRootAllowed(abs) {
		writeJSON(w, http.StatusForbidden, umlStatusResponse{State: "error", Error: "project is outside the allowed roots for this server"})
		return
	}
	if h.umlIndexer.Building(abs) {
		writeJSON(w, http.StatusOK, umlStatusResponse{State: "indexing", Building: true})
		return
	}
	if err, ok := h.umlIndexer.LastError(abs); ok {
		writeJSON(w, http.StatusOK, umlStatusResponse{State: "error", Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, umlStatusResponse{State: "idle"})
}

// projectRootAllowed reports whether abs is contained in any locally-
// allowed root (workDir or a saved local project root). Symlink-resolved
// and lexical containment both count, matching the file-tree boundary.
func (h *Handler) projectRootAllowed(abs string) bool {
	for _, root := range h.allowedProjectRoots() {
		if root == "" {
			continue
		}
		if containedIn(abs, root) || containedLexical(abs, root) {
			return true
		}
	}
	return false
}
