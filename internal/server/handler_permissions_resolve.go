package server

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// Permission decisions for POST /api/permissions/resolve. `decision` is the
// new, explicit field; the legacy boolean `approved` maps to allow/deny.
const (
	PermDecisionAllow      = "allow"
	PermDecisionDeny       = "deny"
	PermDecisionAlwaysRule = "always_rule" // persist the narrowest matching rule
	PermDecisionAlwaysTool = "always_tool" // allow ALL future uses of the tool
)

// validPermDecision reports whether s is a decision value this endpoint
// accepts.
func validPermDecision(s string) bool {
	switch s {
	case PermDecisionAllow, PermDecisionDeny, PermDecisionAlwaysRule, PermDecisionAlwaysTool:
		return true
	}
	return false
}

// PermissionEvent is the `permission` SSE frame emitted on the session mirror
// when a tool call pauses on a PERMISSION_ASK sentinel (headless serve mode,
// where no OnPermissionAsk callback is wired). It carries the fields the web
// PermissionDialog reads (tool + command) plus the rule/summary/deny reason for
// context. Scope/Prefix/OutOfScopePath drive the always-allow button
// availability rules (parity with the TUI dialog). RequestID is the paused
// tool-call ID, which the browser echoes back to /api/permissions/resolve.
type PermissionEvent struct {
	RequestID  string `json:"request_id"`
	Tool       string `json:"tool"`
	Command    string `json:"command,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Summary    string `json:"summary,omitempty"`
	DenyReason string `json:"deny_reason,omitempty"`
	// ModelUnavailable mirrors PermissionRequest.ModelUnavailable: the judge
	// never ran, so the browser must not render this as a denial.
	ModelUnavailable string `json:"model_unavailable,omitempty"`
	// Scope/Prefix mirror PermissionRequest.Scope/Prefix so the browser can
	// apply the same always-allow availability rules as the TUI (git prefixes
	// and shell control keywords exclude "always rule"; bash excludes
	// "always tool").
	Scope  string `json:"scope,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	// OutOfScopePath mirrors PermissionRequest.OutOfScopePath: an "always"
	// answer persists this path root to extra_allowed_paths instead of any
	// bash-prefix/tool rule.
	OutOfScopePath string `json:"out_of_scope_path,omitempty"`
}

// newPermissionEvent projects a parsed PermissionRequest onto the SSE frame the
// browser renders. Command falls back to the raw args JSON so file/edit tools
// (which carry no Command) still surface what the agent wants to do.
func newPermissionEvent(requestID string, req agent.PermissionRequest) PermissionEvent {
	command := req.Command
	if command == "" && len(req.Args) > 0 {
		command = string(req.Args)
	}
	scope := ""
	if req.Scope != "" {
		scope = string(req.Scope)
	}
	return PermissionEvent{
		RequestID:        requestID,
		Tool:             req.ToolName,
		Command:          command,
		Rule:             req.Rule,
		Summary:          req.Summary,
		DenyReason:       req.DenyReason,
		ModelUnavailable: req.ModelUnavailable,
		Scope:            scope,
		Prefix:           req.Prefix,
		OutOfScopePath:   req.OutOfScopePath,
	}
}

// parsePermissionAsk extracts the PermissionRequest from a paused permission
// tool result. Mirrors the TUI's parsePermissionRequest so both UIs read the
// same payload. Returns false when content is not a permission ask.
func parsePermissionAsk(content string) (agent.PermissionRequest, bool) {
	var req agent.PermissionRequest
	payload := strings.TrimPrefix(content, tool.SentinelPermissionAsk)
	if payload == content || strings.TrimSpace(payload) == "" {
		return req, false
	}
	if err := json.Unmarshal([]byte(payload), &req); err != nil || req.ToolName == "" {
		return req, false
	}
	return req, true
}

// isPermissionAskMsg reports whether a single tool-role message is a pending
// permission ask, for findPendingSession's per-message search across a
// trailing round that may contain more than one (see trailingToolRunStart).
func isPermissionAskMsg(m agent.Message) bool {
	return m.Role == "tool" && strings.HasPrefix(m.Content, tool.SentinelPermissionAsk)
}

