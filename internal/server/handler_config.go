package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/ocr"
	"github.com/u007/ocode/internal/redact"
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
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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
	// Mirror the TUI's finishModelSwitch: record the pick in the shared
	// recent-models list so the web/desktop model selector's "Recently Used"
	// section stays in sync with the TUI.
	if strings.Contains(req.Model, "/") {
		if err := config.SaveRecentModel(req.Model); err != nil {
			log.Printf("save recent model: %v", err)
		}
	}
	// Push a fresh status snapshot so connected web clients (status bar,
	// sidebar) reflect the new model immediately instead of showing the
	// mount-time/last-TUI value until the next turn.
	h.pushStatusSnapshot()
	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

// thinkingBudgetLevelInfo returns the canonical level list for the thinking
// budget API payload. Each entry pairs the level label with its token budget.
func thinkingBudgetLevelInfo() []map[string]any {
	info := make([]map[string]any, 0, len(config.ThinkingBudgetLevels))
	for i, budget := range config.ThinkingBudgetLevels {
		info = append(info, map[string]any{"level": config.ThinkingBudgetLabels[i], "budget": budget})
	}
	return info
}

// HandleGetThinkingBudget returns the current extended-thinking budget (0 =
// off) plus the canonical level list, so the web sidebar can display the
// reasoning level and render a switch with the same options as the TUI's
// /effort command.
func (h *Handler) HandleGetThinkingBudget(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	budget := 0
	if h.cfg != nil {
		budget = h.cfg.ThinkingBudget
	}
	// When a TUI is attached it owns the live reasoning level; prefer its
	// snapshot so the web mirrors what the user last set with ctrl+d /effort.
	if rc := h.rc; rc != nil {
		if live := rc.TUIStatus(); live.MainModel != "" {
			budget = live.ThinkingBudget
		}
	}
	h.mu.Unlock()
	level := config.ThinkingBudgetLabels[config.ThinkingLevelIndexForBudget(budget)]
	writeJSON(w, http.StatusOK, map[string]any{
		"budget": budget,
		"level":  level,
		"levels": thinkingBudgetLevelInfo(),
	})
}

// HandleSetThinkingBudget sets the extended-thinking budget for the main
// model. Accepts either {"level": "med"} or {"budget": 8000}; the budget must
// be one of the canonical config.ThinkingBudgetLevels values. It persists the
// choice (config.SaveLastThinkingBudget) and pushes a fresh status snapshot so
// the web sidebar/status bar update immediately. In headless/desktop mode the
// budget takes effect for subsequently built agent sessions; when a TUI is
// bridged the running TUI keeps its own level until the user changes it there
// (mirroring the model-switch limitation — see
// docs/architecture/sidebar-tui-parity-gaps.md).
func (h *Handler) HandleSetThinkingBudget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level  string `json:"level"`
		Budget *int   `json:"budget"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Level == "" && req.Budget == nil {
		writeError(w, http.StatusBadRequest, `one of "level" or "budget" is required`)
		return
	}
	if req.Level != "" && req.Budget != nil {
		writeError(w, http.StatusBadRequest, `provide either "level" or "budget", not both`)
		return
	}

	budget := -1
	if req.Budget != nil {
		budget = *req.Budget
	}
	if req.Level != "" {
		level := strings.ToLower(strings.TrimSpace(req.Level))
		found := false
		for i, lvl := range config.ThinkingBudgetLabels {
			if lvl == level {
				budget = config.ThinkingBudgetLevels[i]
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusBadRequest, "unknown level, use one of: off, low, med, high, xhigh, max")
			return
		}
	}

	idx := config.ThinkingLevelIndexForBudget(budget)
	if config.ThinkingBudgetLevels[idx] != budget {
		writeError(w, http.StatusBadRequest, "budget must be one of: 0, 1024, 8000, 16000, 32000, 65536")
		return
	}

	h.mu.Lock()
	if h.cfg == nil {
		h.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	h.cfg.ThinkingBudget = budget
	h.mu.Unlock()

	if err := config.SaveLastThinkingBudget(budget); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.pushStatusSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"budget": budget,
		"level":  config.ThinkingBudgetLabels[idx],
		"levels": thinkingBudgetLevelInfo(),
	})
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

// HandleSetSmallModel sets the small model and/or flips the runtime on/off
// gate. Both fields are persisted to config (mirroring the TUI's small-model
// sidebar toggle, which calls config.SaveSmallModelEnabled). Either field may
// be provided on its own: {"model": "..."} (or "auto" to clear) sets the model
// without touching the gate; {"enabled": bool} toggles the gate without
// changing the model.
func (h *Handler) HandleSetSmallModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model   string `json:"model"`
		Enabled *bool  `json:"enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Model == "" && req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "model or enabled is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	if req.Enabled != nil {
		h.cfg.Ocode.SmallModelEnabled = *req.Enabled
		if err := config.SaveSmallModelEnabled(*req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.Model != "" {
		if req.Model == "auto" {
			h.cfg.Ocode.SmallModel = ""
			resolved := agent.ResolveSmallModel(h.cfg)
			if err := config.SaveSmallModel(resolved); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			h.cfg.Ocode.SmallModel = resolved
			h.pushStatusSnapshot()
			writeJSON(w, http.StatusOK, map[string]any{
				"model":   resolved,
				"enabled": h.cfg.Ocode.SmallModelEnabled,
				"source":  "auto",
			})
			return
		}

		if err := config.SaveSmallModel(req.Model); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.cfg.Ocode.SmallModel = req.Model
	}

	// Push a fresh status snapshot so the web's status bar / sidebar reflect
	// the new small model + gate immediately (headless: SSE broadcast; RC
	// bridge: merged into the TUI's snapshot).
	h.pushStatusSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"model":   h.cfg.Ocode.SmallModel,
		"enabled": h.cfg.Ocode.SmallModelEnabled,
		"source":  "manual",
	})
}

