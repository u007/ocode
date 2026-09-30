package server

import (
	"fmt"
	"log"
	"strconv"

	"github.com/u007/ocode/internal/agent"
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

// Advisor per-session metadata keys. These three are always written together by
// persistSessionAdvisorConfig, so they are never partially present: the
// presence of advisorModelMetadataKey is the pin marker for the whole set.
//
// The model is stored QUALIFIED ("provider/model") because that is the single
// string the runtime resolves with (agent.AdvisorConfig.Model); an empty value
// is a real, honored value meaning "use the built-in default", not "unset".
const (
	advisorModelMetadataKey       = "advisor_model"
	advisorClaudeCodeMetadataKey  = "advisor_claude_code"
	advisorCheckpointsMetadataKey = "advisor_checkpoints"
)

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
// gate, model and trigger set. Every session-tagged status snapshot must call
// this: buildStatusSnapshot alone reports the process-wide default, which would
// make a per-session value look global again in the sidebar.
func (h *Handler) applySessionAdvisorFields(snap *TUIStatus, sessionID string) {
	if snap == nil || sessionID == "" {
		return
	}
	snap.AdvisorEnabled = h.effectiveSessionAdvisorEnabled(sessionID, snap.AdvisorEnabled)
	ac := h.effectiveSessionAdvisorConfig(sessionID)
	snap.AdvisorModel = ac.Model
	snap.AdvisorCheckpoints = ac.Checkpoints
}

// globalAdvisorConfig is the process-wide default that seeds a session's first
// resolution. Read under h.mu because HandleSetAdvisor mutates h.cfg.
func (h *Handler) globalAdvisorConfig() agent.AdvisorConfig {
	h.mu.Lock()
	defer h.mu.Unlock()
	return agent.AdvisorConfigFromConfig(h.cfg)
}

// processAdvisorEnabled reads the process-wide advisor gate under h.mu (the
// unscoped PUT writes it there).
func (h *Handler) processAdvisorEnabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.advisorEnabled
}

// sessionAdvisorConfigForDir loads a session's pinned advisor configuration
// from its transcript metadata. ok=false means the session has no pin yet and
// should take the process-wide default the first time it needs one.
func sessionAdvisorConfigForDir(projectRoot, sessionID string) (agent.AdvisorConfig, bool) {
	s, err := session.LoadForDir(projectRoot, sessionID)
	if err != nil || s == nil || s.Metadata == nil {
		return agent.AdvisorConfig{}, false
	}
	raw, ok := s.Metadata[advisorModelMetadataKey]
	if !ok {
		return agent.AdvisorConfig{}, false
	}
	cfg := agent.AdvisorConfig{}
	switch v := raw.(type) {
	case string:
		cfg.Model = v
	case nil:
		// A JSON null round-trips as nil; treat it as the empty (default) model,
		// which is still a pin.
	default:
		return agent.AdvisorConfig{}, false
	}
	if b, ok := s.Metadata[advisorClaudeCodeMetadataKey].(bool); ok {
		cfg.ClaudeCode = b
	}
	if raw, ok := s.Metadata[advisorCheckpointsMetadataKey]; ok {
		cfg.Checkpoints = advisorCheckpointsFromMeta(raw)
	}
	return cfg, true
}

