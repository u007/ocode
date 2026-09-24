package server

import (
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/tool"
)

// mcpSessionOverride reads the per-session enabled/disabled override for one
// server, if the session has one. ok is false when the session never toggled
// this server, meaning the caller should fall back to the process-wide config.
func (h *Handler) mcpSessionOverride(sessionID, name string) (enabled, ok bool) {
	if sessionID == "" {
		return false, false
	}
	h.mcpSessionOverridesMu.RLock()
	defer h.mcpSessionOverridesMu.RUnlock()
	perServer, ok := h.mcpSessionOverrides[sessionID]
	if !ok {
		return false, false
	}
	enabled, ok = perServer[name]
	return enabled, ok
}

// setMCPSessionOverride records (or clears) a per-session MCP enable/disable
// override. Passing enabled==nil removes the override so the session follows
// the process-wide config again.
func (h *Handler) setMCPSessionOverride(sessionID, name string, enabled *bool) {
	if sessionID == "" {
		return
	}
	h.mcpSessionOverridesMu.Lock()
	defer h.mcpSessionOverridesMu.Unlock()
	if enabled == nil {
		if perServer, ok := h.mcpSessionOverrides[sessionID]; ok {
			delete(perServer, name)
			if len(perServer) == 0 {
				delete(h.mcpSessionOverrides, sessionID)
			}
		}
		return
	}
	perServer, ok := h.mcpSessionOverrides[sessionID]
	if !ok {
		perServer = make(map[string]bool)
		h.mcpSessionOverrides[sessionID] = perServer
	}
	perServer[name] = *enabled
}

// mcpEnabledForSession resolves whether one MCP server is enabled for a given
// session: its own override wins, otherwise the process-wide config. A nil
// config or a missing server is reported as disabled.
func (h *Handler) mcpEnabledForSession(cfg *config.Config, sessionID, name string) bool {
	if enabled, ok := h.mcpSessionOverride(sessionID, name); ok {
		return enabled
	}
	if cfg == nil {
		return false
	}
	mc, ok := cfg.MCP[name]
	if !ok {
		return false
	}
	return mc.Enabled
}

