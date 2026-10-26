package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/browsertabs"
)

// HandleGetBrowserTabs returns the shared embedded-browser state for every
// project as `{projects: {"<host::path>": {tabs, active_id, surfaces}}}`. The
// web browser store hydrates from it, and a client re-reads it on every
// `browser_tabs_changed` event.
func (h *Handler) HandleGetBrowserTabs(w http.ResponseWriter, r *http.Request) {
	if h.browserTabsStore == nil {
		writeJSON(w, http.StatusOK, browserTabsAllResponse{Projects: map[string]browsertabs.ProjectBrowsers{}})
		return
	}
	writeJSON(w, http.StatusOK, browserTabsAllResponse{Projects: h.browserTabsStore.All()})
}

type browserTabsAllResponse struct {
	Projects map[string]browsertabs.ProjectBrowsers `json:"projects"`
}

type browserTabsConflictResponse struct {
	Error    string                                 `json:"error"`
	Projects map[string]browsertabs.ProjectBrowsers `json:"projects"`
}

// HandleSetBrowserTabs stores shared browser state. The body is
// `{projects: {"<host::path>": {tabs, active_id, surfaces, revision}}}`, a MERGE
// of the projects the caller knows about: each provided key replaces that
// project's state, a key with an empty `tabs` list deletes it, and keys absent
// from the body are preserved.
//
// Each entry must echo the `revision` the caller read (0 for a project not yet
// stored). If any entry is stale the whole request is refused with 409 and
// nothing is written. The 409 body is `{error, projects}`, where `projects` is
// the current state in the same shape as GET, so the caller can re-merge and
// retry without a second GET.
//
// Every entry is validated before anything is written. An invalid entry fails
// the whole request with 400, because a silently repaired value would differ
// from what the sending client holds and the two would then keep overwriting
// each other.
//
// Every successful write publishes an unscoped `browser_tabs_changed` bus
// event so other clients on this server refetch and converge.
func (h *Handler) HandleSetBrowserTabs(w http.ResponseWriter, r *http.Request) {
	if h.browserTabsStore == nil {
		writeError(w, http.StatusInternalServerError, "browser tab store not available")
		return
	}
	var body struct {
		Projects map[string]browsertabs.ProjectBrowsers `json:"projects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Projects == nil {
		writeError(w, http.StatusBadRequest, "projects is required")
		return
	}
	for key, pb := range body.Projects {
		if strings.TrimSpace(key) == "" {
			writeError(w, http.StatusBadRequest, "project key must not be empty")
			return
		}
		if err := pb.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("browser tabs for %q: %v", key, err))
			return
		}
	}
	if err := h.browserTabsStore.ApplyBulk(body.Projects); err != nil {
		var conflict *browsertabs.RevisionConflictError
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, browserTabsConflictResponse{
				Error:    conflict.Error(),
				Projects: h.browserTabsStore.All(),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("set browser tabs: %v", err))
		return
	}
	h.bus.Publish("browser_tabs_changed", "", "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
