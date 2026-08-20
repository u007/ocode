package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

func (h *Handler) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	names := config.ListProfiles()
	out := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		delta, _ := config.GetProfile(name)
		count := config.ProfileOverrideCount(delta)
		credCount := auth.ProfileCredentialCount(name)
		display := ""
		if delta.DisplayName != nil {
			display = *delta.DisplayName
		}
		out = append(out, map[string]interface{}{
			"name":           name,
			"displayName":    display,
			"overrideCount":  count,
			"credentialCount": credCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"profiles": out,
	})
}

func (h *Handler) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string  `json:"name"`
		DisplayName *string `json:"displayName"`
		CopyFrom    *string `json:"copyFrom"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := config.ValidateProfileName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// check duplicate
	if _, ok := config.GetProfile(req.Name); ok {
		writeError(w, http.StatusConflict, "profile already exists")
		return
	}
	delta := config.ProfileDelta{}
	if req.DisplayName != nil && strings.TrimSpace(*req.DisplayName) != "" {
		s := strings.TrimSpace(*req.DisplayName)
		delta.DisplayName = &s
	}
	if req.CopyFrom != nil && *req.CopyFrom != "" {
		src := strings.TrimSpace(*req.CopyFrom)
		if src != "" {
			if srcDelta, ok := config.GetProfile(src); ok {
				delta = srcDelta
				if req.DisplayName != nil {
					delta.DisplayName = req.DisplayName
				}
			} else if src == "base" {
				// copy nothing, already empty
			} else {
				writeError(w, http.StatusBadRequest, "copyFrom not found")
				return
			}
		}
	}
	if err := config.SaveProfile(req.Name, delta); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// emit event for UI refresh
	if h.bus != nil {
		h.bus.Publish("profile.changed", "", "", map[string]string{"action": "created", "name": req.Name})
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name})
}

func (h *Handler) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	// block if any window is on this profile (server-enforced, uses in-memory cache)
	h.windowProfilesMu.RLock()
	for _, active := range h.windowProfiles {
		if active == name {
			h.windowProfilesMu.RUnlock()
			writeError(w, http.StatusConflict, "profile is active in a window; switch to Default first")
			return
		}
	}
	h.windowProfilesMu.RUnlock()
	if err := config.DeleteProfile(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = auth.DeleteProfileCredentials(name)
	_ = config.ClearWindowStateProfile(name)
	// also clear from cache (defensive, ClearWindowStateProfile already persists)
	h.windowProfilesMu.Lock()
	for win, active := range h.windowProfiles {
		if active == name {
			delete(h.windowProfiles, win)
		}
	}
	h.windowProfilesMu.Unlock()
	if h.bus != nil {
		h.bus.Publish("profile.changed", "", "", map[string]string{"action": "deleted", "name": name})
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

func (h *Handler) handleRenameProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req struct {
		NewName string `json:"newName"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.NewName = strings.TrimSpace(req.NewName)
	if err := config.ValidateProfileName(req.NewName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := config.GetProfile(req.NewName); ok {
		writeError(w, http.StatusConflict, "target profile already exists")
		return
	}
	if err := config.RenameProfile(name, req.NewName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = auth.RenameProfileCredentials(name, req.NewName)
	_ = config.RenameWindowStateProfile(name, req.NewName)
	h.windowProfilesMu.Lock()
	for win, active := range h.windowProfiles {
		if active == name {
			h.windowProfiles[win] = req.NewName
		}
	}
	h.windowProfilesMu.Unlock()
	if h.bus != nil {
		h.bus.Publish("profile.changed", "", "", map[string]string{"action": "renamed", "from": name, "to": req.NewName})
	}
	writeJSON(w, http.StatusOK, map[string]string{"from": name, "to": req.NewName})
}

func (h *Handler) handleGetWindowActiveProfile(w http.ResponseWriter, r *http.Request) {
	windowID := r.PathValue("id")
	if windowID == "" {
		writeError(w, http.StatusBadRequest, "window id required")
		return
	}
	p := h.getWindowProfile(windowID)
	eff := h.getEffectiveWindowProfile(windowID)
	writeJSON(w, http.StatusOK, map[string]string{
		"windowId":         windowID,
		"activeProfile":    p,
		"effectiveProfile": eff,
	})
}

func (h *Handler) getWindowProfile(windowID string) string {
	h.windowProfilesMu.RLock()
	defer h.windowProfilesMu.RUnlock()
	return h.windowProfiles[windowID]
}

func (h *Handler) getEffectiveWindowProfile(windowID string) string {
	if v := os.Getenv("OCODE_PROFILE"); v != "" {
		return v
	}
	return h.getWindowProfile(windowID)
}

// globalEffectiveProfile returns env override or most-recent window's profile
// (win-1 preferred) for server-wide agent construction until per-session
// window threading lands. Uses in-memory cache, no file I/O.
func (h *Handler) globalEffectiveProfile() string {
	if v := os.Getenv("OCODE_PROFILE"); v != "" {
		return v
	}
	h.windowProfilesMu.RLock()
	defer h.windowProfilesMu.RUnlock()
	if p, ok := h.windowProfiles["win-1"]; ok && p != "" {
		return p
	}
	for _, p := range h.windowProfiles {
		if p != "" {
			return p
		}
	}
	return ""
}

func (h *Handler) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	delta, ok := config.GetProfile(name)
	if !ok {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":            name,
		"delta":           delta,
		"overrideCount":   config.ProfileOverrideCount(delta),
		"credentialCount": auth.ProfileCredentialCount(name),
	})
}