func (h *Handler) HandleListMCP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()

	if cfg == nil || len(cfg.MCP) == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	// An optional session_id makes the list reflect that chat's per-session
	// overrides (the sidebar passes the active session so its toggle state is
	// accurate for THIS chat), falling back to the process-wide config.
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))

	type mcpEntry struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Enabled bool   `json:"enabled"`
	}
	out := make([]mcpEntry, 0, len(cfg.MCP))
	for name, mc := range cfg.MCP {
		enabled := mc.Enabled
		if sid := sessionID; sid != "" {
			enabled = h.mcpEnabledForSession(cfg, sid, name)
		}
		out = append(out, mcpEntry{Name: name, Type: mc.Type, Enabled: enabled})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

// HandleSetMCPEnabled flips one MCP server on/off. It always persists the
// process-wide config (matching the TUI `/mcp` semantics, so the choice is
// durable and applies to new sessions), and — when the request carries a
// session_id — records a per-session override and rebuilds JUST that session's
// agent so the new tool set takes effect in the current chat without restarting
// the app or disturbing other open chats.
func (h *Handler) HandleSetMCPEnabled(w http.ResponseWriter, r *http.Request, name string, enabled bool) {
	h.mu.Lock()
	cfg := h.cfg
	if cfg == nil {
		h.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	if _, ok := cfg.MCP[name]; !ok {
		h.mu.Unlock()
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	h.mu.Unlock()

	if err := config.SaveMCPEnabled(name, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update the in-memory process-wide config under the handler lock. The next
	// session to boot (and any session that rebuilds) re-enumerates from this.
	h.mu.Lock()
	if h.cfg != nil {
		if cur, ok := h.cfg.MCP[name]; ok {
			cur.Enabled = enabled
			h.cfg.MCP[name] = cur
		}
	}
	h.mu.Unlock()

	// Per-session override + scoped rebuild: only when the caller identified the
	// chat that owns this toggle. Without a session_id the call keeps the legacy
	// process-global behavior (persist + apply process-wide), which is what the
	// Settings form and `/mcp enable|disable` rely on.
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID != "" {
		h.setMCPSessionOverride(sessionID, name, &enabled)
		h.rebuildAgentForMCP(sessionID)
	}

	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"name":       name,
		"status":     state,
		"session_id": sessionID,
	})
}

// rebuildAgentForMCP rebuilds the resident agent for a single session so its
// MCP tool set reflects a just-toggled server. Unlike reconcileProfileAgent it
// forces the rebuild regardless of profile/model state, because the tool set
// (not the client) is what changed. It is a no-op when the session has no live
// agent, or when a turn is currently active: tearing the agent down mid-turn
// would disturb in-flight work, so the rebuild is deferred to the next turn
// (which reconciles on its own).
func (h *Handler) rebuildAgentForMCP(sessionID string) {
	as := h.lookupAgentSession(sessionID)
	if as == nil {
		return
	}
	entry := h.sessions.Lookup(sessionID)
	if entry == nil {
		return
	}
	if h.sessions.IsTurnActive(sessionID) {
		// The running turn keeps its tool set; the next turn will rebuild with
		// the new one. Log so the deferral is visible in the logs tab.
		debuglog.Log.Append(debuglog.Entry{
			Kind:      debuglog.KindMCP,
			Message:   "mcp toggle deferred: turn active for session " + sessionID,
			SessionID: sessionID,
		})
		return
	}
	newAs, stage, err := h.buildAgentSession(sessionID, as.model, as.messages, entry.ProjectRoot)
	if err != nil {
		log.Printf("serve error: rebuild agent for %s after mcp toggle (stage %s): %v", sessionID, stage, err)
		return
	}
	newAs.agent.SetParentAdvisorInFlight(as.agent.AdvisorGuard())
	h.replaceAgentSession(sessionID, newAs)
}

// mcpToolsForSession enumerates MCP tools honoring a session's per-session
// overrides. buildAgentSession uses this instead of the process-wide mcpCache
// so a sidebar toggle takes effect on the toggling chat only. It clones the
// session's effective config, applies the overrides, and re-runs the
// enumeration (bounded by the caller / bootstrap timeout).
func (h *Handler) mcpToolsForSession(cfg *config.Config, sessionID string) ([]tool.Tool, []string) {
	if cfg == nil {
		return nil, nil
	}
	// Fast path: a session with no overrides reuses the process-wide cache, so
	// we pay the extra enumeration only for chats that actually toggled a server.
	if !h.hasMCPSessionOverrides(sessionID) {
		return nil, nil
	}
	effCfg := h.applyMCPSessionOverrides(cfg, sessionID)
	if effCfg == nil {
		return nil, nil
	}
	res := agent.LoadMCPToolsForConfig(effCfg)
	return res.Tools, res.Errors
}

func (h *Handler) hasMCPSessionOverrides(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	h.mcpSessionOverridesMu.RLock()
	defer h.mcpSessionOverridesMu.RUnlock()
	return len(h.mcpSessionOverrides[sessionID]) > 0
}

// applyMCPSessionOverrides returns a shallow copy of cfg whose MCP map has this
// session's per-server overrides applied. The MCP map is cloned so the shared
// process-wide config is never mutated.
func (h *Handler) applyMCPSessionOverrides(cfg *config.Config, sessionID string) *config.Config {
	h.mcpSessionOverridesMu.RLock()
	perServer := h.mcpSessionOverrides[sessionID]
	overrides := make(map[string]bool, len(perServer))
	for k, v := range perServer {
		overrides[k] = v
	}
	h.mcpSessionOverridesMu.RUnlock()

	if len(overrides) == 0 {
		return cfg
	}
	clone := *cfg
	clone.MCP = make(map[string]config.MCPConfig, len(cfg.MCP))
	for k, v := range cfg.MCP {
		clone.MCP[k] = v
	}
	for name, enabled := range overrides {
		mc, ok := clone.MCP[name]
		if !ok {
			continue
		}
		mc.Enabled = enabled
		clone.MCP[name] = mc
	}
	return &clone
}

// clearMCPSessionOverrides drops a session's MCP overrides. Called when a
// session is released so the map does not grow without bound.
func (h *Handler) clearMCPSessionOverrides(sessionID string) {
	if sessionID == "" {
		return
	}
	h.mcpSessionOverridesMu.Lock()
	delete(h.mcpSessionOverrides, sessionID)
	h.mcpSessionOverridesMu.Unlock()
}
