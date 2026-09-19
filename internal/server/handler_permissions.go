package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
)

// allAgents returns every live agent the handler controls: the per-session
// server-owned agents plus, when proxying, the TUI's RC agent. It is used for
// PROCESS-WIDE policy mutations — tool rules and bash-prefix rules, which are
// persisted config shared by every session. It is deliberately NOT used for
// live permission MODES, which are per chat session (see
// HandleSetPermissionMode / applySessionPermissionMode); applying a mode to
// every agent here is exactly the cross-session leak this file no longer has.
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
	// Optional ?session_id= scopes the reported live mode to one chat session.
	// Permission modes are per-session (a yolo/sandbox toggle must never leak
	// into another chat or project), so the web passes its active session id.
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))

	// Build the manager from the config WHILE holding h.mu. LoadFromOcode
	// iterates Permissions.Tools and Bash.Prefixes, and the rule setters
	// (HandleSetPermission / HandleSetBashRule) mutate those same maps under
	// h.mu — so reading them after unlocking is a concurrent map read/write
	// (a Go fatal, not just a race-detector warning). Only the cheap map walk
	// happens under the lock; the session-scoped resolution below re-takes it.
	h.mu.Lock()
	cfg := h.cfg
	if cfg == nil {
		h.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	pm := agent.NewPermissionManager()
	pm.LoadFromOcode(cfg.Ocode.Permissions)
	h.mu.Unlock()

	// The session's live mode wins over the config default: a toggle moves its
	// agent without touching the durable config, and GET must report what is
	// actually in force for that session. Without a session id the config
	// default is the only honest answer. Called with no lock held:
	// effectivePermissionMode reads the RC bridge and agent map (h.mu) and may
	// load session metadata from disk.
	liveMode := h.effectivePermissionMode(sessionID, pm)

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
		"mode":               string(liveMode),
		"auto_allow":         pm.AutoPermissionEnabled(),
		"sandbox_supported":  agent.SandboxSupported(),
		"effective_behavior": effectivePermissionBehavior(liveMode),
		"rules":              rules,
		"bash_rules":         bashRules,
	})
}

// effectivePermissionMode resolves the permission mode actually in force for
// sessionID. Precedence:
//  1. the session's live agent (it holds the authoritative mode once built), then
//  2. the session's persisted metadata override (survives restart/eviction), then
//  3. the supplied fallback manager / config default.
//
// An empty sessionID cannot resolve to a session, so it yields the fallback
// (config default) — the correct answer for process-wide callers like the
// settings form, which must not report one session's toggle as global.
func (h *Handler) effectivePermissionMode(sessionID string, fallback *agent.PermissionManager) agent.PermissionMode {
	if sessionID != "" {
		// The bridged TUI session's agent is owned by the TUI and lives on the
		// bridge, not in h.agents — read it explicitly so the web mirrors the
		// TUI's actual mode.
		if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
			if ag := rc.Agent(); ag != nil {
				if pm := ag.Permissions(); pm != nil {
					return pm.Mode()
				}
			}
		}
		if as := h.lookupAgentSession(sessionID); as != nil && as.agent != nil {
			if pm := as.agent.Permissions(); pm != nil {
				return pm.Mode()
			}
		}
		if mode, ok := h.sessionPermissionMode(sessionID); ok {
			return mode
		}
	}
	if fallback != nil {
		return fallback.Mode()
	}
	return agent.PermissionModeNormal
}

// sessionPermissionMode reads a session's persisted permission-mode override
// from its transcript metadata. Returns ok=false when the session has no
// override (it follows the config default) or cannot be resolved.
func (h *Handler) sessionPermissionMode(sessionID string) (agent.PermissionMode, bool) {
	if sessionID == "" {
		return "", false
	}
	projectRoot := h.sessionProjectRoot(sessionID)
	if projectRoot == "" {
		// Unresolvable session: LoadForDir("") would fall back to the process
		// workdir and could read an unrelated project's storage. Report no
		// override so the caller uses the config default instead.
		return "", false
	}
	return sessionPermissionModeForDir(projectRoot, sessionID)
}

// sessionPermissionModeForDir loads the permission-mode override from the
// session's metadata under projectRoot. Unknown/empty keys and invalid modes
// report ok=false so callers fall back to the config default.
func sessionPermissionModeForDir(projectRoot, sessionID string) (agent.PermissionMode, bool) {
	s, err := session.LoadForDir(projectRoot, sessionID)
	if err != nil || s == nil || s.Metadata == nil {
		return "", false
	}
	raw, ok := s.Metadata[permissionModeMetadataKey].(string)
	if !ok || raw == "" {
		return "", false
	}
	return normalizePermissionMode(raw)
}

