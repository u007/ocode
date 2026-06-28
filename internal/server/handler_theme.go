package server

import (
	"net/http"

	"github.com/u007/ocode/internal/theme"
)

// ThemeColorsResponse is the response for GET /api/theme.
type ThemeColorsResponse struct {
	Name   string            `json:"name"`
	Label  string            `json:"label,omitempty"`
	Colors theme.ThemeColors `json:"colors"`
}

// HandleGetTheme returns the configured theme's colors.
func (h *Handler) HandleGetTheme(w http.ResponseWriter, r *http.Request) {
	name := "tokyonight" // default
	if h.cfg != nil && h.cfg.Ocode.TUI.Theme != "" {
		name = h.cfg.Ocode.TUI.Theme
	}
	t := theme.Get(name)
	writeJSON(w, http.StatusOK, ThemeColorsResponse{
		Name:   name,
		Label:  theme.DisplayName(name),
		Colors: t.Colors,
	})
}
