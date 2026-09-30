package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
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

// ── Per-session advisor MODEL + trigger set ─────────────────────────────
//
// The on/off gate above is per session; these cover the model and the
// checkpoint (trigger) set, which were process-global until now. The invariant
// they all protect: changing the global default must reach NEW chats only and
// never rewrite what an existing chat already owns.

// advisorIsolationHandler is permModeHandler plus config-file isolation. The
// unscoped advisor endpoint persists to ocodeconfig.json, so HOME *and*
// XDG_CONFIG_HOME must point at a throwaway dir or the test would rewrite the
// developer's real global advisor settings.
//
// It deliberately does NOT seed the agents: seeding is what pins a session, and
// a pre-pinned session can never show "a new chat takes the current default".
// Tests that assert on live agents call seedAdvisorAgents explicitly.
func advisorIsolationHandler(t *testing.T, n int, defaultModel string, defaultCheckpoints []string) (*Handler, []*agent.Agent, []string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	h, agents, ids := permModeHandler(t, n)
	h.mu.Lock()
	h.cfg = &config.Config{}
	h.mu.Unlock()
	provider, model := config.SplitProviderModel(defaultModel)
	setGlobalAdvisorDefault(t, h, provider, model, defaultCheckpoints)
	return h, agents, ids
}

// seedAdvisorAgents mirrors the buildAgentSession seed line. permModeHandler
// builds agents with a nil config, so without this they carry no per-session
// advisor config and the agent-level assertions would only prove that nothing
// was ever installed.
func seedAdvisorAgents(t *testing.T, h *Handler, agents []*agent.Agent, ids []string) {
	t.Helper()
	for i, id := range ids {
		agents[i].SetAdvisorConfig(h.advisorConfigSeed(id))
	}
}

func putAdvisorConfig(t *testing.T, h *Handler, sessionID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	// Copy so the caller's map isn't mutated across the two write paths.
	sent := map[string]any{}
	for k, v := range body {
		sent[k] = v
	}
	if sessionID != "" {
		sent["session_id"] = sessionID
	}
	raw, _ := json.Marshal(sent)
	rec := httptest.NewRecorder()
	h.HandleSetAdvisor(rec, httptest.NewRequest("PUT", "/api/config/advisor", bytes.NewReader(raw)))
	return rec
}

func getAdvisorConfigFor(t *testing.T, h *Handler, sessionID string) map[string]any {
	t.Helper()
	url := "/api/config/advisor"
	if sessionID != "" {
		url += "?session_id=" + sessionID
	}
	rec := httptest.NewRecorder()
	h.HandleGetAdvisor(rec, httptest.NewRequest("GET", url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET advisor (session %q): %d (%s)", sessionID, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode GET advisor: %v", err)
	}
	return out
}

