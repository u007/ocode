package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/u007/ocode/internal/monaco"
)

// HandleGetMonacoSettings returns the current editor settings.
func (h *Handler) HandleGetMonacoSettings(w http.ResponseWriter, _ *http.Request) {
	if h.monaco == nil {
		writeJSON(w, http.StatusOK, monaco.DefaultSettings())
		return
	}
	writeJSON(w, http.StatusOK, h.monaco.LoadSettings())
}

// HandleSetMonacoSettings persists editor settings.
func (h *Handler) HandleSetMonacoSettings(w http.ResponseWriter, r *http.Request) {
	if h.monaco == nil {
		writeError(w, http.StatusInternalServerError, "monaco store not available")
		return
	}

	var s monaco.Settings
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if err := h.monaco.SaveSettings(s); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save settings: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleListMonacoExtensions returns all known extensions and their state.
func (h *Handler) HandleListMonacoExtensions(w http.ResponseWriter, _ *http.Request) {
	if h.monaco == nil {
		writeJSON(w, http.StatusOK, monaco.BuiltinExtensions())
		return
	}
	writeJSON(w, http.StatusOK, h.monaco.LoadExtensions())
}

// HandleToggleMonacoExtension flips the enabled state for a single extension.
func (h *Handler) HandleToggleMonacoExtension(w http.ResponseWriter, r *http.Request) {
	if h.monaco == nil {
		writeError(w, http.StatusInternalServerError, "monaco store not available")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "extension name is required")
		return
	}

	if err := h.monaco.ToggleExtension(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Return updated list
	writeJSON(w, http.StatusOK, h.monaco.LoadExtensions())
}