func (h *Handler) HandleGetAdvisor(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.AdvisorConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.Advisor
	}
	h.mu.Unlock()
	if cfg.Checkpoints == nil {
		cfg.Checkpoints = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     cfg.Enabled,
		"provider":    cfg.Provider,
		"model":       cfg.Model,
		"claude_code": cfg.ClaudeCode,
		"checkpoints": cfg.Checkpoints,
	})
}

func (h *Handler) HandleSetAdvisor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider    *string  `json:"provider"`
		Model       *string  `json:"model"`
		ClaudeCode  *bool    `json:"claude_code"`
		Checkpoints []string `json:"checkpoints"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == nil && req.Provider == nil && req.ClaudeCode == nil && req.Checkpoints == nil {
		writeError(w, http.StatusBadRequest, "no advisor fields provided")
		return
	}

	h.mu.Lock()
	if h.cfg == nil {
		h.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	// Merge onto the current block so a partial PUT (e.g. web's
	// {"model":"..."} only) cannot clear the other fields.
	cur := h.cfg.Ocode.Advisor
	if req.Provider != nil {
		cur.Provider = *req.Provider
		// Provider change also toggles the Claude Code backend by convention.
		cur.ClaudeCode = (*req.Provider == "claude-code")
	}
	if req.Model != nil {
		cur.Model = *req.Model
	}
	if req.ClaudeCode != nil {
		cur.ClaudeCode = *req.ClaudeCode
	}
	if req.Checkpoints != nil {
		cur.Checkpoints = req.Checkpoints
	}
	if err := config.SaveOcodeAdvisorConfig(cur); err != nil {
		h.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.cfg.Ocode.Advisor = cur
	h.pushStatusSnapshot()
	h.mu.Unlock()

	if cur.Checkpoints == nil {
		cur.Checkpoints = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     cur.Enabled,
		"provider":    cur.Provider,
		"model":       cur.Model,
		"claude_code": cur.ClaudeCode,
		"checkpoints": cur.Checkpoints,
	})
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
		writeError(w, http.StatusBadRequest, "invalid request body")
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
	baseURL := ""
	failMode := ""
	allowRemoteTier2 := false
	var skipLLMIfClean *bool
	customWords := []string{}
	if h.cfg != nil {
		rc := h.cfg.Ocode.Security.Redaction
		enabled = rc.Enabled
		mode = config.ResolveRedactionMode(rc)
		model = rc.Model
		baseURL = rc.BaseURL
		failMode = rc.FailMode
		allowRemoteTier2 = rc.AllowRemoteTier2
		skipLLMIfClean = rc.SkipLLMIfClean
		customWords = rc.CustomWords
	}
	h.mu.Unlock()
	if customWords == nil {
		customWords = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":            enabled,
		"mode":               mode,
		"model":              model,
		"base_url":           baseURL,
		"fail_mode":          failMode,
		"allow_remote_tier2": allowRemoteTier2,
		"skip_llm_if_clean":  skipLLMIfClean,
		"custom_words":       customWords,
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

	// Propagate to live agent sessions so the toggle takes effect immediately.
	h.applyRedactionToLiveSessions(h.cfg.Ocode.Security.Redaction)

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

	// Mode only affects scanning behaviour; re-apply so live sessions pick it up.
	h.applyRedactionToLiveSessions(h.cfg.Ocode.Security.Redaction)

	writeJSON(w, http.StatusOK, map[string]string{"mode": req.Mode})
}

// HandleSetMaskModel sets the tier-2 scanning model for secret redaction.
func (h *Handler) HandleSetMaskModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.mu.Lock()
	h.cfg.Ocode.Security.Redaction.Model = req.Model
	config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.Model = req.Model
	})
	h.mu.Unlock()

	// Rebuild the tier-2 scanner for live sessions (e.g. a newly chosen local
	// LM Studio model) without restarting the server.
	h.applyRedactionToLiveSessions(h.cfg.Ocode.Security.Redaction)

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

// HandleGetTerminalConfig reports the interactive pty terminal's availability
// and scrollback setting for the server's single configured workdir. The
// terminal itself is always enabled; "available" reflects whether the server
// can safely expose it (authentication configured or loopback bind).
func (h *Handler) HandleGetTerminalConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	scrollback := config.DefaultTerminalScrollbackLines
	fontFamily := ""
	fontSize := config.DefaultTerminalFontSize
	shell := ""
	workDir := h.workDir
	available := h.terminalAuthConfigured || h.terminalLoopback
	if h.cfg != nil {
		scrollback = config.NormalizeTerminalScrollbackLines(h.cfg.Ocode.TerminalScrollbackLines)
		fontFamily = h.cfg.Ocode.TerminalFontFamily
		if h.cfg.Ocode.TerminalFontSize > 0 {
			fontSize = config.NormalizeTerminalFontSize(h.cfg.Ocode.TerminalFontSize)
		}
		shell = h.cfg.Ocode.TerminalShell
	}
	h.mu.Unlock()
	if !available {
		workDir = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":        available,
		"scrollback_lines": scrollback,
		"font_family":      fontFamily,
		"font_size":        fontSize,
		"shell":            shell,
		"default_shell":    config.DefaultTerminalShell(),
		"available_shells": config.AvailableShells(),
		"work_dir":         workDir,
	})
}

// HandleSetTerminalConfig persists the terminal's scrollback, font family,
// and font size settings. There is no enable/disable toggle: the interactive
// terminal is always enabled. Fields absent from the request keep their
// current stored values.
func (h *Handler) HandleSetTerminalConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScrollbackLines *int    `json:"scrollback_lines"`
		FontFamily      *string `json:"font_family"`
		FontSize        *int    `json:"font_size"`
		Shell           *string `json:"shell"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ScrollbackLines == nil && req.FontFamily == nil && req.FontSize == nil && req.Shell == nil {
		writeError(w, http.StatusBadRequest, "at least one of scrollback_lines, font_family, font_size, shell is required")
		return
	}

	// Persist only the fields present in the request via targeted savers, so
	// concurrent updates to different fields cannot clobber each other with a
	// stale pre-read snapshot.
	if req.ScrollbackLines != nil {
		if err := config.SaveTerminalScrollbackLines(*req.ScrollbackLines); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
			return
		}
	}
	if req.FontFamily != nil {
		if err := config.SaveTerminalFontFamily(*req.FontFamily); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
			return
		}
	}
	if req.FontSize != nil {
		if err := config.SaveTerminalFontSize(*req.FontSize); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
			return
		}
	}
	if req.Shell != nil {
		if err := config.SaveTerminalShell(*req.Shell); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
			return
		}
	}

	h.mu.Lock()
	if h.cfg != nil {
		if req.ScrollbackLines != nil {
			h.cfg.Ocode.TerminalScrollbackLines = config.NormalizeTerminalScrollbackLines(*req.ScrollbackLines)
		}
		if req.FontFamily != nil {
			h.cfg.Ocode.TerminalFontFamily = strings.TrimSpace(*req.FontFamily)
		}
		if req.FontSize != nil {
			if *req.FontSize <= 0 {
				h.cfg.Ocode.TerminalFontSize = 0
			} else {
				h.cfg.Ocode.TerminalFontSize = config.NormalizeTerminalFontSize(*req.FontSize)
			}
		}
		if req.Shell != nil {
			h.cfg.Ocode.TerminalShell = strings.TrimSpace(*req.Shell)
		}
	}
	scrollback := config.DefaultTerminalScrollbackLines
	fontFamily := ""
	fontSize := config.DefaultTerminalFontSize
	shell := ""
	workDir := h.workDir
	available := h.terminalAuthConfigured || h.terminalLoopback
	if h.cfg != nil {
		scrollback = config.NormalizeTerminalScrollbackLines(h.cfg.Ocode.TerminalScrollbackLines)
		fontFamily = h.cfg.Ocode.TerminalFontFamily
		if h.cfg.Ocode.TerminalFontSize > 0 {
			fontSize = config.NormalizeTerminalFontSize(h.cfg.Ocode.TerminalFontSize)
		}
		shell = h.cfg.Ocode.TerminalShell
	}
	h.mu.Unlock()

	if !available {
		workDir = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":        available,
		"scrollback_lines": scrollback,
		"font_family":      fontFamily,
		"font_size":        fontSize,
		"shell":            shell,
		"default_shell":    config.DefaultTerminalShell(),
		"available_shells": config.AvailableShells(),
		"work_dir":         workDir,
	})
}

