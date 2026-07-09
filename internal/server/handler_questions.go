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

// QuestionEvent is the `question` SSE frame emitted on the session mirror when
// the agent pauses on a `question` tool prompt. It carries the raw
// tool.QuestionPrompt list (header/question/options/multiple) and the tool-call
// ID, which doubles as the request id the browser echoes back to answer. The
// "Something else" free-text option is NOT included here — the client adds it
// automatically, exactly as the TUI does (see questionOtherIndex in
// internal/tui/question_prompt.go).
type QuestionEvent struct {
	RequestID string                `json:"request_id"`
	Questions []tool.QuestionPrompt `json:"questions"`
}

// questionAnswerPayload mirrors the JSON the TUI writes back as the `question`
// tool result (internal/tui/question_prompt.go). The server re-marshals the
// answers the browser sends into this exact shape so the model receives an
// identical tool result regardless of which UI answered.
type questionAnswerPayload struct {
	Header   string                `json:"header,omitempty"`
	Question string                `json:"question"`
	Answers  []questionAnswerValue `json:"answers"`
}

type questionAnswerValue struct {
	Label  string `json:"label"`
	Text   string `json:"text,omitempty"`
	Custom bool   `json:"custom,omitempty"`
}

// parseQuestionAsk extracts the QuestionPrompt list from a paused `question`
// tool result. Mirrors the TUI's parseQuestionPrompt so both UIs read the same
// payload. Returns false when content is not a question prompt.
func parseQuestionAsk(content string) ([]tool.QuestionPrompt, bool) {
	_, payload, found := strings.Cut(content, tool.SentinelQuestionPrompt)
	if !found {
		return nil, false
	}
	payload = strings.TrimSpace(payload)
	if wait := strings.Index(payload, tool.SentinelWaitingForUser); wait >= 0 {
		payload = strings.TrimSpace(payload[:wait])
	}
	if payload == "" {
		return nil, false
	}
	var prompts []tool.QuestionPrompt
	if err := json.Unmarshal([]byte(payload), &prompts); err != nil || len(prompts) == 0 {
		return nil, false
	}
	return prompts, true
}

// isQuestionAsk reports whether a tool result is an unanswered question prompt
// (still carrying the waiting-for-user sentinel).
func isQuestionAsk(content string) bool {
	return strings.HasPrefix(content, tool.SentinelQuestionPrompt) &&
		strings.Contains(content, tool.SentinelWaitingForUser)
}

// tailIsQuestionAsk reports whether the newest message is an unanswered
// question prompt. Any later message means the ask was resolved. Mirror of
// tailIsPermissionAsk in run_states.go.
func tailIsQuestionAsk(msgs []agent.Message) bool {
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	return last.Role == "tool" && isQuestionAsk(last.Content)
}

// applyQuestionAnswer replaces the pending question prompt (a tool result at the
// tail whose ToolID matches requestID) with the answer JSON, in place. It
// returns true when a matching unanswered prompt was found. Replacing in place
// — rather than appending a second tool result like the TUI does and relying on
// session sanitisation — keeps exactly one tool result per tool_use_id, which
// the re-Step sends straight to the provider without a duplicate-id 400.
func applyQuestionAnswer(msgs []agent.Message, requestID, answerJSON string) bool {
	if len(msgs) == 0 {
		return false
	}
	last := &msgs[len(msgs)-1]
	if last.Role != "tool" || last.ToolID != requestID || !isQuestionAsk(last.Content) {
		return false
	}
	last.Content = answerJSON
	return true
}

// HandleAnswerQuestion resolves a pending `question` prompt raised by the agent
// and continues the turn. Body: {request_id, session_id?, answers}. It mirrors
// the TUI answer path (submitQuestionAnswers): inject the selected answers as
// the question tool result, then re-run the agent.
//
// Only works in headless serve mode, where the server owns the agent. In /rc
// bridge mode the TUI owns the agent and its own question dialog; the server has
// no hook to resolve it without TUI changes, so it returns 409 (see TODO.md).
func (h *Handler) HandleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID string                  `json:"request_id"`
		SessionID string                  `json:"session_id,omitempty"`
		Answers   []questionAnswerPayload `json:"answers"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RequestID == "" {
		writeError(w, http.StatusBadRequest, "request_id is required")
		return
	}
	if req.Answers == nil {
		writeError(w, http.StatusBadRequest, "answers is required")
		return
	}

	h.mu.Lock()
	if h.rc != nil {
		h.mu.Unlock()
		writeError(w, http.StatusConflict, "question answering over the web is not supported while a TUI session is bridged; answer in the TUI")
		return
	}

	// Locate the session whose pending question matches request_id. Prefer the
	// explicit session_id; otherwise scan (tool-call IDs are unique).
	var as *agentSession
	var sessID string
	if req.SessionID != "" {
		if cand, ok := h.agents[req.SessionID]; ok && tailIsQuestionAsk(cand.messages) {
			as, sessID = cand, req.SessionID
		}
	} else {
		for id, cand := range h.agents {
			if tailIsQuestionAsk(cand.messages) && cand.messages[len(cand.messages)-1].ToolID == req.RequestID {
				as, sessID = cand, id
				break
			}
		}
	}
	h.mu.Unlock()

	if as == nil {
		writeError(w, http.StatusNotFound, "no pending question found for request_id")
		return
	}

	as.mu.Lock()
	defer as.mu.Unlock()

	answerJSON, err := json.Marshal(req.Answers)
	if err != nil {
		writeError(w, http.StatusBadRequest, "answers are not serializable")
		return
	}
	working := append([]agent.Message(nil), as.messages...)
	if !applyQuestionAnswer(working, req.RequestID, string(answerJSON)) {
		writeError(w, http.StatusConflict, "question already answered or superseded")
		return
	}

	h.wireHeadlessAgentCallbacks(as.agent)

	resp, err := as.agent.Step(working)
	if err != nil {
		log.Printf("serve error: question answer step: %v", err)
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
	h.broadcastEvent(SSEEvent{Event: "question_resolved", Data: map[string]string{"request_id": req.RequestID}})
	h.broadcastEvent(SSEEvent{Event: "messages", Data: as.messages})
	h.broadcastEvent(SSEEvent{Event: "turn_done", Data: DoneEvent{SessionID: sessID, Model: as.model}})

	writeJSON(w, http.StatusOK, ChatResponse{
		Content:   content.String(),
		SessionID: sessID,
		Model:     as.model,
	})
}
