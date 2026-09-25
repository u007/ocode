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

func TestHandleGetChatVerbosityConfigDefaults(t *testing.T) {
	h := testConfigHandler(t)
	w := httptest.NewRecorder()
	h.HandleGetChatVerbosityConfig(w, httptest.NewRequest(http.MethodGet, "/api/config/ocode/chat-verbosity", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"effective"`) {
		t.Fatalf("GET response must not carry a redundant effective field: %s", w.Body.String())
	}
	var got struct {
		Preset    string                        `json:"preset"`
		Overrides config.ChatVerbosityOverrides `json:"overrides"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Preset != config.ChatVerbosityFull {
		t.Fatalf("preset = %q, want full", got.Preset)
	}
	if got.Overrides.OlderThinking != config.ChatDisplayPreset ||
		got.Overrides.ToolCalls != config.ChatDisplayPreset ||
		got.Overrides.ToolOutput != config.ChatDisplayPreset ||
		got.Overrides.ActivityNotices != config.ChatDisplayPreset {
		t.Fatalf("overrides = %+v, want all preset", got.Overrides)
	}
}

func TestHandleSetChatVerbosityConfigPersistsSpecKeysAndPublishes(t *testing.T) {
	h := testConfigHandler(t)
	events := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(events)

	body := `{"preset":"quiet","overrides":{"older_thinking":"expanded","tool_calls":"collapsed","tool_output":"collapsed","activity_notices":"collapsed"}}`
	w := httptest.NewRecorder()
	h.HandleSetChatVerbosityConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/chat-verbosity", strings.NewReader(body)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.ChatVerbosity
	h.mu.Unlock()
	if got.Preset != "quiet" || got.Overrides.OlderThinking != "expanded" || got.Overrides.ActivityNotices != "collapsed" {
		t.Fatalf("in-memory config = %+v, want quiet policy", got)
	}

	select {
	case event := <-events:
		if event.Event != "chat_verbosity_changed" {
			t.Fatalf("event = %q, want chat_verbosity_changed", event.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for chat_verbosity_changed")
	}
}

func TestHandleSetChatVerbosityConfigRejectsInvalidPreset(t *testing.T) {
	h := testConfigHandler(t)
	w := httptest.NewRecorder()
	h.HandleSetChatVerbosityConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/chat-verbosity", strings.NewReader(`{"preset":"loud"}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.ChatVerbosity
	h.mu.Unlock()
	if got.Preset == "loud" {
		t.Fatal("invalid request updated in-memory config")
	}
}

func TestHandleSetChatVerbosityConfigRejectsUnknownCategory(t *testing.T) {
	h := testConfigHandler(t)
	w := httptest.NewRecorder()
	h.HandleSetChatVerbosityConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/chat-verbosity", strings.NewReader(`{"preset":"full","overrides":{"notices":"collapsed"}}`)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown override category, body=%s", w.Code, w.Body.String())
	}
}