// ── Ocode config sections (web Settings tab) ──────────────────────────────

// HandleGetRecapConfig reports the /recap model selection and timeout.
func (h *Handler) HandleGetRecapConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	model, enabled, timeout := "", false, 0
	if h.cfg != nil {
		model = h.cfg.Ocode.RecapModel
		enabled = h.cfg.Ocode.RecapModelEnabled
		timeout = h.cfg.Ocode.RecapTimeoutSeconds
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"recap_model":           model,
		"recap_model_enabled":   enabled,
		"recap_timeout_seconds": timeout,
	})
}

// HandleSetRecapConfig persists the /recap model selection and timeout.
func (h *Handler) HandleSetRecapConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RecapModel          string `json:"recap_model"`
		RecapModelEnabled   bool   `json:"recap_model_enabled"`
		RecapTimeoutSeconds int    `json:"recap_timeout_seconds"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := config.SaveOcodeRecapConfig(req.RecapModel, req.RecapModelEnabled, req.RecapTimeoutSeconds); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}

	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.RecapModel = req.RecapModel
		h.cfg.Ocode.RecapModelEnabled = req.RecapModelEnabled
		h.cfg.Ocode.RecapTimeoutSeconds = req.RecapTimeoutSeconds
	}
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"recap_model":           req.RecapModel,
		"recap_model_enabled":   req.RecapModelEnabled,
		"recap_timeout_seconds": req.RecapTimeoutSeconds,
	})
}

// HandleGetCommitMsgConfig reports the commit-message generation model and prompt.
func (h *Handler) HandleGetCommitMsgConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	model, prompt := "", ""
	if h.cfg != nil {
		model = h.cfg.Ocode.CommitMsgModel
		prompt = h.cfg.Ocode.CommitMsgPrompt
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"commit_msg_model":  model,
		"commit_msg_prompt": prompt,
	})
}

// HandleSetCommitMsgConfig persists the commit-message generation model and prompt.
func (h *Handler) HandleSetCommitMsgConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CommitMsgModel  string `json:"commit_msg_model"`
		CommitMsgPrompt string `json:"commit_msg_prompt"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeCommitMsgConfig(req.CommitMsgModel, req.CommitMsgPrompt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.CommitMsgModel = req.CommitMsgModel
		h.cfg.Ocode.CommitMsgPrompt = req.CommitMsgPrompt
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"commit_msg_model":  req.CommitMsgModel,
		"commit_msg_prompt": req.CommitMsgPrompt,
	})
}

