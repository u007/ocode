package server

import (
	"net/http"
	"sort"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// allAgents returns every live agent the handler controls: the per-session
// server-owned agents plus, when proxying, the TUI's RC agent. Permission and
// YOLO toggles from external clients (web UI, Telegram bot) must reach the RC
// agent too, otherwise /rc instances ignore remote mode changes.
func (h *Handler) allAgents() []*agent.Agent {
	out := make([]*agent.Agent, 0, len(h.agents)+1)
	for _, as := range h.agents {
		if as.agent != nil {
			out = append(out, as.agent)
		}
	}
	if h.rc != nil {
		if a := h.rc.Agent(); a != nil {
			out = append(out, a)
		}
	}
	return out
}

func (h *Handler) HandleGetPermissions(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	pm := agent.NewPermissionManager()
	pm.LoadFromOcode(h.cfg.Ocode.Permissions)

	// The LIVE mode wins over the config default: a session-scoped toggle
	// (yolo / sandbox) moves agents without touching the durable config, and
	// GET must report what is actually in force (an agent in sandbox while the
	// config still says normal).
	liveMode := h.livePermissionModeLocked(pm)

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
		"mode":                string(liveMode),
		"auto_allow":          pm.AutoPermissionEnabled(),
		"sandbox_supported":   agent.SandboxSupported(),
		"effective_behavior":  effectivePermissionBehavior(liveMode),
		"rules":               rules,
		"bash_rules":          bashRules,
	})
}

// livePermissionModeLocked resolves the authoritative mode: the process-global
// runtime override wins (it is what PUT /api/permissions/mode and the yolo
// toggle set), falling back to the config-derived manager default. Lock-free
// read (atomic) so it is safe from callers holding h.mu AND from snapshot
// paths that must not re-enter it.
func (h *Handler) livePermissionModeLocked(fallback *agent.PermissionManager) agent.PermissionMode {
	if p := h.livePermissionModeOverride.Load(); p != nil {
		return *p
	}
	if fallback != nil {
		return fallback.Mode()
	}
	return agent.PermissionModeNormal
}

// livePermissionModeSnapshot reads the runtime override (lock-free) for
// callers that do not hold h.mu (buildStatusSnapshot). Returns empty when no
// override is set so the caller can apply the config default.
func (h *Handler) livePermissionModeSnapshot() agent.PermissionMode {
	if p := h.livePermissionModeOverride.Load(); p != nil {
		return *p
	}
	return ""
}

// setLivePermissionModeOverride records the runtime override (the mode to
// apply to the process: live agents now, and new sessions at registration).
func (h *Handler) setLivePermissionModeOverride(mode agent.PermissionMode) {
	h.livePermissionModeOverride.Store(&mode)
}

// effectivePermissionBehavior describes what sandbox mode actually does on
// this OS: "confined" (real backend), "degraded_normal" (Windows: prompts like
// normal), and the plain mode name otherwise. Exposed in the permission status
// shape so the UI can surface the Windows degrade honestly. Delegates to the
// agent package — the single source of truth (same seam Decide uses).
func effectivePermissionBehavior(mode agent.PermissionMode) string {
	return agent.EffectivePermissionBehavior(mode)
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

	if err := config.SaveSingleToolRule(req.Tool, req.Level); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Keep in-memory snapshot consistent.
	if h.cfg.Ocode.Permissions.Tools == nil {
		h.cfg.Ocode.Permissions.Tools = map[string]string{}
	}
	h.cfg.Ocode.Permissions.Tools[req.Tool] = req.Level

	for _, a := range h.allAgents() {
		if pm := a.Permissions(); pm != nil {
			pm.SetRule(req.Tool, level)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"tool": req.Tool, "level": req.Level})
}

func (h *Handler) HandleGetYolo(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	enabled := false
	for _, a := range h.allAgents() {
		if pm := a.Permissions(); pm != nil {
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
	mode := agent.PermissionModeNormal
	if req.Enabled {
		mode = agent.PermissionModeYOLO
	}
	h.setLivePermissionModeOverride(mode)
	for _, a := range h.allAgents() {
		if pm := a.Permissions(); pm != nil {
			pm.SetMode(mode)
		}
	}
	h.mu.Unlock()

	// Broadcast the new live mode to connected web clients (status SSE).
	// Safe to run after unlocking: buildStatusSnapshot's live-mode read is
	// lock-free (atomic), and it is also safe from locked callers
	// (HandleSetAdvisor calls pushStatusSnapshot while holding h.mu).
	h.pushStatusSnapshot()
	writeJSON(w, http.StatusOK, map[string]bool{"yolo": req.Enabled})
}

// HandleSetPermissionMode sets the live permission mode on every agent
// (session-scoped, never persisted — matching HandleSetYolo). Valid modes:
// normal, yolo, locked, sandbox. sandbox is the fourth mode; like yolo it is
// live-only (Decision 2) so a restart resumes normal.
func (h *Handler) HandleSetPermissionMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	mode := agent.PermissionMode(req.Mode)
	switch mode {
	case agent.PermissionModeNormal, agent.PermissionModeYOLO, agent.PermissionModeLocked, agent.PermissionModeSandbox:
		// accepted
	default:
		writeError(w, http.StatusBadRequest, "mode must be one of normal, yolo, locked, sandbox")
		return
	}

	// SetMode on live agents is an in-memory mutation (map-lock-speed); it does
	// not touch the disk, so no I/O happens under h.mu.
	h.mu.Lock()
	// Record the process-global runtime override so GET /api/permissions,
	// status snapshots, and any agent registered AFTER this toggle (new tabs,
	// resumed sessions) resolve to the same live mode — "first live agent"
	// would be nondeterministic (map order) and would let a new session
	// silently revert to the config default.
	h.setLivePermissionModeOverride(mode)
	for _, a := range h.allAgents() {
		if pm := a.Permissions(); pm != nil {
			pm.SetMode(mode)
		}
	}
	h.mu.Unlock()

	// Broadcast the new live mode to connected web clients (status SSE). Must
	// run after unlocking: the snapshot path re-enters h.mu.
	h.pushStatusSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{"mode": string(mode)})
}
