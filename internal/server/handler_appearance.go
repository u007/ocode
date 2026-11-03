package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/appearance"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/theme"
)

// appearanceThemesDefaultLimit is the page size for GET /api/appearance/themes.
// Spec §15.1 requires the theme list to be paginated so a machine with a large
// extension set cannot produce an unbounded payload. The cap is the same kind of
// bound as the default: a client asking for everything gets one page at a time.
const (
	appearanceThemesDefaultLimit = 200
	appearanceThemesMaxLimit     = 1000
)

// AppearanceThemeItem is one entry in the unified theme list. Source is "vscode"
// for a discovered extension theme; builtin themes are served by /api/themes
// with their registry names and appear here only when a caller merges both.
type AppearanceThemeItem struct {
	ID           string              `json:"id"`
	Label        string              `json:"label"`
	Source       string              `json:"source"`
	Kind         appearance.Polarity `json:"kind"`
	Extension    string              `json:"extension,omitempty"`
	HasIconTheme bool                `json:"has_icon_theme"`
}

// AppearanceThemesResponse is the response for GET /api/appearance/themes.
// Total is the size of the full list, so a client can page it.
type AppearanceThemesResponse struct {
	Themes   []AppearanceThemeItem `json:"themes"`
	Total    int                   `json:"total"`
	Offset   int                   `json:"offset"`
	Limit    int                   `json:"limit"`
	Warnings []string              `json:"warnings,omitempty"`
}

// AppearanceSettingsResponse is the response for GET/PUT
// /api/appearance/settings.
//
// ColorTheme and IconTheme echo what is stored, which may be an id that no
// longer resolves (an extension the user removed). EffectiveColorTheme is what a
// renderer should actually apply: the stored id when it resolves, otherwise
// tui.theme, otherwise tokyonight — a builtin theme NAME, not a vscode: id, so a
// consumer must route it by its prefix. Warnings explain a fallback; spec §13
// requires the failure to degrade rather than to block startup.
type AppearanceSettingsResponse struct {
	ColorTheme          string   `json:"color_theme"`
	IconTheme           string   `json:"icon_theme"`
	ExtraExtensionDirs  []string `json:"extra_extension_dirs"`
	EffectiveColorTheme string   `json:"effective_color_theme"`
	Warnings            []string `json:"warnings,omitempty"`
}

// appearanceSettingsRequest is a settings write. It intentionally has no
// effective/warnings fields: they are derived, not stored.
type appearanceSettingsRequest struct {
	ColorTheme         string   `json:"color_theme"`
	IconTheme          string   `json:"icon_theme"`
	ExtraExtensionDirs []string `json:"extra_extension_dirs"`
}

// appearanceService returns the configured discovery service, or nil when none
// was constructed. Callers must handle nil: the endpoints answer 503 instead of
// pretending there are no themes.
func (h *Handler) appearanceService() *appearance.Service {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.appearance
}

// appearanceConfig returns the stored appearance block. It is read under h.mu
// like every other config read in a handler.
func (h *Handler) appearanceConfig() config.AppearanceConfig {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return config.AppearanceConfig{}
	}
	return h.cfg.Ocode.Appearance
}

// HandleAppearanceThemes lists discovered VS Code colour themes.
//
// Discovery is local-only on purpose (spec §12): the list is a property of this
// machine's installed editors, so this handler takes no `?host=` and is never
// registered behind the remote proxy. Discovery runs outside h.mu.
func (h *Handler) HandleAppearanceThemes(w http.ResponseWriter, r *http.Request) {
	svc := h.appearanceService()
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, "appearance service unavailable")
		return
	}
	refs, warnings, err := svc.Themes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "discover themes: "+err.Error())
		return
	}

	limit, offset := appearancePage(r)
	total := len(refs)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := make([]AppearanceThemeItem, 0, end-offset)
	for _, ref := range refs[offset:end] {
		page = append(page, AppearanceThemeItem{
			ID:           ref.ID,
			Label:        ref.Label,
			Source:       ref.Source,
			Kind:         ref.Kind,
			Extension:    ref.Extension,
			HasIconTheme: ref.HasIconTheme,
		})
	}
	writeJSON(w, http.StatusOK, AppearanceThemesResponse{
		Themes:   page,
		Total:    total,
		Offset:   offset,
		Limit:    limit,
		Warnings: warnings,
	})
}

// appearancePage parses limit/offset, applying the default page size and the
// hard cap. A malformed or non-positive value falls back to the default rather
// than failing the request: a bad query string must not hide the theme list.
func appearancePage(r *http.Request) (limit, offset int) {
	query := r.URL.Query()
	limit = appearanceThemesDefaultLimit
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > appearanceThemesMaxLimit {
		limit = appearanceThemesMaxLimit
	}
	if raw := strings.TrimSpace(query.Get("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			offset = parsed
		}
	}
	return limit, offset
}