// HandleGetCompactConfig reports the auto-compact settings block.
func (h *Handler) HandleGetCompactConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.CompactConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.Compact
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

// HandleSetCompactConfig persists the auto-compact settings block.
func (h *Handler) HandleSetCompactConfig(w http.ResponseWriter, r *http.Request) {
	var req config.CompactConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeCompactConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Compact = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}

// HandleGetAutoPermissionConfig reports the LLM auto-approval block.
func (h *Handler) HandleGetAutoPermissionConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.AutoPermissionConfig{}
	if h.cfg != nil && h.cfg.Ocode.Permissions.Auto != nil {
		cfg = *h.cfg.Ocode.Permissions.Auto
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

// HandleSetAutoPermissionConfig persists the auto-approval block. The Model
// field is owned exclusively by the /permissions model setter, so it is
// preserved from the current config rather than taken from the request.
func (h *Handler) HandleSetAutoPermissionConfig(w http.ResponseWriter, r *http.Request) {
	var req config.AutoPermissionConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeAutoPermissionConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	preservedModel := ""
	if h.cfg != nil {
		if h.cfg.Ocode.Permissions.Auto != nil {
			preservedModel = h.cfg.Ocode.Permissions.Auto.Model
		}
		req.Model = preservedModel
		h.cfg.Ocode.Permissions.Auto = &req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}

// discoveryConfigDTO carries DiscoveryConfig over the wire. DiscoveryConfig has
// no Go struct tags — the keys below match the hand-mapped discoveryMap keys in
// writeOcodeConfigFile exactly.
type discoveryConfigDTO struct {
	Enabled          bool     `json:"enabled"`
	EmbeddingModel   string   `json:"embedding_model"`
	EmbeddingBackend string   `json:"embedding_backend"`
	LocalModelStatus string   `json:"local_model_status"`
	LocalServerURL   string   `json:"local_server_url"`
	PinnedSkills     []string `json:"pinned_skills"`
	IgnorePaths      []string `json:"ignore_paths"`
}

// HandleGetDiscoveryConfig reports the discovery-based skill/MCP retrieval settings.
func (h *Handler) HandleGetDiscoveryConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	d := config.DiscoveryConfig{}
	if h.cfg != nil {
		d = h.cfg.Ocode.Discovery
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, discoveryConfigDTO{
		Enabled: d.Enabled, EmbeddingModel: d.EmbeddingModel, EmbeddingBackend: d.EmbeddingBackend,
		LocalModelStatus: d.LocalModelStatus, LocalServerURL: d.LocalServerURL,
		PinnedSkills: d.PinnedSkills, IgnorePaths: d.IgnorePaths,
	})
}

// HandleSetDiscoveryConfig persists the discovery-based skill/MCP retrieval settings.
func (h *Handler) HandleSetDiscoveryConfig(w http.ResponseWriter, r *http.Request) {
	var req discoveryConfigDTO
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := config.DiscoveryConfig{
		Enabled: req.Enabled, EmbeddingModel: req.EmbeddingModel, EmbeddingBackend: req.EmbeddingBackend,
		LocalModelStatus: req.LocalModelStatus, LocalServerURL: req.LocalServerURL,
		PinnedSkills: req.PinnedSkills, IgnorePaths: req.IgnorePaths,
	}
	if err := config.SaveOcodeDiscoveryConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Discovery = cfg
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}

// HandleGetTUIConfigSection reports the TUI theme/input/keybind settings.
func (h *Handler) HandleGetTUIConfigSection(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.TUIConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.TUI
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

// HandleSetTUIConfigSection persists the TUI theme/input/keybind settings.
func (h *Handler) HandleSetTUIConfigSection(w http.ResponseWriter, r *http.Request) {
	var req config.TUIConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeTUIConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.TUI = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}

// HandleGetEditorConfig reports the editor/editor-mode/ide-mode settings.
func (h *Handler) HandleGetEditorConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	editor, editorMode, ideMode := "", "", ""
	if h.cfg != nil {
		editor = h.cfg.Ocode.Editor
		editorMode = h.cfg.Ocode.EditorMode
		ideMode = h.cfg.Ocode.IDEMode
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"editor": editor, "editor_mode": editorMode, "ide_mode": ideMode,
	})
}