// advisorCheckpointsFromMeta normalizes the stored trigger list. JSON gives
// back []any of strings, and a nil/empty list is a real "no triggers" value
// rather than an absent pin.
func advisorCheckpointsFromMeta(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// sessionAdvisorConfig resolves the owning project root for sessionID and
// loads its pinned configuration. An unresolvable session reports no pin so
// callers use the process default rather than reading unrelated storage.
func (h *Handler) sessionAdvisorConfig(sessionID string) (agent.AdvisorConfig, bool) {
	if sessionID == "" {
		return agent.AdvisorConfig{}, false
	}
	projectRoot := h.sessionProjectRoot(sessionID)
	if projectRoot == "" {
		return agent.AdvisorConfig{}, false
	}
	return sessionAdvisorConfigForDir(projectRoot, sessionID)
}

// persistSessionAdvisorConfig writes a session's advisor configuration to its
// transcript metadata. Metadata-only: a full load→save conflicts with stored
// rows the loader filters out (see session.UpdateMetadataForDir). This NEVER
// touches the global ocodeconfig.json — a per-session write must not be able to
// change what a new chat starts with.
func (h *Handler) persistSessionAdvisorConfig(sessionID string, cfg agent.AdvisorConfig) error {
	projectRoot := h.sessionProjectRoot(sessionID)
	return session.UpdateMetadataForDir(projectRoot, sessionID, func(md map[string]any) {
		md[advisorModelMetadataKey] = cfg.Model
		md[advisorClaudeCodeMetadataKey] = cfg.ClaudeCode
		// Store an empty list, never nil, so "no triggers" is distinguishable
		// from "not pinned" after the round trip.
		if cfg.Checkpoints == nil {
			md[advisorCheckpointsMetadataKey] = []string{}
		} else {
			md[advisorCheckpointsMetadataKey] = cfg.Checkpoints
		}
	})
}

// applySessionAdvisorConfig sets sessionID's live agent configuration (when
// built) and persists the pin. This is the write path for a per-session model or
// trigger change and never touches another session.
//
// The bridged TUI session's agent is owned by the TUI and lives on the bridge,
// so a web toggle reaches it through the bridge; the TUI owns that session's
// persistence, so the server-side metadata write is skipped.
func (h *Handler) applySessionAdvisorConfig(sessionID string, cfg agent.AdvisorConfig) error {
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		if ag := rc.Agent(); ag != nil {
			ag.SetAdvisorConfig(cfg)
		}
		return nil
	}
	// Persist first: if the write fails the live agent is left untouched, so the
	// configuration a session runs with never diverges from the one that
	// survives a restart.
	if err := h.persistSessionAdvisorConfig(sessionID, cfg); err != nil {
		return fmt.Errorf("persist advisor config for session %s: %w", sessionID, err)
	}
	if as := h.lookupAgentSession(sessionID); as != nil && as.agent != nil {
		as.agent.SetAdvisorConfig(cfg)
	}
	return nil
}

// pinSessionAdvisorConfig gives an unpinned session the current process-wide
// default and returns what it now owns. It is CHECK-THEN-SET, never an upsert:
// a session that already has a pin keeps it, which is what makes "changing the
// default only affects new chats" true.
//
// Every path that needs a session's advisor configuration to exist goes through
// this one helper, so build-time and write-time cannot disagree. Races are
// benign: both racers read the same default and would write the same value, and
// UpdateMetadataForDir serializes the read-modify-write per session.
func (h *Handler) pinSessionAdvisorConfig(sessionID string) agent.AdvisorConfig {
	if cfg, ok := h.sessionAdvisorConfig(sessionID); ok {
		return cfg
	}
	// The bridged session is owned by the TUI: don't write its storage, just
	// report the default (the TUI resolves its own).
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		return h.globalAdvisorConfig()
	}
	cfg := h.globalAdvisorConfig()
	if h.sessionProjectRoot(sessionID) == "" {
		// Unresolvable session (a typo, or evicted storage): nothing to pin to,
		// so report the default without a pointless write.
		return cfg
	}
	if err := h.persistSessionAdvisorConfig(sessionID, cfg); err != nil {
		// A failed pin must not be silent: the session keeps working on the
		// default, but a lost write means it could later pick up a different
		// default without the user asking.
		log.Printf("advisor: pin default config for session %s failed: %v", sessionID, err)
	}
	return cfg
}

// advisorConfigSeed is what a freshly built agent for sessionID should start
// with: the session's own pin, or the current default if it has none (pinned on
// the spot). Every build path uses it, so a rebuild reproduces the same model
// and triggers the session started with.
func (h *Handler) advisorConfigSeed(sessionID string) agent.AdvisorConfig {
	if sessionID == "" {
		return h.globalAdvisorConfig()
	}
	return h.pinSessionAdvisorConfig(sessionID)
}

// effectiveSessionAdvisorConfig resolves the advisor configuration actually in
// force for sessionID. Precedence:
//  1. the session's live agent (bridged TUI agent or resident server agent),
//     which holds the authoritative value once built, then
//  2. the session's persisted pin, then
//  3. the process-wide default.
//
// Like effectiveSessionAdvisorEnabled this never writes: a read must not pin.
func (h *Handler) effectiveSessionAdvisorConfig(sessionID string) agent.AdvisorConfig {
	if sessionID == "" {
		return h.globalAdvisorConfig()
	}
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		if ag := rc.Agent(); ag != nil {
			if cfg, ok := ag.ResolvedAdvisorConfig(); ok {
				return cfg
			}
		}
	}
	if as := h.lookupAgentSession(sessionID); as != nil && as.agent != nil {
		if cfg, ok := as.agent.ResolvedAdvisorConfig(); ok {
			return cfg
		}
	}
	if cfg, ok := h.sessionAdvisorConfig(sessionID); ok {
		return cfg
	}
	return h.globalAdvisorConfig()
}
