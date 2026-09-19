package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/shell/sandbox"
)

// sandboxSupports indirection so tests read the same GOOS table the handler
// uses.
func sandboxSupports() bool { return sandbox.Supported() }

// permModeHandler returns a handler with n live agents registered, each bound
// to its own session id + project root so per-session resolution works.
func permModeHandler(t *testing.T, n int) (*Handler, []*agent.Agent, []string) {
	t.Helper()
	h := NewHandler()
	var agents []*agent.Agent
	var ids []string
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	for i := 0; i < n; i++ {
		id := session.NewSessionID()
		saveSessionToDir(t, proj, id)
		ag := agent.NewAgent(nil, nil, nil, nil)
		h.sessions.Register(id, proj)
		h.agents[id] = &agentSession{agent: ag, model: "fake-model"}
		agents = append(agents, ag)
		ids = append(ids, id)
	}
	return h, agents, ids
}

func putMode(t *testing.T, h *Handler, sessionID, mode string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"mode": mode, "session_id": sessionID})
	rec := httptest.NewRecorder()
	h.HandleSetPermissionMode(rec, httptest.NewRequest("PUT", "/api/permissions/mode", bytes.NewReader(raw)))
	return rec
}

// TestSetPermissionModeAcceptsSandbox: PUT sandbox for a session => that live
// agent is sandbox and GET scoped to it reflects it.
func TestSetPermissionModeAcceptsSandbox(t *testing.T) {
	h, agents, ids := permModeHandler(t, 1)
	rec := putMode(t, h, ids[0], "sandbox")
	if rec.Code != 200 {
		t.Fatalf("PUT sandbox => %d: %s", rec.Code, rec.Body.String())
	}
	if agents[0].Permissions().Mode() != agent.PermissionModeSandbox {
		t.Fatalf("live mode = %s, want sandbox", agents[0].Permissions().Mode())
	}

	get := httptest.NewRecorder()
	h.HandleGetPermissions(get, httptest.NewRequest("GET", "/api/permissions?session_id="+ids[0], nil))
	var got map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if got["mode"] != "sandbox" {
		t.Fatalf("GET mode = %v, want sandbox", got["mode"])
	}
}

// TestSetPermissionModeRejectsInvalid: an unknown mode is a 400 and leaves the
// live mode untouched.
func TestSetPermissionModeRejectsInvalid(t *testing.T) {
	h, agents, ids := permModeHandler(t, 1)
	rec := putMode(t, h, ids[0], "bogus")
	if rec.Code != 400 {
		t.Fatalf("PUT bogus => %d, want 400", rec.Code)
	}
	if agents[0].Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("live mode changed to %s after rejected PUT", agents[0].Permissions().Mode())
	}
}

// TestSetPermissionModeRequiresSession: a session-less PUT is rejected (400)
// and touches no agent — the whole point of per-session modes is that there is
// no process-global write.
func TestSetPermissionModeRequiresSession(t *testing.T) {
	h, agents, _ := permModeHandler(t, 1)
	rec := putMode(t, h, "", "sandbox")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("session-less PUT => %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if agents[0].Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("session-less PUT changed agent mode to %s", agents[0].Permissions().Mode())
	}
}

// TestSetPermissionModeUnknownSession: a typo'd/unknown session id 404s instead
// of silently persisting an orphan override.
func TestSetPermissionModeUnknownSession(t *testing.T) {
	h, _, _ := permModeHandler(t, 1)
	rec := putMode(t, h, "ses_does_not_exist", "sandbox")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session PUT => %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

// TestSetPermissionModeIsolatesSessions is THE regression test for the reported
// bug: setting yolo/sandbox on chat A must not change chat B (or any other
// session), regardless of map order.
func TestSetPermissionModeIsolatesSessions(t *testing.T) {
	h, agents, ids := permModeHandler(t, 2)
	if rec := putMode(t, h, ids[0], "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT A => %d", rec.Code)
	}
	if agents[0].Permissions().Mode() != agent.PermissionModeSandbox {
		t.Fatalf("A mode = %s, want sandbox", agents[0].Permissions().Mode())
	}
	if agents[1].Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("B mode = %s, want untouched normal (mode leaked across sessions)", agents[1].Permissions().Mode())
	}

	// GET without a session id reports the config default, never A's toggle.
	get := httptest.NewRecorder()
	h.HandleGetPermissions(get, httptest.NewRequest("GET", "/api/permissions", nil))
	var got map[string]any
	_ = json.Unmarshal(get.Body.Bytes(), &got)
	if got["mode"] != "normal" {
		t.Fatalf("session-less GET mode = %v, want config default normal", got["mode"])
	}

	// GET scoped to B still reports normal.
	getB := httptest.NewRecorder()
	h.HandleGetPermissions(getB, httptest.NewRequest("GET", "/api/permissions?session_id="+ids[1], nil))
	var gotB map[string]any
	_ = json.Unmarshal(getB.Body.Bytes(), &gotB)
	if gotB["mode"] != "normal" {
		t.Fatalf("B GET mode = %v, want normal", gotB["mode"])
	}
}

