package server

import (
	"fmt"
	"strconv"

	"github.com/u007/ocode/internal/session"
)

// advisorEnabledMetadataKey is the session-metadata key holding a chat's
// per-session advisor on/off override. It mirrors the per-session model
// ("model") and permission mode ("permission_mode") overrides, which also live
// in transcript metadata so they survive resume and server restart.
//
// The runtime gate itself (Agent.SetAdvisorEnabled) still wins for a live
// session — this key is what a freshly built agent seeds from and what the
// per-session status snapshot falls back to when no agent is resident.
const advisorEnabledMetadataKey = "advisor_enabled"

// sessionAdvisorEnabledForDir loads a session's persisted advisor override
// from its transcript metadata. ok=false means the session has no override and
// should follow the process-wide default (Handler.advisorEnabled / config).
func sessionAdvisorEnabledForDir(projectRoot, sessionID string) (bool, bool) {
	s, err := session.LoadForDir(projectRoot, sessionID)
	if err != nil || s == nil || s.Metadata == nil {
		return false, false
	}
	switch v := s.Metadata[advisorEnabledMetadataKey].(type) {
	case bool:
		return v, true
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b, true
		}
	}
	return false, false
}

// sessionAdvisorEnabled resolves the owning project root for sessionID and
// loads its persisted override. An unresolvable session reports no override so
// callers use the process default rather than reading an unrelated project's
// storage.
func (h *Handler) sessionAdvisorEnabled(sessionID string) (bool, bool) {
	if sessionID == "" {
		return false, false
	}
	projectRoot := h.sessionProjectRoot(sessionID)
	if projectRoot == "" {
		return false, false
	}
	return sessionAdvisorEnabledForDir(projectRoot, sessionID)
}

// persistSessionAdvisorEnabled writes (or clears, when enabled == nil) a
// chat's advisor override in its transcript metadata. Metadata-only: a full
// load→save conflicts with stored rows the loader filters out (see
// session.UpdateMetadataForDir).
func (h *Handler) persistSessionAdvisorEnabled(sessionID string, enabled *bool) error {
	projectRoot := h.sessionProjectRoot(sessionID)
	return session.UpdateMetadataForDir(projectRoot, sessionID, func(md map[string]any) {
		if enabled == nil {
			delete(md, advisorEnabledMetadataKey)
			return
		}
		md[advisorEnabledMetadataKey] = *enabled
	})
}

// applySessionAdvisorEnabled sets sessionID's live agent gate (when built) and
// persists the override. This is the single write path for the per-session
// advisor gate and never touches another session — the isolation guarantee.
//
// The bridged TUI session's agent is owned by the TUI and lives on the bridge,
// so a web toggle reaches it through the bridge; the TUI owns that session's
// persistence, so the server-side metadata write is skipped.
func (h *Handler) applySessionAdvisorEnabled(sessionID string, enabled bool) error {
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		if ag := rc.Agent(); ag != nil {
			ag.SetAdvisorEnabled(enabled)
		}
		return nil
	}
	// Persist first: if the write fails the live agent is left untouched, so
	// the gate a session runs with never diverges from the one that survives a
	// restart.
	if err := h.persistSessionAdvisorEnabled(sessionID, &enabled); err != nil {
		return fmt.Errorf("persist advisor enabled for session %s: %w", sessionID, err)
	}
	if as := h.lookupAgentSession(sessionID); as != nil && as.agent != nil {
		as.agent.SetAdvisorEnabled(enabled)
	}
	return nil
}

// advisorSeed returns the gate a freshly built agent for sessionID should
// start with: the session's persisted override when present, else fallback
// (the process-wide default). This is what makes a toggle survive resume and
// server restart — every build path (bootstrap, profile reconcile, plugin
// reload) re-seeds from the same metadata.
func (h *Handler) advisorSeed(sessionID string, fallback bool) bool {
	if enabled, ok := h.sessionAdvisorEnabled(sessionID); ok {
		return enabled
	}
	return fallback
}

// effectiveSessionAdvisorEnabled resolves the advisor gate actually in force
// for sessionID. Precedence:
//  1. the session's live agent (bridged TUI agent or resident server agent),
//     which holds the authoritative value once built, then
//  2. the session's persisted metadata override (survives restart/eviction),
//     then
//  3. fallback — the process-wide default from config.
//
// Agent.AdvisorEnabled is an atomic read and lookupAgentSession takes only
// h.mu, so this is safe from the snapshot builders (which hold as.mu, per the
// as.mu → h.mu lock order).
func (h *Handler) effectiveSessionAdvisorEnabled(sessionID string, fallback bool) bool {
	if sessionID == "" {
		return fallback
	}
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		if ag := rc.Agent(); ag != nil {
			return ag.AdvisorEnabled()
		}
	}
	if as := h.lookupAgentSession(sessionID); as != nil && as.agent != nil {
		return as.agent.AdvisorEnabled()
	}
	return h.advisorSeed(sessionID, fallback)
}

// applySessionAdvisorFields stamps snap with the session's effective advisor
// gate. Every session-tagged status snapshot must call this: buildStatusSnapshot
// alone reports the process-wide default, which would make a per-session
// toggle look global again in the sidebar.
func (h *Handler) applySessionAdvisorFields(snap *TUIStatus, sessionID string) {
	if snap == nil || sessionID == "" {
		return
	}
	snap.AdvisorEnabled = h.effectiveSessionAdvisorEnabled(sessionID, snap.AdvisorEnabled)
}