// HandleResolvePermission resolves a pending PERMISSION_ASK raised by the agent
// and continues the turn. Body:
//
//	{request_id, session_id?, approved?, decision?}
//
// `decision` is one of allow | deny | always_rule | always_tool; the legacy
// boolean `approved` still works and maps to allow/deny. It mirrors the TUI's
// handlePermissionChoice → executeApprovedTool path: on approval the
// just-approved tool call is executed via HandleApprovedToolCall (which
// bypasses the permission re-check) and its result replaces the sentinel in
// place; on denial a denied tool result is injected. Either way the turn is
// re-Step'd so the model sees the outcome.
//
// The always_* decisions route through the same guarded persist path the TUI
// uses: agent.AlwaysRuleChoiceAvailable / AlwaysToolChoiceAvailable gate which
// choices exist at all, agent.IsHarmfulRequest refuses to persist harmful
// operations, out-of-workspace asks persist only the path root to
// extra_allowed_paths, webfetch-domain asks set the session domain cache, and
// everything else lands as a bash-prefix or user-confirmed tool rule in both
// the live PermissionManager and ocodeconfig.json.
//
// The `permission_resolved` SSE frame is broadcast BEFORE the approved call
// runs and the turn re-Steps, so every watching dialog dismisses immediately
// instead of lingering for the length of the continuation round.
//
// Only works in headless serve mode, where the server owns the agent. In /rc
// bridge mode the TUI owns the agent and its own permission dialog, so the
// decision is forwarded over the bridge instead (the TUI applies the same
// guards), mirroring HandleAnswerQuestion.
func (h *Handler) HandleResolvePermission(w http.ResponseWriter, r *http.Request) {
	var bodyReq struct {
		RequestID string `json:"request_id"`
		SessionID string `json:"session_id,omitempty"`
		Approved  *bool  `json:"approved,omitempty"`
		Decision  string `json:"decision,omitempty"`
	}
	if err := readBodyJSON(r, &bodyReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if bodyReq.RequestID == "" {
		writeError(w, http.StatusBadRequest, "request_id is required")
		return
	}
	decision := strings.ToLower(strings.TrimSpace(bodyReq.Decision))
	if decision == "" {
		if bodyReq.Approved == nil {
			writeError(w, http.StatusBadRequest, "decision or approved is required")
			return
		}
		if *bodyReq.Approved {
			decision = PermDecisionAllow
		} else {
			decision = PermDecisionDeny
		}
	}
	if !validPermDecision(decision) {
		writeError(w, http.StatusBadRequest, "invalid decision: "+decision)
		return
	}

	if rc := h.RCBridge(); rc != nil {
		// A TUI session is bridged: the TUI owns the agent, so forward the
		// decision to its RC bridge instead of resolving on the server. The
		// TUI's rcResolveMsg mapping understands deny / always_rule /
		// always_tool (and legacy "allow"); it re-applies every guard locally.
		rcDecision := decision
		if rcDecision == PermDecisionAlwaysRule {
			rcDecision = "always" // legacy alias the TUI has accepted since the Telegram bridge
		}
		if !rc.SendResolution(RCResolution{RequestID: bodyReq.RequestID, Decision: rcDecision}) {
			writeError(w, http.StatusServiceUnavailable, "resolve channel full; try again")
			return
		}
		// No server-side dismissal broadcast here: in bridge mode /api/events
		// streams from the TUI's bridge channel, not headlessSubs, so a
		// broadcastEvent would never reach the web dialog. Dismissal signals:
		// the resolving tab dispatches locally on 200, and every other watcher
		// gets the TUI's own broadcastRC("permission_resolved") once the
		// resolution is applied (auto-tagged with the bridge session id).
		writeJSON(w, http.StatusOK, ChatResponse{})
		return
	}

	// Locate the session whose pending permission ask matches request_id. Prefer
	// the explicit session_id; otherwise scan (tool-call IDs are unique). The
	// session comes back with its lock held, so the tail cannot be resolved out
	// from under us by a racing request. The match can be anywhere in the
	// trailing tool-call round, not just the literal last message — a round
	// that dispatched several tool calls needing approval pauses with more
	// than one unresolved sentinel at once.
	as, sessID := h.findPendingSession(bodyReq.SessionID, bodyReq.RequestID, isPermissionAskMsg)
	if as == nil {
		writeError(w, http.StatusNotFound, "no pending permission found for request_id")
		return
	}
	defer as.mu.Unlock()

	askIdx := -1
	for i := trailingToolRunStart(as.messages); i < len(as.messages); i++ {
		if as.messages[i].ToolID == bodyReq.RequestID && isPermissionAskMsg(as.messages[i]) {
			askIdx = i
			break
		}
	}
	if askIdx < 0 {
		writeError(w, http.StatusConflict, "pending permission is not a valid ask")
		return
	}
	permReq, ok := parsePermissionAsk(as.messages[askIdx].Content)
	if !ok {
		writeError(w, http.StatusConflict, "pending permission is not a valid ask")
		return
	}

	// Always-allow guard rails — identical rules to the TUI dialog, enforced
	// server-side too so a hand-crafted request cannot bypass them.
	if decision == PermDecisionAlwaysRule || decision == PermDecisionAlwaysTool {
		if decision == PermDecisionAlwaysRule && !agent.AlwaysRuleChoiceAvailable(permReq) {
			writeError(w, http.StatusConflict,
				"always-allow rule is not available for this request — it must be approved individually")
			return
		}
		if decision == PermDecisionAlwaysTool && !agent.AlwaysToolChoiceAvailable(permReq) {
			writeError(w, http.StatusConflict,
				"always-allow tool is not available for this request — it must be approved individually")
			return
		}
		if agent.IsHarmfulRequest(permReq) {
			log.Printf("serve: always-allow refused (harmful): session=%s tool=%s", sessID, permReq.ToolName)
			writeError(w, http.StatusConflict,
				"cannot always allow this operation — it is considered harmful and always requires human approval")
			return
		}
		persistAlwaysAllow(decision, permReq, as.agent.Permissions())
	}

	working := append([]agent.Message(nil), as.messages...)

	// Tell every watcher the dialog can be dismissed NOW — before the approved
	// tool runs and before the continuation round. The TUI closes its modal the
	// instant a choice is made; a long-running bash command or a slow model
	// round-trip must not keep the web/desktop dialog on screen.
	h.broadcastEvent(SSEEvent{
		SessionID: sessID,
		Event:     "permission_resolved",
		Data:      map[string]string{"request_id": bodyReq.RequestID},
	})

	if decision != PermDecisionDeny {
		pathRoot := agent.OutOfScopePathRoot(permReq)
		result, err := executeApprovedWithTempPath(as.agent, permReq.ToolName, permReq.Args, bodyReq.RequestID, pathRoot)
		if err != nil {
			result = "Error: " + err.Error()
		}
		working[askIdx].Content = agent.TruncateToolResult(bodyReq.RequestID, result)
	} else {
		working[askIdx].Content = "denied: tool " + permReq.ToolName + " denied by user"
	}

	// The round that raised this ask may have dispatched several tool calls
	// needing approval at once, each pausing with its own sentinel before the
	// user answered any of them. Re-Stepping now would feed the model a
	// mid-transcript tool result that is still raw PERMISSION_ASK: JSON — a
	// malformed tool-call/tool-result pairing that the model has no good way
	// to recover from (typically it retries the call, which raises a brand
	// new ask that looks to the user like the same dialog popping right back
	// up). Instead, persist just this one resolution and wait for the
	// remaining ask(s) — the client already has them queued from the earlier
	// `permission` SSE frames.
	for i := trailingToolRunStart(as.messages); i < len(as.messages); i++ {
		if i != askIdx && isPermissionAskMsg(working[i]) {
			as.messages = working
			_ = h.saveSession(sessID, "", as.messages, nil)
			h.broadcastEvent(SSEEvent{SessionID: sessID, Event: "messages", Data: as.messages})
			writeJSON(w, http.StatusOK, ChatResponse{SessionID: sessID, Model: as.model})
			return
		}
	}

	h.wireHeadlessAgentCallbacks(sessID, as.agent)

	// Mirrors runTurn: turnActive true only while Step actually runs, so a
	// reload during this continuation's streaming can buffer/replay it too
	// (see appendLiveFrame) instead of only covering the turn's first Step.
	h.sessions.setTurnActive(sessID, true)
	defer h.sessions.setTurnActive(sessID, false)
	// A close that arrived while this continuation was running (turnActive
	// true) could not release the agent mid-Step; drain the marker once the
	// continuation unwinds, exactly like the async-job and sync-turn paths.
	defer h.drainPendingClose(sessID)

	resp, err := as.agent.Step(working)
	if err != nil {
		log.Printf("serve error: permission resolve step: %v", err)
		// The approved tool already ran and its result is in `working`; keep it
		// (plus any rounds Step completed) instead of leaving the session on the
		// unresolved sentinel.
		h.commitPartialTranscript(sessID, as, append(working, resp...), true)
		h.broadcastEvent(SSEEvent{
			SessionID: sessID,
			Event:     "error",
			Data:      map[string]string{"error": err.Error()},
		})
		writeError(w, http.StatusInternalServerError, "agent error: "+err.Error())
		return
	}

	as.messages = append(working, resp...)

	var content strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			content.WriteString(m.Content)
		}
	}

	_ = h.saveSession(sessID, "", as.messages, nil)

	// Stream the continuation.
	h.broadcastEvent(SSEEvent{SessionID: sessID, Event: "messages", Data: as.messages})
	h.broadcastEvent(SSEEvent{SessionID: sessID, Event: "turn_done", Data: DoneEvent{SessionID: sessID, Model: as.model}})
	// Refresh the sidebar's Context gauge after the continuation turn grew the
	// transcript (no-op when a TUI bridge owns the status feed).
	h.publishTurnStatusSnapshot(sessID)

	// Post-turn auto-compaction check (mirrors runTurn).
	as.agent.MaybeCompactAsync(as.messages)

	writeJSON(w, http.StatusOK, ChatResponse{
		Content:   content.String(),
		SessionID: sessID,
		Model:     as.model,
	})
}

