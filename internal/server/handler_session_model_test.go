package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/session"
)

// setSessionModel calls the PUT endpoint handler for id with the given model.
func setSessionModel(t *testing.T, h *Handler, id, model string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"model": model})
	rec := httptest.NewRecorder()
	h.HandleSetSessionModel(rec, httptest.NewRequest("PUT", "/api/sessions/"+id+"/model", bytes.NewReader(raw)), id)
	return rec
}

// TestEffectiveSessionModelFallbackConfigReplacement exercises the synchronized
// config replacement path concurrently with fallback model reads. A plain
// expected-value assertion would not catch the h.cfg pointer race that this
// protects against.
func TestEffectiveSessionModelFallbackConfigReplacement(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()
	if cfg == nil {
		t.Skip("no config loaded")
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		for range 20 {
			// Mirror the synchronized h.cfg replacement performed by
			// SetWorkDir without concurrently changing h.workDir, which is
			// intentionally outside this regression's scope.
			h.mu.Lock()
			h.cfg = cfg
			h.mu.Unlock()
		}
	})
	wg.Go(func() {
		for range 200 {
			_ = h.effectiveSessionModel(id)
		}
	})
	wg.Wait()
}

// sessionStatusMainModel fetches the per-session status snapshot and returns
// its main_model.
func sessionStatusMainModel(t *testing.T, h *Handler, id string) string {
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
	return snap.MainModel
}

// TestPerSessionModelOverrideIsolation is the regression test for the sidebar
// bug where picking a model in one chat tab reflected on every other session.
// Setting a model on session A must change only A's effective model (status
// snapshot, context model, persisted metadata) and leave session B on the
// global config default. Clearing A's override must return it to the default.
func TestPerSessionModelOverrideIsolation(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	h.cfg.Model = "openai/global-default"

	a := session.NewSessionID()
	b := session.NewSessionID()
	saveSessionToDir(t, proj, a)
	saveSessionToDir(t, proj, b)

	// Baseline: both sessions follow the global default.
	if got := sessionStatusMainModel(t, h, a); got != "openai/global-default" {
		t.Fatalf("session A baseline main_model = %q, want global default", got)
	}

	rec := setSessionModel(t, h, a, "anthropic/claude-session")
	if rec.Code != http.StatusOK {
		t.Fatalf("set model A: %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Model     string `json:"model"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode set response: %v", err)
	}
	if resp.Model != "anthropic/claude-session" || resp.SessionID != a {
		t.Fatalf("set response = %+v, want effective model + session id echoed", resp)
	}

	// A now reports the override; B is untouched (isolation).
	if got := sessionStatusMainModel(t, h, a); got != "anthropic/claude-session" {
		t.Fatalf("A main_model = %q, want override", got)
	}
	if got := sessionStatusMainModel(t, h, b); got != "openai/global-default" {
		t.Fatalf("B main_model = %q, want untouched global default", got)
	}
	if got := h.effectiveSessionModel(a); got != "anthropic/claude-session" {
		t.Fatalf("effectiveSessionModel(A) = %q, want override", got)
	}

	// The override is durable on disk: it survives a fresh load (the resume /
	// server-restart path) and it also flows into the Context gauge's model.
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	s, err := session.LoadForDir(proj, a)
	if err != nil {
		t.Fatalf("reload A: %v", err)
	}
	if m, _ := s.Metadata["model"].(string); m != "anthropic/claude-session" {
		t.Fatalf("persisted metadata model = %v, want override", s.Metadata["model"])
	}
	// Turn saves pass nil metadata — the append path must preserve the
	// override so it survives subsequent turns, not just the initial write.
	if err := h.saveSession(a, "", s.Messages, nil); err != nil {
		t.Fatalf("nil-metadata turn save: %v", err)
	}
	if got := h.effectiveSessionModel(a); got != "anthropic/claude-session" {
		t.Fatalf("effectiveSessionModel after nil-metadata turn save = %q, want override preserved", got)
	}
	statusRec := httptest.NewRecorder()
	h.HandleSessionStatus(statusRec, httptest.NewRequest("GET", "/api/sessions/"+a+"/status", nil), a)
	var snap TUIStatus
	if err := json.Unmarshal(statusRec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode A status: %v", err)
	}
	if snap.ContextModel != "anthropic/claude-session" {
		t.Fatalf("A context_model = %q, want override to flow into context gauge", snap.ContextModel)
	}

	// Clear: A falls back to the global default and the metadata key is gone.
	clearRec := httptest.NewRecorder()
	h.HandleClearSessionModel(clearRec, httptest.NewRequest("DELETE", "/api/sessions/"+a+"/model", nil), a)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear A: %d, want 200 (%s)", clearRec.Code, clearRec.Body.String())
	}
	if got := sessionStatusMainModel(t, h, a); got != "openai/global-default" {
		t.Fatalf("A main_model after clear = %q, want global default", got)
	}
	s, err = session.LoadForDir(proj, a)
	if err != nil {
		t.Fatalf("reload A after clear: %v", err)
	}
	if _, ok := s.Metadata["model"]; ok {
		t.Fatalf("metadata still holds model override after clear: %v", s.Metadata["model"])
	}
}

