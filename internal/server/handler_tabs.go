package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/u007/ocode/internal/tabs"
)

// HandleGetTabs returns the open-session tab state. With no query it returns
// every project's state as `{projects: {root: {tabs, active}}}` — the shape
// the web project store restores on boot. With `path` (URL-encoded) it
// returns that one project's state; missing state yields an empty object,
// not an error.
func (h *Handler) HandleGetTabs(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if h.tabsStore == nil {
		if path == "" {
			writeJSON(w, http.StatusOK, tabsAllResponse{Projects: map[string]tabs.ProjectTabs{}})
			return
		}
		writeJSON(w, http.StatusOK, tabs.ProjectTabs{})
		return
	}
	if path == "" {
		writeJSON(w, http.StatusOK, tabsAllResponse{Projects: h.tabsStore.All()})
		return
	}
	writeJSON(w, http.StatusOK, h.tabsStore.Get(path))
}

type tabsAllResponse struct {
	Projects map[string]tabs.ProjectTabs `json:"projects"`
}

// HandleSetTabs stores the open-session tab state. Two body shapes:
//
//   - `{projects: {root: {tabs:[{id,title,sub_tab}], active}}}` — a MERGE of
//     every project the caller knows about. Each provided project replaces
//     that project's entry; a provided project with an empty `tabs` list
//     deletes it (how a window persists "I closed this project's last tab").
//     Projects ABSENT from the body are preserved untouched.
//   - `{path, tabs:[{id,title}], active}` — a merge of one project's state;
//     an empty tab list clears it.
//
// The merge (rather than a whole-map replacement) is what keeps the desktop
// app, a shared browser URL, and any concurrently running server process from
// dropping each other's projects: a client only ever sends the projects it
// knows about, and several servers share one tabs.json (see internal/tabs).
//
// Every successful write publishes an unscoped `tabs_changed` bus event so
// other windows on THIS server (another browser, a shared Tailscale URL, the
// desktop shell) refetch and converge. The event bus is per-process; other
// processes converge by reloading the shared file on their next read.
func (h *Handler) HandleSetTabs(w http.ResponseWriter, r *http.Request) {
	if h.tabsStore == nil {
		writeError(w, http.StatusInternalServerError, "tab store not available")
		return
	}
	var body struct {
		Projects map[string]tabs.ProjectTabs `json:"projects"`
		Path     string                      `json:"path"`
		Tabs     []tabs.Tab                  `json:"tabs"`
		Active   string                      `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Projects != nil {
		patch := make(map[string]tabs.ProjectTabs, len(body.Projects))
		for root, pt := range body.Projects {
			if root == "" {
				continue
			}
			patch[root] = tabs.ProjectTabs{Tabs: dropIDLessTabs(pt.Tabs), Active: pt.Active}
		}
		if err := h.tabsStore.ApplyBulk(patch); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("set tabs: %v", err))
			return
		}
		h.bus.Publish("tabs_changed", "", "", nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path or projects is required")
		return
	}
	if err := h.tabsStore.Set(body.Path, tabs.ProjectTabs{Tabs: dropIDLessTabs(body.Tabs), Active: body.Active}); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("set tabs: %v", err))
		return
	}
	h.bus.Publish("tabs_changed", "", "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// dropIDLessTabs rejects tabs that carry no id (defensive; the client never
// sends them).
func dropIDLessTabs(in []tabs.Tab) []tabs.Tab {
	clean := make([]tabs.Tab, 0, len(in))
	for _, t := range in {
		if t.ID != "" {
			clean = append(clean, t)
		}
	}
	return clean
}