// HandleSetEditorConfig persists the editor/editor-mode/ide-mode settings.
func (h *Handler) HandleSetEditorConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Editor     string `json:"editor"`
		EditorMode string `json:"editor_mode"`
		IDEMode    string `json:"ide_mode"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeEditorConfig(req.Editor, req.EditorMode, req.IDEMode); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Editor = req.Editor
		h.cfg.Ocode.EditorMode = req.EditorMode
		h.cfg.Ocode.IDEMode = req.IDEMode
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"editor": req.Editor, "editor_mode": req.EditorMode, "ide_mode": req.IDEMode,
	})
}

// HandleGetImageGenConfig reports the image-generation settings.
func (h *Handler) HandleGetImageGenConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.ImageGenConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.ImageGen
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

// HandleSetImageGenConfig persists the image-generation settings.
func (h *Handler) HandleSetImageGenConfig(w http.ResponseWriter, r *http.Request) {
	var req config.ImageGenConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveImageGenConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.ImageGen = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}

// HandleGetPathsConfig reports the extra-allowed-paths and upload-directory settings.
func (h *Handler) HandleGetPathsConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	paths, uploadDir := []string(nil), ""
	if h.cfg != nil {
		paths = h.cfg.Ocode.ExtraAllowedPaths
		uploadDir = h.cfg.Ocode.UploadDir
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"extra_allowed_paths": paths,
		"upload_dir":          uploadDir,
	})
}

// HandleSetPathsConfig persists the extra-allowed-paths and upload-directory settings.
func (h *Handler) HandleSetPathsConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ExtraAllowedPaths []string `json:"extra_allowed_paths"`
		UploadDir         string   `json:"upload_dir"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodePathsConfig(req.ExtraAllowedPaths, req.UploadDir); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.ExtraAllowedPaths = req.ExtraAllowedPaths
		h.cfg.Ocode.UploadDir = req.UploadDir
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"extra_allowed_paths": req.ExtraAllowedPaths,
		"upload_dir":          req.UploadDir,
	})
}

