package server

import (
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/config"
)

// Model favorites HTTP API — the web/desktop counterpart of the TUI model
// picker's ctrl+f favorite toggle (internal/tui/picker.go). Favorites live in
// the shared opencode model-state file managed by internal/config/state.go;
// handlers must go through those helpers (they own the file locking) and never
// touch the file directly.
//
// The model id travels in the JSON request body rather than a path parameter
// because "provider/model" ids contain "/" and can't be safely routed as a
// single path segment.
//
// Routes (registered in registerRoutes, authMiddleware-wrapped):
//
//	PUT    /api/models/favorite   {"model": "provider/model"}  → add favorite
//	DELETE /api/models/favorite   {"model": "provider/model"}  → remove favorite
//
// Both are idempotent (adding an existing favorite / removing an absent one is
// a no-op on disk) and respond with the full resulting favorites list so the
// client can resynchronize its star states without a follow-up GET.

// favoriteModelRequest is the body shared by the add/remove handlers.
type favoriteModelRequest struct {
	Model string `json:"model"`
}

// writeFavoritesResponse emits the canonical favorites response shape.
func writeFavoritesResponse(w http.ResponseWriter, id string, favorited bool) {
	writeJSON(w, http.StatusOK, map[string]any{
		"model":     id,
		"favorite":  favorited,
		"favorites": config.LoadFavorites(),
	})
}

// validateFavoriteModelID reports whether id is a usable "provider/model"
// identifier. It mirrors config.SaveFavoriteModel's own guard so the handler
// can answer 400 for bad input instead of surfacing a 500.
func validateFavoriteModelID(id string) bool {
	if id == "" || !strings.Contains(id, "/") {
		return false
	}
	prefix, rest, _ := strings.Cut(id, "/")
	return prefix != "" && rest != ""
}

// HandleAddFavoriteModel adds "provider/model" to the favorites list.
func (h *Handler) HandleAddFavoriteModel(w http.ResponseWriter, r *http.Request) {
	var req favoriteModelRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := strings.TrimSpace(req.Model)
	if !validateFavoriteModelID(id) {
		writeError(w, http.StatusBadRequest, `model must be a non-empty "provider/model" id`)
		return
	}
	if err := config.SaveFavoriteModel(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save favorite: "+err.Error())
		return
	}
	writeFavoritesResponse(w, id, true)
}

// HandleRemoveFavoriteModel removes "provider/model" from the favorites list.
func (h *Handler) HandleRemoveFavoriteModel(w http.ResponseWriter, r *http.Request) {
	var req favoriteModelRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := strings.TrimSpace(req.Model)
	if !validateFavoriteModelID(id) {
		writeError(w, http.StatusBadRequest, `model must be a non-empty "provider/model" id`)
		return
	}
	if err := config.RemoveFavoriteModel(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove favorite: "+err.Error())
		return
	}
	writeFavoritesResponse(w, id, false)
}
