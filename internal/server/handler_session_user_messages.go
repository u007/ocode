package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/session"
)

// The session-user-messages endpoint backs the web/desktop alt+up/alt+down
// jump between the user's own messages (the counterpart of the TUI's
// user_jump.go).
//
// Why it exists: the web client only holds a tail window of the transcript,
// capped at MAX_SLICE_MESSAGES (400) by the store. A client-side count would
// therefore report a different total than the TUI on the same session — the
// exact divergence the search endpoint (handler_session_search.go) was built
// to remove. Returning the authoritative index list server-side means the web
// "msg 3/17" readout means the same thing the TUI's does.
//
// The response carries indices, not text, for the same reason the search
// endpoint does: the client needs jump targets and a count, and shipping
// message bodies would add payload for no UI benefit. Indices are positions in
// the same post-load array that session.PaginatedLoad slices, so they are
// exactly the pagination indices the client uses to page older history in.
const (
	// sessionUserMessagesDefaultLimit caps returned indices when the caller
	// does not ask for a specific cap.
	sessionUserMessagesDefaultLimit = 5000
	// sessionUserMessagesMaxLimit bounds a caller-supplied cap so one request
	// cannot be asked to materialise an unbounded slice.
	sessionUserMessagesMaxLimit = 50000
)

// sessionUserMessagesResponse is the payload for
// GET /api/sessions/{id}/user-messages.
type sessionUserMessagesResponse struct {
	// Total is the number of countable user messages. It can exceed
	// len(Indices) when Truncated is true.
	Total int `json:"total"`
	// Indices are the positions of those messages, ascending.
	Indices []int `json:"indices"`
	// Truncated reports that the scan stopped at the cap with more messages
	// remaining, so a caller can say "showing first N".
	Truncated bool `json:"truncated"`
	// Scanned is the transcript length the indices refer to. The client uses
	// it to anchor its loaded window against the server array.
	Scanned int `json:"scanned"`
}

// isCountableUserMessage reports whether a persisted transcript message counts
// as one of "the user's messages" for jump navigation.
//
// It mirrors the TUI's isVisibleUserMessage (internal/tui/user_jump.go) so
// both surfaces agree on the denominator. The TUI additionally filters
// msg.transient, which is a render-only flag with no persisted counterpart —
// transient notices are injected into the view and never written to the
// transcript, so nothing here needs to exclude them.
//
// Slash-command echoes are excluded: they are role "user" on the wire, but
// they are chrome (a record that you ran /theme) rather than conversation, and
// counting them would make the readout disagree with what the user perceives
// as their prompts.
func isCountableUserMessage(role, content string) bool {
	if role != "user" {
		return false
	}
	return !strings.HasPrefix(strings.TrimSpace(content), "/")
}

// HandleListSessionUserMessages serves GET /api/sessions/{id}/user-messages
//
// Query params:
//   - limit (optional): cap on returned indices (default 5000, max 50000).
func (h *Handler) HandleListSessionUserMessages(w http.ResponseWriter, r *http.Request, id string) {
	limit := sessionUserMessagesDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > sessionUserMessagesMaxLimit {
		limit = sessionUserMessagesMaxLimit
	}

	// Resolve through the registry so sessions from any registered project
	// work, not just the server workdir (mirrors HandleGetSession).
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Full load (0 = everything). This is the same parse HandleGetSession
	// performs per page request; the slice is then scanned in memory.
	msgs, _, err := session.PaginatedLoad(entry.ProjectRoot, id, 0, 0)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	indices := make([]int, 0, 16)
	total := 0
	truncated := false
	for i, m := range msgs {
		if !isCountableUserMessage(m.Role, m.Content) {
			continue
		}
		total++
		if len(indices) >= limit {
			// Keep counting so Total stays exact (the readout shows the real
			// number of messages), but stop growing the index slice.
			truncated = true
			continue
		}
		indices = append(indices, i)
	}

	writeJSON(w, http.StatusOK, sessionUserMessagesResponse{
		Total:     total,
		Indices:   indices,
		Truncated: truncated,
		Scanned:   len(msgs),
	})
}
