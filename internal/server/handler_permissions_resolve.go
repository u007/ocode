package server

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"slices"
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
	RequestID string `json:"request_id"`
	Tool      string `json:"tool"`
	Command   string `json:"command,omitempty"`
	// Args preserves the complete execution parameters independently of Command.
	Args       json.RawMessage `json:"args,omitempty"`
	Rule       string          `json:"rule,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	DenyReason string          `json:"deny_reason,omitempty"`
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
	// UntrustedContent/Source/Summary carry a content-guardrail ask (scope
	// "content") through SSE and the pending_asks reconcile payload. They are
	// omitempty so every ordinary permission frame is byte-identical to before.
	UntrustedContent string `json:"untrusted_content,omitempty"`
	UntrustedSource  string `json:"untrusted_source,omitempty"`
	UntrustedSummary string `json:"untrusted_summary,omitempty"`
	// UntrustedScores / UntrustedFailure carry the per-question judge output and
	// the reason the guardrail could not clear the result, so the browser shows
	// the scores rather than one collapsed verdict.
	UntrustedScores  []agent.ContentGuardScore `json:"untrusted_scores,omitempty"`
	UntrustedFailure string                    `json:"untrusted_failure,omitempty"`
	// AgentName names the sub-agent that raised the ask, and is empty for a
	// main-agent ask. Without it the dialog can only say "a sub-agent asked",
	// which is useless when several are parked at once. omitempty, so every
	// main-agent frame stays byte-identical to before.
	AgentName string `json:"agent_name,omitempty"`
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
		Args:             req.Args,
		Rule:             req.Rule,
		Summary:          req.Summary,
		DenyReason:       req.DenyReason,
		ModelUnavailable: req.ModelUnavailable,
		Scope:            scope,
		Prefix:           req.Prefix,
		OutOfScopePath:   req.OutOfScopePath,
		UntrustedContent: req.UntrustedContent,
		UntrustedSource:  req.UntrustedSource,
		UntrustedSummary: req.UntrustedSummary,
		UntrustedScores:  req.UntrustedScores,
		UntrustedFailure: req.UntrustedFailure,
		AgentName:        req.AgentName,
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

	// ── Sub-agent asks ────────────────────────────────────────────────────────
	// A sub-agent ask is NOT a sentinel in the parent's transcript: the child's
	// goroutine is parked on a channel in its session's own registry, which has
	// its own mutex and never takes as.mu. This branch MUST come before
	// findPendingSession, because that helper takes a BLOCKING as.mu — and a
	// child ask parks while the parent's turn holds exactly that lock, so
	// reaching it first would pin the HTTP connection behind the parked child for
	// the whole park.
	//
	// The branch is also far simpler than the main-agent path below: there is no
	// sentinel to rewrite and no continuation to dispatch. The decision goes
	// straight into the channel the child is already blocked on.
	if h.handleChildPermResolve(w, bodyReq.SessionID, bodyReq.RequestID, decision) {
		return
	}

	// Locate the session whose pending permission ask matches request_id. Prefer
	// the explicit session_id; otherwise scan (tool-call IDs are unique). The
	// session comes back with its lock held, so the tail cannot be resolved out
	// from under us by a racing request. The match can be anywhere in the
	// trailing tool-call round, not just the literal last message — a round
	// that dispatched several tool calls needing approval pauses with more
	// than one unresolved sentinel at once. On success the lock is handed to the
	// background continuation (dispatchAskContinuation); every error path below
	// must therefore unlock as.mu explicitly.
	as, sessID := h.findPendingSession(bodyReq.SessionID, bodyReq.RequestID, isPermissionAskMsg)
	if as == nil {
		writeError(w, http.StatusNotFound, "no pending permission found for request_id")
		return
	}

	askIdx := -1
	for i := trailingToolRunStart(as.messages); i < len(as.messages); i++ {
		if as.messages[i].ToolID == bodyReq.RequestID && isPermissionAskMsg(as.messages[i]) {
			askIdx = i
			break
		}
	}
	if askIdx < 0 {
		as.mu.Unlock()
		writeError(w, http.StatusConflict, "pending permission is not a valid ask")
		return
	}
	permReq, ok := parsePermissionAsk(as.messages[askIdx].Content)
	if !ok {
		as.mu.Unlock()
		writeError(w, http.StatusConflict, "pending permission is not a valid ask")
		return
	}

	// Always-allow guard rails — identical rules to the TUI dialog, enforced
	// server-side too so a hand-crafted request cannot bypass them.
	if decision == PermDecisionAlwaysRule || decision == PermDecisionAlwaysTool {
		if decision == PermDecisionAlwaysRule && !agent.AlwaysRuleChoiceAvailable(permReq) {
			as.mu.Unlock()
			writeError(w, http.StatusConflict,
				"always-allow rule is not available for this request — it must be approved individually")
			return
		}
		if decision == PermDecisionAlwaysTool && !agent.AlwaysToolChoiceAvailable(permReq) {
			as.mu.Unlock()
			writeError(w, http.StatusConflict,
				"always-allow tool is not available for this request — it must be approved individually")
			return
		}
		if agent.IsHarmfulRequest(permReq) {
			log.Printf("serve: always-allow refused (harmful): session=%s tool=%s", sessID, permReq.ToolName)
			as.mu.Unlock()
			writeError(w, http.StatusConflict,
				"cannot always allow this operation — it is considered harmful and always requires human approval")
			return
		}
		h.persistAlwaysAllow(decision, permReq, as.agent.Permissions())
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

	// The approved tool execution and the re-Step run off the request goroutine
	// so the endpoint can acknowledge with 202 immediately: the tool can run for
	// a long time and Step can take minutes, and holding the connection (and the
	// browser's await) for their whole duration is what made the ask dialog's
	// submit look hung with no result.
	model := as.model
	h.dispatchAskContinuation(sessID, as, func() {
		// A content-guardrail ask is about an ALREADY-EXECUTED result. Approval
		// delivers the vetted text and denial withholds it; neither re-runs the
		// tool. Re-executing would issue a second webfetch/MCP call — new,
		// unvetted bytes plus a real side effect — and would discard the very
		// content the user just reviewed.
		//
		// Approval is truncated like any other tool result. The ask deliberately
		// carries the FULL flagged text (the user cannot judge a result is safe
		// without reading it) and ResolveContentAsk returns it verbatim, so
		// without this a >192KB MCP response or fetched page would enter the
		// context whole on the strength of one approval click. Safe here because
		// ResolveContentAsk has already replaced the sentinel, so truncate.go's
		// "never cut an ask" rule does not apply.
		if agent.IsContentAsk(permReq) {
			working[askIdx].Content = agent.TruncateToolResult(bodyReq.RequestID,
				agent.ResolveContentAsk(permReq, decision != PermDecisionDeny))
		} else if decision != PermDecisionDeny {
			pathRoot := agent.OutOfScopePathRoot(permReq)
			result, err := executeApprovedWithTempPathFn(as.agent, permReq.ToolName, permReq.Args, bodyReq.RequestID, pathRoot)
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
		// Mirror the answered sentinel onto disk before anything else persists
		// this transcript (see rewriteAskResult).
		h.rewriteAskResult(sessID, working, askIdx)

		// A re-executed approved call can raise a NEW content-guardrail ask. The
		// guardrail vets the RESULT; the user approved the CALL, not the text it
		// returned, so a second verdict is a new question that needs a new answer.
		// TruncateToolResult passes an ask sentinel through untouched (see
		// truncate.go: cutting an ask makes it unparseable and the question
		// disappears), so the slot resolved above can now hold a fresh sentinel
		// whose payload carries the full unvetted content. Stepping on that would
		// hand the model precisely what the guardrail exists to withhold.
		// The TUI stops on the sentinel prefix for exactly this reason
		// (model.go, the []agent.Message case, which skips askAgent); this is the
		// server-side half of that same guard.
		//
		// parsePermissionAsk — not the bare prefix — decides. Content that merely
		// STARTS with "PERMISSION_ASK:" is ordinary remote text, and treating it
		// as an ask would park the session on a dialog that can never be answered.
		if newAsk, isNewAsk := parsePermissionAsk(working[askIdx].Content); isNewAsk {
			as.messages = working
			// No saveSession here, deliberately: the rewriteAskResult above
			// already wrote THIS row (msgs[seq] is working[askIdx], which by now
			// holds the new sentinel), and working differs from the stored
			// transcript at no other index. A whole-transcript save would be a
			// no-op; the sibling branch below needs one only because it also
			// commits OTHER rows of the round.
			h.broadcastEvent(SSEEvent{SessionID: sessID, Event: "messages", Data: as.messages})
			// A `messages` frame alone renders as tool output, not a dialog, and
			// this ask was raised by an approved re-execution rather than by Step,
			// so the generic sentinel emitter in handler.go never saw it. Without
			// this explicit frame the client never learns the question exists.
			h.broadcastEvent(SSEEvent{SessionID: sessID, Event: "permission",
				Data: newPermissionEvent(working[askIdx].ToolID, newAsk)})
			return
		}

		for i := trailingToolRunStart(as.messages); i < len(as.messages); i++ {
			if i != askIdx && isPermissionAskMsg(working[i]) {
				as.messages = working
				if err := h.saveSession(sessID, "", as.messages, nil); err != nil {
					log.Printf("serve: save after permission resolve for %s: %v", sessID, err)
				}
				h.broadcastEvent(SSEEvent{SessionID: sessID, Event: "messages", Data: as.messages})
				return
			}
		}

		h.wireHeadlessAgentCallbacks(sessID, as.agent)
		h.wireLivePersist(sessID, as, working)
		// Mirrors runTurn: turnActive true only while Step actually runs, so a
		// reload during this continuation's streaming can buffer/replay it too
		// (see appendLiveFrame) instead of only covering the turn's first Step.
		h.sessions.setTurnActive(sessID, true)
		// Step can run for minutes. Publish heartbeats for its duration or the
		// web client's stall watchdog marks the still-running continuation
		// "stalled" (see startTurnHeartbeat). The stop defer is declared last so it
		// runs first, keeping the existing drain/setTurnActive(false) order intact.
		stopHeartbeat := h.startTurnHeartbeat(sessID)
		defer h.sessions.setTurnActive(sessID, false)
		// A close that arrived while this continuation was running (turnActive
		// true) could not release the agent mid-Step; drain the marker once the
		// continuation unwinds, exactly like the async-job and sync-turn paths.
		defer h.drainPendingClose(sessID)
		defer stopHeartbeat()
		// Live agent-loop activity for the web/desktop status bar, same as runTurn
		// (a no-op when a TUI bridge owns the feed). Declared last so it runs first.
		stopActivity := h.startAgentActivityBroadcast(sessID, as.agent)
		defer stopActivity()

		resp, err := as.agent.Step(working)
		if err != nil {
			log.Printf("serve error: permission resolve step: %v", err)
			// The approved tool already ran and its result is in `working`; keep it
			// (plus any rounds Step completed) instead of leaving the session on the
			// unresolved sentinel.
			h.commitPartialTranscript(sessID, as, working, resp, true)
			h.broadcastEvent(SSEEvent{
				SessionID: sessID,
				Event:     "error",
				Data:      map[string]string{"error": err.Error()},
			})
			return
		}

		as.messages = append(append([]agent.Message(nil), working...), resp...)

		h.persistTurnTranscript(sessID, as, len(working), "permission-continuation")

		// Stream the continuation.
		h.broadcastEvent(SSEEvent{SessionID: sessID, Event: "messages", Data: as.messages})
		h.broadcastEvent(SSEEvent{SessionID: sessID, Event: "turn_done", Data: DoneEvent{SessionID: sessID, Model: as.model}})
		// Refresh the sidebar's Context gauge after the continuation turn grew the
		// transcript (no-op when a TUI bridge owns the status feed).
		h.publishTurnStatusSnapshot(sessID)

		// Post-turn auto-compaction check (mirrors runTurn).
		as.agent.MaybeCompactAsync(as.messages)
	})

	writeJSON(w, http.StatusAccepted, ChatResponse{SessionID: sessID, Model: model})
}

// executeApprovedWithTempPathFn is the seam a test replaces to make an
// approved call return an arbitrary result. The one that matters is a
// content-guardrail ask sentinel: the real executor produces one only once the
// guardrail is configured AND its judge returns a flagged verdict, and
// newClientFn (the only lever for that) is unexported in package agent, so a
// server test cannot reach the state through the public surface.
var executeApprovedWithTempPathFn = executeApprovedWithTempPath

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

// handleChildPermResolve resolves a SUB-AGENT permission ask and reports whether
// it owned the request. Returns false (writing nothing) when requestID is not a
// parked child ask, so the caller falls through to the unchanged main-agent path.
//
// Every always-allow guard the main-agent path enforces is enforced here too, in
// the same order, and NOTHING is delivered to the parked child when a guard
// fails — otherwise a hand-crafted resolve could wave an always_* request
// through for a sub-agent (or persist a rule for it) after the dialog hid the
// button for good reason.
//
// The delivered level is a PLAIN allow even for always_rule / always_tool:
// persistAlwaysAllow has already persisted the rule against the session's
// PermissionManager, which sub-agents SHARE with the parent. Deliberately NOT
// PersistRule/PersistTool, because applyPermissionResponse would then install a
// blanket SetUserConfirmedRule(toolName) allow — and for an out-of-scope-path ask
// that is exactly the blanket grant the out-of-scope guard exists to prevent.
// The TUI sends those flags because it persists through its own dialog path and
// answers out-of-scope asks separately; here persistAlwaysAllow is that path.
func (h *Handler) handleChildPermResolve(w http.ResponseWriter, sessionID, requestID, decision string) bool {
	as, ask := h.findChildPermAsk(sessionID, requestID)
	if ask == nil {
		// The id left the registry without this request owning it: already
		// answered (double click / two tabs), auto-denied on timeout or parent
		// cancel, or swept by denyAll. The browser may still be holding it.
		// Answer 404 HERE rather than falling through: findPendingSession takes a
		// blocking as.mu, and this session's turn holds exactly that lock while
		// the child runs on, so the fall-through would pin the HTTP connection
		// for the rest of the park. Same error shape as today's answer for an
		// already-resolved main-agent ask, so the client (which treats 404/409 as
		// "stale, stay dismissed") is unaffected.
		if sessionID != "" {
			if prev := h.lookupAgentSession(sessionID); prev != nil && prev.childAsks.wasResolvedRecently(requestID) {
				writeError(w, http.StatusNotFound, "no pending permission found for request_id")
				return true
			}
		}
		return false
	}
	sessID := sessionID
	if sessID == "" {
		// findChildPermAsk scanned, so recover the owning session's id for the
		// broadcast. Never takes as.mu, so it cannot block behind a parked child.
		if owner := h.sessionIDForChildPerm(requestID); owner != "" {
			sessID = owner
		}
	}

	// Same always-allow guards as the main-agent path, in the same order. Nothing
	// has been removed from the registry yet, so a 409 here leaves the ask live
	// and the dialog still answerable.
	if decision == PermDecisionAlwaysRule || decision == PermDecisionAlwaysTool {
		if decision == PermDecisionAlwaysRule && !agent.AlwaysRuleChoiceAvailable(ask.req) {
			writeError(w, http.StatusConflict,
				"always-allow rule is not available for this request — it must be approved individually")
			return true
		}
		if decision == PermDecisionAlwaysTool && !agent.AlwaysToolChoiceAvailable(ask.req) {
			writeError(w, http.StatusConflict,
				"always-allow tool is not available for this request — it must be approved individually")
			return true
		}
		if agent.IsHarmfulRequest(ask.req) {
			log.Printf("serve: always-allow refused (harmful, sub-agent): session=%s tool=%s", sessID, ask.req.ToolName)
			writeError(w, http.StatusConflict,
				"cannot always allow this operation — it is considered harmful and always requires human approval")
			return true
		}
	}

	// Commit: take() is the single point of removal, so a concurrent second
	// resolve finds nothing and 404s rather than delivering twice.
	if got := as.childAsks.take(requestID); got == nil {
		writeError(w, http.StatusNotFound, "no pending permission found for request_id")
		return true
	}
	as.childAsks.markResolved(requestID)
	if decision == PermDecisionAlwaysRule || decision == PermDecisionAlwaysTool {
		h.persistAlwaysAllow(decision, ask.req, as.agent.Permissions())
	}

	// Dismiss every watcher's dialog NOW, before the child's tool call runs —
	// same ordering rationale as the main-agent path: a long-running approved
	// command must not keep the dialog on screen.
	h.broadcastEvent(SSEEvent{
		SessionID: sessID,
		Event:     "permission_resolved",
		Data:      map[string]string{"request_id": requestID},
	})

	if decision == PermDecisionDeny {
		ask.deliver(agent.PermissionDeny)
	} else {
		ask.deliver(agent.PermissionAllow)
	}
	writeJSON(w, http.StatusOK, ChatResponse{})
	return true
}

// sessionIDForChildPerm returns the id of the live session whose registry holds
// requestID, or "". Used only to recover the session id for the
// permission_resolved broadcast when the client did not send one.
func (h *Handler) sessionIDForChildPerm(requestID string) string {
	as, _ := h.findChildPermAsk("", requestID)
	if as == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, candidate := range h.agents {
		if candidate == as {
			return id
		}
	}
	return ""
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
func (h *Handler) persistAlwaysAllow(decision string, permReq agent.PermissionRequest, pm *agent.PermissionManager) {
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
		// initBuiltinTools resets the process-global allowlist from the config
		// snapshot on every session build, so the grant must live in h.cfg too or
		// the next new session/sub-agent silently drops it and re-asks.
		//
		// NOTE the coupling this creates, because it is not obvious from either
		// end: h.cfg.Ocode.ExtraAllowedPaths is ALSO the root list the SQLite
		// browser's path boundary reads (Handler.pathWithinAllowedRoots, via
		// handler_db.go), so clicking "always allow" on an extra dir permanently
		// widens what GET/POST /api/db/* may open. That surface is read-only
		// (see internal/dbbrowse), and the sidebar's "Extra Dirs" section
		// discloses the live list, but the widening is real — a reader of this
		// function should not have to know it to reason about the grant.
		h.mu.Lock()
		if h.cfg != nil && !slices.ContainsFunc(h.cfg.Ocode.ExtraAllowedPaths, func(p string) bool { return filepath.Clean(p) == cleaned }) {
			h.cfg.Ocode.ExtraAllowedPaths = append(h.cfg.Ocode.ExtraAllowedPaths, cleaned)
		}
		h.mu.Unlock()
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