// HandleGetLimitsConfig reports the execution limits.
func (h *Handler) HandleGetLimitsConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	maxSteps, maxImageDim, maxConcurrent, undoMaxAgeDelta := 0, 0, 0, 10
	if h.cfg != nil {
		maxSteps = h.cfg.Ocode.MaxSteps
		maxImageDim = h.cfg.Ocode.MaxImageDim
		maxConcurrent = h.cfg.Ocode.MaxConcurrentAgents
		undoMaxAgeDelta = h.cfg.Ocode.UndoMaxAgeDelta
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"max_steps":             maxSteps,
		"image_max_dim":         maxImageDim,
		"max_concurrent_agents": maxConcurrent,
		"undo_max_age_delta":    undoMaxAgeDelta,
	})
}

// HandleSetLimitsConfig persists the execution limits.
func (h *Handler) HandleSetLimitsConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxSteps            int `json:"max_steps"`
		ImageMaxDim         int `json:"image_max_dim"`
		MaxConcurrentAgents int `json:"max_concurrent_agents"`
		UndoMaxAgeDelta     int `json:"undo_max_age_delta"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeLimits(req.MaxSteps, req.ImageMaxDim, req.MaxConcurrentAgents, req.UndoMaxAgeDelta); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.MaxSteps = req.MaxSteps
		h.cfg.Ocode.MaxImageDim = req.ImageMaxDim
		h.cfg.Ocode.MaxConcurrentAgents = req.MaxConcurrentAgents
		h.cfg.Ocode.UndoMaxAgeDelta = req.UndoMaxAgeDelta
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"max_steps":             req.MaxSteps,
		"image_max_dim":         req.ImageMaxDim,
		"max_concurrent_agents": req.MaxConcurrentAgents,
		"undo_max_age_delta":    req.UndoMaxAgeDelta,
	})
}

// HandleGetFeaturesConfig reports the memory/doc-prompt feature toggles.
func (h *Handler) HandleGetFeaturesConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	mem, doc := false, false
	if h.cfg != nil {
		mem = h.cfg.Ocode.MemoryEnabled
		doc = h.cfg.Ocode.DocPromptEnabled
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"memory_enabled":     mem,
		"doc_prompt_enabled": doc,
	})
}

// HandleSetFeaturesConfig persists the memory/doc-prompt feature toggles.
func (h *Handler) HandleSetFeaturesConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MemoryEnabled    bool `json:"memory_enabled"`
		DocPromptEnabled bool `json:"doc_prompt_enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeFeatures(req.MemoryEnabled, req.DocPromptEnabled); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.MemoryEnabled = req.MemoryEnabled
		h.cfg.Ocode.DocPromptEnabled = req.DocPromptEnabled
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"memory_enabled":     req.MemoryEnabled,
		"doc_prompt_enabled": req.DocPromptEnabled,
	})
}

