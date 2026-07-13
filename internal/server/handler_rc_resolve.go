package server

import (
	"net/http"

	"github.com/u007/ocode/internal/tool"
)

// handleRCPermissionResolve receives a permission decision for a paused
// /rc turn from an external client (e.g. the Telegram bot) and forwards it to
// the TUI via the bridge's ResolveCh. It only means "queued", not
// "matched and applied" — the TUI applies it when it next reads the channel.
func (h *Handler) handleRCPermissionResolve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	rc := h.rc
	h.mu.Unlock()
	if rc == nil || rc.ResolveCh == nil {
		writeError(w, http.StatusNotFound, "no active remote-control session")
		return
	}
	// Authentication is enforced by the server's authMiddleware (Bearer, query
	// token, and HTTP Basic Auth), which wraps this route — duplicating the
	// check here would diverge from that logic and miss Basic Auth support.
	var req struct {
		RequestID string `json:"request_id"`
		Decision  string `json:"decision"` // allow | deny | always
	}
	if err := readBodyJSON(r, &req); err != nil || req.RequestID == "" || req.Decision == "" {
		writeError(w, http.StatusBadRequest, "request_id and decision are required")
		return
	}
	switch req.Decision {
	case "allow", "deny", "always":
	default:
		writeError(w, http.StatusBadRequest, "decision must be allow, deny, or always")
		return
	}
	if !rc.SendResolution(RCResolution{RequestID: req.RequestID, Decision: req.Decision}) {
		writeError(w, http.StatusServiceUnavailable, "resolve channel full; try again")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

// handleRCQuestionAnswer receives answers to a paused /rc question prompt from
// an external client and forwards them to the TUI via the bridge's ResolveCh.
func (h *Handler) handleRCQuestionAnswer(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	rc := h.rc
	h.mu.Unlock()
	if rc == nil || rc.ResolveCh == nil {
		writeError(w, http.StatusNotFound, "no active remote-control session")
		return
	}
	// Authentication is enforced by the server's authMiddleware (Bearer, query
	// token, and HTTP Basic Auth), which wraps this route — duplicating the
	// check here would diverge from that logic and miss Basic Auth support.
	var req struct {
		RequestID string                   `json:"request_id"`
		Answers   []tool.QuestionAnswerSet `json:"answers"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.RequestID == "" {
		writeError(w, http.StatusBadRequest, "request_id is required")
		return
	}
	if !rc.SendResolution(RCResolution{RequestID: req.RequestID, Answers: req.Answers}) {
		writeError(w, http.StatusServiceUnavailable, "resolve channel full; try again")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}
