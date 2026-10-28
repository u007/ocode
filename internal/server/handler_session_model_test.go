package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/usage"
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

	// Clear: A falls back to the global default. The key itself stays with an
	// empty value — that sentinel is what distinguishes "explicitly follow the
	// default" from a legacy chat with no model metadata (which recovers its own
	// model from the usage ledger instead).
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
	if v, ok := s.Metadata["model"]; !ok || v != "" {
		t.Fatalf("metadata after clear = %v (present=%v), want the empty follow-default sentinel", v, ok)
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

// TestHandleChatPinsNewSessionModelAtCreation is the regression test for the
// "switch chat and it runs the LAST switched model" report. A chat created
// without an explicit pick must be pinned to the process-wide default IN EFFECT
// AT CREATION, so a later default change (which is what pre-seeds a NEW chat)
// cannot retroactively move an existing chat.
func TestHandleChatPinsNewSessionModelAtCreation(t *testing.T) {
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
	if m, _ := s.Metadata["model"].(string); m != "openai/global-default" {
		t.Fatalf("persisted metadata model = %v, want the creation-time default", s.Metadata["model"])
	}
	// The default moving on (a later pick for a NEW chat) must not touch it.
	h.cfg.Model = "openai/other-default"
	if got := h.effectiveSessionModel(resp.SessionID); got != "openai/global-default" {
		t.Fatalf("pinned session model = %q, want the creation-time model", got)
	}
}

// TestEffectiveSessionModelRecoversLegacyModel proves the migration path for
// chats created before every session was pinned: with no override in the
// transcript metadata, the model is recovered from the newest usage-ledger row
// attributed to that chat, so resuming it runs what it actually last ran
// instead of the process-wide default someone picked later.
func TestEffectiveSessionModelRecoversLegacyModel(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	h.cfg.Model = "opencode-go/last-switched"

	legacy := session.NewSessionID()
	saveSessionToDir(t, proj, legacy)
	// No "model" in metadata — this is what a pre-fix chat looks like.
	usage.RecordUsageForSession(time.Now(), legacy, "nemotron-3.5-super-vl:free", "openrouter", 10, 5, 0, 15, 0)
	usage.RecordUsageForSession(time.Now(), legacy, "deepseek-v4.1-flash", "opencode-go", 10, 5, 0, 15, 0)

	if got := h.effectiveSessionModel(legacy); got != "opencode-go/deepseek-v4.1-flash" {
		t.Fatalf("legacy session model = %q, want the newest ledger model for that chat", got)
	}
	if got := sessionStatusMainModel(t, h, legacy); got != "opencode-go/deepseek-v4.1-flash" {
		t.Fatalf("legacy status main_model = %q, want the recovered model", got)
	}

	// A chat with no ledger history and no override still follows the default.
	fresh := session.NewSessionID()
	saveSessionToDir(t, proj, fresh)
	if got := h.effectiveSessionModel(fresh); got != "opencode-go/last-switched" {
		t.Fatalf("session with no history = %q, want the process default", got)
	}
}

// TestClearSessionModelIgnoresLedgerModel pins the "explicit clear still means
// follow the default" contract: the empty sentinel written by the DELETE
// endpoint must suppress the usage-ledger recovery, otherwise clearing a model
// would silently keep the old one.
func TestClearSessionModelIgnoresLedgerModel(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	h.cfg.Model = "openai/global-default"

	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	usage.RecordUsageForSession(time.Now(), id, "claude-sonnet", "anthropic", 1, 1, 0, 2, 0)
	if got := h.effectiveSessionModel(id); got != "anthropic/claude-sonnet" {
		t.Fatalf("pre-clear model = %q, want the ledger model", got)
	}

	clearRec := httptest.NewRecorder()
	h.HandleClearSessionModel(clearRec, httptest.NewRequest("DELETE", "/api/sessions/"+id+"/model", nil), id)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear: %d, want 200 (%s)", clearRec.Code, clearRec.Body.String())
	}
	if got := h.effectiveSessionModel(id); got != "openai/global-default" {
		t.Fatalf("post-clear model = %q, want the global default (ledger ignored)", got)
	}
}