// permissionModeMetadataKey is the session-metadata key holding a chat's
// per-session permission-mode override. Mirrors the per-session model
// override ("model"), which also lives in transcript metadata so it survives
// resume and server restart.
const permissionModeMetadataKey = "permission_mode"

// normalizePermissionMode validates a raw mode string, returning the typed
// mode and true only for the four supported modes.
func normalizePermissionMode(raw string) (agent.PermissionMode, bool) {
	mode := agent.PermissionMode(strings.TrimSpace(strings.ToLower(raw)))
	switch mode {
	case agent.PermissionModeNormal, agent.PermissionModeYOLO, agent.PermissionModeLocked, agent.PermissionModeSandbox:
		return mode, true
	default:
		return "", false
	}
}

// sessionProjectRoot resolves the owning project root for sessionID, or "".
// Used by the permission handlers, which must resolve the session outside
// h.mu (resolution may touch the disk).
func (h *Handler) sessionProjectRoot(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	if entry, err := h.sessions.Resolve(sessionID); err == nil {
		return entry.ProjectRoot
	}
	return ""
}

// persistSessionPermissionMode writes (or clears, when mode == "") a chat's
// permission-mode override into its transcript metadata so it survives resume
// and restart. A metadata-only write — a full load→save would conflict with
// stored rows the loader filters out (see session.UpdateMetadataForDir).
func (h *Handler) persistSessionPermissionMode(sessionID string, mode agent.PermissionMode) error {
	// The bridged TUI session is persisted by the TUI itself; writing here
	// would race/serve the wrong authority. Callers skip persistence for it.
	projectRoot := h.sessionProjectRoot(sessionID)
	return session.UpdateMetadataForDir(projectRoot, sessionID, func(md map[string]any) {
		if mode == "" {
			delete(md, permissionModeMetadataKey)
			return
		}
		md[permissionModeMetadataKey] = string(mode)
	})
}

// applySessionPermissionMode sets sessionID's live agent (when built) and
// persists the override. This is the single write path for per-session
// permission modes. It never touches other sessions — the isolation guarantee.
func (h *Handler) applySessionPermissionMode(sessionID string, mode agent.PermissionMode) error {
	// The bridged TUI session's agent is owned by the TUI and lives on the
	// bridge, not in h.agents; reach it through the bridge so an RC/web toggle
	// still moves the session it targets. The TUI owns that session's
	// persistence, so skip the server-side metadata write.
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		if ag := rc.Agent(); ag != nil {
			if pm := ag.Permissions(); pm != nil {
				pm.SetMode(mode)
			}
		}
		return nil
	}
	// Persist first: if the metadata write fails the live agent is left
	// untouched, so the mode the session runs with never diverges from the
	// one that survives a restart.
	if err := h.persistSessionPermissionMode(sessionID, mode); err != nil {
		return fmt.Errorf("persist permission mode for session %s: %w", sessionID, err)
	}
	if as := h.lookupAgentSession(sessionID); as != nil && as.agent != nil {
		if pm := as.agent.Permissions(); pm != nil {
			pm.SetMode(mode)
		}
	}
	return nil
}

// effectiveSessionPermissionMode is the session-scoped counterpart used by the
// per-session status snapshot builders: it resolves the mode from the live
// agent or the persisted metadata, falling back to the config default.
func (h *Handler) effectiveSessionPermissionMode(sessionID string) agent.PermissionMode {
	// The bridged TUI session is driven by the TUI itself: its live snapshot
	// already carries permission_mode and wins over any local resolution.
	if rc := h.RCBridge(); rc != nil && rc.SessionID == sessionID {
		if live := rc.TUIStatus().PermissionMode; live != "" {
			if mode, ok := normalizePermissionMode(live); ok {
				return mode
			}
		}
	}
	var fallback *agent.PermissionManager
	h.mu.Lock()
	if h.cfg != nil {
		fallback = agent.NewPermissionManager()
		fallback.LoadFromOcode(h.cfg.Ocode.Permissions)
	}
	h.mu.Unlock()
	return h.effectivePermissionMode(sessionID, fallback)
}