// TestYoloToggleIsolatesSessions mirrors the isolation guarantee for the legacy
// boolean yolo surface the sidebar/Telegram use.
func TestYoloToggleIsolatesSessions(t *testing.T) {
	h, agents, ids := permModeHandler(t, 2)
	raw, _ := json.Marshal(map[string]any{"enabled": true, "session_id": ids[0]})
	rec := httptest.NewRecorder()
	h.HandleSetYolo(rec, httptest.NewRequest("PUT", "/api/permissions/yolo", bytes.NewReader(raw)))
	if rec.Code != 200 {
		t.Fatalf("PUT yolo A => %d: %s", rec.Code, rec.Body.String())
	}
	if agents[0].Permissions().Mode() != agent.PermissionModeYOLO {
		t.Fatalf("A mode = %s, want yolo", agents[0].Permissions().Mode())
	}
	if agents[1].Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("B mode = %s, want normal (yolo leaked)", agents[1].Permissions().Mode())
	}

	// GET /api/permissions/yolo scoped: A true, B false.
	getA := httptest.NewRecorder()
	h.HandleGetYolo(getA, httptest.NewRequest("GET", "/api/permissions/yolo?session_id="+ids[0], nil))
	var yA map[string]bool
	_ = json.Unmarshal(getA.Body.Bytes(), &yA)
	if !yA["yolo"] {
		t.Fatalf("A yolo = false, want true")
	}
	getB := httptest.NewRecorder()
	h.HandleGetYolo(getB, httptest.NewRequest("GET", "/api/permissions/yolo?session_id="+ids[1], nil))
	var yB map[string]bool
	_ = json.Unmarshal(getB.Body.Bytes(), &yB)
	if yB["yolo"] {
		t.Fatalf("B yolo = true, want false")
	}
}