// TestPinSessionModel covers the first-turn pin: a legacy chat is frozen to the
// model it resolved with, while an explicit pick (or the explicit clear
// sentinel) is never overwritten.
func TestPinSessionModel(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)

	legacy := session.NewSessionID()
	saveSessionToDir(t, proj, legacy)
	h.pinSessionModel(legacy, "openai/pinned")
	if got := h.effectiveSessionModel(legacy); got != "openai/pinned" {
		t.Fatalf("legacy after pin = %q, want the pinned model", got)
	}

	picked := session.NewSessionID()
	saveSessionToDir(t, proj, picked)
	if rec := setSessionModel(t, h, picked, "anthropic/chosen"); rec.Code != http.StatusOK {
		t.Fatalf("set model: %d (%s)", rec.Code, rec.Body.String())
	}
	h.pinSessionModel(picked, "openai/pin-must-not-win")
	if got := h.effectiveSessionModel(picked); got != "anthropic/chosen" {
		t.Fatalf("explicit pick overwritten by pin: %q", got)
	}

	cleared := session.NewSessionID()
	saveSessionToDir(t, proj, cleared)
	if _, err := h.setSessionModelOverride(cleared, ""); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	h.pinSessionModel(cleared, "openai/pin-must-not-win")
	md, err := session.ReadMetadataForDir(proj, cleared)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if v, ok := md["model"]; !ok || v != "" {
		t.Fatalf("clear sentinel replaced by pin: %v (present=%v)", v, ok)
	}
}

// TestSetSessionModelWithFilteredTranscriptRows reproduces the live failure
// "stale snapshot: N message(s) but N+k stored": the stored transcript holds
// rows the load path filters out (a tool message with no ToolID — orphan /
// PERMISSION_ASK sentinel), so a load→modify→full-save of the transcript
// compares a shorter, shifted sequence against the stored rows and conflicts
// forever. Setting the per-session model must touch only the metadata row and
// leave the transcript rows alone.
func TestSetSessionModelWithFilteredTranscriptRows(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	h.cfg.Model = "openai/global-default"

	id := session.NewSessionID()
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	msgs := []agent.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "tool", Content: "orphan result"}, // empty ToolID → filtered on load
	}
	if err := session.SaveForDir(proj, id, "t", msgs, nil); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if s, err := session.LoadForDir(proj, id); err != nil || len(s.Messages) >= len(msgs) {
		t.Fatalf("precondition: loaded view must be shorter than stored (err=%v, loaded=%d, stored=%d)", err, len(s.Messages), len(msgs))
	}

	rec := setSessionModel(t, h, id, "anthropic/claude-session")
	if rec.Code != http.StatusOK {
		t.Fatalf("set model: %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if got := h.effectiveSessionModel(id); got != "anthropic/claude-session" {
		t.Fatalf("effectiveSessionModel = %q, want override", got)
	}
	// Transcript untouched — the loaded view is the same two messages.
	if s, err := session.LoadForDir(proj, id); err != nil || len(s.Messages) != 2 {
		t.Fatalf("transcript changed after model set: err=%v loaded=%d, want 2", err, len(s.Messages))
	}

	clearRec := httptest.NewRecorder()
	h.HandleClearSessionModel(clearRec, httptest.NewRequest("DELETE", "/api/sessions/"+id+"/model", nil), id)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear: %d, want 200 (%s)", clearRec.Code, clearRec.Body.String())
	}
	if got := h.effectiveSessionModel(id); got != "openai/global-default" {
		t.Fatalf("effectiveSessionModel after clear = %q, want global default", got)
	}
}

