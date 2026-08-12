package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/u007/ocode/internal/debuglog"
)

func (h *Handler) HandleLogStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	writeEntries := func(entries []debuglog.Entry) {
		for _, e := range entries {
			entry := map[string]string{
				"kind":    string(e.Kind),
				"message": e.Message,
			}
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}

	// Send existing entries immediately
	entries, cursor := debuglog.Log.SnapshotSince(0)
	writeEntries(entries)
	flusher.Flush()

	// Subscribe to new entries
	notify := debuglog.Log.Notify()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
			var fresh []debuglog.Entry
			fresh, cursor = debuglog.Log.SnapshotSince(cursor)
			writeEntries(fresh)
			flusher.Flush()
		case <-ticker.C:
			// Periodic flush to keep connection alive
			flusher.Flush()
		}
	}
}

// logBusForwardLoop publishes each new debug-log entry on the unified bus
// exactly once. Entries carry the emitting agent's session id (see
// debuglog.Entry.SessionID, agent.Agent.SetSessionID); an empty id means
// process-global (startup, registry loads, background maintenance) and is
// delivered to every subscriber regardless of session — see LogPanel.tsx. It
// polls SnapshotSince on a short ticker instead of listening on debuglog's
// Notify channel: notify is a single shared buffered(1) channel whose
// signals are consumed competitively, and the legacy per-connection
// HandleLogStream loop already listens on it — a second permanent listener
// would steal its wakeups. The existing backlog at startup is not republished;
// bus consumers fetch history via GET /api/logs.
func (h *Handler) logBusForwardLoop(stop <-chan struct{}) {
	cursor := debuglog.Log.Cursor()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			var fresh []debuglog.Entry
			fresh, cursor = debuglog.Log.SnapshotSince(cursor)
			for _, e := range fresh {
				h.bus.Publish("logs", "", e.SessionID, map[string]string{
					"kind":       string(e.Kind),
					"message":    e.Message,
					"session_id": e.SessionID,
				})
			}
		}
	}
}

// HandleGetLogs returns the log backlog, optionally scoped to a session. A
// ?session_id= query param filters to that session's own entries plus
// untagged process-global ones; omitting it returns everything (used by
// tooling/debugging that wants the full backlog).
func (h *Handler) HandleGetLogs(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	entries := debuglog.Log.Snapshot()
	type logEntry struct {
		Kind      string `json:"kind"`
		Message   string `json:"message"`
		SessionID string `json:"session_id,omitempty"`
	}
	result := make([]logEntry, 0, len(entries))
	for _, e := range entries {
		if sessionID != "" && e.SessionID != "" && e.SessionID != sessionID {
			continue
		}
		result = append(result, logEntry{
			Kind:      string(e.Kind),
			Message:   e.Message,
			SessionID: e.SessionID,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// HandleClearLogs clears logs. A ?session_id= query param clears only that
// session's own entries (process-global entries and other sessions'
// entries survive); omitting it clears the whole backlog.
func (h *Handler) HandleClearLogs(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		debuglog.Log.Clear()
	} else {
		debuglog.Log.ClearSession(sessionID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