// TestYoloToggleRequiresSession: the legacy boolean surface is scoped too; a
// session-less PUT is 400 and touches no agent.
func TestYoloToggleRequiresSession(t *testing.T) {
	h, agents, _ := permModeHandler(t, 1)
	raw, _ := json.Marshal(map[string]any{"enabled": true})
	rec := httptest.NewRecorder()
	h.HandleSetYolo(rec, httptest.NewRequest("PUT", "/api/permissions/yolo", bytes.NewReader(raw)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("session-less yolo PUT => %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if agents[0].Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("session-less yolo PUT changed agent mode to %s", agents[0].Permissions().Mode())
	}
}

// TestSetPermissionModePersistsPerSession: the override is durable in the
// session's metadata (survives restart/agent eviction), but only for the
// session it targets.
func TestSetPermissionModePersistsPerSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate any global config writes
	h, _, ids := permModeHandler(t, 2)
	if rec := putMode(t, h, ids[0], "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	proj := h.sessionProjectRoot(ids[0])
	s, err := session.LoadForDir(proj, ids[0])
	if err != nil {
		t.Fatalf("reload A: %v", err)
	}
	if got, _ := s.Metadata[permissionModeMetadataKey].(string); got != "sandbox" {
		t.Fatalf("A persisted mode = %q, want sandbox", got)
	}
	// B has no override.
	sB, err := session.LoadForDir(proj, ids[1])
	if err != nil {
		t.Fatalf("reload B: %v", err)
	}
	if _, ok := sB.Metadata[permissionModeMetadataKey]; ok {
		t.Fatalf("B gained a permission override: %v", sB.Metadata)
	}
	// The durable config default is untouched.
	if h.cfg.Ocode.Permissions.Mode == "sandbox" {
		t.Fatal("config default became sandbox — live modes must not persist there")
	}
}

// TestSetPermissionModeCarriesToRebuiltSession locks the per-session
// persistence path: an agent rebuilt for a session (new tab/resume/eviction)
// inherits that session's own persisted override, and only that session's.
func TestSetPermissionModeCarriesToRebuiltSession(t *testing.T) {
	h, _, ids := permModeHandler(t, 1)
	if rec := putMode(t, h, ids[0], "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	// Simulate eviction + rebuild for the same session id.
	proj := h.sessionProjectRoot(ids[0])
	delete(h.agents, ids[0]) // evict the live agent
	ag := agent.NewAgent(nil, nil, nil, nil)
	h.registerAgentSession(ids[0], &agentSession{agent: ag, model: "fake-model"}, proj)
	if ag.Permissions().Mode() != agent.PermissionModeSandbox {
		t.Fatalf("rebuilt session mode = %s, want sandbox", ag.Permissions().Mode())
	}

	// A different session does NOT inherit it.
	other := session.NewSessionID()
	saveSessionToDir(t, proj, other)
	ag2 := agent.NewAgent(nil, nil, nil, nil)
	h.registerAgentSession(other, &agentSession{agent: ag2, model: "fake-model"}, proj)
	if ag2.Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("new unrelated session mode = %s, want normal", ag2.Permissions().Mode())
	}
}

// TestGetPermissionsStatusShape locks the authoritative status shape when
// scoped to a session: mode + sandbox_supported + effective_behavior (confined
// on a supported OS, degraded_normal otherwise).
func TestGetPermissionsStatusShape(t *testing.T) {
	h, _, ids := permModeHandler(t, 1)
	if rec := putMode(t, h, ids[0], "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	get := httptest.NewRecorder()
	h.HandleGetPermissions(get, httptest.NewRequest("GET", "/api/permissions?session_id="+ids[0], nil))
	var got map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if got["mode"] != "sandbox" {
		t.Fatalf("mode = %v, want sandbox", got["mode"])
	}
	wantSupported := sandboxSupports() // compile-time GOOS table
	if got["sandbox_supported"] != wantSupported {
		t.Fatalf("sandbox_supported = %v, want %v", got["sandbox_supported"], wantSupported)
	}
	wantBehavior := "confined"
	if !wantSupported {
		wantBehavior = "degraded_normal"
	}
	if got["effective_behavior"] != wantBehavior {
		t.Fatalf("effective_behavior = %v, want %v", got["effective_behavior"], wantBehavior)
	}
}

// TestGetPermissionsConfigReadIsRaceFree pins the lock scope of
// HandleGetPermissions: LoadFromOcode walks the config's Permissions.Tools /
// Bash.Prefixes maps, and the rule setters mutate those same maps under h.mu.
// Reading them after unlocking was a concurrent map read/write (a Go fatal, not
// just a -race warning). Run with -race to catch a regression.
func TestGetPermissionsConfigReadIsRaceFree(t *testing.T) {
	h, _, _ := permModeHandler(t, 1)
	h.mu.Lock()
	if h.cfg == nil {
		h.cfg = &config.Config{}
	}
	h.cfg.Ocode.Permissions.Tools = map[string]string{"read": "allow"}
	h.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.HandleGetPermissions(rec, httptest.NewRequest("GET", "/api/permissions", nil))
		}()
		go func() {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"tool": "bash", "level": "allow"})
			rec := httptest.NewRecorder()
			h.HandleSetPermission(rec, httptest.NewRequest("POST", "/api/permissions", bytes.NewReader(body)))
		}()
	}
	wg.Wait()
}

