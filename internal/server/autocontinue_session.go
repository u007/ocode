package server

import (
	"fmt"
	"strconv"

	"github.com/u007/ocode/internal/session"
)

// autoContinueEnabledMetadataKey is the session-metadata key holding a chat's
// per-session auto-continue on/off override. It mirrors the per-session
// advisor gate (advisorEnabledMetadataKey): the toggle is session-lifetime,
// persisted to the transcript metadata, and never written to global config.
const autoContinueEnabledMetadataKey = "auto_continue_enabled"

// sessionAutoContinueEnabledForDir loads a session's persisted auto-continue
// override from its transcript metadata. ok=false means the session has no
// override and should follow the process-wide default.
func sessionAutoContinueEnabledForDir(projectRoot, sessionID string) (bool, bool) {
	s, err := session.LoadForDir(projectRoot, sessionID)
	if err != nil || s == nil || s.Metadata == nil {
		return false, false
	}
	switch v := s.Metadata[autoContinueEnabledMetadataKey].(type) {
	case bool:
		return v, true
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b, true
		}
	}
	return false, false
}

// sessionAutoContinueEnabled resolves the owning project root for sessionID and
// loads its persisted override. An unresolvable session reports no override so
// callers use the process default.
func (h *Handler) sessionAutoContinueEnabled(sessionID string) (bool, bool) {
	if sessionID == "" {
		return false, false
	}
	projectRoot := h.sessionProjectRoot(sessionID)
	if projectRoot == "" {
		return false, false
	}
	return sessionAutoContinueEnabledForDir(projectRoot, sessionID)
}

// persistSessionAutoContinueEnabled writes (or clears, when enabled == nil) a
// chat's auto-continue override in its transcript metadata.
func (h *Handler) persistSessionAutoContinueEnabled(sessionID string, enabled *bool) error {
	projectRoot := h.sessionProjectRoot(sessionID)
	return session.UpdateMetadataForDir(projectRoot, sessionID, func(md map[string]any) {
		if enabled == nil {
			delete(md, autoContinueEnabledMetadataKey)
			return
		}
		md[autoContinueEnabledMetadataKey] = *enabled
	})
}

// applySessionAutoContinueEnabled sets sessionID's live agent gate (when built)
// and persists the override. Bridged TUI sessions are handled through the
// bridge because the TUI owns that session's agent.
func (h *Handler) applySessionAutoContinueEnabled(sessionID string, enabled bool) error {
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		if ag := rc.Agent(); ag != nil {
			ag.SetAutoContinueEnabled(enabled)
		}
		return nil
	}
	if err := h.persistSessionAutoContinueEnabled(sessionID, &enabled); err != nil {
		return fmt.Errorf("persist auto-continue enabled for session %s: %w", sessionID, err)
	}
	if as := h.lookupAgentSession(sessionID); as != nil && as.agent != nil {
		as.agent.SetAutoContinueEnabled(enabled)
	}
	return nil
}

// autoContinueSeed returns the gate a freshly built agent for sessionID should
// start with: the session's persisted override when present, else fallback (the
// process-wide default).
func (h *Handler) autoContinueSeed(sessionID string, fallback bool) bool {
	if enabled, ok := h.sessionAutoContinueEnabled(sessionID); ok {
		return enabled
	}
	return fallback
}

// effectiveSessionAutoContinueEnabled resolves the auto-continue gate actually in
// force for sessionID. Precedence: live agent → persisted metadata → fallback.
func (h *Handler) effectiveSessionAutoContinueEnabled(sessionID string, fallback bool) bool {
	if sessionID == "" {
		return fallback
	}
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		if ag := rc.Agent(); ag != nil {
			return ag.AutoContinueEnabled()
		}
	}
	if as := h.lookupAgentSession(sessionID); as != nil && as.agent != nil {
		return as.agent.AutoContinueEnabled()
	}
	return h.autoContinueSeed(sessionID, fallback)
}

// autoContinueFlag reads the process-wide auto-continue gate under h.mu.
func (h *Handler) autoContinueFlag() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return false
	}
	return h.cfg.Ocode.AutoContinueEnabled
}

// applySessionAutoContinueFields stamps snap with the session's effective
// auto-continue gate. Every session-tagged status snapshot must call this so
// one chat's toggle does not render on every other tab's sidebar.
func (h *Handler) applySessionAutoContinueFields(snap *TUIStatus, sessionID string) {
	if snap == nil || sessionID == "" {
		return
	}
	snap.AutoContinueOn = h.effectiveSessionAutoContinueEnabled(sessionID, snap.AutoContinueOn)
}
