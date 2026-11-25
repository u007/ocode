package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendStatsPostThenGet(t *testing.T) {
	s := New("localhost:0", "", "", nil)

	body, _ := json.Marshal(map[string]any{
		"window_id":      "main",
		"terminal_count": 3,
		"terminal_lines": 12000,
		"session_count":  2,
		"message_count":  40,
		"message_bytes":  50000,
		"dom_node_count": 8000,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/debug/frontend-stats", bytes.NewReader(body))
	r.Header.Set("X-Ocode-Desktop", "1")
	s.mux.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("POST status = %d, want 204, body=%s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/debug/frontend-stats", nil)
	r2.Header.Set("X-Ocode-Desktop", "1")
	s.mux.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Fatalf("GET status = %d, want 200", w2.Code)
	}

	var samples []frontendStatsSample
	if err := json.Unmarshal(w2.Body.Bytes(), &samples); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	got := samples[0]
	if got.WindowID != "main" || got.TerminalCount != 3 || got.TerminalLines != 12000 ||
		got.SessionCount != 2 || got.MessageCount != 40 || got.MessageBytes != 50000 || got.DOMNodeCount != 8000 {
		t.Fatalf("unexpected sample: %+v", got)
	}
}

func TestFrontendStatsRingCapsRetention(t *testing.T) {
	r := newFrontendStatsRing()
	for i := 0; i < frontendStatsRingCap+10; i++ {
		r.add(frontendStatsSample{DOMNodeCount: i})
	}
	got := r.snapshot()
	if len(got) != frontendStatsRingCap {
		t.Fatalf("len(snapshot) = %d, want %d", len(got), frontendStatsRingCap)
	}
	// Oldest 10 should have been evicted — the first retained sample is index 10.
	if got[0].DOMNodeCount != 10 {
		t.Fatalf("got[0].DOMNodeCount = %d, want 10", got[0].DOMNodeCount)
	}
}

func TestFrontendStatsPostRejectsInvalidJSON(t *testing.T) {
	s := New("localhost:0", "", "", nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/debug/frontend-stats", bytes.NewReader([]byte("not json")))
	r.Header.Set("X-Ocode-Desktop", "1")
	s.mux.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestFrontendStatsRequiresAuthAndDesktopHeader(t *testing.T) {
	s := New("localhost:0", "user", "secret", nil)
	body := bytes.NewBufferString(`{"window_id":"main"}`)

	for _, tc := range []struct {
		name   string
		header string
		want   int
	}{
		{name: "unauthenticated", header: "", want: 401},
		{name: "not desktop", header: "Bearer secret", want: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/debug/frontend-stats", bytes.NewReader(body.Bytes()))
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestFrontendStallLogged(t *testing.T) {
	s := New("localhost:0", "", "", nil)
	post := func(header string, body map[string]any) int {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/debug/frontend-stall", bytes.NewReader(b))
		if header != "" {
			r.Header.Set("X-Ocode-Desktop", header)
		}
		s.mux.ServeHTTP(w, r)
		return w.Code
	}
	ok := map[string]any{"window_id": "main", "stall_ms": 9800, "dom_node_count": 18000, "active_compactions": 2}
	if got := post("1", ok); got != 204 {
		t.Fatalf("valid stall status = %d, want 204", got)
	}
	if got := post("", ok); got != 404 {
		t.Fatalf("non-desktop status = %d, want 404", got)
	}
	if got := post("1", map[string]any{"window_id": "", "stall_ms": 1}); got != 400 {
		t.Fatalf("missing window_id status = %d, want 400", got)
	}
	if got := post("1", map[string]any{"window_id": "main", "stall_ms": -1}); got != 400 {
		t.Fatalf("negative stall status = %d, want 400", got)
	}
}

func TestFrontendStallLogsActionAndDetail(t *testing.T) {
	var logs bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(origOut) })

	s := New("localhost:0", "", "", nil)
	b, _ := json.Marshal(map[string]any{
		"window_id": "main", "stall_ms": 300, "dom_node_count": 10, "active_compactions": 0,
		"action": "model-dialog-open", "detail": "commit=1 rows=2",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/debug/frontend-stall", bytes.NewReader(b))
	r.Header.Set("X-Ocode-Desktop", "1")
	s.mux.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("status = %d, want 204, body=%s", w.Code, w.Body.String())
	}
	want := `action="model-dialog-open" detail="commit=1 rows=2"`
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("log missing %s; log=%q", want, logs.String())
	}
}

func TestFrontendStallRejectsOverlongActionOrDetail(t *testing.T) {
	s := New("localhost:0", "", "", nil)
	post := func(body map[string]any) int {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/debug/frontend-stall", bytes.NewReader(b))
		r.Header.Set("X-Ocode-Desktop", "1")
		s.mux.ServeHTTP(w, r)
		return w.Code
	}
	base := func() map[string]any {
		return map[string]any{"window_id": "main", "stall_ms": 300}
	}
	longAction := base()
	longAction["action"] = strings.Repeat("a", 201)
	if got := post(longAction); got != 400 {
		t.Fatalf("over-long action status = %d, want 400", got)
	}
	longDetail := base()
	longDetail["detail"] = strings.Repeat("d", 501)
	if got := post(longDetail); got != 400 {
		t.Fatalf("over-long detail status = %d, want 400", got)
	}
}