// applySessionPermissionFields stamps snap with the session's effective
// permission mode and its derived sandbox support/behavior. Every
// session-tagged status snapshot must call this: buildStatusSnapshot alone
// reports the process-wide config default, which would make a per-session
// yolo/sandbox toggle look global again in the sidebar.
func (h *Handler) applySessionPermissionFields(snap *TUIStatus, sessionID string) {
	if sessionID == "" {
		return
	}
	mode := h.effectiveSessionPermissionMode(sessionID)
	snap.PermissionMode = string(mode)
	snap.PermissionSandboxSupported = agent.SandboxSupported()
	snap.PermissionEffectiveBehavior = effectivePermissionBehavior(mode)
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
	// Session-scoped like HandleGetPermissions: report the mode for the given
	// chat, not whichever agent happened to be first in the map.
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	h.mu.Lock()
	var fallback *agent.PermissionManager
	if h.cfg != nil {
		fallback = agent.NewPermissionManager()
		fallback.LoadFromOcode(h.cfg.Ocode.Permissions)
	}
	h.mu.Unlock()

	mode := h.effectivePermissionMode(sessionID, fallback)
	writeJSON(w, http.StatusOK, map[string]bool{"yolo": mode == agent.PermissionModeYOLO})
}

func (h *Handler) HandleSetYolo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled   bool   `json:"enabled"`
		SessionID string `json:"session_id"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
	}
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errPermissionSessionRequired.Error())
		return
	}

	mode := agent.PermissionModeNormal
	if req.Enabled {
		mode = agent.PermissionModeYOLO
	}
	if err := h.setPermissionModeForSession(sessionID, mode); err != nil {
		writeError(w, permissionModeErrorStatus(err), err.Error())
		return
	}

	// Push a session-tagged status snapshot so only this chat's sidebar
	// updates. Never broadcast process-wide: that is what made one chat's
	// yolo appear on every other tab.
	h.pushSessionStatusSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"yolo": req.Enabled})
}

// HandleSetPermissionMode sets the live permission mode for ONE chat session
// (session-scoped, persisted in the session's metadata so it survives resume
// and restart). Valid modes: normal, yolo, locked, sandbox. It never touches
// another session or project — that isolation is the whole point of the
// endpoint. To change the persisted default a brand-new session starts in, use
// PUT /api/config/ocode/permissions-mode (HandleSetPermissionModeConfig).
func (h *Handler) HandleSetPermissionMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode      string `json:"mode"`
		SessionID string `json:"session_id"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	mode, ok := normalizePermissionMode(req.Mode)
	if !ok {
		writeError(w, http.StatusBadRequest, "mode must be one of normal, yolo, locked, sandbox")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
	}
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errPermissionSessionRequired.Error())
		return
	}
	if err := h.setPermissionModeForSession(sessionID, mode); err != nil {
		writeError(w, permissionModeErrorStatus(err), err.Error())
		return
	}

	// Session-tagged push: only the requesting chat's sidebar re-renders.
	h.pushSessionStatusSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{"mode": string(mode), "session_id": sessionID})
}

// setPermissionModeForSession validates and applies a per-session mode. The
// session id must resolve to a real session (otherwise a typo would silently
// persist an orphan override and report a mode that can never take effect).
func (h *Handler) setPermissionModeForSession(sessionID string, mode agent.PermissionMode) error {
	if sessionID == "" {
		return errPermissionSessionRequired
	}
	if h.sessionProjectRoot(sessionID) == "" {
		return errSessionNotFoundForPermission
	}
	return h.applySessionPermissionMode(sessionID, mode)
}

// errPermissionSessionRequired is returned when a permission-mode mutation
// arrives without a session id. The mode is per-session by design; accepting a
// session-less write would reintroduce the process-global footgun this change
// removes.
var errPermissionSessionRequired = &permissionModeError{"session_id is required (permission modes are per chat session)"}

// errSessionNotFoundForPermission prevents persisting an override for a session
// that does not exist.
var errSessionNotFoundForPermission = &permissionModeError{"session not found"}

type permissionModeError struct{ msg string }

func (e *permissionModeError) Error() string { return e.msg }

// permissionModeErrorStatus maps a setPermissionModeForSession error to an
// HTTP status: the request-shaped permissionModeError values (unknown session)
// are 404, anything else (e.g. the metadata write failing on disk) is a server
// error and must not masquerade as "session not found".
func permissionModeErrorStatus(err error) int {
	var pme *permissionModeError
	if errors.As(err, &pme) {
		return http.StatusNotFound
	}
	slog.Error("permission mode update failed", "error", err)
	return http.StatusInternalServerError
}