// setGlobalAdvisorDefault changes the new-chat default through the unscoped
// endpoint, the way the Settings form does.
func setGlobalAdvisorDefault(t *testing.T, h *Handler, provider, model string, checkpoints []string) {
	t.Helper()
	rec := putAdvisorConfig(t, h, "", map[string]any{
		"provider": provider, "model": model, "checkpoints": checkpoints,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("set global default: %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestAdvisorModelPickIsIsolatedToItsSession is the regression test for the
// report that one update to a chat's advisor model showed up in every other
// chat: a session-scoped PUT must move only that session's live agent, pin only
// its metadata, and leave other sessions and the new-chat default alone.
func TestAdvisorModelPickIsIsolatedToItsSession(t *testing.T) {
	h, agents, ids := advisorIsolationHandler(t, 2, "deepseek/deepseek-v4-pro", []string{"plan"})
	seedAdvisorAgents(t, h, agents, ids)
	a, b := ids[0], ids[1]

	// Both chats start on the default.
	for _, id := range ids {
		if got := getAdvisorConfigFor(t, h, id)["model"]; got != "deepseek-v4-pro" {
			t.Fatalf("%s baseline model = %v, want deepseek-v4-pro", id, got)
		}
	}

	// Pick a different model in chat A only.
	rec := putAdvisorConfig(t, h, a, map[string]any{"provider": "openai", "model": "gpt-5.1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("pick for A: %d (%s)", rec.Code, rec.Body.String())
	}
	if got := agents[0].AdvisorModel(); got != "openai/gpt-5.1" {
		t.Fatalf("A live agent model = %q, want openai/gpt-5.1", got)
	}
	if got := agents[1].AdvisorModel(); got != "deepseek/deepseek-v4-pro" {
		t.Fatalf("B live agent must be untouched, got %q", got)
	}
	if got := getAdvisorConfigFor(t, h, a)["model"]; got != "gpt-5.1" {
		t.Fatalf("GET A model = %v, want gpt-5.1", got)
	}
	if got := getAdvisorConfigFor(t, h, b)["model"]; got != "deepseek-v4-pro" {
		t.Fatalf("B must still report the default, got %v", got)
	}
	// The new-chat default is untouched by a per-chat pick.
	if got := getAdvisorConfigFor(t, h, "")["model"]; got != "deepseek-v4-pro" {
		t.Fatalf("global default changed by a per-session pick: %v", got)
	}
	// Durable: A is pinned, and a partial PUT did not clear its triggers.
	cfgA, ok := sessionAdvisorConfigForDir(h.sessionProjectRoot(a), a)
	if !ok || cfgA.Model != "openai/gpt-5.1" {
		t.Fatalf("A metadata pin = (%+v, %v), want openai/gpt-5.1", cfgA, ok)
	}
	if got := getAdvisorConfigFor(t, h, a)["checkpoints"]; !reflect.DeepEqual(got, []any{"plan"}) {
		t.Fatalf("A partial PUT must not clear its triggers, got %v", got)
	}
}

// TestChangingDefaultAffectsOnlyNewChats is the policy the user asked for: once
// a chat has resolved its advisor, changing the default leaves that chat alone,
// and only a chat that has not resolved yet takes the new value.
func TestChangingDefaultAffectsOnlyNewChats(t *testing.T) {
	h, _, ids := advisorIsolationHandler(t, 2, "deepseek/deepseek-v4-pro", []string{"plan"})

	existing := ids[0]
	if got := getAdvisorConfigFor(t, h, existing)["model"]; got != "deepseek-v4-pro" {
		t.Fatalf("baseline model = %v", got)
	}

	// The user changes the default (model and triggers) afterwards.
	setGlobalAdvisorDefault(t, h, "openai", "gpt-5.1", []string{"done"})

	if got := getAdvisorConfigFor(t, h, existing)["model"]; got != "deepseek-v4-pro" {
		t.Fatalf("existing chat followed the new default: %v", got)
	}
	if got := getAdvisorConfigFor(t, h, existing)["checkpoints"]; !reflect.DeepEqual(got, []any{"plan"}) {
		t.Fatalf("existing chat's triggers changed: %v", got)
	}
	// A chat first seen after the change takes the new default.
	if got := getAdvisorConfigFor(t, h, ids[1])["model"]; got != "gpt-5.1" {
		t.Fatalf("new chat model = %v, want gpt-5.1", got)
	}
	if got := getAdvisorConfigFor(t, h, ids[1])["checkpoints"]; !reflect.DeepEqual(got, []any{"done"}) {
		t.Fatalf("new chat triggers = %v, want [done]", got)
	}
}

// TestAdvisorPinSurvivesEvictionAndRebuild covers restart / LRU eviction: with no
// live agent, resolution must come from the pin, and a rebuilt agent must be
// seeded from it rather than from the (now different) process default.
func TestAdvisorPinSurvivesEvictionAndRebuild(t *testing.T) {
	h, _, ids := advisorIsolationHandler(t, 1, "deepseek/deepseek-v4-pro", []string{"plan"})
	id := ids[0]

	if rec := putAdvisorConfig(t, h, id, map[string]any{"provider": "openai", "model": "gpt-5.1"}); rec.Code != http.StatusOK {
		t.Fatalf("pick: %d (%s)", rec.Code, rec.Body.String())
	}
	setGlobalAdvisorDefault(t, h, "anthropic", "claude-sonnet-5-5", []string{"done"})

	delete(h.agents, id)
	if got := h.effectiveSessionAdvisorConfig(id).Model; got != "openai/gpt-5.1" {
		t.Fatalf("after eviction = %q, want the pinned openai/gpt-5.1", got)
	}
	if got := h.advisorConfigSeed(id).Model; got != "openai/gpt-5.1" {
		t.Fatalf("advisorConfigSeed = %q, want the pinned openai/gpt-5.1", got)
	}
	// A real rebuild honours the pin.
	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, h.sessionProjectRoot(id))
	if err != nil {
		t.Fatalf("rebuild: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if got := as.agent.AdvisorModel(); got != "openai/gpt-5.1" {
		t.Fatalf("rebuilt agent model = %q, want the pinned openai/gpt-5.1", got)
	}
}

// TestAdvisorPinPinnedToDefaultDoesNotFollowGlobal: an explicit "use the
// default" pick is a real value. The session must keep resolving to the
// built-in default, not to the process-wide model, which may since have changed.
func TestAdvisorPinPinnedToDefaultDoesNotFollowGlobal(t *testing.T) {
	h, agents, ids := advisorIsolationHandler(t, 1, "deepseek/deepseek-v4-pro", []string{"plan"})
	seedAdvisorAgents(t, h, agents, ids)
	id := ids[0]

	// Explicitly clear the model in this chat (= use the built-in default).
	if rec := putAdvisorConfig(t, h, id, map[string]any{"model": "", "provider": ""}); rec.Code != http.StatusOK {
		t.Fatalf("clear model: %d (%s)", rec.Code, rec.Body.String())
	}
	setGlobalAdvisorDefault(t, h, "openai", "gpt-5.1", []string{"plan"})

	if got := agents[0].AdvisorModel(); got != "" {
		t.Fatalf("agent model = %q, want empty (pinned to the built-in default)", got)
	}
	if got := h.effectiveSessionAdvisorConfig(id).Model; got != "" {
		t.Fatalf("resolved model = %q, want empty", got)
	}
	// Its triggers are still its own, not the default's.
	if got := agents[0].AdvisorCheckpoints(); !reflect.DeepEqual(got, []string{"plan"}) {
		t.Fatalf("triggers = %v, want [plan]", got)
	}
}

// TestAdvisorConfigUnknownSession404: a session-scoped PUT for a session that
// does not resolve must 404 and write nothing, so a typo cannot persist an
// orphan pin.
func TestAdvisorConfigUnknownSession404(t *testing.T) {
	h, agents, _ := advisorIsolationHandler(t, 1, "deepseek/deepseek-v4-pro", []string{"plan"})
	before := agents[0].AdvisorModel()

	rec := putAdvisorConfig(t, h, "does-not-exist", map[string]any{"model": "x/y"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if agents[0].AdvisorModel() != before {
		t.Fatal("no live agent should be touched by a rejected pick")
	}
	// The unscoped default still works after the rejection.
	if rec := putAdvisorConfig(t, h, "", map[string]any{"model": "gpt-5.1"}); rec.Code != http.StatusOK {
		t.Fatalf("global after 404: %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestSessionAdvisorConfigForDirRoundTrip covers the persistence primitive,
// including the empty-model and empty-trigger cases that must survive as real
// values rather than reverting to "unpinned".
func TestSessionAdvisorConfigForDirRoundTrip(t *testing.T) {
	// No seeding here: this test asserts the UNPINNED state, and
	// persistSessionAdvisorConfig writes only session metadata (never the
	// global config file), so no config isolation is needed.
	h, _, ids := permModeHandler(t, 1)
	id := ids[0]
	proj := h.sessionProjectRoot(id)

	if _, ok := sessionAdvisorConfigForDir(proj, id); ok {
		t.Fatal("expected no pin initially")
	}
	want := agent.AdvisorConfig{
		Model:       "openai/gpt-5.1",
		ClaudeCode:  true,
		Checkpoints: []string{"plan", "done"},
	}
	if err := h.persistSessionAdvisorConfig(id, want); err != nil {
		t.Fatalf("persist: %v", err)
	}
	got, ok := sessionAdvisorConfigForDir(proj, id)
	if !ok || got.Model != want.Model || got.ClaudeCode != want.ClaudeCode || !reflect.DeepEqual(got.Checkpoints, want.Checkpoints) {
		t.Fatalf("round trip = (%+v, %v), want %+v", got, ok, want)
	}

	// An empty model with no triggers is a PIN, not an absence.
	if err := h.persistSessionAdvisorConfig(id, agent.AdvisorConfig{}); err != nil {
		t.Fatalf("persist empty: %v", err)
	}
	got, ok = sessionAdvisorConfigForDir(proj, id)
	if !ok {
		t.Fatal("an explicitly cleared model must still count as pinned")
	}
	if got.Model != "" || len(got.Checkpoints) != 0 {
		t.Fatalf("empty pin round trip = %+v, want the zero value", got)
	}
}

// TestPerSessionAdvisorTriggersAreIndependent: a trigger change in one chat
// must not reach another chat's completion checkpoints, which is what
// advisorCheckpointEnabled reads.
func TestPerSessionAdvisorTriggersAreIndependent(t *testing.T) {
	h, agents, ids := advisorIsolationHandler(t, 2, "deepseek/deepseek-v4-pro", []string{"plan"})
	seedAdvisorAgents(t, h, agents, ids)
	a, b := ids[0], ids[1]
	getAdvisorConfigFor(t, h, a)
	getAdvisorConfigFor(t, h, b)

	if rec := putAdvisorConfig(t, h, a, map[string]any{"checkpoints": []string{"done"}}); rec.Code != http.StatusOK {
		t.Fatalf("set triggers for A: %d (%s)", rec.Code, rec.Body.String())
	}
	if got := agents[0].AdvisorCheckpoints(); !reflect.DeepEqual(got, []string{"done"}) {
		t.Fatalf("A triggers = %v, want [done]", got)
	}
	if got := agents[1].AdvisorCheckpoints(); !reflect.DeepEqual(got, []string{"plan"}) {
		t.Fatalf("B triggers changed to %v, want [plan]", got)
	}
}

// advisorStatusSnapshot fetches a session's full per-session status snapshot.
func advisorStatusSnapshot(t *testing.T, h *Handler, id string) TUIStatus {
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
	return snap
}

// TestSessionStatusReportsPerSessionAdvisorModel is what the sidebar and the
// status panel actually read. Each chat's snapshot must carry THAT chat's model
// and triggers; buildStatusSnapshot's process-wide default would otherwise make
// one chat's pick look like it applied everywhere.
func TestSessionStatusReportsPerSessionAdvisorModel(t *testing.T) {
	h, _, ids := advisorIsolationHandler(t, 2, "deepseek/deepseek-v4-pro", []string{"plan"})
	a, b := ids[0], ids[1]
	// Resolve both so neither is reading the bare default by accident.
	getAdvisorConfigFor(t, h, a)
	getAdvisorConfigFor(t, h, b)

	// Change A's model AND its triggers; B must be untouched.
	rec := putAdvisorConfig(t, h, a, map[string]any{
		"provider": "openai", "model": "gpt-5.1", "checkpoints": []string{"done"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("pick for A: %d (%s)", rec.Code, rec.Body.String())
	}

	snapA := advisorStatusSnapshot(t, h, a)
	if snapA.AdvisorModel != "openai/gpt-5.1" {
		t.Fatalf("A snapshot model = %q, want openai/gpt-5.1", snapA.AdvisorModel)
	}
	if !reflect.DeepEqual(snapA.AdvisorCheckpoints, []string{"done"}) {
		t.Fatalf("A snapshot triggers = %v, want [done]", snapA.AdvisorCheckpoints)
	}

	snapB := advisorStatusSnapshot(t, h, b)
	if snapB.AdvisorModel != "deepseek/deepseek-v4-pro" {
		t.Fatalf("B snapshot model = %q, want its own deepseek/deepseek-v4-pro", snapB.AdvisorModel)
	}
	if !reflect.DeepEqual(snapB.AdvisorCheckpoints, []string{"plan"}) {
		t.Fatalf("B snapshot triggers = %v, want [plan]", snapB.AdvisorCheckpoints)
	}
}
