package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/agent"
)

// toolDigestBodyMaxBytes caps the explain request before it is decoded. An
// oversize body is refused with 413, never truncated: a clipped fallback would
// explain a call the user did not see in full.
const toolDigestBodyMaxBytes = 1 << 20

// HandleToolCallDigest explains a PENDING tool call as Markdown.
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
// Statuses: 404 ask_unavailable when request_id names no live ask and no usable
// fallback was posted (nothing starts); 409 explanation_superseded when a newer
// run took the same ask (benign for the caller); 413 for an oversize body.
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
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "explanation request is larger than 1 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	requestID := strings.TrimSpace(body.RequestID)
	in := agent.ToolCallDigestInput{
		Tool:    strings.TrimSpace(body.Tool),
		Command: body.Command,
		Args:    body.Args,
		Rule:    body.Rule,
		Scope:   body.Scope,
	}
	// The authoritative ask (or an accepted fallback) is resolved first, so an
	// input that cannot be explained never reaches coordinator admission, a
	// cache lookup or a semaphore slot.
	if authoritative, ok := h.pendingAskDigestInput(id, requestID); ok {
		in = authoritative
	} else if requestID != "" && !usableFallbackDigest(in) {
		writeExpectedErrorCode(w, http.StatusNotFound, "ask_unavailable",
			"this request is no longer pending, and no fallback parameters were posted")
		return
	} else if !usableFallbackDigest(in) {
		writeError(w, http.StatusBadRequest, "tool or a pending request_id is required; a bare tool name needs command or args")
		return
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
	// as.agent is set once when the session is built and never reassigned, so it
	// is read without a lock. The explanation is independent of whether a chat
	// turn holds as.mu: a busy session is never a conflict.
	a := as.agent
	if a == nil {
		writeError(w, http.StatusServiceUnavailable, "session has no agent to explain the call")
		return
	}

	// r.Context() cancels the provider call when the dialog closes or the
	// browser navigates away, so an abandoned click does not keep paying.
	digest, err := a.SummarizeToolCallWithKeyCtx(r.Context(), in, requestID)
	if errors.Is(err, agent.ErrToolDigestSuperseded) {
		writeExpectedErrorCode(w, http.StatusConflict, "explanation_superseded",
			"this explanation was replaced by a newer request for the same tool call")
		return
	}
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

// usableFallbackDigest reports whether posted parameters can stand in for an ask
// that is no longer in the transcript: a tool name plus something to explain
// (arguments or a command). A bare tool name explains nothing.
func usableFallbackDigest(in agent.ToolCallDigestInput) bool {
	return in.Tool != "" && (in.Command != "" || len(in.Args) > 0)
}

// invalidateToolDigests drops the explanations owned by as's agent. It reads only
// the agent pointer, which is never reassigned, and takes no session lock, so it
// is safe on every path that already holds as.mu.
func invalidateToolDigests(as *agentSession) {
	if as == nil || as.agent == nil {
		return
	}
	agent.InvalidateToolDigests(as.agent.OpenCodeSessionID())
}

// writeExpectedErrorCode answers a normal dialog race (a stale ask or a
// superseded explanation). It is not logged as "serve error": these are expected
// outcomes, not server faults.
func writeExpectedErrorCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
