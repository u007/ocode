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

	err = loadFromString(`{"model": "gpt-4o"}`, cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %s", cfg.Model)
	}
}

func TestMCPConfigDefaultsToEnabledAndInfersLocal(t *testing.T) {
	cfg := &Config{
		Tools:      make(map[string]bool),
		Permission: make(map[string]interface{}),
		Provider:   make(map[string]interface{}),
		MCP:        make(map[string]MCPConfig),
	}

	err := loadFromString(`{"mcp":{"demo":{"command":"echo hello"}}}`, cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := cfg.MCP["demo"]
	if !got.Enabled {
		t.Fatal("expected missing mcp.enabled to default to true")
	}
	if got.Type != "local" {
		t.Fatalf("expected local type, got %q", got.Type)
	}
	if len(got.Command) != 2 || got.Command[0] != "echo" || got.Command[1] != "hello" {
		t.Fatalf("expected string command to be split, got %#v", got.Command)
	}
	if got.Timeout != 5000 {
		t.Fatalf("expected default timeout 5000, got %d", got.Timeout)
	}
}

func TestSaveMCPEnabledWritesOpencodeConfig(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.WriteFile(filepath.Join(tmpDir, "opencode.json"), []byte(`{"mcp":{"demo":{"command":["echo","ok"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := SaveMCPEnabled("demo", false); err != nil {
		t.Fatalf("failed to save mcp enabled flag: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.MCP["demo"].Enabled {
		t.Fatal("expected mcp server to be disabled")
	}
}

func TestSaveMCPServerPreservesExistingOpencodeConfig(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	path := filepath.Join(tmpDir, "opencode.json")
	if err := os.WriteFile(path, []byte(`{"model":"gpt-4o","plugins":["alpha"],"mcp":{"existing":{"command":["echo","ok"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	err := SaveMCPServer("demo", MCPConfig{Type: "local", Command: []string{"echo", "demo"}, Enabled: true, Timeout: 5000})
	if err != nil {
		t.Fatalf("failed to save mcp server: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "gpt-4o" {
		t.Fatalf("expected existing model to be preserved, got %#v", got["model"])
	}
	plugins, ok := got["plugins"].([]any)
	if !ok || len(plugins) != 1 || plugins[0] != "alpha" {
		t.Fatalf("expected existing plugins to be preserved, got %#v", got["plugins"])
	}
	mcp := got["mcp"].(map[string]any)
	if _, ok := mcp["existing"]; !ok {
		t.Fatal("expected existing mcp server to be preserved")
	}
	if _, ok := mcp["demo"]; !ok {
		t.Fatal("expected new mcp server to be added")
	}
}

func TestClearMCPAuthorizationPreservesExistingOpencodeConfig(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	path := filepath.Join(tmpDir, "opencode.json")
	content := `{"model":"gpt-4o","plugins":["alpha"],"mcp":{"demo":{"type":"remote","url":"https:\/\/example.com","x-extra":true,"headers":{"Authorization":"Bearer token","X-Test":"yes"}}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	cleared, err := ClearMCPAuthorization("demo")
	if err != nil {
		t.Fatalf("failed to clear authorization: %v", err)
	}
	if !cleared {
		t.Fatal("expected authorization to be cleared")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	plugins, ok := got["plugins"].([]any)
	if !ok || len(plugins) != 1 || plugins[0] != "alpha" {
		t.Fatalf("expected existing plugins to be preserved, got %#v", got["plugins"])
	}
	demo := got["mcp"].(map[string]any)["demo"].(map[string]any)
	if demo["x-extra"] != true {
		t.Fatalf("expected unknown server field to be preserved, got %#v", demo["x-extra"])
	}
	headers := demo["headers"].(map[string]any)
	if _, ok := headers["Authorization"]; ok {
		t.Fatal("expected authorization header to be removed")
	}
	if headers["X-Test"] != "yes" {
		t.Fatalf("expected other header to be preserved, got %#v", headers["X-Test"])
	}
}

func TestSaveTUIThemeWritesOcodeConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := SaveTUITheme("catppuccin"); err != nil {
		t.Fatalf("failed to save theme: %v", err)
	}

	path := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
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

func TestSaveTUIThemePreservesExistingOcodeConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	path := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"tui":{"mouse":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SaveTUITheme("dracula"); err != nil {
		t.Fatalf("failed to save theme: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		TUI TUIConfig `json:"tui"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to parse saved config: %v", err)
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

func TestFavoriteModelsState(t *testing.T) {
	tmpState, err := os.MkdirTemp("", "ocode-state")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpState)
	t.Setenv("XDG_STATE_HOME", tmpState)

	if err := SaveRecentModel("provider/recent"); err != nil {
		t.Fatal(err)
	}
	if err := SaveFavoriteModel("provider/favorite"); err != nil {
		t.Fatal(err)
	}
	if err := SaveFavoriteModel("provider/favorite"); err != nil {
		t.Fatal(err)
	}

	favorites := LoadFavorites()
	if len(favorites) != 1 || favorites[0] != "provider/favorite" {
		t.Fatalf("expected one favorite, got %#v", favorites)
	}
	if !IsFavorite("provider/favorite") {
		t.Fatal("expected provider/favorite to be favorite")
	}
	if err := RemoveFavoriteModel("provider/favorite"); err != nil {
		t.Fatal(err)
	}
	if IsFavorite("provider/favorite") {
		t.Fatal("expected favorite to be removed")
	}
	recents := LoadRecentModels()
	if len(recents) != 1 || recents[0] != "provider/recent" {
		t.Fatalf("expected recents preserved, got %#v", recents)
	}
}

func TestFavoriteModelsValidateProviderModel(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := SaveFavoriteModel("missing-slash"); err == nil {
		t.Fatal("expected invalid favorite model id to fail")
	}
	if err := RemoveFavoriteModel("missing-slash"); err == nil {
		t.Fatal("expected invalid favorite model id to fail")
	}
}

func TestLoadPrefersLastModelOverRecent(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(tmpDir, "opencode.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecentModel("recent/model"); err != nil {
		t.Fatal(err)
	}
	if err := SaveLastModel("gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "gpt-4o-mini" {
		t.Fatalf("expected last model, got %s", cfg.Model)
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
