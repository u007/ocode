package server

import (
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/config"
)

// Model recents HTTP API — the web/desktop counterpart of removing a row from
// the Recently Used section of the model picker. Recents live in the shared
// opencode model-state file managed by internal/config/state.go; handlers must
// go through those helpers (they own the file locking) and never touch the
// file directly.
//
// The model id travels in the JSON request body rather than a path parameter
// because "provider/model" ids contain "/" and can't be safely routed as a
// single path segment.
//
// Routes (registered in registerRoutes, authMiddleware-wrapped):
//
//	DELETE /api/models/recent   {"model": "provider/model"}  → remove recent
//
// Idempotent (removing an absent recent is a no-op on disk) and responds with
// the full resulting recents list so the client can resynchronize the
// Recently Used section without a model refetch.

type recentModelRequest struct {
	Model string `json:"model"`
}

// writeRecentsResponse emits the canonical recents response shape.
func writeRecentsResponse(w http.ResponseWriter, id string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"model":   id,
		"recent":  false,
		"recents": config.LoadRecentModels(),
	})
}

// validateRecentModelID reports whether id is a usable "provider/model"
// identifier. It mirrors config.RemoveRecentModel's own guard so the handler
// can answer 400 for bad input instead of surfacing a 500.
func validateRecentModelID(id string) bool {
	if id == "" || !strings.Contains(id, "/") {
		return false
	}
	prefix, rest, _ := strings.Cut(id, "/")
	return prefix != "" && rest != ""
}

// HandleRemoveRecentModel removes "provider/model" from the recent list.
func (h *Handler) HandleRemoveRecentModel(w http.ResponseWriter, r *http.Request) {
	var req recentModelRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := strings.TrimSpace(req.Model)
	if !validateRecentModelID(id) {
		writeError(w, http.StatusBadRequest, `model must be a non-empty "provider/model" id`)
		return
	}
	if err := config.RemoveRecentModel(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove recent: "+err.Error())
		return
	}
	writeRecentsResponse(w, id)
}
