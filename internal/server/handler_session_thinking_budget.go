package server

import (
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
)

// thinkingBudgetMetadataKey is the session-metadata key holding a chat's
// per-session extended-thinking budget override. Mirrors the per-session
// model ("model") and permission-mode ("permission_mode") overrides: it lives
// in transcript metadata so it survives resume and server restart.
const thinkingBudgetMetadataKey = "thinking_budget"

// sessionThinkingBudget reads a session's persisted thinking-budget override.
// ok=false means the session has no override (follows the config default) or
// cannot be resolved.
func (h *Handler) sessionThinkingBudget(id string) (int, bool) {
	if id == "" {
		return 0, false
	}
	entry, err := h.sessions.Resolve(id)
	if err != nil || entry.ProjectRoot == "" {
		return 0, false
	}
	s, err := session.LoadForDir(entry.ProjectRoot, id)
	if err != nil || s == nil || s.Metadata == nil {
		return 0, false
	}
	// JSON round-trips numbers as float64; an in-memory write may still hold
	// the int the handler stored.
	switch v := s.Metadata[thinkingBudgetMetadataKey].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

// effectiveSessionThinkingBudget returns the session's override when set,
// otherwise the process-wide cfg.ThinkingBudget.
func (h *Handler) effectiveSessionThinkingBudget(id string) int {
	if b, ok := h.sessionThinkingBudget(id); ok {
		return b
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return 0
	}
	return h.cfg.ThinkingBudget
}

// applySessionThinkingBudget stamps snap with the session's effective budget.
// Every session-tagged snapshot must call this: buildStatusSnapshot alone
// reports the process-wide default, which makes a per-session reasoning pick
// look global in the sidebar.
func (h *Handler) applySessionThinkingBudget(snap *TUIStatus, id string) {
	if id == "" {
		return
	}
	snap.ThinkingBudget = h.effectiveSessionThinkingBudget(id)
}

// setSessionThinkingBudgetOverride persists (or clears, when budget < 0) the
// override and returns the budget now in effect.
func (h *Handler) setSessionThinkingBudgetOverride(id string, budget int) (int, error) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		return 0, err
	}
	err = session.UpdateMetadataForDir(entry.ProjectRoot, id, func(md map[string]any) {
		if budget < 0 {
			delete(md, thinkingBudgetMetadataKey)
		} else {
			md[thinkingBudgetMetadataKey] = budget
		}
	})
	if err != nil {
		return 0, err
	}
	return h.effectiveSessionThinkingBudget(id), nil
}

// HandleSetSessionThinkingBudget sets a per-session reasoning level. Body is
// {"level": "med"} with one of config.ThinkingBudgetLabels. The global
// config default is NOT touched. The live agent (if any) picks the new budget
// up on its next turn via reconcileProfileAgent.
func (h *Handler) HandleSetSessionThinkingBudget(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Level string `json:"level"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	level := strings.ToLower(strings.TrimSpace(req.Level))
	budget, ok := config.ThinkingBudgetForLabel(level)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown level, use one of: off, low, med, high, xhigh, max")
		return
	}
	effective, err := h.setSessionThinkingBudgetOverride(id, budget)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h.pushSessionStatusSnapshot(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"budget":     effective,
		"level":      config.ThinkingBudgetLabels[config.ThinkingLevelIndexForBudget(effective)],
		"session_id": id,
	})
}

// HandleClearSessionThinkingBudget removes the override so the session follows
// the process-wide config budget again.
func (h *Handler) HandleClearSessionThinkingBudget(w http.ResponseWriter, r *http.Request, id string) {
	effective, err := h.setSessionThinkingBudgetOverride(id, -1)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h.pushSessionStatusSnapshot(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"budget":     effective,
		"level":      config.ThinkingBudgetLabels[config.ThinkingLevelIndexForBudget(effective)],
		"session_id": id,
	})
}
