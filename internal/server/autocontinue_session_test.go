package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
)

func putAutoContinue(t *testing.T, h *Handler, enabled bool, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"enabled": enabled}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.HandleSetAutoContinue(rec, httptest.NewRequest("PUT", "/api/config/ocode/autocontinue", bytes.NewReader(raw)))
	return rec
}

func sessionStatusAutoContinue(t *testing.T, h *Handler, id string) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/status", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %s: %d (%s)", id, rec.Code, rec.Body.String())
	}
	var snap TUIStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode status %s: %v", id, err)
	}
	return snap.AutoContinueOn
}

// TestPerSessionAutoContinueToggleIsolation is the regression test for the web
// bug where toggling auto-continue in one chat flipped it for every other chat.
func TestPerSessionAutoContinueToggleIsolation(t *testing.T) {
	h, agents, ids := permModeHandler(t, 2)
	if h.cfg == nil {
		h.cfg = &config.Config{}
	}
	h.cfg.Ocode.AutoContinueEnabled = true
	for _, ag := range agents {
		ag.SetAutoContinueEnabled(true)
	}
	a, b := ids[0], ids[1]

	if !sessionStatusAutoContinue(t, h, a) || !sessionStatusAutoContinue(t, h, b) {
		t.Fatal("baseline: both sessions should report the global default true")
	}

	rec := putAutoContinue(t, h, false, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle A off: %d (%s)", rec.Code, rec.Body.String())
	}
	if agents[0].AutoContinueEnabled() {
		t.Fatal("A live agent should be off")
	}
	if !agents[1].AutoContinueEnabled() {
		t.Fatal("B live agent must be untouched")
	}
	if !h.cfg.Ocode.AutoContinueEnabled {
		t.Fatal("process-wide default must not change on a per-session toggle")
	}
	if sessionStatusAutoContinue(t, h, a) {
		t.Fatal("A status should report off")
	}
	if !sessionStatusAutoContinue(t, h, b) {
		t.Fatal("B status must stay on")
	}

	// Durable: A's override is in transcript metadata, B has none.
	proj := h.sessionProjectRoot(a)
	s, err := session.LoadForDir(proj, a)
	if err != nil {
		t.Fatalf("reload A: %v", err)
	}
	if v, ok := s.Metadata["auto_continue_enabled"].(bool); !ok || v {
		t.Fatalf("A metadata auto_continue_enabled = %v, want false", s.Metadata["auto_continue_enabled"])
	}
	sb, err := session.LoadForDir(h.sessionProjectRoot(b), b)
	if err != nil {
		t.Fatalf("reload B: %v", err)
	}
	if _, ok := sb.Metadata["auto_continue_enabled"]; ok {
		t.Fatalf("B must not have an auto-continue override, got %v", sb.Metadata["auto_continue_enabled"])
	}

	// Eviction (server restart / LRU): resolution falls back to persisted override.
	delete(h.agents, a)
	if h.effectiveSessionAutoContinueEnabled(a, true) {
		t.Fatal("after eviction A should resolve off from persisted metadata")
	}
	if h.autoContinueSeed(a, true) {
		t.Fatal("autoContinueSeed(A, true) should be the persisted override (off)")
	}
}

// enabled must not be silently dropped when the same request also carries a
// model/clear: the per-session toggle applies, the global default is untouched.
func TestPerSessionAutoContinueEnabledAppliesAlongsideClear(t *testing.T) {
	h, agents, ids := permModeHandler(t, 2)
	if h.cfg == nil {
		h.cfg = &config.Config{}
	}
	h.cfg.Ocode.AutoContinueEnabled = true
	for _, ag := range agents {
		ag.SetAutoContinueEnabled(true)
	}
	raw, _ := json.Marshal(map[string]any{"enabled": false, "session_id": ids[0], "clear": true})
	rec := httptest.NewRecorder()
	h.HandleSetAutoContinue(rec, httptest.NewRequest("PUT", "/api/config/ocode/autocontinue", bytes.NewReader(raw)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d (%s)", rec.Code, rec.Body.String())
	}
	if agents[0].AutoContinueEnabled() {
		t.Fatal("session enabled=false was dropped when combined with clear")
	}
	if !agents[1].AutoContinueEnabled() || !h.cfg.Ocode.AutoContinueEnabled {
		t.Fatal("other session and global default must be untouched")
	}
}