// TestSessionStatusCarriesPerSessionPermissionMode locks the SSE/status path:
// a session's own status snapshot exposes its own permission mode, and a second
// session keeps reporting normal.
func TestSessionStatusCarriesPerSessionPermissionMode(t *testing.T) {
	h, _, ids := permModeHandler(t, 2)
	if rec := putMode(t, h, ids[0], "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}

	snapA := sessionStatusSnapshot(t, h, ids[0])
	if snapA.PermissionMode != "sandbox" {
		t.Fatalf("A permission_mode = %q, want sandbox", snapA.PermissionMode)
	}
	want := "confined"
	if !snapA.PermissionSandboxSupported {
		want = "degraded_normal"
	}
	if snapA.PermissionEffectiveBehavior != want {
		t.Fatalf("A effective_behavior = %q, want %q", snapA.PermissionEffectiveBehavior, want)
	}

	snapB := sessionStatusSnapshot(t, h, ids[1])
	if snapB.PermissionMode != "normal" {
		t.Fatalf("B permission_mode = %q, want normal (mode leaked across sessions)", snapB.PermissionMode)
	}
}

// sessionStatusSnapshot fetches GET /api/sessions/:id/status into a TUIStatus.
func sessionStatusSnapshot(t *testing.T, h *Handler, id string) TUIStatus {
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
	return snap
}

// TestStatusSnapshotWithoutSessionReportsDefault: the process-wide snapshot
// (GET /api/tui-status fallback) has no session, so it reports the persisted
// config default rather than any session's live toggle.
func TestStatusSnapshotWithoutSessionReportsDefault(t *testing.T) {
	h, _, ids := permModeHandler(t, 1)
	if rec := putMode(t, h, ids[0], "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	snap := h.buildStatusSnapshot()
	if snap.PermissionMode != "normal" {
		t.Fatalf("process-wide snapshot permission_mode = %q, want config default normal", snap.PermissionMode)
	}
	if !snap.PermissionSandboxSupported && sandboxSupports() {
		t.Fatal("snapshot sandbox_supported = false on a supported OS")
	}
}

// TestSandboxConfigNoteSurfacesIntegrityWarning verifies the persisted mode endpoint includes a note when sandbox is active.
func TestSandboxConfigNoteSurfacesIntegrityWarning(t *testing.T) {
	h, _, _ := permModeHandler(t, 1)
	get := httptest.NewRecorder()
	h.HandleGetPermissionModeConfig(http.ResponseWriter(get), httptest.NewRequest("GET", "/api/config/ocode/permissions-mode", nil))
	if get.Code != 200 {
		t.Fatalf("GET config => %d", get.Code)
	}
	var resp struct {
		Mode string `json:"mode"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET decode: %v", err)
	}
	if resp.Note == "" && resp.Mode == "sandbox" {
		t.Errorf("sandbox mode response missing integrity-only note; got note=%q mode=%q", resp.Note, resp.Mode)
	}
}

// TestHandleChatPersistsNewSessionPermissionMode proves the draft-tab path: a
// brand-new session created by POST /api/chat carrying permission_mode
// persists the override at creation, and a later agent build picks it up.
func TestHandleChatPersistsNewSessionPermissionMode(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	h.cfg.Model = "openai/global-default"

	raw, _ := json.Marshal(map[string]any{
		"content":         "hi",
		"project_path":    proj,
		"permission_mode": "sandbox",
		"async":           true,
	})
	rec := httptest.NewRecorder()
	h.HandleChat(rec, httptest.NewRequest("POST", "/api/chat", bytes.NewReader(raw)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("chat => %d: %s", rec.Code, rec.Body.String())
	}
	var resp ChatResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.SessionID == "" {
		t.Fatal("chat response missing session id")
	}
	s, err := session.LoadForDir(proj, resp.SessionID)
	if err != nil {
		t.Fatalf("reload new session: %v", err)
	}
	if got, _ := s.Metadata[permissionModeMetadataKey].(string); got != "sandbox" {
		t.Fatalf("new session persisted mode = %q, want sandbox", got)
	}
}

// TestNormalizePermissionMode table-tests the validation used by every read
// and write path.
func TestNormalizePermissionMode(t *testing.T) {
	cases := map[string]bool{
		"normal": true, "yolo": true, "locked": true, "sandbox": true,
		" YOLO ": true, "bogus": false, "": false, "Sandboxx": false,
	}
	for in, want := range cases {
		_, ok := normalizePermissionMode(in)
		if ok != want {
			t.Errorf("normalizePermissionMode(%q) ok = %v, want %v", in, ok, want)
		}
	}
}
