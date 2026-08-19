package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// HandleEvents is the single unified SSE endpoint. Every event on the bus is
// streamed as one SSE message whose data is the full envelope JSON
// ({event, project, session_id, seq, data}). The client passes the projects
// it is viewing as ?projects=a,b,c (repeated params also accepted) — this
// drives the subscriber-aware git/spending emitters. On disconnect the
// subscriber is removed cleanly.
func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	projects := parseEventsProjects(r)
	sub := h.bus.Subscribe(projects)
	defer h.bus.Unsubscribe(sub)

	// Ensure the server-push emitters (runs, git, spending) are running while
	// a client is connected. Each loop self-exits ~30s after the last
	// subscriber leaves; the once-guards restart them on the next connection.
	h.startRunsEmitter()
	h.startWatchEmitters()
	h.startLogsEmitter()
	h.startTerminalProcessesEmitter()

	// Immediate connection comment + flush so the client's HTTP request
	// completes its handshake and the response headers are visible before the
	// first event arrives (comment frames are ignored by EventSource).
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	// Heartbeat so proxies/browsers keep the connection alive even when no
	// events flow (comment frames are ignored by EventSource).
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case env := <-sub:
			data, err := json.Marshal(env)
			if err != nil {
				// Marshalling our own envelopes should never fail.
				fmt.Fprintf(w, "event: envelope\ndata: %s\n\n", `{"event":"error","data":{"error":"failed to encode envelope"}}`)
				flusher.Flush()
				continue
			}
			fmt.Fprintf(w, "event: envelope\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// parseEventsProjects extracts the declared project roots from ?projects=.
// Accepts comma-separated values and repeated params; dedupes and drops
// empties.
func parseEventsProjects(r *http.Request) []string {
	var out []string
	seen := make(map[string]bool)
	for _, raw := range r.URL.Query()["projects"] {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
