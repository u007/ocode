package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/session"
)

// HandleListProjects returns all saved project roots.
func (h *Handler) HandleListProjects(w http.ResponseWriter, _ *http.Request) {
	if h.projects == nil {
		writeJSON(w, http.StatusOK, []projects.Project{})
		return
	}
	list := h.projects.List()
	writeJSON(w, http.StatusOK, list)
}

// HandleAddProject adds a new project root to the saved list.
func (h *Handler) HandleAddProject(w http.ResponseWriter, r *http.Request) {
	if h.projects == nil {
		writeError(w, http.StatusInternalServerError, "project store not available")
		return
	}

	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	if err := h.projects.Add(body.Path); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("add project: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleRemoveProject removes a project root from the saved list.
func (h *Handler) HandleRemoveProject(w http.ResponseWriter, r *http.Request) {
	if h.projects == nil {
		writeError(w, http.StatusInternalServerError, "project store not available")
		return
	}

	// The path is URL-encoded in the path value.
	path := r.PathValue("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	if err := h.projects.Remove(path); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("remove project: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleListProjectSessions returns sessions scoped to a specific project root.
// The project root is passed as a query parameter `path` (URL-encoded).
func (h *Handler) HandleListProjectSessions(w http.ResponseWriter, r *http.Request) {
	projectPath := r.URL.Query().Get("path")
	if projectPath == "" {
		writeError(w, http.StatusBadRequest, "path query parameter is required")
		return
	}

	// Verify this is a saved project.
	if h.projects != nil {
		found := false
		for _, p := range h.projects.List() {
			if p.Path == projectPath {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "project not found in saved list")
			return
		}
	}

	sessions, err := session.ListForDir(projectPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list sessions: %v", err))
		return
	}

	result := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, SessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
			UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, result)
}