// HandleAppearanceTheme resolves one `vscode:` theme id to its converted
// palettes. A syntactically invalid id is a 400; a well-formed id that no
// discovered extension contributes is a 404.
func (h *Handler) HandleAppearanceTheme(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "resolve theme: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

// HandleGetAppearanceSettings reports the stored appearance selection plus the
// theme a renderer should actually apply.
//
// Resolving the configured colour theme warms the conversion cache and, more
// importantly, is the only honest way to answer "is this still installed?" — an
// extension can disappear between sessions, and a settings page that keeps
// claiming it is active is a silent colour fallback the spec forbids.
func (h *Handler) HandleGetAppearanceSettings(w http.ResponseWriter, r *http.Request) {
	cfg := h.appearanceConfig()
	svc := h.appearanceService()
	writeJSON(w, http.StatusOK, h.appearanceSettingsResponse(r, cfg, svc))
}

// appearanceSettingsResponse derives the response from the stored block. The
// effective theme falls back to the TUI theme, then to tokyonight, recording a
// warning the first time a configured vscode: theme does not resolve.
func (h *Handler) appearanceSettingsResponse(r *http.Request, cfg config.AppearanceConfig, svc *appearance.Service) AppearanceSettingsResponse {
	resp := AppearanceSettingsResponse{
		ColorTheme:         cfg.ColorTheme,
		IconTheme:          cfg.IconTheme,
		ExtraExtensionDirs: cfg.ExtraExtensionDirs,
	}

	effective := cfg.ColorTheme
	if effective != "" {
		if strings.HasPrefix(effective, "vscode:") {
			if svc == nil {
				resp.Warnings = append(resp.Warnings, "appearance service unavailable; using the built-in theme")
				effective = ""
			} else if _, err := svc.Resolve(r.Context(), effective); err != nil {
				resp.Warnings = append(resp.Warnings,
					"configured colour theme "+cfg.ColorTheme+" is unavailable; using the fallback: "+err.Error())
				effective = ""
			}
		} else if _, ok := theme.GetTheme(effective); !ok {
			// A built-in name is stored verbatim by the TUI's own settings path,
			// so an unknown one is possible. Reporting it as effective would be a
			// lie about what the renderer shows.
			resp.Warnings = append(resp.Warnings,
				"configured colour theme "+cfg.ColorTheme+" is not a known built-in theme; using the fallback")
			effective = ""
		}
	}
	if effective == "" {
		effective = effectiveFallbackTheme(h)
	}
	resp.EffectiveColorTheme = effective
	return resp
}

// effectiveFallbackTheme is the theme a renderer applies when no configured
// colour theme resolves: the TUI theme when it is a registered built-in, then
// tokyonight (spec §13). Unlike currentThemeName this never returns an
// unregistered name, because this value is what the SPA will actually render.
func effectiveFallbackTheme(h *Handler) string {
	name := currentThemeName(h)
	if _, ok := theme.GetTheme(name); ok {
		return name
	}
	return "tokyonight"
}

// HandleSetAppearanceSettings validates and persists the appearance selection.
//
// A `vscode:` colour theme must resolve before anything is written, so a typo or
// an id from an uninstalled extension can never become the stored value (spec
// §13). An empty colour theme is a legitimate "use the built-in theme" choice.
// Icon theme existence is not checked yet: Task 4 owns the icon theme model, so
// only the id shape is enforced. Extra roots are normalized (trimmed, deduped)
// and need not exist — a root that is absent is simply skipped by discovery.
func (h *Handler) HandleSetAppearanceSettings(w http.ResponseWriter, r *http.Request) {
	var req appearanceSettingsRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	next := config.NormalizeAppearanceConfig(config.AppearanceConfig{
		ColorTheme:         req.ColorTheme,
		IconTheme:          req.IconTheme,
		ExtraExtensionDirs: req.ExtraExtensionDirs,
	})

	svc := h.appearanceService()
	if next.ColorTheme != "" && strings.HasPrefix(next.ColorTheme, "vscode:") {
		if !appearance.ValidThemeID(next.ColorTheme) {
			writeError(w, http.StatusBadRequest, "invalid color_theme id")
			return
		}
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "appearance service unavailable")
			return
		}
		if _, err := svc.Resolve(r.Context(), next.ColorTheme); err != nil {
			writeError(w, http.StatusBadRequest, "unknown color_theme: "+err.Error())
			return
		}
	}
	if next.IconTheme != "" && strings.HasPrefix(next.IconTheme, "vscode:") && !appearance.ValidThemeID(next.IconTheme) {
		writeError(w, http.StatusBadRequest, "invalid icon_theme id")
		return
	}

	if err := config.SaveOcodeAppearanceConfig(next); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save appearance settings: "+err.Error())
		return
	}
	// Re-read from disk so the response is the stored block, not this function's
	// idea of it: a concurrent writer (or a normalization difference) must be
	// visible to the client that is about to render the result.
	stored := next
	if loaded, err := config.LoadOcodeConfigCopy(); err != nil {
		// The write already succeeded; failing the response now would tell the
		// client nothing was saved when it was. Report the stored-equals-request
		// value and log the read-back failure.
		log.Printf("appearance: read back settings after save: %v", err)
	} else {
		stored = loaded.Appearance
	}

	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Appearance = stored
	}
	h.mu.Unlock()

	if svc != nil {
		// A new extra root must be scannable immediately, not after the memo
		// expires, and the selection may point into that new root.
		svc.SetExtraRoots(stored.ExtraExtensionDirs)
	}
	h.bus.Publish("appearance_changed", "", "", map[string]any{
		"settings": h.appearanceSettingsResponse(r, stored, svc),
	})
	writeJSON(w, http.StatusOK, h.appearanceSettingsResponse(r, stored, svc))
}
