package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// frontendStatsSample is a periodic snapshot pushed by the desktop/web
// renderer. The renderer's OS-level RSS gives no attribution to what's
// actually retaining memory (JS heap vs GPU vs native), so the frontend
// reports application-level counts instead: live xterm.js terminal
// instances and their total scrollback line count, chat session/message
// totals (including the string size of message content, which is where
// provider reasoning-continuation blobs would show up), and DOM node
// count. A vmmap/footprint pass on the WebContent process already ruled
// out GPU/IOAccelerator as the sink (see TODO.md's 2026-08-24 chronic-leak
// investigation), pointing at JS-level or WebCore-native retention instead.
type frontendStatsSample struct {
	ReceivedAt    time.Time `json:"received_at"`
	WindowID      string    `json:"window_id"`
	TerminalCount int       `json:"terminal_count"`
	TerminalLines int       `json:"terminal_lines"`
	SessionCount  int       `json:"session_count"`
	MessageCount  int       `json:"message_count"`
	MessageBytes  int       `json:"message_bytes"`
	DOMNodeCount  int       `json:"dom_node_count"`
}

// frontendStatsRingCap bounds retention to 2 hours at the reporter's 30s
// interval — long enough to capture the ramp into a hang without growing
// unbounded itself.
const frontendStatsRingCap = 240

type frontendStatsRing struct {
	mu      sync.Mutex
	samples []frontendStatsSample
}

func newFrontendStatsRing() *frontendStatsRing {
	return &frontendStatsRing{samples: make([]frontendStatsSample, 0, frontendStatsRingCap)}
}

func (r *frontendStatsRing) add(s frontendStatsSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = append(r.samples, s)
	if len(r.samples) > frontendStatsRingCap {
		r.samples = r.samples[len(r.samples)-frontendStatsRingCap:]
	}
}

func (r *frontendStatsRing) snapshot() []frontendStatsSample {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]frontendStatsSample, len(r.samples))
	copy(out, r.samples)
	return out
}

func (s *Server) handlePostFrontendStats(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Ocode-Desktop") != "1" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var in struct {
		WindowID      string `json:"window_id"`
		TerminalCount int    `json:"terminal_count"`
		TerminalLines int    `json:"terminal_lines"`
		SessionCount  int    `json:"session_count"`
		MessageCount  int    `json:"message_count"`
		MessageBytes  int    `json:"message_bytes"`
		DOMNodeCount  int    `json:"dom_node_count"`
		// DebugNote is a temporary passthrough for ad-hoc frontend
		// instrumentation (e.g. capturing a JS call-stack string) — logged,
		// not persisted in the ring. Remove once the instrumentation that
		// populates it is removed.
		DebugNote string `json:"debug_note,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		log.Printf("frontend-stats: decode request: %v", err)
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if in.WindowID == "" || in.TerminalCount < 0 || in.TerminalLines < 0 ||
		in.SessionCount < 0 || in.MessageCount < 0 || in.MessageBytes < 0 || in.DOMNodeCount < 0 {
		http.Error(w, "invalid stats", http.StatusBadRequest)
		return
	}
	sample := frontendStatsSample{
		ReceivedAt:    time.Now(),
		WindowID:      in.WindowID,
		TerminalCount: in.TerminalCount,
		TerminalLines: in.TerminalLines,
		SessionCount:  in.SessionCount,
		MessageCount:  in.MessageCount,
		MessageBytes:  in.MessageBytes,
		DOMNodeCount:  in.DOMNodeCount,
	}
	s.frontendStats.add(sample)
	log.Printf("frontend-stats: window=%s terminals=%d(%d lines) sessions=%d messages=%d(%dB) dom_nodes=%d",
		sample.WindowID, sample.TerminalCount, sample.TerminalLines, sample.SessionCount,
		sample.MessageCount, sample.MessageBytes, sample.DOMNodeCount)
	if in.DebugNote != "" {
		note := in.DebugNote
		if len(note) > 4000 {
			note = note[:4000]
		}
		log.Printf("frontend-stats: debug_note window=%s: %s", sample.WindowID, note)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetFrontendStats(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Ocode-Desktop") != "1" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, s.frontendStats.snapshot())
}
