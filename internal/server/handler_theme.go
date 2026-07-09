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

// ThemesListResponse is the response for GET /api/themes.
type ThemesListResponse struct {
	Current string          `json:"current"`
	Themes  []ThemeListItem `json:"themes"`
}

// ThemeListItem is one selectable theme.
type ThemeListItem struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// currentThemeName resolves the configured theme name, defaulting to
// tokyonight when none is set.
func currentThemeName(h *Handler) string {
	name := "tokyonight"
	if h.cfg != nil && h.cfg.Ocode.TUI.Theme != "" {
		name = h.cfg.Ocode.TUI.Theme
	}
	return name
}

// HandleGetTheme returns the configured theme's colors. When ?name= is given
// it returns that theme's colors instead (used by the web theme picker).
func (h *Handler) HandleGetTheme(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = currentThemeName(h)
	}
	t := theme.Get(name)
	writeJSON(w, http.StatusOK, ThemeColorsResponse{
		Name:   name,
		Label:  theme.DisplayName(name),
		Colors: t.Colors,
	})
}

// HandleListThemes returns every available theme plus the currently selected
// one, so the web UI can render a theme picker.
func (h *Handler) HandleListThemes(w http.ResponseWriter, r *http.Request) {
	current := currentThemeName(h)
	names := theme.AvailableThemes()
	themes := make([]ThemeListItem, 0, len(names))
	for _, n := range names {
		themes = append(themes, ThemeListItem{Name: n, Label: theme.DisplayName(n)})
	}
	writeJSON(w, http.StatusOK, ThemesListResponse{Current: current, Themes: themes})
}
