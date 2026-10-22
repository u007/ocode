package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/agent"
)

// toolDigestBodyMaxBytes caps the explain request. The agent clamps what it
// SENDS to the model, but an unbounded JSON body is still decoded into memory
// on an authenticated endpoint, so cap it before decoding.
const toolDigestBodyMaxBytes = 64 << 10

// HandleToolCallDigest explains a PENDING tool call in plain prose.
//
//	POST /api/sessions/{id}/tool-call-digest
//	{request_id?, tool?, command?, args?, rule?, scope?} -> {summary, model}
//
// The ask dialog's "Explain this tool call" button calls it. Two properties are
// load-bearing:
//
//   - The digest is NOT a verdict. It runs no judge, writes nothing, and cannot
//     approve or deny anything; it only returns text for the human to read.
//   - When request_id names a pending ask in THIS session's transcript, the
//     parameters are taken from that ask (the authoritative copy) and the
//     client-posted fields are ignored. Client fields are only a fallback for
//     asks that are not in the parent transcript (sub-agent asks) or that
//     resolved between render and click.
//
// Errors are real HTTP errors, not a 200 carrying an `error` field: the dialog
// treats a failed explanation as a failed action, and fetchJSON already turns a
// non-2xx body into a message the dialog can render.
func (h *Handler) HandleToolCallDigest(w http.ResponseWriter, r *http.Request, id string) {
	r.Body = http.MaxBytesReader(w, r.Body, toolDigestBodyMaxBytes)
	var body struct {
		RequestID string          `json:"request_id,omitempty"`
		Tool      string          `json:"tool,omitempty"`
		Command   string          `json:"command,omitempty"`
		Args      json.RawMessage `json:"args,omitempty"`
		Rule      string          `json:"rule,omitempty"`
		Scope     string          `json:"scope,omitempty"`
	}
	if err := readBodyJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	in := agent.ToolCallDigestInput{
		Tool:    strings.TrimSpace(body.Tool),
		Command: body.Command,
		Args:    body.Args,
		Rule:    body.Rule,
		Scope:   body.Scope,
	}
	if authoritative, ok := h.pendingAskDigestInput(id, strings.TrimSpace(body.RequestID)); ok {
		in = authoritative
	}
	if in.Tool == "" {
		writeError(w, http.StatusBadRequest, "tool or a pending request_id is required")
		return
	}

	as, err := h.getOrCreateAgentSession(id)
	if err != nil || as == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	// TryLock, never Lock: a running turn holds as.mu for its whole duration, so
	// a blocking read here would pin this HTTP request behind the turn. The
	// dialog is shown while an ask is pending (no turn running), so TryLock
	// normally succeeds; when it does not, failing fast is the honest answer.
	if !as.mu.TryLock() {
		writeError(w, http.StatusConflict, "session is busy; try again when the current turn finishes")
		return
	}
	a := as.agent
	as.mu.Unlock()
	if a == nil {
		writeError(w, http.StatusServiceUnavailable, "session has no agent to explain the call")
		return
	}

	// r.Context() cancels the provider call when the dialog closes or the
	// browser navigates away, so an abandoned click does not keep paying.
	digest, err := a.SummarizeToolCallCtx(r.Context(), in)
	if err != nil {
		log.Printf("serve: tool-call digest failed: session=%s tool=%s err=%v", id, in.Tool, err)
		writeError(w, http.StatusBadGateway, "explain failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"summary": digest.Summary,
		"model":   digest.Model,
	})
}

// pendingAskDigestInput returns the authoritative parameters for a pending
// permission ask, or false when requestID does not name one in this session.
//
// It never blocks: the transcript is read under TryLock, and an ask that is
// already rolling into its continuation returns false so the caller falls back
// to the posted parameters (which the dialog rendered from the same ask).
func (h *Handler) pendingAskDigestInput(sessionID, requestID string) (agent.ToolCallDigestInput, bool) {
	if sessionID == "" || requestID == "" {
		return agent.ToolCallDigestInput{}, false
	}
	as := h.lookupAgentSession(sessionID)
	if as == nil {
		return agent.ToolCallDigestInput{}, false
	}
	if !as.mu.TryLock() {
		return agent.ToolCallDigestInput{}, false
	}
	defer as.mu.Unlock()
	// Scan the whole trailing tool-call round: a round can pause on more than
	// one unresolved ask, so the match is not necessarily the last message.
	for i := trailingToolRunStart(as.messages); i < len(as.messages); i++ {
		m := as.messages[i]
		if m.ToolID != requestID || !isPermissionAskMsg(m) {
			continue
		}
		req, ok := parsePermissionAsk(m.Content)
		if !ok {
			return agent.ToolCallDigestInput{}, false
		}
		return agent.ToolCallDigestInput{
			Tool:    req.ToolName,
			Command: req.Command,
			Args:    req.Args,
			Rule:    req.Rule,
			Scope:   string(req.Scope),
		}, true
	}
	return agent.ToolCallDigestInput{}, false
}
