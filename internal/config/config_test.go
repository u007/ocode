package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetProjectConfigPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ocode-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	configPath := filepath.Join(tmpDir, "opencode.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	path, err := getProjectConfigPath()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	resolvedExpected, err := filepath.EvalSymlinks(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolvedGot, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedGot != resolvedExpected {
		t.Errorf("expected %s, got %s", resolvedExpected, resolvedGot)
	}
}

func TestLoadCreatesOcodeConfigFiles(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	tmpDir, err := os.MkdirTemp("", "ocode-project")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	if err := os.WriteFile(filepath.Join(tmpDir, "opencode.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", tmpHome)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ocode == nil {
		t.Fatal("expected ocode config to load")
	}
	if !cfg.Ocode.Compact.Enabled {
		t.Fatal("expected compact to default to enabled")
	}

	if err := SaveOcodeConfig(cfg.Ocode); err != nil {
		t.Fatalf("failed to save ocode config: %v", err)
	}

	globalPath := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
	if _, err := os.Stat(globalPath); err != nil {
		t.Fatalf("expected %s to be created: %v", globalPath, err)
	}
}

func TestLoadFromStringValidJSON(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)
	t.Setenv("HOME", tmpHome)

	cfg := &Config{
		Tools:      make(map[string]bool),
		Permission: make(map[string]interface{}),
		Provider:   make(map[string]interface{}),
		MCP:        make(map[string]MCPConfig),
	}

	err = loadFromString(`{"model": "gpt-4o", "small_model": "gpt-4o-mini"}`, cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %s", cfg.Model)
	}
	if cfg.SmallModel != "gpt-4o-mini" {
		t.Fatalf("expected small_model gpt-4o-mini, got %s", cfg.SmallModel)
	}
}

func TestLoadFromStringPartialConfig(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)
	t.Setenv("HOME", tmpHome)

	cfg := &Config{
		Model:      "original-model",
		Tools:      make(map[string]bool),
		Permission: make(map[string]interface{}),
		Provider:   make(map[string]interface{}),
		MCP:        make(map[string]MCPConfig),
	}

	err = loadFromString(`{"default_agent": "review"}`, cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Model != "original-model" {
		t.Fatalf("expected model to remain original-model, got %s", cfg.Model)
	}
	if cfg.DefaultAgent != "review" {
		t.Fatalf("expected default_agent review, got %s", cfg.DefaultAgent)
	}
}

func TestLoadFromStringMalformedJSON(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)
	t.Setenv("HOME", tmpHome)

	cfg := &Config{
		Tools:      make(map[string]bool),
		Permission: make(map[string]interface{}),
		Provider:   make(map[string]interface{}),
		MCP:        make(map[string]MCPConfig),
	}

	err = loadFromString(`{invalid json}`, cfg)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoadWithConfigDirEnv(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)
	t.Setenv("HOME", tmpHome)

	tmpDir, err := os.MkdirTemp("", "ocode-configdir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "opencode.json")
	if err := os.WriteFile(configPath, []byte(`{"model": "custom-model"}`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OPENCODE_CONFIG_DIR", tmpDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Model != "custom-model" {
		t.Fatalf("expected model custom-model from config dir, got %s", cfg.Model)
	}
}

func TestLoadWithConfigContentEnv(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)
	t.Setenv("HOME", tmpHome)

	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model": "env-inline-model"}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Model != "env-inline-model" {
		t.Fatalf("expected model env-inline-model from content env, got %s", cfg.Model)
	}
}

func TestLoadWithOpenCodeDir(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)
	t.Setenv("HOME", tmpHome)

	tmpDir, err := os.MkdirTemp("", "ocode-opencodedir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, ".opencode"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".opencode", "opencode.json"), []byte(`{"model": "opencode-dir-model"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "opencode.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Model != "opencode-dir-model" {
		t.Fatalf("expected model opencode-dir-model from .opencode/, got %s", cfg.Model)
	}
}
