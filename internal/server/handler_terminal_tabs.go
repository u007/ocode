package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/u007/ocode/internal/termtabs"
)

// HandleGetTerminalTabs returns the shared open-terminal-tab state. With no
// query it returns every project's state as
// `{projects: {"<host::path>": {terminals: [...]}}}`, the shape the web
// terminal store hydrates from. With `key` (URL-encoded) it returns that one
// project's state; a missing key yields an empty object, not an error.
//
// This endpoint is what makes a terminal started in one client visible in
// another. The shell itself was always shared (a remote project's pty is a
// child of the host's `serve --remote`); before this, only the tab LIST was
// per-origin localStorage, so a second browser saw an empty strip even though
// the shell was sitting right there.
func (h *Handler) HandleGetTerminalTabs(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if h.termTabsStore == nil {
		if key == "" {
			writeJSON(w, http.StatusOK, terminalTabsAllResponse{Projects: map[string]termtabs.ProjectTerminals{}})
			return
		}
		writeJSON(w, http.StatusOK, termtabs.ProjectTerminals{})
		return
	}
	if key == "" {
		writeJSON(w, http.StatusOK, terminalTabsAllResponse{Projects: h.termTabsStore.All()})
		return
	}
	writeJSON(w, http.StatusOK, h.termTabsStore.Get(key))
}

type terminalTabsAllResponse struct {
	Projects map[string]termtabs.ProjectTerminals `json:"projects"`
}

// HandleSetTerminalTabs stores the shared open-terminal-tab state. Two body
// shapes, mirroring HandleSetTabs:
//
//   - `{projects: {"<host::path>": {terminals: [{id,title,renamed,osc_title}]}}}`
//     — a MERGE of every project the caller knows about. Each provided key
//     replaces that project's entry; a provided key with an empty `terminals`
//     list deletes it (how a client persists "I closed this project's last
//     tab"). Keys ABSENT from the body are preserved untouched — without that
//     merge, a browser that has never seen the desktop app's projects would
//     wipe them on its next save.
//   - `{key, terminals: [...]}` — a merge of one project's state; an empty
//     list clears it.
//
// `activeId` is intentionally NOT part of the payload: which tab a window has
// focused is per-client view state (it can be PROCESSES_TAB_ID, which is not a
// terminal), and sharing it would let one window yank another's selection.
//
// Every successful write publishes an unscoped `terminal_tabs_changed` bus
// event so other clients on THIS server refetch and converge. Other processes
// converge by reloading the shared file on their next read.
func (h *Handler) HandleSetTerminalTabs(w http.ResponseWriter, r *http.Request) {
	if h.termTabsStore == nil {
		writeError(w, http.StatusInternalServerError, "terminal tab store not available")
		return
	}
	var body struct {
		Projects  map[string]termtabs.ProjectTerminals `json:"projects"`
		Key       string                               `json:"key"`
		Terminals []termtabs.Terminal                  `json:"terminals"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Projects != nil {
		patch := make(map[string]termtabs.ProjectTerminals, len(body.Projects))
		for key, pt := range body.Projects {
			if key == "" {
				continue
			}
			patch[key] = termtabs.ProjectTerminals{Terminals: dropIDLessTerminals(pt.Terminals)}
		}
		if err := h.termTabsStore.ApplyBulk(patch); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("set terminal tabs: %v", err))
			return
		}
		h.bus.Publish("terminal_tabs_changed", "", "", nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if body.Key == "" {
		writeError(w, http.StatusBadRequest, "key or projects is required")
		return
	}
	pt := termtabs.ProjectTerminals{Terminals: dropIDLessTerminals(body.Terminals)}
	if err := h.termTabsStore.Set(body.Key, pt); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("set terminal tabs: %v", err))
		return
	}
	h.bus.Publish("terminal_tabs_changed", "", "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// dropIDLessTerminals rejects terminals that carry no id. An id-less entry is
// unattachable — the client would render a tab that can never open a shell —
// so it is dropped rather than persisted.
func dropIDLessTerminals(in []termtabs.Terminal) []termtabs.Terminal {
	clean := make([]termtabs.Terminal, 0, len(in))
	for _, t := range in {
		if t.ID != "" {
			clean = append(clean, t)
		}
	}
	return clean
}
