package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// maxViewerStateBody bounds one PUT of the file-preview record. The SPA sends
// a small, client-capped summary (location, selection, visible text), so
// anything larger is a bug or abuse, not a bigger preview.
const maxViewerStateBody = 64 << 10

// viewerSnapshot is the last preview record pushed for one session. Raw is the
// SPA's JSON object, kept unparsed: the tool parses it at read time and ignores
// fields it does not know, so the SPA can add fields without a server change.
type viewerSnapshot struct {
	Raw        json.RawMessage
	ReceivedAt time.Time
}

// HandlePutSessionViewer handles PUT /api/sessions/{id}/viewer. The SPA pushes
// the record on every change to the preview beside a chat, so the viewer_state
// tool can answer without a round trip to the browser.
func (h *Handler) HandlePutSessionViewer(w http.ResponseWriter, r *http.Request, id string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxViewerStateBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			slog.Warn("viewer state: body over limit", "session", id, "limit", maxViewerStateBody)
			writeError(w, http.StatusRequestEntityTooLarge, "viewer state is larger than the 64 KB limit")
			return
		}
		slog.Warn("viewer state: read body", "session", id, "err", err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
		slog.Warn("viewer state: body is not a JSON object", "session", id, "err", err)
		writeError(w, http.StatusBadRequest, "viewer state must be a JSON object")
		return
	}

	h.viewerMu.Lock()
	if h.viewerStates == nil {
		h.viewerStates = make(map[string]viewerSnapshot)
	}
	h.viewerStates[id] = viewerSnapshot{Raw: json.RawMessage(body), ReceivedAt: time.Now()}
	h.viewerMu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteSessionViewer handles DELETE /api/sessions/{id}/viewer: the SPA
// calls it when the preview closes, so the tool reports no preview.
func (h *Handler) HandleDeleteSessionViewer(w http.ResponseWriter, r *http.Request, id string) {
	h.dropViewerState(id)
	w.WriteHeader(http.StatusNoContent)
}

// viewerState returns the last snapshot for a session id, if one was pushed.
func (h *Handler) viewerState(id string) (viewerSnapshot, bool) {
	h.viewerMu.Lock()
	defer h.viewerMu.Unlock()
	snap, ok := h.viewerStates[id]
	return snap, ok
}

// rekeyViewerState moves the snapshot from oldID to newID on /reset-id. A
// record already under newID is replaced: newID is a fresh session id, so this
// only matters for a stale record and the moved one is the live state.
func (h *Handler) rekeyViewerState(oldID, newID string) {
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	h.viewerMu.Lock()
	defer h.viewerMu.Unlock()
	snap, ok := h.viewerStates[oldID]
	if !ok {
		return
	}
	delete(h.viewerStates, oldID)
	h.viewerStates[newID] = snap
}

// dropViewerState forgets a session's snapshot when its tab closes.
func (h *Handler) dropViewerState(id string) {
	h.viewerMu.Lock()
	defer h.viewerMu.Unlock()
	delete(h.viewerStates, id)
}
