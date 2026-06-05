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
			data, _ := json.Marshal(map[string]string{
				"kind":    string(e.Kind),
				"message": e.Message,
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}

	// Send existing entries immediately
	entries := debuglog.Log.Snapshot()
	writeEntries(entries)
	flusher.Flush()
	lastCount := len(entries)

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
			entries := debuglog.Log.Snapshot()
			if len(entries) < lastCount {
				lastCount = 0
			}
			if len(entries) > lastCount {
				writeEntries(entries[lastCount:])
				lastCount = len(entries)
			}
			flusher.Flush()
		case <-ticker.C:
			// Periodic flush to keep connection alive
			flusher.Flush()
		}
	}
}

func (h *Handler) HandleGetLogs(w http.ResponseWriter, r *http.Request) {
	entries := debuglog.Log.Snapshot()
	type logEntry struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	result := make([]logEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, logEntry{
			Kind:    string(e.Kind),
			Message: e.Message,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) HandleClearLogs(w http.ResponseWriter, r *http.Request) {
	debuglog.Log.Clear()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
