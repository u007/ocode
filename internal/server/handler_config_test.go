package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
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
