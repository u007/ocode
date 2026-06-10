package auth

import (
	"os"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestResolveKeyWithConfig_OPENCODE_AUTH_TOKEN(t *testing.T) {
	// Save original env
	origToken := os.Getenv("OPENCODE_AUTH_TOKEN")
	origOpenAIKey := os.Getenv("OPENAI_API_KEY")
	defer func() {
		os.Setenv("OPENCODE_AUTH_TOKEN", origToken)
		os.Setenv("OPENAI_API_KEY", origOpenAIKey)
	}()

	// Test 1: OPENCODE_AUTH_TOKEN takes precedence over everything
	os.Setenv("OPENCODE_AUTH_TOKEN", "global-token")
	os.Setenv("OPENAI_API_KEY", "provider-env-token")

	p := FindProvider("openai")
	if p == nil {
		t.Fatal("openai provider not found")
	}

	cfg := &config.Config{
		Provider: map[string]interface{}{
			"openai": map[string]interface{}{
				"options": map[string]interface{}{
					"apiKey": "config-token",
				},
			},
		},
	}

	key := resolveKeyWithConfig(p, "openai", cfg)
	if key != "global-token" {
		t.Errorf("expected OPENCODE_AUTH_TOKEN to take precedence, got %q", key)
	}
}

func TestResolveKeyWithConfig_ProviderEnvVar(t *testing.T) {
	// Save original env
	origToken := os.Getenv("OPENCODE_AUTH_TOKEN")
	origOpenAIKey := os.Getenv("OPENAI_API_KEY")
	defer func() {
		os.Setenv("OPENCODE_AUTH_TOKEN", origToken)
		os.Setenv("OPENAI_API_KEY", origOpenAIKey)
	}()

	// Test 2: Provider env var used when OPENCODE_AUTH_TOKEN not set
	os.Unsetenv("OPENCODE_AUTH_TOKEN")
	os.Setenv("OPENAI_API_KEY", "provider-env-token")

	p := FindProvider("openai")
	if p == nil {
		t.Fatal("openai provider not found")
	}

	cfg := &config.Config{
		Provider: map[string]interface{}{
			"openai": map[string]interface{}{
				"options": map[string]interface{}{
					"apiKey": "config-token",
				},
			},
		},
	}

	key := resolveKeyWithConfig(p, "openai", cfg)
	if key != "provider-env-token" {
		t.Errorf("expected provider env var to be used, got %q", key)
	}
}

func TestResolveKeyWithConfig_ConfigAPIKey(t *testing.T) {
	// Save original env
	origToken := os.Getenv("OPENCODE_AUTH_TOKEN")
	origOpenAIKey := os.Getenv("OPENAI_API_KEY")
	defer func() {
		os.Setenv("OPENCODE_AUTH_TOKEN", origToken)
		os.Setenv("OPENAI_API_KEY", origOpenAIKey)
	}()

	// Test 3: Config apiKey used when no env vars set
	os.Unsetenv("OPENCODE_AUTH_TOKEN")
	os.Unsetenv("OPENAI_API_KEY")

	p := FindProvider("openai")
	if p == nil {
		t.Fatal("openai provider not found")
	}

	cfg := &config.Config{
		Provider: map[string]interface{}{
			"openai": map[string]interface{}{
				"options": map[string]interface{}{
					"apiKey": "config-token",
				},
			},
		},
	}

	key := resolveKeyWithConfig(p, "openai", cfg)
	if key != "config-token" {
		t.Errorf("expected config apiKey to be used, got %q", key)
	}
}

func TestResolveKeyWithConfig_ConfigEnvRef(t *testing.T) {
	// Save original env
	origToken := os.Getenv("OPENCODE_AUTH_TOKEN")
	origOpenAIKey := os.Getenv("OPENAI_API_KEY")
	origCustomKey := os.Getenv("CUSTOM_API_KEY")
	defer func() {
		os.Setenv("OPENCODE_AUTH_TOKEN", origToken)
		os.Setenv("OPENAI_API_KEY", origOpenAIKey)
		os.Setenv("CUSTOM_API_KEY", origCustomKey)
	}()

	// Test 4: Config {env:VAR} reference resolved
	os.Unsetenv("OPENCODE_AUTH_TOKEN")
	os.Unsetenv("OPENAI_API_KEY")
	os.Setenv("CUSTOM_API_KEY", "from-env-ref")

	p := FindProvider("openai")
	if p == nil {
		t.Fatal("openai provider not found")
	}

	cfg := &config.Config{
		Provider: map[string]interface{}{
			"openai": map[string]interface{}{
				"options": map[string]interface{}{
					"apiKey": "{env:CUSTOM_API_KEY}",
				},
			},
		},
	}

	key := resolveKeyWithConfig(p, "openai", cfg)
	if key != "from-env-ref" {
		t.Errorf("expected config env ref to be resolved, got %q", key)
	}
}

func TestResolveKeyWithConfig_StoredCredential(t *testing.T) {
	// Save original env
	origToken := os.Getenv("OPENCODE_AUTH_TOKEN")
	origOpenAIKey := os.Getenv("OPENAI_API_KEY")
	defer func() {
		os.Setenv("OPENCODE_AUTH_TOKEN", origToken)
		os.Setenv("OPENAI_API_KEY", origOpenAIKey)
	}()

	// Test 5: Stored credential used as fallback
	os.Unsetenv("OPENCODE_AUTH_TOKEN")
	os.Unsetenv("OPENAI_API_KEY")

	// Reset store for test
	home := t.TempDir()
	os.Setenv("HOME", home)
	os.Setenv("APPDATA", home)

	storeMu.Lock()
	cache = nil
	cacheLoaded = false
	storeMu.Unlock()

	// Store a credential
	err := Set("openai", Credential{Kind: KindAPIKey, Key: "stored-token"})
	if err != nil {
		t.Fatalf("failed to store credential: %v", err)
	}

	p := FindProvider("openai")
	if p == nil {
		t.Fatal("openai provider not found")
	}

	cfg := &config.Config{} // empty config

	key := resolveKeyWithConfig(p, "openai", cfg)
	if key != "stored-token" {
		t.Errorf("expected stored credential to be used, got %q", key)
	}
}

func TestResolveKeyWithConfig_PrecedenceOrder(t *testing.T) {
	// Save original env
	origToken := os.Getenv("OPENCODE_AUTH_TOKEN")
	origOpenAIKey := os.Getenv("OPENAI_API_KEY")
	origCustomKey := os.Getenv("CUSTOM_API_KEY")
	defer func() {
		os.Setenv("OPENCODE_AUTH_TOKEN", origToken)
		os.Setenv("OPENAI_API_KEY", origOpenAIKey)
		os.Setenv("CUSTOM_API_KEY", origCustomKey)
	}()

	// Test full precedence: OPENCODE_AUTH_TOKEN > provider env > config > stored
	os.Setenv("OPENCODE_AUTH_TOKEN", "global-token")
	os.Setenv("OPENAI_API_KEY", "provider-env-token")
	os.Setenv("CUSTOM_API_KEY", "from-env-ref")

	p := FindProvider("openai")
	if p == nil {
		t.Fatal("openai provider not found")
	}

	cfg := &config.Config{
		Provider: map[string]interface{}{
			"openai": map[string]interface{}{
				"options": map[string]interface{}{
					"apiKey": "{env:CUSTOM_API_KEY}",
				},
			},
		},
	}

	key := resolveKeyWithConfig(p, "openai", cfg)
	if key != "global-token" {
		t.Errorf("expected OPENCODE_AUTH_TOKEN to win, got %q", key)
	}

	// Now test without OPENCODE_AUTH_TOKEN
	os.Unsetenv("OPENCODE_AUTH_TOKEN")
	key = resolveKeyWithConfig(p, "openai", cfg)
	if key != "provider-env-token" {
		t.Errorf("expected provider env var to win, got %q", key)
	}

	// Now test without provider env var
	os.Unsetenv("OPENAI_API_KEY")
	key = resolveKeyWithConfig(p, "openai", cfg)
	if key != "from-env-ref" {
		t.Errorf("expected config env ref to win, got %q", key)
	}
}

func TestResolveEnvVarRef(t *testing.T) {
	origKey := os.Getenv("TEST_VAR")
	defer func() { os.Setenv("TEST_VAR", origKey) }()

	os.Setenv("TEST_VAR", "test-value")

	// Test normal value
	result := ResolveEnvVarRef("plain-value")
	if result != "plain-value" {
		t.Errorf("expected plain value unchanged, got %q", result)
	}

	// Test env ref
	result = ResolveEnvVarRef("{env:TEST_VAR}")
	if result != "test-value" {
		t.Errorf("expected env var resolved, got %q", result)
	}

	// Test unset env var
	result = ResolveEnvVarRef("{env:UNSET_VAR}")
	if result != "" {
		t.Errorf("expected empty for unset env var, got %q", result)
	}

	// Test malformed ref (not matching pattern)
	result = ResolveEnvVarRef("{env:TEST_VAR")
	if result != "{env:TEST_VAR" {
		t.Errorf("expected malformed ref unchanged, got %q", result)
	}
	result = ResolveEnvVarRef("env:TEST_VAR}")
	if result != "env:TEST_VAR}" {
		t.Errorf("expected malformed ref unchanged, got %q", result)
	}
}