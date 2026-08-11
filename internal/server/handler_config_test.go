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
