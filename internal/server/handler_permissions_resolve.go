package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// PermissionEvent is the `permission` SSE frame emitted on the session mirror
// when a tool call pauses on a PERMISSION_ASK sentinel (headless serve mode,
// where no OnPermissionAsk callback is wired). It carries the fields the web
// PermissionDialog reads (tool + command) plus the rule/summary/deny reason for
// context. RequestID is the paused tool-call ID, which the browser echoes back
// to /api/permissions/resolve.
type PermissionEvent struct {
	RequestID  string `json:"request_id"`
	Tool       string `json:"tool"`
	Command    string `json:"command,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Summary    string `json:"summary,omitempty"`
	DenyReason string `json:"deny_reason,omitempty"`
}

// newPermissionEvent projects a parsed PermissionRequest onto the SSE frame the
// browser renders. Command falls back to the raw args JSON so file/edit tools
// (which carry no Command) still surface what the agent wants to do.
func newPermissionEvent(requestID string, req agent.PermissionRequest) PermissionEvent {
	command := req.Command
	if command == "" && len(req.Args) > 0 {
		command = string(req.Args)
	}
	return PermissionEvent{
		RequestID:  requestID,
		Tool:       req.ToolName,
		Command:    command,
		Rule:       req.Rule,
		Summary:    req.Summary,
		DenyReason: req.DenyReason,
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

// HandleResolvePermission resolves a pending PERMISSION_ASK raised by the agent
// and continues the turn. Body: {request_id, session_id?, approved}. It mirrors
// the TUI approve/deny path (handlePermissionChoice → executeApprovedTool): on
// approval the just-approved tool call is executed via HandleApprovedToolCall
// (which bypasses the permission re-check) and its result replaces the sentinel
// in place; on denial a denied tool result is injected. Either way the turn is
// re-Step'd so the model sees the outcome.
//
// Only works in headless serve mode, where the server owns the agent. In /rc
// bridge mode the TUI owns the agent and its own permission dialog, so it
// returns 409, mirroring HandleAnswerQuestion.
//
// Allow-always is intentionally not supported: the web dialog offers only
// approve/deny, and rule persistence pulls in the harmful-request guard,
// out-of-scope-path handling, and PermissionManager writes that the TUI layers
// on. See TODO.md.
func (h *Handler) HandleResolvePermission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID string `json:"request_id"`
		SessionID string `json:"session_id,omitempty"`
		Approved  bool   `json:"approved"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RequestID == "" {
		writeError(w, http.StatusBadRequest, "request_id is required")
		return
	}

	h.mu.Lock()
	if h.rc != nil {
		h.mu.Unlock()
		writeError(w, http.StatusConflict, "permission answering over the web is not supported while a TUI session is bridged; answer in the TUI")
		return
	}

	// Locate the session whose pending permission ask matches request_id. Prefer
	// the explicit session_id; otherwise scan (tool-call IDs are unique).
	var as *agentSession
	var sessID string
	if req.SessionID != "" {
		if cand, ok := h.agents[req.SessionID]; ok && tailIsPermissionAsk(cand.messages) &&
			cand.messages[len(cand.messages)-1].ToolID == req.RequestID {
			as, sessID = cand, req.SessionID
		}
	} else {
		for id, cand := range h.agents {
			if tailIsPermissionAsk(cand.messages) && cand.messages[len(cand.messages)-1].ToolID == req.RequestID {
				as, sessID = cand, id
				break
			}
		}
	}
	h.mu.Unlock()

	if as == nil {
		writeError(w, http.StatusNotFound, "no pending permission found for request_id")
		return
	}

	as.mu.Lock()
	defer as.mu.Unlock()

	// Re-check under the session lock: another resolver may have raced in.
	if !tailIsPermissionAsk(as.messages) || as.messages[len(as.messages)-1].ToolID != req.RequestID {
		writeError(w, http.StatusConflict, "permission already resolved or superseded")
		return
	}

	last := &as.messages[len(as.messages)-1]
	permReq, ok := parsePermissionAsk(last.Content)
	if !ok {
		writeError(w, http.StatusConflict, "pending permission is not a valid ask")
		return
	}
	working := append([]agent.Message(nil), as.messages...)

	if req.Approved {
		result, err := as.agent.HandleApprovedToolCall(permReq.ToolName, permReq.Args)
		if err != nil {
			result = "Error: " + err.Error()
		}
		working[len(working)-1].Content = agent.TruncateToolResult(req.RequestID, result)
	} else {
		working[len(working)-1].Content = "denied: tool " + permReq.ToolName + " denied by user"
	}

	h.wireHeadlessAgentCallbacks(as.agent)

	resp, err := as.agent.Step(working)
	if err != nil {
		log.Printf("serve error: permission resolve step: %v", err)
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

	_ = session.Save(sessID, "", as.messages, nil)

	// Tell the mirror the dialog can be dismissed, then stream the continuation.
	h.broadcastEvent(SSEEvent{Event: "permission_resolved", Data: map[string]string{"request_id": req.RequestID}})
	h.broadcastEvent(SSEEvent{Event: "messages", Data: as.messages})
	h.broadcastEvent(SSEEvent{Event: "turn_done", Data: DoneEvent{SessionID: sessID, Model: as.model}})

	writeJSON(w, http.StatusOK, ChatResponse{
		Content:   content.String(),
		SessionID: sessID,
		Model:     as.model,
	})
}