// executeApprovedWithTempPath wraps HandleApprovedToolCall exactly like the
// TUI's executeApprovedTool: when the ask was an out-of-workspace path, the
// path root is temporarily registered as allowed for the duration of this one
// execution and released afterwards.
func executeApprovedWithTempPath(ag *agent.Agent, toolName string, args json.RawMessage, callID, pathRoot string) (string, error) {
	releaseAfter := false
	if pathRoot != "" {
		releaseAfter = tool.AcquireTemporaryAllowedPath(pathRoot)
	}
	if releaseAfter {
		defer tool.ReleaseTemporaryAllowedPath(pathRoot)
	}
	return ag.HandleApprovedToolCall(toolName, args, callID)
}

// persistAlwaysAllow applies a user's explicit "always allow" decision to the
// live PermissionManager and persists it to config, mirroring the TUI's
// handlePermissionChoice "a"/"t" branches:
//
//   - out-of-workspace path asks persist ONLY the path root to
//     extra_allowed_paths (never a blanket bash-prefix/tool rule);
//   - webfetch-domain asks update the session domain cache (in-memory by
//     design — domain grants are session-scoped);
//   - bash-prefix asks persist a prefix rule;
//   - always_tool (and any other tool-level ask) persists a user-confirmed
//     tool rule.
//
// Config write failures are logged and do not fail the resolution: the
// in-memory rule already governs this session, matching TUI behaviour.
func persistAlwaysAllow(decision string, permReq agent.PermissionRequest, pm *agent.PermissionManager) {
	if pm == nil {
		return
	}

	if decision == PermDecisionAlwaysRule && agent.IsOutOfScopePathRequest(permReq) {
		root := agent.OutOfScopePathRoot(permReq)
		if root == "" {
			return
		}
		cleaned := filepath.Clean(root)
		if !tool.AddExtraAllowedPath(cleaned) {
			return // already registered
		}
		if err := config.SaveExtraAllowedPath(cleaned); err != nil {
			log.Printf("serve: failed to save extra_allowed_paths %q: %v", cleaned, err)
		}
		return
	}

	switch {
	case decision == PermDecisionAlwaysRule && permReq.ToolName == "webfetch" && strings.HasPrefix(permReq.Rule, "webfetch.domain."):
		// Session-scoped by design (same as the TUI): the domain cache is not
		// written back to config.
		pm.SetWebfetchDomain(strings.TrimPrefix(permReq.Rule, "webfetch.domain."), agent.PermissionAllow)
	case decision == PermDecisionAlwaysRule && permReq.Scope == agent.PermissionScopeBashPrefix && permReq.Prefix != "":
		pm.SetBashPrefixRule(permReq.Prefix, agent.PermissionAllow)
		if err := config.SaveSingleBashPrefixRule(permReq.Prefix, string(agent.PermissionAllow)); err != nil {
			log.Printf("serve: failed to save bash prefix rule %q: %v", permReq.Prefix, err)
		}
	default:
		// always_tool, and always_rule on a plain tool-level ask (where both
		// choices persist the same tool-level rule).
		pm.SetUserConfirmedRule(permReq.ToolName, agent.PermissionAllow)
		if err := config.SaveSingleToolRule(permReq.ToolName, string(agent.PermissionAllow)); err != nil {
			log.Printf("serve: failed to save tool rule %q: %v", permReq.ToolName, err)
		}
	}
}
