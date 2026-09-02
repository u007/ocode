package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/shell/sandbox"
)

// sandboxSupports indirection so tests read the same GOOS table the handler
// uses.
func sandboxSupports() bool { return sandbox.Supported() }

// permModeHandler returns a handler with n live agents registered.
func permModeHandler(t *testing.T, n int) (*Handler, []*agent.Agent) {
	t.Helper()
	h := NewHandler()
	var agents []*agent.Agent
	for i := 0; i < n; i++ {
		ag := agent.NewAgent(nil, nil, nil, nil)
		h.agents[strings.Join([]string{"sess", string(rune('a' + i))}, "-")] = &agentSession{agent: ag, model: "fake-model"}
		agents = append(agents, ag)
	}
	return h, agents
}

func putMode(t *testing.T, h *Handler, mode string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/permissions/mode", strings.NewReader(`{"mode":"`+mode+`"}`))
	h.HandleSetPermissionMode(rec, req)
	return rec
}

// TestSetPermissionModeAcceptsSandbox: PUT sandbox => live agent mode is
// sandbox and GET reflects it.
func TestSetPermissionModeAcceptsSandbox(t *testing.T) {
	h, agents := permModeHandler(t, 1)
	rec := putMode(t, h, "sandbox")
	if rec.Code != 200 {
		t.Fatalf("PUT sandbox => %d: %s", rec.Code, rec.Body.String())
	}
	if agents[0].Permissions().Mode() != agent.PermissionModeSandbox {
		t.Fatalf("live mode = %s, want sandbox", agents[0].Permissions().Mode())
	}

	get := httptest.NewRecorder()
	h.HandleGetPermissions(get, httptest.NewRequest("GET", "/api/permissions", nil))
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
	h, agents := permModeHandler(t, 1)
	rec := putMode(t, h, "bogus")
	if rec.Code != 400 {
		t.Fatalf("PUT bogus => %d, want 400", rec.Code)
	}
	if agents[0].Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("live mode changed to %s after rejected PUT", agents[0].Permissions().Mode())
	}
}

// TestSetPermissionModePropagatesToAllAgents: both live agents flip.
func TestSetPermissionModePropagatesToAllAgents(t *testing.T) {
	h, agents := permModeHandler(t, 2)
	if rec := putMode(t, h, "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	for i, ag := range agents {
		if ag.Permissions().Mode() != agent.PermissionModeSandbox {
			t.Fatalf("agent %d mode = %s, want sandbox", i, ag.Permissions().Mode())
		}
	}
}

// TestSetPermissionModeDoesNotPersist: the disk default is untouched by the
// session-scoped toggle (sandbox never becomes the durable mode).
func TestSetPermissionModeDoesNotPersist(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate any config writes
	h, _ := permModeHandler(t, 1)
	if rec := putMode(t, h, "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	// GET /api/permissions mode derives from LIVE agents, so it reports
	// sandbox while the config (durable default) stays normal.
	get := httptest.NewRecorder()
	h.HandleGetPermissions(get, httptest.NewRequest("GET", "/api/permissions", nil))
	var got map[string]any
	_ = json.Unmarshal(get.Body.Bytes(), &got)
	if got["mode"] != "sandbox" {
		t.Fatalf("live GET mode = %v, want sandbox", got["mode"])
	}
	if h.cfg.Ocode.Permissions.Mode == "sandbox" {
		t.Fatal("config default became sandbox — must not persist")
	}
}

// TestGetPermissionsStatusShape locks the authoritative status shape: live
// mode + sandbox_supported + effective_behavior (confined on a supported OS,
// degraded_normal otherwise).
func TestGetPermissionsStatusShape(t *testing.T) {
	h, _ := permModeHandler(t, 1)
	if rec := putMode(t, h, "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	get := httptest.NewRecorder()
	h.HandleGetPermissions(get, httptest.NewRequest("GET", "/api/permissions", nil))
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

// TestStatusSnapshotCarriesPermissionMode locks the SSE status path: the
// server snapshot exposes the live permission mode + support + behavior so the
// web sidebar shows real state over status SSE (not just GET).
func TestStatusSnapshotCarriesPermissionMode(t *testing.T) {
	h, _ := permModeHandler(t, 1)
	if rec := putMode(t, h, "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	snap := h.buildStatusSnapshot()
	if snap.PermissionMode != "sandbox" {
		t.Fatalf("snapshot permission_mode = %q, want sandbox", snap.PermissionMode)
	}
	if !snap.PermissionSandboxSupported && sandboxSupports() {
		t.Fatal("snapshot sandbox_supported = false on a supported OS")
	}
	want := "confined"
	if !snap.PermissionSandboxSupported {
		want = "degraded_normal"
	}
	if snap.PermissionEffectiveBehavior != want {
		t.Fatalf("snapshot effective_behavior = %q, want %q", snap.PermissionEffectiveBehavior, want)
	}
}

// TestSetPermissionModeCarriesToNewSessions locks the process-global override:
// an agent registered AFTER the toggle inherits the live mode, so a new tab
// does not silently revert to the config default (map-order hazards avoided).
func TestSetPermissionModeCarriesToNewSessions(t *testing.T) {
	h, _ := permModeHandler(t, 1)
	if rec := putMode(t, h, "sandbox"); rec.Code != 200 {
		t.Fatalf("PUT => %d", rec.Code)
	}
	ag := agent.NewAgent(nil, nil, nil, nil)
	h.registerAgentSession("sess-new", &agentSession{agent: ag, model: "fake-model"}, "")
	if ag.Permissions().Mode() != agent.PermissionModeSandbox {
		t.Fatalf("new session mode = %s, want sandbox (override must apply at registration)", ag.Permissions().Mode())
	}
}