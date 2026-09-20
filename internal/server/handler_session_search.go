package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// The session-search endpoint backs the web/desktop find bar's full-transcript
// search (the `/search` command and Ctrl/Cmd+F).
//
// Why it exists: the web find bar computes matches client-side over the LOADED
// window, which the store caps at MAX_SLICE_MESSAGES (400) — so on a long
// session Ctrl/Cmd+F silently missed earlier hits, while the TUI (which loads
// the whole transcript) found them. Searching server-side removes that
// divergence without growing client memory.
//
// Cost is bounded: session.PaginatedLoad already parses the ENTIRE session file
// on every page fetch and slices afterwards, so this endpoint adds only a scan
// to a cost the server was already paying (measured ~4.6ms on the largest real
// session in the store: 611 messages / 581KB).
//
// The response carries indices, not text. The find bar needs a count and jump
// targets; shipping snippets would add per-match substring work and payload for
// no UI benefit. Indices are positions in the same post-load message array that
// PaginatedLoad slices, so they are exactly the pagination indices the client
// uses when paging older history in.

const (
	// sessionSearchDefaultLimit caps returned indices when the caller does not
	// ask for a specific cap.
	sessionSearchDefaultLimit = 5000
	// sessionSearchMaxLimit bounds a caller-supplied cap so one request cannot
	// be asked to materialise an unbounded slice.
	sessionSearchMaxLimit = 50000
)

// sessionSearchResponse is the payload for GET /api/sessions/{id}/search.
type sessionSearchResponse struct {
	// Total is the number of messages in the transcript that matched. It can
	// exceed len(Indices) when Truncated is true.
	Total int `json:"total"`
	// Indices are the matching message positions, ascending.
	Indices []int `json:"indices"`
	// Truncated reports that the scan stopped at the cap with more matches
	// remaining, so a caller can say "showing first N".
	Truncated bool `json:"truncated"`
	// Scanned is the transcript length the indices refer to. The client uses it
	// to anchor its loaded window against the server array.
	Scanned int `json:"scanned"`
}

// HandleSearchSession serves GET /api/sessions/{id}/search?q=…&limit=…
//
// Query params:
//   - q (required): case-insensitive substring.
//   - limit (optional): cap on returned indices (default 5000, max 50000).
func (h *Handler) HandleSearchSession(w http.ResponseWriter, r *http.Request, id string) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	limit := sessionSearchDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > sessionSearchMaxLimit {
		limit = sessionSearchMaxLimit
	}

	// Resolve the session's owning project through the registry so sessions
	// from any registered project are searchable, not just the server workdir
	// (mirrors HandleGetSession).
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

	needle := strings.ToLower(query)
	indices := make([]int, 0, 16)
	total := 0
	truncated := false
	for i, m := range msgs {
		if !agent.MessageMatchesQuery(m, needle) {
			continue
		}
		total++
		if len(indices) >= limit {
			// Keep counting so Total stays exact (the counter shows the real
			// number of hits), but stop growing the index slice.
			truncated = true
			continue
		}
		indices = append(indices, i)
	}

	writeJSON(w, http.StatusOK, sessionSearchResponse{
		Total:     total,
		Indices:   indices,
		Truncated: truncated,
		Scanned:   len(msgs),
	})
}
