package config

import (
	"encoding/json"
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

func TestSaveTUIThemeWritesOpencodeConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := SaveTUITheme("catppuccin"); err != nil {
		t.Fatalf("failed to save theme: %v", err)
	}

	path := filepath.Join(tmpHome, ".config", "opencode", "opencode.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected theme to be saved to %s: %v", path, err)
	}

	var got struct {
		TUI TUIConfig `json:"tui"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to parse saved config: %v", err)
	}
	if got.TUI.Theme != "catppuccin" {
		t.Fatalf("expected saved tui.theme catppuccin, got %q", got.TUI.Theme)
	}
}

func TestSaveTUIThemePreservesExistingOpencodeConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	path := filepath.Join(tmpHome, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"model":"gpt-4o","tui":{"mouse":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SaveTUITheme("dracula"); err != nil {
		t.Fatalf("failed to save theme: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to parse saved config: %v", err)
	}
	if got.Model != "gpt-4o" {
		t.Fatalf("expected existing model to be preserved, got %q", got.Model)
	}
	if got.TUI.Theme != "dracula" {
		t.Fatalf("expected saved tui.theme dracula, got %q", got.TUI.Theme)
	}
	if got.TUI.Mouse == nil || *got.TUI.Mouse {
		t.Fatalf("expected existing tui.mouse=false to be preserved, got %#v", got.TUI.Mouse)
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

func TestLoadPrefersRecentModelOverConfig(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	tmpState, err := os.MkdirTemp("", "ocode-state")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpState)

	tmpDir, err := os.MkdirTemp("", "ocode-project")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_STATE_HOME", tmpState)
	if err := os.WriteFile(filepath.Join(tmpDir, "opencode.json"), []byte(`{"model": "config/model"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecentModel("recent/model"); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "recent/model" {
		t.Fatalf("expected recent model, got %s", cfg.Model)
	}
}

func TestLoadKeepsExplicitEnvModelOverRecent(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	tmpState, err := os.MkdirTemp("", "ocode-state")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpState)

	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_STATE_HOME", tmpState)
	t.Setenv("OPENCODE_MODEL", "env/model")
	if err := SaveRecentModel("recent/model"); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "env/model" {
		t.Fatalf("expected env model, got %s", cfg.Model)
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
