package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// putAdvisor calls PUT /api/config/advisor-enabled with an optional session_id.
func putAdvisor(t *testing.T, h *Handler, enabled bool, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"enabled": enabled}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.HandleSetAdvisorEnabled(rec, httptest.NewRequest("PUT", "/api/config/advisor-enabled", bytes.NewReader(raw)))
	return rec
}

// sessionStatusAdvisor fetches a session's per-session status snapshot and
// returns its advisor_enabled field.
func sessionStatusAdvisor(t *testing.T, h *Handler, id string) bool {
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
	return snap.AdvisorEnabled
}

func getAdvisorEnabledField(t *testing.T, h *Handler, sessionID string) bool {
	t.Helper()
	url := "/api/config/advisor-enabled"
	if sessionID != "" {
		url += "?session_id=" + sessionID
	}
	rec := httptest.NewRecorder()
	h.HandleGetAdvisorEnabled(rec, httptest.NewRequest("GET", url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET advisor-enabled: %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode GET advisor-enabled: %v", err)
	}
	return resp.Enabled
}

func boolPtr(b bool) *bool { return &b }

// TestPerSessionAdvisorToggleIsolation is the regression test for the sidebar
// bug where toggling the advisor in one chat flipped it for every other chat:
// a session-scoped PUT must move only that session's live agent, persist only
// its metadata, and leave the process-wide default and other sessions alone.
func TestPerSessionAdvisorToggleIsolation(t *testing.T) {
	h, agents, ids := permModeHandler(t, 2)
	h.advisorEnabled = true
	a, b := ids[0], ids[1]

	// Baseline: both sessions follow the process-wide default.
	if !sessionStatusAdvisor(t, h, a) || !sessionStatusAdvisor(t, h, b) {
		t.Fatal("baseline: both sessions should report the global default true")
	}

	rec := putAdvisor(t, h, false, a)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle A off: %d (%s)", rec.Code, rec.Body.String())
	}
	// A's live agent changed; B's did not; the process default did not.
	if agents[0].AdvisorEnabled() {
		t.Fatal("A live agent should be off")
	}
	if !agents[1].AdvisorEnabled() {
		t.Fatal("B live agent must be untouched")
	}
	if !h.advisorEnabled {
		t.Fatal("process-wide default must not change on a per-session toggle")
	}
	if sessionStatusAdvisor(t, h, a) {
		t.Fatal("A status should report off")
	}
	if !sessionStatusAdvisor(t, h, b) {
		t.Fatal("B status should still report the global default on")
	}

	// GET scoped to A reports off; the unscoped (process) read stays on.
	if getAdvisorEnabledField(t, h, a) {
		t.Fatal("GET ?session_id=A should report off")
	}
	if !getAdvisorEnabledField(t, h, "") {
		t.Fatal("GET without session_id should report the global default on")
	}

	// Durable: A's override is in transcript metadata, B has none.
	proj := h.sessionProjectRoot(a)
	s, err := session.LoadForDir(proj, a)
	if err != nil {
		t.Fatalf("reload A: %v", err)
	}
	if v, ok := s.Metadata[advisorEnabledMetadataKey].(bool); !ok || v {
		t.Fatalf("A metadata advisor_enabled = %v, want false", s.Metadata[advisorEnabledMetadataKey])
	}
	sb, err := session.LoadForDir(h.sessionProjectRoot(b), b)
	if err != nil {
		t.Fatalf("reload B: %v", err)
	}
	if _, ok := sb.Metadata[advisorEnabledMetadataKey]; ok {
		t.Fatalf("B must not have an advisor override, got %v", sb.Metadata[advisorEnabledMetadataKey])
	}

	// Eviction (server restart / LRU): resolution falls back to the persisted
	// override, not the process default, so a rebuild keeps A off.
	delete(h.agents, a)
	if h.effectiveSessionAdvisorEnabled(a, true) {
		t.Fatal("after eviction A should resolve off from persisted metadata")
	}
	// advisorSeed is what buildAgentSession uses on rebuild: it must return the
	// persisted override (false), not the fallback (true).
	if h.advisorSeed(a, true) {
		t.Fatal("advisorSeed(A, true) should be the persisted override (off)")
	}
}