func (h *Handler) handleGetProfileEffective(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := config.GetProfile(name); !ok {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	base, err := config.LoadOcodeConfigCopy()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	delta, _ := config.GetProfile(name)
	baseCopy := *base
	baseCopy.Profiles = nil
	eff := config.EffectiveOcodeConfig(base, name)
	if eff != nil {
		eff.Profiles = nil
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":      name,
		"base":      baseCopy,
		"delta":     delta,
		"effective": eff,
	})
}

func (h *Handler) handleGetProfileAuth(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := config.GetProfile(name); !ok {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	out := []map[string]interface{}{}
	for _, p := range auth.Providers {
		cred, ok := auth.GetProfileCredential(name, p.ID)
		if !ok {
			continue
		}
		masked := ""
		if cred.Key != "" {
			if len(cred.Key) > 8 {
				masked = cred.Key[:4] + "••••" + cred.Key[len(cred.Key)-4:]
			} else {
				masked = "••••"
			}
		} else if cred.AccessToken != "" {
			masked = "oauth ••••"
		}
		out = append(out, map[string]interface{}{
			"provider": p.ID,
			"label":    p.Label,
			"masked":   masked,
			"kind":     cred.Kind,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"credentials": out})
}

func (h *Handler) handleSetProfileAuth(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	provider := r.PathValue("provider")
	if err := config.ValidateProfileName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := config.GetProfile(name); !ok {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	if auth.FindProvider(provider) == nil {
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	var req struct {
		APIKey      string  `json:"apiKey"`
		Key         string  `json:"key"`
		BaseURL     *string `json:"baseUrl"`
		AccessToken string  `json:"accessToken"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	key := req.APIKey
	if key == "" {
		key = req.Key
	}
	if key == "" && req.AccessToken == "" {
		writeError(w, http.StatusBadRequest, "apiKey or key required")
		return
	}
	cred := auth.Credential{Kind: auth.KindAPIKey, Key: key}
	if req.AccessToken != "" {
		cred = auth.Credential{Kind: auth.KindOAuth, AccessToken: req.AccessToken}
	}
	if req.BaseURL != nil {
		cred.BaseURL = *req.BaseURL
	}
	if err := auth.SetProfileCredential(name, provider, cred); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.bus != nil {
		h.bus.Publish("profile.changed", "", "", map[string]string{"action": "auth_set", "name": name, "provider": provider})
	}
	writeJSON(w, http.StatusOK, map[string]string{"provider": provider, "status": "saved"})
}

func (h *Handler) handleDeleteProfileAuth(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	provider := r.PathValue("provider")
	if _, ok := config.GetProfile(name); !ok {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	if err := auth.RemoveProfileCredential(name, provider); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.bus != nil {
		h.bus.Publish("profile.changed", "", "", map[string]string{"action": "auth_deleted", "name": name, "provider": provider})
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": provider})
}

func (h *Handler) handleResetProfileField(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	field := r.PathValue("field")
	if err := config.ValidateProfileName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	delta, ok := config.GetProfile(name)
	if !ok {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	normalized := strings.TrimSpace(field)
	if normalized == "" {
		writeError(w, http.StatusBadRequest, "field required")
		return
	}
	changed := false
	switch {
	case normalized == "display_name":
		if delta.DisplayName != nil {
			delta.DisplayName = nil
			changed = true
		}
	case normalized == "model":
		if delta.Model != nil {
			delta.Model = nil
			changed = true
		}
	case normalized == "small_model":
		if delta.SmallModel != nil {
			delta.SmallModel = nil
			changed = true
		}
	case normalized == "small_model_enabled":
		if delta.SmallModelEnabled != nil {
			delta.SmallModelEnabled = nil
			changed = true
		}
	case normalized == "recap_model":
		if delta.RecapModel != nil {
			delta.RecapModel = nil
			changed = true
		}
	case normalized == "recap_model_enabled":
		if delta.RecapModelEnabled != nil {
			delta.RecapModelEnabled = nil
			changed = true
		}
	case normalized == "editor":
		if delta.Editor != nil {
			delta.Editor = nil
			changed = true
		}
	case normalized == "editor_mode":
		if delta.EditorMode != nil {
			delta.EditorMode = nil
			changed = true
		}
	case normalized == "ide_mode":
		if delta.IDEMode != nil {
			delta.IDEMode = nil
			changed = true
		}
	case normalized == "max_steps":
		if delta.MaxSteps != nil {
			delta.MaxSteps = nil
			changed = true
		}
	case normalized == "image_max_dim":
		if delta.MaxImageDim != nil {
			delta.MaxImageDim = nil
			changed = true
		}
	case normalized == "memory_enabled":
		if delta.MemoryEnabled != nil {
			delta.MemoryEnabled = nil
			changed = true
		}
	case normalized == "doc_prompt_enabled":
		if delta.DocPromptEnabled != nil {
			delta.DocPromptEnabled = nil
			changed = true
		}
	case normalized == "terminal_shell":
		if delta.TerminalShell != nil {
			delta.TerminalShell = nil
			changed = true
		}
	case normalized == "terminal_font_family":
		if delta.TerminalFontFamily != nil {
			delta.TerminalFontFamily = nil
			changed = true
		}
	case normalized == "terminal_font_size":
		if delta.TerminalFontSize != nil {
			delta.TerminalFontSize = nil
			changed = true
		}
	case normalized == "terminal_scrollback_lines":
		if delta.TerminalScrollbackLines != nil {
			delta.TerminalScrollbackLines = nil
			changed = true
		}
	case normalized == "tui":
		if delta.TUI != nil {
			delta.TUI = nil
			changed = true
		}
	case strings.HasPrefix(normalized, "provider."):
		id := strings.TrimPrefix(normalized, "provider.")
		if delta.Provider != nil {
			if _, exists := delta.Provider[id]; exists {
				delete(delta.Provider, id)
				if len(delta.Provider) == 0 {
					delta.Provider = nil
				}
				changed = true
			}
		}
	case strings.HasPrefix(normalized, "mcp."):
		id := strings.TrimPrefix(normalized, "mcp.")
		if delta.MCP != nil {
			if _, exists := delta.MCP[id]; exists {
				delete(delta.MCP, id)
				if len(delta.MCP) == 0 {
					delta.MCP = nil
				}
				changed = true
			}
		}
	case normalized == "provider":
		if len(delta.Provider) > 0 {
			delta.Provider = nil
			changed = true
		}
	case normalized == "mcp":
		if len(delta.MCP) > 0 {
			delta.MCP = nil
			changed = true
		}
	case normalized == "permission":
		if len(delta.Permission) > 0 {
			delta.Permission = nil
			changed = true
		}
	default:
		writeError(w, http.StatusBadRequest, "unknown field: "+normalized)
		return
	}
	if !changed {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already inherited", "field": normalized})
		return
	}
	if err := config.SaveProfile(name, delta); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.bus != nil {
		h.bus.Publish("profile.changed", "", "", map[string]string{"action": "field_reset", "name": name, "field": normalized})
	}
	writeJSON(w, http.StatusOK, map[string]string{"reset": normalized})
}

func (h *Handler) handleSetWindowActiveProfile(w http.ResponseWriter, r *http.Request) {
	windowID := r.PathValue("id")
	var req struct {
		Profile string `json:"profile"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	profile := strings.TrimSpace(req.Profile)
	// empty means Default
	if profile != "" {
		if err := config.ValidateProfileName(profile); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, ok := config.GetProfile(profile); !ok {
			writeError(w, http.StatusNotFound, "profile not found")
			return
		}
	}
	if err := config.SetActiveProfile(windowID, profile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.windowProfilesMu.Lock()
	if profile == "" {
		delete(h.windowProfiles, windowID)
	} else {
		h.windowProfiles[windowID] = profile
	}
	h.windowProfilesMu.Unlock()
	// hot-reload hint: reload config for that window's next turn uses new profile
	// emit via bus so webviews can re-render header/settings without reload
	if h.bus != nil {
		h.bus.Publish("profile.windowChanged", "", "", map[string]string{"windowId": windowID, "profile": profile})
	}
	writeJSON(w, http.StatusOK, map[string]string{"windowId": windowID, "activeProfile": profile})
}