// TestSetSessionModelValidation covers the endpoint guards: a model id without
// a provider prefix is rejected (mirroring the global config-model endpoint),
// and an unknown session 404s.
func TestSetSessionModelValidation(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)

	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)

	if rec := setSessionModel(t, h, id, "gpt-4o-mini"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bare model id: %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if rec := setSessionModel(t, h, "ses_missing", "anthropic/claude-x"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d, want 404", rec.Code)
	}
}

// TestHandleChatPersistsNewSessionModel proves the "new chat with model X"
// path is durable: POST /api/chat carrying an explicit model on a brand-new
// session writes the override into the transcript metadata at creation, so
// resume/restart keep the session on its picked model.
func TestHandleChatPersistsNewSessionModel(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	h.cfg.Model = "openai/global-default"

	raw, _ := json.Marshal(map[string]any{
		"content": "hello",
		"model":   "anthropic/claude-first",
		"async":   true,
	})
	rec := httptest.NewRecorder()
	h.HandleChat(rec, httptest.NewRequest("POST", "/api/chat", bytes.NewReader(raw)))
	// 202 once the message is durable; the bootstrap may still fail in the
	// background (no credentials), which is irrelevant to what we assert.
	if rec.Code != http.StatusAccepted {
		t.Fatalf("chat status %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.SessionID == "" {
		t.Fatalf("decode chat response: %v (%s)", err, rec.Body.String())
	}

	if got := h.effectiveSessionModel(resp.SessionID); got != "anthropic/claude-first" {
		t.Fatalf("new session effective model = %q, want the requested model", got)
	}
	s, err := session.LoadForDir(proj, resp.SessionID)
	if err != nil {
		t.Fatalf("load new session: %v", err)
	}
	if m, _ := s.Metadata["model"].(string); m != "anthropic/claude-first" {
		t.Fatalf("persisted metadata model = %v, want the requested model", s.Metadata["model"])
	}
}

// TestHandleChatNewSessionWithoutModelHasNoOverride asserts the flip side: a
// session created without an explicit model carries no override and follows
// the global default — including when the default later changes.
func TestHandleChatNewSessionWithoutModelHasNoOverride(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	h.cfg.Model = "openai/global-default"

	// No "model" in the body: HandleChat resolves the effective model
	// internally (the global default here) and must NOT persist an override.
	raw, _ := json.Marshal(map[string]any{
		"content": "hello",
		"async":   true,
	})
	rec := httptest.NewRecorder()
	h.HandleChat(rec, httptest.NewRequest("POST", "/api/chat", bytes.NewReader(raw)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("chat status %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.SessionID == "" {
		t.Fatalf("decode chat response: %v (%s)", err, rec.Body.String())
	}

	s, err := session.LoadForDir(proj, resp.SessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if _, ok := s.Metadata["model"]; ok {
		t.Fatalf("session without an explicit pick must not persist an override: %v", s.Metadata["model"])
	}
	// Flipping the global default moves this session with it.
	h.cfg.Model = "openai/other-default"
	if got := h.effectiveSessionModel(resp.SessionID); got != "openai/other-default" {
		t.Fatalf("no-override session model = %q, want it to follow the global default", got)
	}
}
