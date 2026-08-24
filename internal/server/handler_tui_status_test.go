package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

func testHandlerWithConfig(t *testing.T) *Handler {
	t.Helper()
	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Model: "gpt-4o-mini",
		Ocode: config.OcodeConfig{
			SmallModel:        "opencode-go/deepseek-v4-flash",
			SmallModelEnabled: true,
			Advisor:           config.AdvisorConfig{Model: "advisor-model"},
		},
	}
	h.mu.Unlock()
	return h
}

func TestBuildStatusSnapshot(t *testing.T) {
	h := testHandlerWithConfig(t)

	snap := h.buildStatusSnapshot()

	if snap.MainModel != "gpt-4o-mini" {
		t.Errorf("MainModel = %q, want gpt-4o-mini", snap.MainModel)
	}
	if snap.SmallModel != "opencode-go/deepseek-v4-flash" {
		t.Errorf("SmallModel = %q, want deepseek-v4-flash", snap.SmallModel)
	}
	if !snap.SmallModelOn {
		t.Errorf("SmallModelOn = false, want true")
	}
	if snap.AdvisorModel != "advisor-model" {
		t.Errorf("AdvisorModel = %q, want advisor-model", snap.AdvisorModel)
	}
	if snap.UpdatedAt == "" {
		t.Error("UpdatedAt is empty")
	}
}

// The headless snapshot must report the server's anchored workDir as CWD (the
// desktop shell sets it explicitly; os.Getwd() can be "/" for a Finder-launched
// .app and drift from the configured root).
func TestBuildStatusSnapshotUsesWorkDir(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.workDir = "/tmp/ocode-test-project"

	snap := h.buildStatusSnapshot()
	if snap.CWD != "/tmp/ocode-test-project" {
		t.Errorf("CWD = %q, want %q", snap.CWD, "/tmp/ocode-test-project")
	}
}

// With no workDir configured the snapshot falls back to the process cwd.
func TestBuildStatusSnapshotFallsBackToProcessCwd(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.workDir = ""

	snap := h.buildStatusSnapshot()
	if snap.CWD == "" {
		t.Error("CWD is empty, want process cwd fallback")
	}
}

