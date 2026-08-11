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
