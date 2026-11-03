package server

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/appearance"
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

// isVSCodeThemeID reports whether name addresses a theme discovered in a VS Code
// extension rather than the builtin theme registry. No builtin name contains the
// "vscode:" prefix (spec §5), so the prefix is a safe discriminator.
func isVSCodeThemeID(name string) bool {
	return strings.HasPrefix(name, "vscode:")
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

// handleVSCodeTheme resolves a `vscode:` theme id through the appearance
// service and returns the same 16-field palette /api/appearance/theme/{id}
// reports, so the two endpoints cannot disagree (spec §12).
func (h *Handler) handleVSCodeTheme(w http.ResponseWriter, r *http.Request, id string) {
	// Shape first: a malformed id is a client error (400), not a failed
	// resolution (500). The service enforces the same grammar, but its rejection
	// surfaces as a generic resolve failure, which is why the check lives here.
	if !appearance.ValidThemeID(id) {
		writeError(w, http.StatusBadRequest, "invalid theme id")
		return
	}
	svc := h.appearanceService()
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, "appearance service unavailable")
		return
	}
	converted, err := svc.Resolve(r.Context(), id)
	if err != nil {
		if errors.Is(err, appearance.ErrThemeNotFound) {
			// An explicit vscode: request that cannot be resolved is a 404, not a
			// silent tokyonight palette: substituting an unrelated palette for a
			// named theme is the silent colour fallback the spec forbids.
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "resolve theme: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ThemeColorsResponse{
		Name:   id,
		Label:  converted.Label,
		Colors: converted.Colors,
	})
}

// HandleGetTheme returns the configured theme's colors. When ?name= is given
// it returns that theme's colors instead (used by the web theme picker).
//
// A `vscode:` name is resolved through the appearance service so the picker can
// preview an extension theme before selecting it.
func (h *Handler) HandleGetTheme(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = currentThemeName(h)
	}
	if isVSCodeThemeID(name) {
		h.handleVSCodeTheme(w, r, name)
		return
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
//
// Discovered VS Code themes are appended after the builtin entries, sorted by
// label, so the existing Theme form keeps working during the transition. A
// discovery failure must never break the builtin list: it is logged and the
// endpoint answers with what the registry has.
func (h *Handler) HandleListThemes(w http.ResponseWriter, r *http.Request) {
	current := currentThemeName(h)
	names := theme.AvailableThemes()
	themes := make([]ThemeListItem, 0, len(names))
	for _, n := range names {
		themes = append(themes, ThemeListItem{Name: n, Label: theme.DisplayName(n)})
	}
	if svc := h.appearanceService(); svc != nil {
		refs, _, err := svc.Themes(r.Context())
		if err != nil {
			log.Printf("themes: discover VS Code themes: %v", err)
		}
		// Themes() sorts by label, and appending preserves that order after the
		// registry entries, which is the documented shape of this endpoint.
		for _, ref := range refs {
			themes = append(themes, ThemeListItem{Name: ref.ID, Label: ref.Label})
		}
	}
	writeJSON(w, http.StatusOK, ThemesListResponse{Current: current, Themes: themes})
}
