package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// testConfigHandler builds a *Handler with a zero-valued in-memory OcodeConfig
// and an isolated HOME, so the Handle*Config setters under test persist to a
// throwaway ocodeconfig.json instead of the developer's real global config
// (~/.config/opencode/ocodeconfig.json).
func testConfigHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Ocode: config.OcodeConfig{},
	}
	h.mu.Unlock()
	return h
}

func TestHandleGetRecapConfigDefaults(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/ocode/recap", nil)
	h.HandleGetRecapConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		RecapModel          string `json:"recap_model"`
		RecapModelEnabled   bool   `json:"recap_model_enabled"`
		RecapTimeoutSeconds int    `json:"recap_timeout_seconds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.RecapModel != "" || resp.RecapModelEnabled {
		t.Errorf("expected zero-value defaults, got %+v", resp)
	}
}

func TestHandleSetRecapConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"recap_model":"gpt-4o-mini","recap_model_enabled":true,"recap_timeout_seconds":90}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/recap", strings.NewReader(body))
	h.HandleSetRecapConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.RecapModel != "gpt-4o-mini" || !got.RecapModelEnabled || got.RecapTimeoutSeconds != 90 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetCommitMsgConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"commit_msg_model":"claude-sonnet-5","commit_msg_prompt":"Write a concise commit message."}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/commit-msg", strings.NewReader(body))
	h.HandleSetCommitMsgConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.CommitMsgModel != "claude-sonnet-5" || got.CommitMsgPrompt != "Write a concise commit message." {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetCompactConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"enabled":true,"summary_provider":"anthropic","summary_model":"claude-haiku-4-5",` +
		`"token_threshold":0.8,"keep_recent_turns":4,"keep_recent_tokens":2000,"min_messages":6,` +
		`"summary_timeout_seconds":30,"summary_max_retries":2,"max_summary_input_tokens":50000}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/compact", strings.NewReader(body))
	h.HandleSetCompactConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Compact
	h.mu.Unlock()
	if !got.Enabled || got.SummaryModel != "claude-haiku-4-5" || got.KeepRecentTurns != 4 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetAutoPermissionConfigPersists(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Model: "existing-model"}
	h.mu.Unlock()

	body := `{"enabled":true,"allow_destructive":false,"prompt":"custom prompt",` +
		`"max_context_bytes":4096,"max_context_sources":3,"max_context_lines_per_source":50,` +
		`"min_confidence":0.9,"grants":[]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/permissions-auto", strings.NewReader(body))
	h.HandleSetAutoPermissionConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Permissions.Auto
	h.mu.Unlock()
	if got == nil || !got.Enabled || got.MaxContextBytes != 4096 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
	if got.Model != "existing-model" {
		t.Errorf("Model must be preserved, got %q", got.Model)
	}
}

// TestHandleGetPermissionConcernsServesRubricCatalog pins the settings checkbox
// catalog to the judge's own rubric: the endpoint must expose every concern
// category except "none", in rubric order, with the partly-gated caveats.
func TestHandleGetPermissionConcernsServesRubricCatalog(t *testing.T) {
	h := testConfigHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/ocode/permissions-concerns", nil)
	h.HandleGetPermissionConcerns(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got struct {
		Concerns []agent.RelaxableConcern `json:"concerns"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	want := agent.RelaxableConcerns()
	if len(got.Concerns) != len(want) || len(want) == 0 {
		t.Fatalf("catalog size = %d, want %d", len(got.Concerns), len(want))
	}
	for i := range want {
		if got.Concerns[i].Key != want[i].Key || got.Concerns[i].Label != want[i].Label {
			t.Errorf("concerns[%d] = %+v, want %+v", i, got.Concerns[i], want[i])
		}
	}
	for _, c := range got.Concerns {
		if c.Key == "none" {
			t.Error("\"none\" must not be offered as an enforceable category")
		}
	}
}

// The PUT must round-trip the negative enforcement set (and the setter must not
// invent a default: an omitted field stays empty = everything enforced).
func TestHandleSetAutoPermissionConfigPersistsRelaxedConcerns(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/permissions-auto",
		strings.NewReader(`{"enabled":true,"relaxed_concerns":["secrets","network"]}`))
	h.HandleSetAutoPermissionConfig(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Permissions.Auto
	h.mu.Unlock()
	if got == nil || len(got.RelaxedConcerns) != 2 || !got.ConcernRelaxed("secrets") {
		t.Fatalf("in-memory relaxed_concerns not updated: %+v", got)
	}

	// A later save with no opt-outs clears them (the form always sends the
	// complete list, so the writer must not merge).
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("PUT", "/api/config/ocode/permissions-auto",
		strings.NewReader(`{"enabled":true,"relaxed_concerns":[]}`))
	h.HandleSetAutoPermissionConfig(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w2.Code, w2.Body.String())
	}
	h.mu.Lock()
	got = h.cfg.Ocode.Permissions.Auto
	h.mu.Unlock()
	if len(got.RelaxedConcerns) != 0 {
		t.Fatalf("relaxed_concerns should be cleared, got %#v", got.RelaxedConcerns)
	}
}

// The PUT must push the new auto-permission config to every live agent so an
// already-open chat honors the change on its next judge call. Without the push,
// the resident agent keeps the config it was built with (the reported bug:
// unchecking a concern in Settings had no effect on a running session).
func TestHandleSetAutoPermissionConfigPushesToLiveAgents(t *testing.T) {
	h := testConfigHandler(t)

	agentCfg := &config.Config{}
	agentCfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{
		Enabled:         true,
		Model:           "mock/judge",
		RelaxedConcerns: []string{"secrets"},
	}
	live := agent.NewAgent(nil, nil, agentCfg, nil)
	h.mu.Lock()
	h.agents["ses_live"] = &agentSession{agent: live}
	h.mu.Unlock()

	if got := live.Permissions().AutoPermissionConfig(); got == nil || !got.ConcernRelaxed("secrets") {
		t.Fatalf("precondition: live agent config = %+v", got)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/permissions-auto",
		strings.NewReader(`{"enabled":true,"model":"mock/judge","relaxed_concerns":[]}`))
	h.HandleSetAutoPermissionConfig(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	got := live.Permissions().AutoPermissionConfig()
	if got == nil {
		t.Fatal("live agent lost its auto config")
	}
	if got.ConcernRelaxed("secrets") {
		t.Fatalf("live agent still relaxes secrets after the push: %+v", got.RelaxedConcerns)
	}
	if !live.Permissions().AutoPermissionEnabled() {
		t.Fatal("live agent auto-permission should remain enabled")
	}
}

func TestHandleSetDiscoveryConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"enabled":true,"embedding_model":"bge-m3","embedding_backend":"local",` +
		`"local_model_status":"ready","local_server_url":"","pinned_skills":["foo"],"ignore_paths":["dist/"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/discovery", strings.NewReader(body))
	h.HandleSetDiscoveryConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Discovery
	h.mu.Unlock()
	if !got.Enabled || got.EmbeddingModel != "bge-m3" || len(got.PinnedSkills) != 1 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleGetPermissionModeConfigDefaultsToNormal(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/ocode/permissions-mode", nil)
	h.HandleGetPermissionModeConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp permissionModeConfigDTO
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Mode != "normal" {
		t.Errorf("mode = %q, want normal", resp.Mode)
	}
}

func TestHandleSetPermissionModeConfigPersistsSandbox(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/permissions-mode", strings.NewReader(`{"mode":"sandbox"}`))
	h.HandleSetPermissionModeConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Permissions.Mode
	h.mu.Unlock()
	if got != "sandbox" {
		t.Errorf("in-memory cfg mode = %q, want sandbox (Decision 2 override: sandbox may persist as default)", got)
	}

	// Confirm it round-trips through GET too.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/config/ocode/permissions-mode", nil)
	h.HandleGetPermissionModeConfig(w2, r2)
	var resp permissionModeConfigDTO
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Mode != "sandbox" {
		t.Errorf("GET mode = %q, want sandbox", resp.Mode)
	}
}

func TestHandleSetPermissionModeConfigRejectsInvalid(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/permissions-mode", strings.NewReader(`{"mode":"bogus"}`))
	h.HandleSetPermissionModeConfig(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleSetTUIConfigSectionPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"theme":"dracula","mouse":true,"scroll_speed":2.5,"keybinds":{"quit":"ctrl+c"},` +
		`"leader_timeout":1000,"branchless":false}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/tui", strings.NewReader(body))
	h.HandleSetTUIConfigSection(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.TUI
	h.mu.Unlock()
	if got.Theme != "dracula" || got.Mouse == nil || !*got.Mouse || got.Keybinds["quit"] != "ctrl+c" {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetEditorConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"editor":"code","editor_mode":"external","ide_mode":"none"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/editor", strings.NewReader(body))
	h.HandleSetEditorConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.Editor != "code" || got.EditorMode != "external" || got.IDEMode != "none" {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetImageGenConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"enabled":true,"provider":"gemini","model":"gemini-3-pro-image","output_path":"/tmp/img","timeout":120}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/imagegen", strings.NewReader(body))
	h.HandleSetImageGenConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.ImageGen
	h.mu.Unlock()
	if !got.Enabled || got.Provider != "gemini" || got.Model != "gemini-3-pro-image" || got.Timeout != 120 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetPathsConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"extra_allowed_paths":["/tmp/scratch","/data"],"upload_dir":"/data/uploads"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/paths", strings.NewReader(body))
	h.HandleSetPathsConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if len(got.ExtraAllowedPaths) != 2 || got.UploadDir != "/data/uploads" {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

// GET /api/config/ocode/paths must report the server's GOOS so the Files-tab
// "reveal in file manager" action can label itself (Finder/Explorer/File
// Manager) from the machine the command actually runs on, rather than the
// browser's navigator.platform.
func TestHandleGetPathsConfigIncludesPlatform(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	h.HandleGetPathsConfig(w, httptest.NewRequest("GET", "/api/config/ocode/paths", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := body["platform"].(string); got != runtime.GOOS {
		t.Errorf("platform = %q, want %q", got, runtime.GOOS)
	}
}

func TestHandleSetLimitsConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	// Note: the on-wire key is image_max_dim (the struct JSON tag), not
	// max_image_dim — struct tags are the source of truth (plan constraint #19).
	body := `{"max_steps":150,"image_max_dim":2500,"max_concurrent_agents":4,"undo_max_age_delta":8}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/limits", strings.NewReader(body))
	h.HandleSetLimitsConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.MaxSteps != 150 || got.MaxImageDim != 2500 || got.MaxConcurrentAgents != 4 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
	if got.UndoMaxAgeDelta != 8 {
		t.Errorf("undo_max_age_delta not updated: %d", got.UndoMaxAgeDelta)
	}
}

func TestHandleSetBrowserConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"chrome_path":"","idle_timeout_minutes":5,"screencast_quality":92}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/browser", strings.NewReader(body))
	h.HandleSetBrowserConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Browser
	h.mu.Unlock()
	if got.IdleTimeoutMinutes != 5 {
		t.Errorf("idle_timeout_minutes not updated: %d", got.IdleTimeoutMinutes)
	}
	if got.ScreencastQuality != 92 {
		t.Errorf("screencast_quality not updated: %d", got.ScreencastQuality)
	}

	// Out-of-range quality is rejected.
	w = httptest.NewRecorder()
	r = httptest.NewRequest("PUT", "/api/config/ocode/browser", strings.NewReader(`{"screencast_quality":101}`))
	h.HandleSetBrowserConfig(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleSetFeaturesConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"memory_enabled":true,"doc_prompt_enabled":false}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/features", strings.NewReader(body))
	h.HandleSetFeaturesConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if !got.MemoryEnabled || got.DocPromptEnabled {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetPluginsEnabledConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"ast":true}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/plugins-enabled", strings.NewReader(body))
	h.HandleSetPluginsEnabledConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Plugins
	h.mu.Unlock()
	if !got.AST {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetLocalModelsConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"local/bonsai-8b-1bit":{"enabled":true,"max_parallel":2}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/local-models", strings.NewReader(body))
	h.HandleSetLocalModelsConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.LocalModels
	h.mu.Unlock()
	lm, ok := got["local/bonsai-8b-1bit"]
	if !ok || !lm.Enabled || lm.MaxParallel != 2 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetAdvisorPreservesUnsetFields(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.Advisor = config.AdvisorConfig{
		Model: "old-model", Provider: "anthropic", ClaudeCode: true, Checkpoints: []string{"done"},
	}
	h.mu.Unlock()

	// Only model is sent — provider/claude_code/checkpoints must be preserved.
	body := `{"model":"new-model"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/advisor", strings.NewReader(body))
	h.HandleSetAdvisor(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Advisor
	h.mu.Unlock()
	if got.Model != "new-model" {
		t.Errorf("Model = %q, want new-model", got.Model)
	}
	if got.Provider != "anthropic" || !got.ClaudeCode || len(got.Checkpoints) != 1 {
		t.Errorf("unset fields were cleared: %+v", got)
	}
}

func TestHandleSetAdvisorSetsProviderClaudeCode(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"provider":"claude-code","model":"claude-sonnet-4-6","checkpoints":["plan","done"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/advisor", strings.NewReader(body))
	h.HandleSetAdvisor(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Advisor
	h.mu.Unlock()
	if got.Provider != "claude-code" || !got.ClaudeCode || got.Model != "claude-sonnet-4-6" {
		t.Errorf("advisor not updated: %+v", got)
	}
	if len(got.Checkpoints) != 2 {
		t.Errorf("checkpoints not updated: %+v", got.Checkpoints)
	}
}

func TestHandleGetMaskConfigIncludesAdvancedFields(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.Security.Redaction = config.RedactionConfig{
		Enabled: true, Model: "m", BaseURL: "http://localhost:11434", FailMode: "block",
		AllowRemoteTier2: true, CustomWords: []string{"secret1"},
	}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/mask", nil)
	h.HandleGetMaskConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		BaseURL          string   `json:"base_url"`
		FailMode         string   `json:"fail_mode"`
		AllowRemoteTier2 bool     `json:"allow_remote_tier2"`
		CustomWords      []string `json:"custom_words"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.BaseURL != "http://localhost:11434" || resp.FailMode != "block" ||
		!resp.AllowRemoteTier2 || len(resp.CustomWords) != 1 || resp.CustomWords[0] != "secret1" {
		t.Errorf("advanced fields missing from GET response: %+v", resp)
	}
}

func TestHandleSetMaskAdvancedPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"base_url":"http://localhost:11434","fail_mode":"warn","allow_remote_tier2":true,` +
		`"custom_words":["acme","secret1"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/mask/advanced", strings.NewReader(body))
	h.HandleSetMaskAdvanced(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Security.Redaction
	h.mu.Unlock()
	if got.BaseURL != "http://localhost:11434" || got.FailMode != "warn" ||
		!got.AllowRemoteTier2 || len(got.CustomWords) != 2 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetMaskAdvancedRejectsBadFailMode(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"fail_mode":"explode"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/mask/advanced", strings.NewReader(body))
	h.HandleSetMaskAdvanced(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// Regression for the 2026-08-23 resume-bootstrap failure: a bare model id
// ("gpt-4o-mini" with no provider prefix) persisted as last_model made every
// later start/resume build its client from an unresolvable string, which
// NewClient refuses ("no API key for provider openai"). HandleSetModel must
// reject such ids instead of poisoning persisted state.
func TestHandleSetModelRejectsProviderlessModel(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/model", strings.NewReader(`{"model":"gpt-4o-mini"}`))
	h.HandleSetModel(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := config.GetLastModel(); got != "" {
		t.Fatalf("last_model = %q, want empty (bare id must not be persisted)", got)
	}
	if h.cfg.Model != "" {
		t.Fatalf("cfg.Model = %q, want unchanged", h.cfg.Model)
	}

	// A properly prefixed id is still accepted.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("PUT", "/api/config/model", strings.NewReader(`{"model":"openai/gpt-4o-mini"}`))
	h.HandleSetModel(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("prefixed id: status = %d, want 200", w2.Code)
	}
}

// TestHandleSetBackendConfigRejectsWithMigrationHint verifies that posting the
// legacy production hub URL returns 400 with a migration-specific message
// pointing at the new sync_url setting, rather than a generic "invalid URL"
// error. backend_url is local-dev-only as of the backend/sync split.
func TestHandleSetBackendConfigRejectsWithMigrationHint(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/backend",
		strings.NewReader(`{"backend_url":"https://hub.mercstudio.com"}`))
	h.HandleSetBackendConfig(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Error, "sync_url") {
		t.Fatalf("error should mention the sync_url migration, got %q", resp.Error)
	}
	if strings.Contains(resp.Error, "must be http for localhost") {
		t.Fatalf("error should be the migration hint, not a generic validation error: %q", resp.Error)
	}

	// In-memory cfg must not be mutated by the rejected request.
	h.mu.Lock()
	got := h.cfg.Ocode.BackendURL
	h.mu.Unlock()
	if got != "" {
		t.Fatalf("BackendURL should remain empty on rejected hub value, got %q", got)
	}
}

// TestHandleSetBackendConfigAcceptsLocalhost verifies the migration is scoped:
// a valid localhost backend_url still normalizes and persists normally.
func TestHandleSetBackendConfigAcceptsLocalhost(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/backend",
		strings.NewReader(`{"backend_url":"http://localhost:4096/"}`))
	h.HandleSetBackendConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.BackendURL
	h.mu.Unlock()
	if got != "http://localhost:4096" {
		t.Fatalf("BackendURL = %q, want http://localhost:4096", got)
	}
}

// stubHTRSeams replaces the HTR lifecycle seams with fakes so handler tests
// never resolve assets, probe a port, or spawn a process.
func stubHTRSeams(t *testing.T) (*int, *int, *bool) {
	t.Helper()
	origEnsure, origStop, origStatus, origTabs, origOpts := ensureHTRServeFn, stopHTRServeFn, htrDaemonStatusFn, listHTRTabsFn, htrOptionsFn
	t.Cleanup(func() {
		ensureHTRServeFn, stopHTRServeFn, htrDaemonStatusFn, listHTRTabsFn, htrOptionsFn = origEnsure, origStop, origStatus, origTabs, origOpts
	})
	startCalls, stopCalls := 0, 0
	running := false
	ensureHTRServeFn = func(sup *tool.ProcessSupervisor, opts cdp.HTROptions, lg *log.Logger) (cdp.HTRStatus, error) {
		startCalls++
		running = true
		return cdp.HTRStatus{Running: true, Addr: "127.0.0.1:3846"}, nil
	}
	stopHTRServeFn = func(sup *tool.ProcessSupervisor, port int, lg *log.Logger) (cdp.HTRStatus, error) {
		stopCalls++
		running = false
		return cdp.HTRStatus{Running: false}, nil
	}
	htrDaemonStatusFn = func(port int, socketPath string) cdp.HTRDaemonInfo {
		return cdp.HTRDaemonInfo{Running: running, Managed: running, Addr: "127.0.0.1:3846", Port: 3846, Binary: "/opt/htrcli"}
	}
	listHTRTabsFn = func(port int) ([]cdp.HTRTab, error) {
		return []cdp.HTRTab{{ID: 3, URL: "https://example.com", Title: "Example", Active: true, Browser: "chrome"}}, nil
	}
	htrOptionsFn = func(browser config.BrowserConfig) (cdp.HTROptions, string) {
		return cdp.HTROptions{Enabled: browser.HTREnabled, Port: browser.HTRPort}, ""
	}
	return &startCalls, &stopCalls, &running
}

func TestHandleHTRLifecycle(t *testing.T) {
	h := testConfigHandler(t)
	startCalls, stopCalls, _ := stubHTRSeams(t)

	// Status before start: stopped and disabled (zero-value browser config).
	var st htrStatusResponse
	w := httptest.NewRecorder()
	h.HandleGetHTRStatus(w, httptest.NewRequest("GET", "/api/config/ocode/htr", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Running || st.Enabled {
		t.Fatalf("pre-start status = %+v", st)
	}

	// Start: enables, starts exactly one, reports the port label.
	w = httptest.NewRecorder()
	h.HandleStartHTR(w, httptest.NewRequest("POST", "/api/config/ocode/htr/start", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("start code = %d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if *startCalls != 1 {
		t.Fatalf("start calls = %d, want 1", *startCalls)
	}
	if !st.Enabled || !st.Running || st.Port != 3846 || st.Addr != "127.0.0.1:3846" || st.Binary != "/opt/htrcli" {
		t.Fatalf("start status = %+v", st)
	}
	h.mu.Lock()
	enabled := h.cfg.Ocode.Browser.HTREnabled
	h.mu.Unlock()
	if !enabled {
		t.Fatal("start must persist htr_enabled=true")
	}

	// List tabs.
	w = httptest.NewRecorder()
	h.HandleListHTRTabs(w, httptest.NewRequest("GET", "/api/config/ocode/htr/tabs", nil))
	var tabsResp struct {
		Tabs  []cdp.HTRTab `json:"tabs"`
		Error string       `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tabsResp); err != nil {
		t.Fatal(err)
	}
	if tabsResp.Error != "" || len(tabsResp.Tabs) != 1 || tabsResp.Tabs[0].ID != 3 {
		t.Fatalf("tabs response = %+v", tabsResp)
	}

	// Stop: disables and stops.
	w = httptest.NewRecorder()
	h.HandleStopHTR(w, httptest.NewRequest("POST", "/api/config/ocode/htr/stop", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("stop code = %d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if *stopCalls != 1 {
		t.Fatalf("stop calls = %d, want 1", *stopCalls)
	}
	if st.Enabled || st.Running {
		t.Fatalf("stop status = %+v", st)
	}
	h.mu.Lock()
	enabled = h.cfg.Ocode.Browser.HTREnabled
	h.mu.Unlock()
	if enabled {
		t.Fatal("stop must persist htr_enabled=false")
	}

	// PUT /browser with htr_enabled live-applies the toggle.
	w = httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/browser", strings.NewReader(`{"screencast_quality":90,"htr_enabled":true}`))
	h.HandleSetBrowserConfig(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("browser PUT code = %d body=%s", w.Code, w.Body.String())
	}
	if *startCalls != 2 {
		t.Fatalf("PUT htr_enabled=true must live-start: start calls = %d, want 2", *startCalls)
	}
}

func TestHandleStartHTRSurfacesFailure(t *testing.T) {
	h := testConfigHandler(t)
	origEnsure, origStatus, origOpts := ensureHTRServeFn, htrDaemonStatusFn, htrOptionsFn
	t.Cleanup(func() {
		ensureHTRServeFn, htrDaemonStatusFn, htrOptionsFn = origEnsure, origStatus, origOpts
	})
	htrOptionsFn = func(browser config.BrowserConfig) (cdp.HTROptions, string) {
		return cdp.HTROptions{Enabled: true}, ""
	}
	htrDaemonStatusFn = func(port int, socketPath string) cdp.HTRDaemonInfo {
		return cdp.HTRDaemonInfo{Addr: "127.0.0.1:3846", Port: 3846}
	}
	ensureHTRServeFn = func(sup *tool.ProcessSupervisor, opts cdp.HTROptions, lg *log.Logger) (cdp.HTRStatus, error) {
		return cdp.HTRStatus{}, errors.New("htrcli not found")
	}

	w := httptest.NewRecorder()
	h.HandleStartHTR(w, httptest.NewRequest("POST", "/api/config/ocode/htr/start", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var st htrStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Running || !strings.Contains(st.Error, "htrcli not found") {
		t.Fatalf("failure status = %+v", st)
	}

	// PUT /browser live-apply must surface the same failure as htr_error.
	w = httptest.NewRecorder()
	h.HandleSetBrowserConfig(w, httptest.NewRequest("PUT", "/api/config/ocode/browser", strings.NewReader(`{"screencast_quality":90,"htr_enabled":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("browser PUT code = %d", w.Code)
	}
	var put map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &put); err != nil {
		t.Fatal(err)
	}
	if got, _ := put["htr_error"].(string); !strings.Contains(got, "htrcli not found") {
		t.Fatalf("browser PUT htr_error = %q, want start failure", got)
	}
}
