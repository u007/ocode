package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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
//   - ?host= proxies to that host's own server, which indexes its own
//     filesystem: this server must never answer a remote project with a local
//     index (the paths differ, and the walk would read the wrong machine).
//   - A fresh fingerprint returns 200 with the index. Otherwise the client
//     gets 202 and polls; the build runs in a background goroutine with a
//     30s deadline. The request never pins the HTTP connection on the
//     build (the WKWebView connection budget is finite).
func (h *Handler) HandleUml(w http.ResponseWriter, r *http.Request) {
	if host := hostParam(r); host != "" {
		h.remoteUml(w, r, host, false)
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
	if host := hostParam(r); host != "" {
		h.remoteUml(w, r, host, true)
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

// remoteUmlBodyCap bounds one remote index response. A big monorepo's index
// marshals to several MB (docs and signatures included), so this is generous;
// the local handler's response is equally unbounded.
const remoteUmlBodyCap = 64 << 20

// remoteUml forwards the request to a connected host's own server, which
// indexes the host's filesystem. It never dials and never answers from this
// machine's index: a remote project's paths exist on the other machine, so a
// local walk would either fail containment or index nothing.
func (h *Handler) remoteUml(w http.ResponseWriter, r *http.Request, host string, status bool) {
	project := r.URL.Query().Get("project")
	if project == "" {
		writeJSON(w, http.StatusBadRequest, umlResponse{State: "error", Error: "project is required"})
		return
	}
	path := "/api/uml"
	if status {
		path = "/api/uml/status"
	}
	query := url.Values{}
	query.Set("project", project)
	var out umlResponse
	err := h.pulseRemoteRequest(host, h.pulseRemotePort(host), http.MethodGet, path, query, 30*time.Second, remoteUmlBodyCap, &out)
	if err != nil {
		var se *pulseRemoteStatusError
		if errors.As(err, &se) && se.Status >= 400 && se.Status < 500 {
			// The host refused this project (not registered there, unreadable).
			writeJSON(w, se.Status, umlResponse{State: "error", Error: se.Msg})
			return
		}
		// No live connection, a timeout, or the host's own failure: report it,
		// never fall back to a local index.
		writeJSON(w, http.StatusBadGateway, umlResponse{State: "error", Error: err.Error()})
		return
	}
	// The host's own 202 (still indexing) decodes like a 200 and carries
	// state "indexing"; passing it through keeps the client's poll loop
	// identical for local and remote projects.
	if status {
		writeJSON(w, http.StatusOK, umlStatusResponse{State: out.State, Error: out.Error})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
