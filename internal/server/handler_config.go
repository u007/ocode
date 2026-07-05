package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/ocr"
)

func (h *Handler) HandleGetModel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	model := ""
	if h.cfg != nil {
		model = h.cfg.Model
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"model": model})
}

func (h *Handler) HandleSetModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	h.cfg.Model = req.Model
	if err := config.SaveLastModel(req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

func (h *Handler) HandleGetSmallModel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	current := ""
	enabled := false
	if h.cfg != nil {
		current = h.cfg.Ocode.SmallModel
		enabled = h.cfg.Ocode.SmallModelEnabled
	}
	// When the TUI is attached, prefer the TUI's live runtime flag — the web
	// status bar should mirror what the user just toggled in the TUI, not the
	// persisted config value.
	if rc := h.rc; rc != nil {
		if live := rc.TUIStatus(); live.SmallModelOn || live.SmallModel != "" {
			if live.SmallModel != "" {
				current = live.SmallModel
			}
			enabled = live.SmallModelOn
		}
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"model":    current,
		"enabled":  enabled,
		"priority": agent.SmallModelPriority,
	})
}

func (h *Handler) HandleSetSmallModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, `model is required (use "auto" to clear)`)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	if req.Model == "auto" {
		h.cfg.Ocode.SmallModel = ""
		resolved := agent.ResolveSmallModel(h.cfg)
		if err := config.SaveSmallModel(resolved); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.cfg.Ocode.SmallModel = resolved
		writeJSON(w, http.StatusOK, map[string]string{"model": resolved, "source": "auto"})
		return
	}

	if err := config.SaveSmallModel(req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.cfg.Ocode.SmallModel = req.Model
	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model, "source": "manual"})
}

func (h *Handler) HandleGetAdvisor(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	model := ""
	if h.cfg != nil {
		model = h.cfg.Ocode.Advisor.Model
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"model": model})
}

func (h *Handler) HandleSetAdvisor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	if err := config.SaveAdvisorModel(req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.cfg.Ocode.Advisor.Model = req.Model
	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

// HandleGetAdvisorEnabled reports whether the advisor tool is currently exposed.
// This is a runtime, session-lifetime toggle — it is NOT read from or written to
// config.
func (h *Handler) HandleGetAdvisorEnabled(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	enabled := h.advisorEnabled
	if h.rc != nil {
		if ag := h.rc.Agent(); ag != nil {
			enabled = ag.AdvisorEnabled()
		}
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

// HandleSetAdvisorEnabled flips the advisor tool on/off for every live agent this
// handler controls (and the bridged TUI agent, if any). It deliberately does NOT
// persist to config — the change lasts only for the agents' lifetime.
func (h *Handler) HandleSetAdvisorEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}

	h.mu.Lock()
	h.advisorEnabled = req.Enabled
	for _, as := range h.agents {
		as.agent.SetAdvisorEnabled(req.Enabled)
	}
	if h.rc != nil {
		if ag := h.rc.Agent(); ag != nil {
			ag.SetAdvisorEnabled(req.Enabled)
		}
	}
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// HandleGetOcrEnabled reports whether the OCR tool is currently enabled and
// which model it uses. Reads from the handler's cached config which is kept
// in sync with the TUI config.
func (h *Handler) HandleGetOcrEnabled(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	enabled := false
	model := ""
	if h.cfg != nil {
		enabled = h.cfg.Ocode.Ocr.Enabled
		backend := h.cfg.Ocode.Ocr.Backend
		if backend == "" {
			backend = "openai-compat"
		}
		switch backend {
		case "paddle":
			model = h.cfg.Ocode.Ocr.Paddle.Variant
		default:
			model = h.cfg.Ocode.Ocr.OpenAI.Model
		}
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"enabled": enabled, "model": model})
}

// HandleSetOcrEnabled flips the OCR tool on/off. This is persisted to config
// (unlike the advisor toggle, which is session-only).
func (h *Handler) HandleSetOcrEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}

	h.mu.Lock()
	h.cfg.Ocode.Ocr.Enabled = req.Enabled
	config.SaveOcrConfig(h.cfg.Ocode.Ocr)
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// HandleSetOcrModel sets the OCR model. This is persisted to config.
func (h *Handler) HandleSetOcrModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	h.mu.Lock()
	backend := h.cfg.Ocode.Ocr.Backend
	if backend == "" {
		backend = "openai-compat"
	}
	switch backend {
	case "paddle":
		h.cfg.Ocode.Ocr.Paddle.Variant = req.Model
	default:
		h.cfg.Ocode.Ocr.OpenAI.Model = req.Model
	}
	config.SaveOcrConfig(h.cfg.Ocode.Ocr)
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

// HandleGetOcrConfig returns the full OCR configuration.
func (h *Handler) HandleGetOcrConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := ocr.DefaultOcrConfig()
	if h.cfg != nil {
		cfg = h.cfg.Ocode.Ocr
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

// HandleSetOcrConfig updates the full OCR configuration.
func (h *Handler) HandleSetOcrConfig(w http.ResponseWriter, r *http.Request) {
	var req ocr.OcrConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	h.mu.Lock()
	h.cfg.Ocode.Ocr = req
	config.SaveOcrConfig(req)
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, req)
}

