package server

import (
	"net/http"
	"sort"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

func (h *Handler) HandleGetPermissions(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	pm := agent.NewPermissionManager()
	pm.LoadFromOcode(h.cfg.Ocode.Permissions)

	type ruleEntry struct {
		Tool  string `json:"tool"`
		Level string `json:"level"`
	}

	rawRules := pm.Rules()
	rules := make([]ruleEntry, 0, len(rawRules))
	for tool, level := range rawRules {
		rules = append(rules, ruleEntry{Tool: tool, Level: string(level)})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Tool < rules[j].Tool })

	rawBash := pm.BashPrefixRules()
	bashRules := make([]ruleEntry, 0, len(rawBash))
	for prefix, level := range rawBash {
		bashRules = append(bashRules, ruleEntry{Tool: prefix, Level: string(level)})
	}
	sort.Slice(bashRules, func(i, j int) bool { return bashRules[i].Tool < bashRules[j].Tool })

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":       string(pm.Mode()),
		"auto_allow": pm.AutoPermissionEnabled(),
		"rules":      rules,
		"bash_rules": bashRules,
	})
}

func (h *Handler) HandleSetPermission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tool  string `json:"tool"`
		Level string `json:"level"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Tool == "" || req.Level == "" {
		writeError(w, http.StatusBadRequest, "tool and level are required")
		return
	}

	level := agent.PermissionLevel(req.Level)
	if level != agent.PermissionAllow && level != agent.PermissionDeny && level != agent.PermissionAsk {
		writeError(w, http.StatusBadRequest, "level must be allow, deny, or ask")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	perms := h.cfg.Ocode.Permissions
	if perms.Tools == nil {
		perms.Tools = map[string]string{}
	}
	perms.Tools[req.Tool] = req.Level
	h.cfg.Ocode.Permissions = perms

	if err := config.SaveOcodePermissions(perms); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, as := range h.agents {
		if pm := as.agent.Permissions(); pm != nil {
			pm.SetRule(req.Tool, level)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"tool": req.Tool, "level": req.Level})
}

func (h *Handler) HandleGetYolo(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	enabled := false
	for _, as := range h.agents {
		if pm := as.agent.Permissions(); pm != nil {
			enabled = pm.Mode() == agent.PermissionModeYOLO
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"yolo": enabled})
}

func (h *Handler) HandleSetYolo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	mode := agent.PermissionModeNormal
	if req.Enabled {
		mode = agent.PermissionModeYOLO
	}
	for _, as := range h.agents {
		if pm := as.agent.Permissions(); pm != nil {
			pm.SetMode(mode)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"yolo": req.Enabled})
}
