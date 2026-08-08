package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/u007/ocode/internal/tabs"
)

// HandleGetTabs returns the open-session tab state for a project root,
// passed as the query parameter `path` (URL-encoded). Missing state yields an
// empty object, not an error.
func (h *Handler) HandleGetTabs(w http.ResponseWriter, r *http.Request) {
	if h.tabsStore == nil {
		writeJSON(w, http.StatusOK, tabs.ProjectTabs{})
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path query parameter is required")
		return
	}
	writeJSON(w, http.StatusOK, h.tabsStore.Get(path))
}

// HandleSetTabs stores the open-session tab state for a project root. The
// body is `{path, tabs:[{id,title}], active}` — a full replacement of that
// project's state, matching the debounced client-side writes.
func (h *Handler) HandleSetTabs(w http.ResponseWriter, r *http.Request) {
	if h.tabsStore == nil {
		writeError(w, http.StatusInternalServerError, "tab store not available")
		return
	}
	var body struct {
		Path   string     `json:"path"`
		Tabs   []tabs.Tab `json:"tabs"`
		Active string     `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	// Reject tabs that carry no id (defensive; the client never sends them).
	clean := body.Tabs[:0]
	for _, t := range body.Tabs {
		if t.ID != "" {
			clean = append(clean, t)
		}
	}
	if err := h.tabsStore.Set(body.Path, tabs.ProjectTabs{Tabs: clean, Active: body.Active}); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("set tabs: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