// HandleGetPluginsEnabledConfig reports the opt-in builtin tool gates.
func (h *Handler) HandleGetPluginsEnabledConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.PluginsConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.Plugins
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

// HandleSetPluginsEnabledConfig persists the opt-in builtin tool gates.
func (h *Handler) HandleSetPluginsEnabledConfig(w http.ResponseWriter, r *http.Request) {
	var req config.PluginsConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodePluginsConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Plugins = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}

// HandleGetLocalModelsConfig reports the registered local model instances.
func (h *Handler) HandleGetLocalModelsConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	models := map[string]config.LocalModelConfig{}
	if h.cfg != nil && h.cfg.Ocode.LocalModels != nil {
		models = h.cfg.Ocode.LocalModels
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, models)
}

// HandleSetLocalModelsConfig persists the registered local model instances.
func (h *Handler) HandleSetLocalModelsConfig(w http.ResponseWriter, r *http.Request) {
	req := map[string]config.LocalModelConfig{}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeLocalModels(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.LocalModels = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}

// HandleSetMaskAdvanced persists the advanced redaction fields
// (base_url, fail_mode, allow_remote_tier2, custom_words). The deprecated
// skip_llm_if_clean flag is intentionally not writable here; it is derived
// from Mode via ResolveRedactionMode.
func (h *Handler) HandleSetMaskAdvanced(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL          string   `json:"base_url"`
		FailMode         string   `json:"fail_mode"`
		AllowRemoteTier2 bool     `json:"allow_remote_tier2"`
		CustomWords      []string `json:"custom_words"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.FailMode != "" && req.FailMode != "block" && req.FailMode != "warn" {
		writeError(w, http.StatusBadRequest, "fail_mode must be \"block\" or \"warn\"")
		return
	}

	h.mu.Lock()
	if h.cfg == nil {
		h.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	if err := config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.BaseURL = req.BaseURL
		if req.FailMode != "" {
			rc.FailMode = req.FailMode
		}
		rc.AllowRemoteTier2 = req.AllowRemoteTier2
		rc.CustomWords = req.CustomWords
	}); err != nil {
		h.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.cfg.Ocode.Security.Redaction.BaseURL = req.BaseURL
	if req.FailMode != "" {
		h.cfg.Ocode.Security.Redaction.FailMode = req.FailMode
	}
	h.cfg.Ocode.Security.Redaction.AllowRemoteTier2 = req.AllowRemoteTier2
	h.cfg.Ocode.Security.Redaction.CustomWords = req.CustomWords
	h.mu.Unlock()

	// Rebuild the tier-2 scanner for live sessions (e.g. a new base URL pointing
	// at a local model server) without restarting the server.
	h.applyRedactionToLiveSessions(h.cfg.Ocode.Security.Redaction)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"base_url":           req.BaseURL,
		"fail_mode":          req.FailMode,
		"allow_remote_tier2": req.AllowRemoteTier2,
		"custom_words":       req.CustomWords,
	})
}

// buildRedactionScanner constructs the tier-2 LLM scanner for secret
// redaction from a RedactionConfig, mirroring the TUI's buildLLMScanner. It
// resolves the provider API key from the auth store and applies the local
// model rewrites (LM Studio prefix strip, /localmodel manifest serve id).
// Returns nil when no usable scanner can be built (no model/base_url, a
// non-local endpoint without allow_remote_tier2, or an unresolvable local/*
// manifest).
func (h *Handler) buildRedactionScanner(rc config.RedactionConfig) *redact.LLMScanner {
	baseURL := rc.BaseURL
	model := rc.Model
	// Auto-detect the default base URL for known local providers when a model
	// is set but no explicit base_url was configured.
	if baseURL == "" && model != "" {
		if def := redact.DefaultRedactionBaseURL(model); def != "" {
			baseURL = def
		}
	}
	model = redact.NormalizeRedactionModelName(model, baseURL)
	if baseURL == "" || model == "" {
		return nil
	}
	if !rc.AllowRemoteTier2 && !redact.IsLocalEndpoint(baseURL) {
		return nil
	}

	var apiKey string
	if provider, _ := config.SplitProviderModel(model); provider != "" {
		if key := auth.ResolveKey(provider); key != "" {
			apiKey = key
		} else if cred, ok := auth.Get(provider); ok && cred.Key != "" {
			apiKey = cred.Key
		}
	}

	scannerModel := redact.ScannerRequestModelName(model)
	if strings.HasPrefix(model, "local/") {
		man, ok := discovery.ChatManifestForHost(model)
		if !ok {
			// Fail closed: a local/* id with no chat manifest for this host
			// cannot be served by the /localmodel lifecycle, so the base URL
			// is stale or the id was hand-typed. Skip tier-2 (regex tier-1
			// still applies).
			return nil
		}
		scannerModel = man.ExpectedServeID()
	}

	return redact.NewScanner(baseURL, scannerModel, rc.AllowRemoteTier2, apiKey)
}

// applyRedactionToAgent wires the current redaction configuration into a live
// agent: the tier-1 regex hook, the token registry, and the tier-2 LLM
// scanner. This mirrors the TUI's syncRedactionRuntime and makes the web/
// desktop server honour the same Security & Redaction settings the TUI does.
func (h *Handler) applyRedactionToAgent(ag *agent.Agent, rc config.RedactionConfig) {
	if ag == nil {
		return
	}
	if !rc.Enabled {
		ag.SetRedactionEnabled(false)
		ag.SetRedactionRegistry(nil)
		ag.SetRedactionHook(nil)
		ag.SetRedactionScanner(nil)
		return
	}
	reg := redact.NewRegistry(redact.NewNonce())
	ag.SetRedactionEnabled(true)
	ag.SetRedactionRegistry(reg)
	ag.SetRedactionHook(redact.NetHookEnabled(reg))
	ag.SetRedactionScanner(h.buildRedactionScanner(rc))
}

// applyRedactionToLiveSessions re-applies the redaction configuration to every
// resident agent session so runtime changes made via the Settings UI take
// effect without restarting the server. The h.mu map lock is only held to
// snapshot the session ids; the per-session apply takes each as.mu.
func (h *Handler) applyRedactionToLiveSessions(rc config.RedactionConfig) {
	h.mu.Lock()
	ids := make([]string, 0, len(h.agents))
	for id := range h.agents {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		as := h.lookupAgentSession(id)
		if as == nil {
			continue
		}
		as.mu.Lock()
		h.applyRedactionToAgent(as.agent, rc)
		as.mu.Unlock()
	}
}
