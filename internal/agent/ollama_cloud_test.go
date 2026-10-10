package agent

import (
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// TestNewClientOllamaCloud covers model parsing for the new provider, including
// the tag-bearing model id ("gemma4:31b") whose colon must not be mistaken for
// a provider separator.
// TestProvidersRegistered pins that each provider is registered in BOTH
// registries (auth.Providers for the key prompt, the client providers map for
// transport) with the same env var, plus the base URL where one is fixed.
func TestProvidersRegistered(t *testing.T) {
	cases := []struct {
		name, envVar, baseURL string
	}{
		{"groq", "GROQ_API_KEY", ""},
		{"grok", "XAI_API_KEY", ""},
		{"ollama-cloud", "OLLAMA_API_KEY", "https://ollama.com/v1"},
	}
	for _, tc := range cases {
		p := auth.FindProvider(tc.name)
		if p == nil {
			t.Errorf("%s not registered in auth.Providers", tc.name)
			continue
		}
		if p.EnvVar != tc.envVar {
			t.Errorf("%s auth EnvVar = %q, want %q", tc.name, p.EnvVar, tc.envVar)
		}
		info, ok := providers[tc.name]
		if !ok {
			t.Errorf("%s not present in client providers map", tc.name)
			continue
		}
		if info.envKey != tc.envVar {
			t.Errorf("%s providers envKey = %q, want %q", tc.name, info.envKey, tc.envVar)
		}
		if tc.baseURL != "" && info.baseURL != tc.baseURL {
			t.Errorf("%s providers baseURL = %q, want %q", tc.name, info.baseURL, tc.baseURL)
		}
	}
}

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
