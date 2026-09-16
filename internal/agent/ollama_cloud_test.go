package agent

import (
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// TestOllamaCloudProviderRegistered pins the ollama-cloud registration in both
// registries plus its base URL. A silent drift in the map key, env var, or base
// URL would otherwise break the provider with no failing test (mirrors
// TestGrokProviderRegistered).
func TestOllamaCloudProviderRegistered(t *testing.T) {
	p := auth.FindProvider("ollama-cloud")
	if p == nil {
		t.Fatal("ollama-cloud provider not registered in auth.Providers")
	}
	if p.EnvVar != "OLLAMA_API_KEY" {
		t.Errorf("EnvVar = %q, want OLLAMA_API_KEY", p.EnvVar)
	}

	info, ok := providers["ollama-cloud"]
	if !ok {
		t.Fatal("ollama-cloud not present in client providers map")
	}
	if info.envKey != "OLLAMA_API_KEY" {
		t.Errorf("providers envKey = %q, want OLLAMA_API_KEY", info.envKey)
	}
	if info.baseURL != "https://ollama.com/v1" {
		t.Errorf("providers baseURL = %q, want https://ollama.com/v1", info.baseURL)
	}
}

// TestNewClientOllamaCloud covers model parsing for the new provider, including
// the tag-bearing model id ("gemma4:31b") whose colon must not be mistaken for
// a provider separator.
func TestNewClientOllamaCloud(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "ollama-test-key")
	cfg := &config.Config{}

	for _, model := range []string{"ollama-cloud/gemma4:31b", "ollama-cloud:gemma4:31b"} {
		client := NewClient(cfg, model)
		if client == nil {
			t.Fatalf("NewClient(%q) returned nil", model)
		}
		gc, ok := client.(*GenericClient)
		if !ok {
			t.Fatalf("NewClient(%q) = %T, want *GenericClient", model, client)
		}
		if gc.Provider != "ollama-cloud" {
			t.Errorf("NewClient(%q).Provider = %q, want ollama-cloud", model, gc.Provider)
		}
		if gc.Model != "gemma4:31b" {
			t.Errorf("NewClient(%q).Model = %q, want gemma4:31b", model, gc.Model)
		}
		if gc.BaseURL != "https://ollama.com/v1" {
			t.Errorf("NewClient(%q).BaseURL = %q, want https://ollama.com/v1", model, gc.BaseURL)
		}
		if gc.APIKey == "" {
			t.Errorf("NewClient(%q).APIKey is empty, want the env key", model)
		}
	}
}