// A web-initiated main-model change must broadcast a fresh "status" SSE event
// in headless mode so the web status bar doesn't keep showing the old model.
func TestPushStatusSnapshotBroadcastsInHeadlessMode(t *testing.T) {
	h := testHandlerWithConfig(t)
	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	h.mu.Lock()
	h.cfg.Model = "claude-sonnet-4-5"
	h.mu.Unlock()

	h.pushStatusSnapshot()

	select {
	case ev := <-sub:
		if ev.Event != "status" {
			t.Fatalf("event = %q, want \"status\"", ev.Event)
		}
		data, err := json.Marshal(ev.Data)
		if err != nil {
			t.Fatalf("marshal event data: %v", err)
		}
		var snap TUIStatus
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if snap.MainModel != "claude-sonnet-4-5" {
			t.Errorf("broadcast MainModel = %q, want claude-sonnet-4-5", snap.MainModel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no status event broadcast within 2s")
	}
}

// With an RC bridge attached, pushStatusSnapshot must preserve TUI-owned
// fields (session, cwd, context) while overriding the config-derived model
// fields — the web should mirror a web-initiated change immediately without
// clobbering TUI data.
func TestPushStatusSnapshotMergesWithBridgeSnapshot(t *testing.T) {
	h := testHandlerWithConfig(t)

	bridge := &RCBridge{}
	h.mu.Lock()
	h.rc = bridge
	h.mu.Unlock()

	// Simulate a TUI snapshot with TUI-owned fields set.
	bridge.StatusStore().Set(TUIStatus{
		MainModel:           "gpt-4o-mini",
		SmallModel:          "opencode-go/deepseek-v4-flash",
		SmallModelOn:        true,
		AdvisorModel:        "advisor-model",
		PermissionModel:     "prov/tui-owned",
		PermissionAutoAllow: false,
		SessionID:           "sess-tui-1",
		SessionTitle:        "TUI title",
		CWD:                 "/tmp",
		ContextMaxTokens:    200000,
	}, bridge)

	h.mu.Lock()
	h.cfg.Model = "claude-sonnet-4-5"
	h.cfg.Ocode.SmallModelEnabled = false
	h.cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "prov/from-web"}
	h.mu.Unlock()

	h.pushStatusSnapshot()

	snap := bridge.TUIStatus()
	if snap.MainModel != "claude-sonnet-4-5" {
		t.Errorf("MainModel = %q, want claude-sonnet-4-5", snap.MainModel)
	}
	if snap.SmallModelOn {
		t.Error("SmallModelOn = true, want false after toggle")
	}
	// The permission fields follow the same server-config-wins merge contract:
	// a web-side set must be visible even before the TUI's next organic push.
	if snap.PermissionModel != "prov/from-web" {
		t.Errorf("PermissionModel = %q, want prov/from-web after web-side set", snap.PermissionModel)
	}
	if !snap.PermissionAutoAllow {
		t.Error("PermissionAutoAllow = false, want true after web-side enable")
	}
	// TUI-owned fields must survive the merge.
	if snap.SessionID != "sess-tui-1" {
		t.Errorf("SessionID = %q, want sess-tui-1", snap.SessionID)
	}
	if snap.SessionTitle != "TUI title" {
		t.Errorf("SessionTitle = %q, want TUI title", snap.SessionTitle)
	}
	if snap.CWD != "/tmp" {
		t.Errorf("CWD = %q, want /tmp", snap.CWD)
	}
	if snap.ContextMaxTokens != 200000 {
		t.Errorf("ContextMaxTokens = %d, want 200000", snap.ContextMaxTokens)
	}
}

func TestHandleGetTUIStatusHeadlessFallback(t *testing.T) {
	h := testHandlerWithConfig(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/tui-status", nil)
	h.HandleGetTUIStatus(w, r, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var snap TUIStatus
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if snap.MainModel != "gpt-4o-mini" {
		t.Errorf("MainModel = %q, want gpt-4o-mini", snap.MainModel)
	}
	if !snap.SmallModelOn {
		t.Errorf("SmallModelOn = false, want true")
	}
}

// HandleSetSmallModel must reject a body that carries neither a model nor an
// enabled flag (returns before any persistence, so this is safe to test).
func TestHandleSetSmallModelRejectsEmptyBody(t *testing.T) {
	h := testHandlerWithConfig(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/small-model", strings.NewReader(`{}`))
	h.HandleSetSmallModel(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// The headless snapshot must always carry the thinking budget (0 = off is a
// meaningful value, not an absent field).
func TestBuildStatusSnapshotCarriesThinkingBudget(t *testing.T) {
	h := testHandlerWithConfig(t)
	snap := h.buildStatusSnapshot()
	if snap.ThinkingBudget != 0 {
		t.Errorf("ThinkingBudget = %d, want 0 (off) default", snap.ThinkingBudget)
	}
	h.mu.Lock()
	h.cfg.ThinkingBudget = 8000
	h.mu.Unlock()
	snap = h.buildStatusSnapshot()
	if snap.ThinkingBudget != 8000 {
		t.Errorf("ThinkingBudget = %d, want 8000", snap.ThinkingBudget)
	}
}

// pushStatusSnapshot must propagate a web-initiated budget change through the
// bridge merge so the sidebar reflects it immediately.
func TestPushStatusSnapshotMergesThinkingBudget(t *testing.T) {
	h := testHandlerWithConfig(t)
	bridge := &RCBridge{}
	h.mu.Lock()
	h.rc = bridge
	h.mu.Unlock()
	bridge.StatusStore().Set(TUIStatus{MainModel: "gpt-4o-mini", SessionID: "sess-tui-1"}, bridge)

	h.mu.Lock()
	h.cfg.ThinkingBudget = 16000
	h.mu.Unlock()

	h.pushStatusSnapshot()
	if got := bridge.TUIStatus().ThinkingBudget; got != 16000 {
		t.Errorf("merged ThinkingBudget = %d, want 16000", got)
	}
	if got := bridge.TUIStatus().SessionID; got != "sess-tui-1" {
		t.Errorf("SessionID = %q, want preserved sess-tui-1", got)
	}
}

func TestHandleGetThinkingBudgetHeadless(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.cfg.ThinkingBudget = 8000
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/thinking-budget", nil)
	h.HandleGetThinkingBudget(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Budget int                      `json:"budget"`
		Level  string                   `json:"level"`
		Levels []map[string]interface{} `json:"levels"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Budget != 8000 || resp.Level != "med" {
		t.Errorf("budget/level = %d/%q, want 8000/med", resp.Budget, resp.Level)
	}
	if len(resp.Levels) != len(config.ThinkingBudgetLevels) {
		t.Errorf("levels len = %d, want %d", len(resp.Levels), len(config.ThinkingBudgetLevels))
	}
}

func TestHandleGetThinkingBudgetPrefersBridge(t *testing.T) {
	h := testHandlerWithConfig(t)
	bridge := &RCBridge{}
	h.mu.Lock()
	h.rc = bridge
	h.mu.Unlock()
	bridge.StatusStore().Set(TUIStatus{MainModel: "gpt-4o-mini", ThinkingBudget: 32000}, bridge)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/thinking-budget", nil)
	h.HandleGetThinkingBudget(w, r)

	var resp struct {
		Budget int    `json:"budget"`
		Level  string `json:"level"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Budget != 32000 || resp.Level != "xhigh" {
		t.Errorf("budget/level = %d/%q, want 32000/xhigh", resp.Budget, resp.Level)
	}
}

func TestHandleSetThinkingBudgetByLevel(t *testing.T) {
	h := testHandlerWithConfig(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/thinking-budget", strings.NewReader(`{"level":"high"}`))
	h.HandleSetThinkingBudget(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Budget int    `json:"budget"`
		Level  string `json:"level"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Budget != 16000 || resp.Level != "high" {
		t.Errorf("budget/level = %d/%q, want 16000/high", resp.Budget, resp.Level)
	}
	if h.cfg.ThinkingBudget != 16000 {
		t.Errorf("h.cfg.ThinkingBudget = %d, want 16000", h.cfg.ThinkingBudget)
	}
}

func TestHandleSetThinkingBudgetByBudgetOff(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.cfg.ThinkingBudget = 8000
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/thinking-budget", strings.NewReader(`{"budget":0}`))
	h.HandleSetThinkingBudget(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Budget int    `json:"budget"`
		Level  string `json:"level"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Budget != 0 || resp.Level != "off" {
		t.Errorf("budget/level = %d/%q, want 0/off", resp.Budget, resp.Level)
	}
	if h.cfg.ThinkingBudget != 0 {
		t.Errorf("h.cfg.ThinkingBudget = %d, want 0", h.cfg.ThinkingBudget)
	}
}

func TestHandleSetThinkingBudgetRejectsBadInputs(t *testing.T) {
	h := testHandlerWithConfig(t)
	cases := []string{
		`{}`,
		`{"level":"ultra"}`,
		`{"budget":500}`,
		`{"level":"med","budget":8000}`,
	}
	for _, body := range cases {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/config/thinking-budget", strings.NewReader(body))
		h.HandleSetThinkingBudget(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %q → status %d, want 400", body, w.Code)
		}
	}
	// Nothing may have been persisted for invalid input.
	if h.cfg.ThinkingBudget != 0 {
		t.Errorf("h.cfg.ThinkingBudget = %d after invalid inputs, want 0", h.cfg.ThinkingBudget)
	}
}

// --- permission-model endpoint coverage (mirrors the small-model siblings) ---

func TestHandleSetPermissionModelRejectsEmptyBody(t *testing.T) {
	h := testHandlerWithConfig(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/permission-model", strings.NewReader(`{}`))
	h.HandleSetPermissionModel(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// The {model}/{enabled} fields are independently optional and must not
// clobber each other; "auto" normalizes to "" (fall back to the small model).
// HOME is redirected so config.Save* writes land in a temp dir, mirroring the
// pattern used by internal/config tests.
func TestHandleSetPermissionModelPartialUpdatesAndAutoNormalization(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "prov/old"}
	h.mu.Unlock()

	put := func(body string) (string, bool, int) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/config/permission-model", strings.NewReader(body))
		h.HandleSetPermissionModel(w, r)
		var resp struct {
			Model   string `json:"model"`
			Enabled bool   `json:"enabled"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		return resp.Model, resp.Enabled, w.Code
	}

	// Model-only update: enabled gate untouched.
	if model, enabled, code := put(`{"model":"prov/new"}`); code != 200 || model != "prov/new" || !enabled {
		t.Fatalf("model-only PUT = (%q, %v, %d), want (prov/new, true, 200)", model, enabled, code)
	}
	h.mu.Lock()
	got := *h.cfg.Ocode.Permissions.Auto
	h.mu.Unlock()
	if got.Model != "prov/new" || !got.Enabled {
		t.Fatalf("after model-only PUT, cfg auto = %+v, want Model=prov/new Enabled=true", got)
	}

	// Enabled-only update: model preserved.
	if model, enabled, code := put(`{"enabled":false}`); code != 200 || model != "prov/new" || enabled {
		t.Fatalf("enabled-only PUT = (%q, %v, %d), want (prov/new, false, 200)", model, enabled, code)
	}
	h.mu.Lock()
	got = *h.cfg.Ocode.Permissions.Auto
	h.mu.Unlock()
	if got.Model != "prov/new" || got.Enabled {
		t.Fatalf("after enabled-only PUT, cfg auto = %+v, want Model=prov/new Enabled=false", got)
	}

	// "auto" clears the override instead of persisting a literal model id.
	if model, _, code := put(`{"model":"auto"}`); code != 200 || model != "" {
		t.Fatalf(`"auto" PUT = (%q, %d), want ("", 200)`, model, code)
	}
	h.mu.Lock()
	got = *h.cfg.Ocode.Permissions.Auto
	h.mu.Unlock()
	if got.Model != "" {
		t.Fatalf(`"auto" must normalize to empty, got %q`, got.Model)
	}
}

func TestHandleGetPermissionModelPrefersLiveBridgeSnapshot(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: false, Model: "prov/from-config"}
	bridge := &RCBridge{}
	h.rc = bridge
	h.mu.Unlock()

	bridge.StatusStore().Set(TUIStatus{
		UpdatedAt:           "2026-08-23T00:00:00Z",
		PermissionModel:     "prov/live",
		PermissionAutoAllow: true,
	}, bridge)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/permission-model", nil)
	h.HandleGetPermissionModel(w, r)

	var resp struct {
		Model   string `json:"model"`
		Enabled bool   `json:"enabled"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Model != "prov/live" || !resp.Enabled {
		t.Fatalf("GET with live bridge = (%q, %v), want (prov/live, true)", resp.Model, resp.Enabled)
	}
}

// A nil Permissions.Auto block must not panic the headless snapshot build,
// and a populated block must flow into both new TUIStatus fields.
func TestBuildStatusSnapshotCarriesPermissionFields(t *testing.T) {
	h := testHandlerWithConfig(t)

	snap := h.buildStatusSnapshot() // Auto == nil
	if snap.PermissionModel != "" || snap.PermissionAutoAllow {
		t.Errorf("nil Auto snapshot = (%q, %v), want empty/false", snap.PermissionModel, snap.PermissionAutoAllow)
	}

	h.mu.Lock()
	h.cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "prov/judge"}
	h.mu.Unlock()
	snap = h.buildStatusSnapshot()
	if snap.PermissionModel != "prov/judge" || !snap.PermissionAutoAllow {
		t.Errorf("snapshot = (%q, %v), want (prov/judge, true)", snap.PermissionModel, snap.PermissionAutoAllow)
	}
}
