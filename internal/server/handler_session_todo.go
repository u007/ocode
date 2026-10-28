package server

import (
	"log"
	"net/http"
)

// HandleGetSessionTodo returns the session's plan in the todo_updated event
// shape, for the chat's plan panel initial load. A session without a plan is an
// empty plan (200), not an error. The project root comes from the session
// registry, never the server's default workDir.
func (h *Handler) HandleGetSessionTodo(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	ev, found, err := readTodoUpdatedEvent(entry.ProjectRoot, id)
	if err != nil {
		log.Printf("serve: read todo plan for %s in %s: %v", id, entry.ProjectRoot, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		ev = TodoUpdatedEvent{SessionID: id, Items: []TodoUpdatedItem{}}
	}
	writeJSON(w, http.StatusOK, ev)
}
