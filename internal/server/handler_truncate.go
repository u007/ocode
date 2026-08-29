package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/u007/ocode/internal/session"
)

// TruncateRequest is the body for POST /api/sessions/{id}/truncate.
type TruncateRequest struct {
	KeepUntil int `json:"keepUntil"`
}

// HandleTruncateSession truncates the persisted session transcript to its first
// keepUntil messages (messages[:keepUntil]), matching TUI's
// selectPickerIndex behavior (m.messages = m.messages[:idx]).
func (h *Handler) HandleTruncateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req TruncateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.KeepUntil < 0 {
		writeError(w, http.StatusBadRequest, "keepUntil must be >= 0")
		return
	}

	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if h.sessions.IsTurnActive(id) {
		writeError(w, http.StatusConflict, "cannot truncate while turn is active")
		return
	}

	s, err := session.LoadForDir(entry.ProjectRoot, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if req.KeepUntil > len(s.Messages) {
		writeError(w, http.StatusBadRequest, "keepUntil out of range")
		return
	}

	s.Messages = s.Messages[:req.KeepUntil]
	s.UpdatedAt = time.Now()
	// Persist via session.SaveForDir which handles sqlite/json/ojsonl dispatch and metadata.
	if err := session.SaveForDir(entry.ProjectRoot, s.ID, s.Title, s.Messages, s.Metadata); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}

	writeJSON(w, http.StatusOK, SessionDetail{
		SessionInfo: SessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
			UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
		},
		Messages: s.Messages,
		Total:    len(s.Messages),
	})
}