// HandleGetOcrModels returns the list of available models from each OCR backend.
func (h *Handler) HandleGetOcrModels(w http.ResponseWriter, r *http.Request) {
	type backendModels struct {
		Name   string   `json:"name"`
		Models []string `json:"models"`
		Error  string   `json:"error,omitempty"`
	}
	var result struct {
		Backends []backendModels `json:"backends"`
	}

	// Read the configured OCR base URLs so we contact the user's actual
	// endpoint, not always the default localhost:1234.
	var ocrCfg ocr.OcrConfig
	h.mu.Lock()
	if h.cfg != nil {
		ocrCfg = h.cfg.Ocode.Ocr
	} else {
		ocrCfg = ocr.DefaultOcrConfig()
	}
	h.mu.Unlock()

	for _, name := range ocr.List() {
		be := ocr.Get(name)
		if be == nil {
			continue
		}
		// Pass the backend-specific base URL so ListModels contacts the
		// user's configured endpoint instead of the built-in default.
		baseURL := ""
		apiKey := ""
		switch name {
		case "openai-compat":
			baseURL = ocrCfg.OpenAI.BaseURL
			// Resolve the token in priority order (explicit config → base-URL
			// match → "lmstudio"-named credential). The last step is what makes
			// an already-connected LM Studio work: its credential is saved by
			// provider name with no base_url, so a base-URL match alone misses
			// it and the request 401s, yielding an empty model list.
			apiKey = auth.ResolveOpenAICompatKey(ocrCfg.OpenAI.APIKey, baseURL,
				ocrCfg.Backend == "lmstudio" || ocr.LooksLikeLMStudioBaseURL(baseURL))
		case "paddle":
			baseURL = ocrCfg.Paddle.Endpoint
		}
		models, err := be.ListModels(r.Context(), baseURL, apiKey)
		if err != nil {
			// Surface the reason instead of silently dropping the backend:
			// the most common cause of an "empty" OCR model list is that the
			// configured endpoint (e.g. LM Studio at base_url) is unreachable.
			log.Printf("ocr: backend %q ListModels failed (baseURL=%q): %v", name, baseURL, err)
			result.Backends = append(result.Backends, backendModels{
				Name:  name,
				Error: err.Error(),
			})
			continue
		}
		if len(models) == 0 {
			log.Printf("ocr: backend %q returned no models (baseURL=%q)", name, baseURL)
			result.Backends = append(result.Backends, backendModels{
				Name:  name,
				Error: "no models available at " + baseURL,
			})
			continue
		}
		result.Backends = append(result.Backends, backendModels{
			Name:   name,
			Models: models,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) HandleListAgents(w http.ResponseWriter, r *http.Request) {
	specs := agent.PrimaryAgentSpecs()
	type agentInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	out := make([]agentInfo, len(specs))
	for i, s := range specs {
		out[i] = agentInfo{Name: s.Name, Description: s.Description}
	}
	writeJSON(w, http.StatusOK, out)
}

// ── Mask (secret redaction) config ─────────────────────────────────────────

// HandleGetMaskConfig returns the current mask/redaction config.
func (h *Handler) HandleGetMaskConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	enabled := false
	mode := "lenient"
	model := ""
	if h.cfg != nil {
		enabled = h.cfg.Ocode.Security.Redaction.Enabled
		mode = config.ResolveRedactionMode(h.cfg.Ocode.Security.Redaction)
		model = h.cfg.Ocode.Security.Redaction.Model
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": enabled,
		"mode":    mode,
		"model":   model,
	})
}

// HandleSetMaskEnabled toggles secret redaction on/off.
func (h *Handler) HandleSetMaskEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}

	h.mu.Lock()
	h.cfg.Ocode.Security.Redaction.Enabled = req.Enabled
	config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.Enabled = req.Enabled
	})
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// HandleSetMaskMode sets the redaction mode (lenient/full).
func (h *Handler) HandleSetMaskMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := readBodyJSON(r, &req); err != nil || (req.Mode != "lenient" && req.Mode != "full") {
		writeError(w, http.StatusBadRequest, "mode must be 'lenient' or 'full'")
		return
	}

	h.mu.Lock()
	h.cfg.Ocode.Security.Redaction.Mode = req.Mode
	config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.Mode = req.Mode
	})
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"mode": req.Mode})
}

// HandleSetMaskModel sets the tier-2 scanning model for secret redaction.
func (h *Handler) HandleSetMaskModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	h.mu.Lock()
	h.cfg.Ocode.Security.Redaction.Model = req.Model
	config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.Model = req.Model
	})
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

func (h *Handler) HandleSetAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		SessionID string `json:"session_id,omitempty"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	spec := agent.FindAgentSpec(req.Name)
	if spec == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if req.SessionID != "" {
		h.mu.Lock()
		if as, ok := h.agents[req.SessionID]; ok {
			as.agent.SetSpec(spec)
		}
		h.mu.Unlock()
	}

	writeJSON(w, http.StatusOK, map[string]string{"name": spec.Name, "description": spec.Description})
}