// seedLoopbackCredential registers a usable credential for provider with a
// base URL that refuses connections immediately, so a rebuilt client can be
// CONSTRUCTED in tests without any request leaving the machine.
func seedLoopbackCredential(t *testing.T, provider string) {
	t.Helper()
	prev, hadPrev := auth.Get(provider)
	t.Cleanup(func() {
		if hadPrev {
			_ = auth.Set(provider, prev)
		} else {
			_ = auth.Remove(provider)
		}
	})
	if err := auth.Set(provider, auth.Credential{
		Kind:    auth.KindAPIKey,
		Key:     "test-key",
		BaseURL: "http://127.0.0.1:1",
	}); err != nil {
		t.Fatalf("seed %s credential: %v", provider, err)
	}
}

// TestModelPickAppliesToNextMessageInSameChat is the end-to-end regression for
// "selecting a new model on the same existing chat must reflect on the next
// message": the pick writes the per-session override, and the very next send
// must resolve it, replace the RESIDENT agent (which otherwise keeps the client
// it was constructed with) and dispatch on the picked model.
func TestModelPickAppliesToNextMessageInSameChat(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	seedLoopbackCredential(t, "anthropic")

	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	// Resident on its creation model — the case where a stale client would
	// otherwise keep serving the old model.
	h.cfg.Model = "opencode-go/deepseek-v4.1-flash"
	as := newTestSession(h, id, instantClient{})
	as.model = "opencode-go/deepseek-v4.1-flash"

	if rec := setSessionModel(t, h, id, "anthropic/claude-sonnet-5-5"); rec.Code != http.StatusOK {
		t.Fatalf("pick model: %d (%s)", rec.Code, rec.Body.String())
	}
	if got := h.effectiveSessionModel(id); got != "anthropic/claude-sonnet-5-5" {
		t.Fatalf("after pick effectiveSessionModel = %q, want the picked model", got)
	}

	raw, _ := json.Marshal(map[string]any{"content": "hello", "async": true})
	rec := httptest.NewRecorder()
	h.HandleSendMessage(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/message", bytes.NewReader(raw)), id)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send after pick: %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	if resp.Model != "anthropic/claude-sonnet-5-5" {
		t.Fatalf("dispatch model = %q, want the model picked in this chat", resp.Model)
	}
	// The resident agent must have been REPLACED, not merely annotated: a kept
	// agent still calls the old provider on this turn.
	if got := h.lookupAgentSession(id); got == nil || got == as || got.model != "anthropic/claude-sonnet-5-5" {
		t.Fatalf("resident agent not rebuilt to the picked model: %+v", got)
	}

	// Stop the background turn (its loopback base URL refuses the connection).
	h.HandleCancelSession(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/sessions/"+id+"/cancel", nil), id)
}

// TestModelPickDuringActiveTurnAppliesToFollowingTurn pins the IsTurnActive
// guard: a model picked while the turn is streaming must not tear down the
// running agent mid-turn, and the FOLLOWING turn must pick the new model up.
func TestModelPickDuringActiveTurnAppliesToFollowingTurn(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	seedLoopbackCredential(t, "anthropic")

	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)
	h.cfg.Model = "opencode-go/deepseek-v4.1-flash"
	as := newTestSession(h, id, instantClient{})
	as.model = "opencode-go/deepseek-v4.1-flash"

	if rec := setSessionModel(t, h, id, "anthropic/claude-sonnet-5-5"); rec.Code != http.StatusOK {
		t.Fatalf("pick model: %d (%s)", rec.Code, rec.Body.String())
	}

	// Mid-turn: the running agent keeps its client.
	h.sessions.setTurnActive(id, true)
	mid, err := h.reconcileProfileAgent(id, as, h.effectiveSessionModel(id))
	if err != nil {
		t.Fatalf("mid-turn reconcile: %v", err)
	}
	if mid != as {
		t.Fatalf("mid-turn reconcile replaced the running agent; the turn would be torn down")
	}

	// Turn over: the next turn rebuilds on the picked model.
	h.sessions.setTurnActive(id, false)
	next, err := h.reconcileProfileAgent(id, mid, h.effectiveSessionModel(id))
	if err != nil {
		t.Fatalf("post-turn reconcile: %v", err)
	}
	if next == as || next.model != "anthropic/claude-sonnet-5-5" {
		t.Fatalf("following turn did not adopt the picked model: %+v", next)
	}
}