// TestAdvisorToggleWithoutSessionStaysProcessWide locks the backward-compatible
// path used by the Settings → Advisor form and the pre-session fallback: no
// session_id still flips the process-wide gate and every live agent.
func TestAdvisorToggleWithoutSessionStaysProcessWide(t *testing.T) {
	h, agents, _ := permModeHandler(t, 2)
	h.advisorEnabled = true

	rec := putAdvisor(t, h, false, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("global toggle: %d (%s)", rec.Code, rec.Body.String())
	}
	if h.advisorEnabled {
		t.Fatal("global toggle should clear the process-wide default")
	}
	if agents[0].AdvisorEnabled() || agents[1].AdvisorEnabled() {
		t.Fatal("global toggle should apply to every live agent")
	}
}

// TestAdvisorToggleUnknownSession404: a session-scoped PUT for a session that
// does not resolve must 404 and touch nothing, so a typo cannot persist an
// orphan override.
func TestAdvisorToggleUnknownSession404(t *testing.T) {
	h, agents, _ := permModeHandler(t, 1)
	h.advisorEnabled = true

	rec := putAdvisor(t, h, false, "does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if !agents[0].AdvisorEnabled() {
		t.Fatal("no live agent should be touched by a rejected toggle")
	}
}

// TestSessionAdvisorEnabledForDirRoundTrip covers the persistence primitive,
// including clearing the key.
func TestSessionAdvisorEnabledForDirRoundTrip(t *testing.T) {
	h, _, ids := permModeHandler(t, 1)
	id := ids[0]
	proj := h.sessionProjectRoot(id)

	if _, ok := sessionAdvisorEnabledForDir(proj, id); ok {
		t.Fatal("expected no override initially")
	}
	if err := h.persistSessionAdvisorEnabled(id, boolPtr(false)); err != nil {
		t.Fatalf("persist false: %v", err)
	}
	if v, ok := sessionAdvisorEnabledForDir(proj, id); !ok || v {
		t.Fatalf("round trip = (%v,%v), want (false,true)", v, ok)
	}
	if err := h.persistSessionAdvisorEnabled(id, boolPtr(true)); err != nil {
		t.Fatalf("persist true: %v", err)
	}
	if v, ok := sessionAdvisorEnabledForDir(proj, id); !ok || !v {
		t.Fatalf("round trip = (%v,%v), want (true,true)", v, ok)
	}
	if err := h.persistSessionAdvisorEnabled(id, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := sessionAdvisorEnabledForDir(proj, id); ok {
		t.Fatal("expected override cleared")
	}
}

// TestBuildAgentSessionSeedsAdvisorFromSessionOverride exercises the
// buildAgentSession seed line directly (not just the advisorSeed helper): a
// freshly built agent for a session with a persisted advisor override must
// start with that override, and a session with no override must fall back to
// the process-wide default. Without the seed, a toggle would silently revert
// to the default on resume / rebuild / server restart.
//
// The mcpCache is pre-readied (as in agent_session_profile_test.go) so the
// build does not wait 30s on MCP enumeration.
func TestBuildAgentSessionSeedsAdvisorFromSessionOverride(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	h.advisorEnabled = true

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	// Session with an explicit override (off) — must win over the default on.
	override := session.NewSessionID()
	saveSessionToDir(t, proj, override)
	if err := h.persistSessionAdvisorEnabled(override, boolPtr(false)); err != nil {
		t.Fatalf("persist override: %v", err)
	}
	asOverride, stage, err := h.buildAgentSession(override, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build override agent: %v (stage %s)", err, stage)
	}
	defer asOverride.agent.Shutdown()
	if asOverride.agent.AdvisorEnabled() {
		t.Fatal("agent built for a session with advisor_enabled=false must seed the advisor off")
	}

	// Session with no override — must follow the process-wide default (on).
	plain := session.NewSessionID()
	saveSessionToDir(t, proj, plain)
	asPlain, stage, err := h.buildAgentSession(plain, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build plain agent: %v (stage %s)", err, stage)
	}
	defer asPlain.agent.Shutdown()
	if !asPlain.agent.AdvisorEnabled() {
		t.Fatal("agent built for a session with no override must fall back to the process default (on)")
	}
}
