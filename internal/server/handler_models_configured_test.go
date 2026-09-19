package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/auth"
)

// isolateModelListEnv redirects the config/auth/data dirs so favorites,
// recents and the auth store cannot leak the developer's real state into a
// model-list assertion.
func isolateModelListEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
}

func listModelsForTest(t *testing.T, h *Handler, target string) []ModelInfo {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleListModels(rec, httptest.NewRequest("GET", target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d", target, rec.Code)
	}
	var models []ModelInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("%s decode: %v", target, err)
	}
	return models
}

// configured=true must drop providers that have no credential/config path,
// while the default (no param) still returns the whole registry so direct API
// consumers keep full enumeration.
func TestListModelsConfiguredDropsUnconfiguredProviders(t *testing.T) {
	isolateModelListEnv(t)
	h := NewHandler()

	full := listModelsForTest(t, h, "/api/models")
	if len(full) == 0 {
		t.Skip("embedded models registry is empty")
	}
	filtered := listModelsForTest(t, h, "/api/models?configured=true")
	if len(filtered) == 0 {
		t.Fatal("configured=true returned no models")
	}
	if len(filtered) > len(full) {
		t.Fatalf("filtered (%d) grew past full (%d)", len(filtered), len(full))
	}
	if len(filtered) == len(full) {
		t.Fatalf("configured=true did not drop any of %d models", len(full))
	}

	// Every surviving provider must be reachable through a credential/config
	// path the server understands. Providers of recents/favorites are exempt
	// (they are added before the filter), and "other" is the no-slash fallback.
	allowed := map[string]bool{"lmstudio": true, "local": true, "other": true}
	for _, p := range auth.Providers {
		allowed[p.ID] = true
	}
	for id := range auth.List() {
		allowed[id] = true
	}
	for _, m := range filtered {
		if !allowed[m.Provider] {
			t.Errorf("provider %q survived configured=true with no credential/config path", m.Provider)
		}
	}
	// Registry-only aggregators are never in the auth provider list.
	for _, m := range filtered {
		if m.Provider == "nano-gpt" || m.Provider == "llmgateway-providers" {
			t.Errorf("unconfigured provider %q survived configured=true", m.Provider)
		}
	}
}

func TestListModelsDefaultKeepsUnconfiguredProviders(t *testing.T) {
	isolateModelListEnv(t)
	h := NewHandler()
	full := listModelsForTest(t, h, "/api/models")
	configured := listModelsForTest(t, h, "/api/models?configured=true")
	if len(full) <= len(configured) {
		t.Fatalf("default list must include unconfigured providers: full=%d configured=%d", len(full), len(configured))
	}
}

// configuredProviderIDs must treat a set env var as configuration and never
// report a registry-only aggregator as configured.
func TestConfiguredProviderIDsFromEnv(t *testing.T) {
	isolateModelListEnv(t)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	h := NewHandler()
	got := h.configuredProviderIDs()
	if !got["deepseek"] {
		t.Error("deepseek should be configured when DEEPSEEK_API_KEY is set")
	}
	if !got["lmstudio"] {
		t.Error("lmstudio is keyless/local and should always be configured")
	}
	if got["nano-gpt"] || got["llmgateway-providers"] {
		t.Errorf("registry-only aggregator reported as configured: %v", got)
	}
}

// TestConfiguredProviderIDsUsesAgentProviderEnvVars pins the env-var set to the
// agent package's authoritative provider table: providers absent from the
// curated auth.Providers registry (e.g. mistral, 302ai) must still count as
// configured when their env var is set, or an env-only user silently loses them
// from the model picker.
func TestConfiguredProviderIDsUsesAgentProviderEnvVars(t *testing.T) {
	isolateModelListEnv(t)
	t.Setenv("MISTRAL_API_KEY", "test-key")
	t.Setenv("302AI_API_KEY", "test-key")
	t.Setenv("XIAOMI_API_KEY", "test-key")
	h := NewHandler()
	got := h.configuredProviderIDs()
	for _, id := range []string{"mistral", "302ai", "xiaomi", "xiaomi-token-plan-sgp"} {
		if !got[id] {
			t.Errorf("%s should be configured when its env var is set (agent provider table)", id)
		}
	}
	if !got["lmstudio"] {
		t.Error("lmstudio is keyless/local and should always be configured")
	}
}
