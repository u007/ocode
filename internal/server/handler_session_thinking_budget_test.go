package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/session"
)

func setSessionThinkingBudget(t *testing.T, h *Handler, id, level string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"level": level})
	rec := httptest.NewRecorder()
	h.HandleSetSessionThinkingBudget(rec, httptest.NewRequest("PUT", "/api/sessions/"+id+"/thinking-budget", bytes.NewReader(raw)), id)
	return rec
}

func sessionStatusThinkingBudget(t *testing.T, h *Handler, id string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/status", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %s: %d, want 200 (%s)", id, rec.Code, rec.Body.String())
	}
	var snap TUIStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode status %s: %v", id, err)
	}
	return snap.ThinkingBudget
}

// TestPerSessionThinkingBudgetIsolation: picking a reasoning level in one chat
// must change only that session's effective budget (status snapshot + persisted
// metadata) and leave every other session on the global config default.
func TestPerSessionThinkingBudgetIsolation(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	h.cfg.ThinkingBudget = 8000

	a := session.NewSessionID()
	b := session.NewSessionID()
	saveSessionToDir(t, proj, a)
	saveSessionToDir(t, proj, b)

	if got := sessionStatusThinkingBudget(t, h, a); got != 8000 {
		t.Fatalf("A baseline thinking_budget = %d, want global default 8000", got)
	}

	rec := setSessionThinkingBudget(t, h, a, "max")
	if rec.Code != http.StatusOK {
		t.Fatalf("set A: %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Budget    int    `json:"budget"`
		Level     string `json:"level"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Budget != 65536 || resp.Level != "max" || resp.SessionID != a {
		t.Fatalf("set response = %+v, want budget 65536 / max / session id", resp)
	}

	if got := sessionStatusThinkingBudget(t, h, a); got != 65536 {
		t.Fatalf("A thinking_budget = %d, want override", got)
	}
	if got := sessionStatusThinkingBudget(t, h, b); got != 8000 {
		t.Fatalf("B thinking_budget = %d, want untouched global default", got)
	}
	if h.cfg.ThinkingBudget != 8000 {
		t.Fatalf("global cfg.ThinkingBudget = %d, want untouched 8000", h.cfg.ThinkingBudget)
	}
	if got := h.effectiveSessionThinkingBudget(a); got != 65536 {
		t.Fatalf("effectiveSessionThinkingBudget(A) = %d, want override", got)
	}

	// Durable: survives reload and a nil-metadata turn save.
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	s, err := session.LoadForDir(proj, a)
	if err != nil {
		t.Fatalf("reload A: %v", err)
	}
	if err := h.saveSession(a, "", s.Messages, nil); err != nil {
		t.Fatalf("nil-metadata turn save: %v", err)
	}
	if got := h.effectiveSessionThinkingBudget(a); got != 65536 {
		t.Fatalf("after nil-metadata save = %d, want override preserved", got)
	}

	// "off" is a real level (budget 0), not "follow global".
	if rec := setSessionThinkingBudget(t, h, a, "off"); rec.Code != http.StatusOK {
		t.Fatalf("set A off: %d (%s)", rec.Code, rec.Body.String())
	}
	if got := sessionStatusThinkingBudget(t, h, a); got != 0 {
		t.Fatalf("A thinking_budget after off = %d, want 0", got)
	}

	// Clear: A falls back to the global default and the metadata key is gone.
	clearRec := httptest.NewRecorder()
	h.HandleClearSessionThinkingBudget(clearRec, httptest.NewRequest("DELETE", "/api/sessions/"+a+"/thinking-budget", nil), a)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear A: %d (%s)", clearRec.Code, clearRec.Body.String())
	}
	if got := sessionStatusThinkingBudget(t, h, a); got != 8000 {
		t.Fatalf("A after clear = %d, want global default", got)
	}
	s, err = session.LoadForDir(proj, a)
	if err != nil {
		t.Fatalf("reload A after clear: %v", err)
	}
	if _, ok := s.Metadata[thinkingBudgetMetadataKey]; ok {
		t.Fatalf("metadata key still present after clear: %v", s.Metadata)
	}
}

func TestSetSessionThinkingBudgetRejectsBadInput(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	a := session.NewSessionID()
	saveSessionToDir(t, proj, a)

	if rec := setSessionThinkingBudget(t, h, a, "turbo"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown level: %d, want 400", rec.Code)
	}
	if rec := setSessionThinkingBudget(t, h, "nope-"+a, "max"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d, want 404", rec.Code)
	}
}
