package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func chdirTempForConfigTest(t *testing.T) {
	t.Helper()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}

func TestResolveEditor(t *testing.T) {
	t.Run("config wins", func(t *testing.T) {
		cfg := &OcodeConfig{Editor: "nvim"}
		t.Setenv("VISUAL", "emacs")
		if got := ResolveEditor(cfg); got != "nvim" {
			t.Fatalf("want nvim got %s", got)
		}
	})
	t.Run("VISUAL fallback", func(t *testing.T) {
		t.Setenv("VISUAL", "emacs")
		t.Setenv("EDITOR", "nano")
		if got := ResolveEditor(nil); got != "emacs" {
			t.Fatalf("want emacs got %s", got)
		}
	})
	t.Run("EDITOR fallback", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "nano")
		if got := ResolveEditor(nil); got != "nano" {
			t.Fatalf("want nano got %s", got)
		}
	})
	t.Run("vi default", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		if got := ResolveEditor(nil); got != "vi" {
			t.Fatalf("want vi got %s", got)
		}
	})
}

func TestEditorModeDefaults(t *testing.T) {
	t.Run("default mode is external", func(t *testing.T) {
		cfg := defaultOcodeConfig()
		if cfg.EditorMode != "" {
			t.Fatalf("want empty default EditorMode, got %q", cfg.EditorMode)
		}
	})

	t.Run("LoadOcodeConfig defaults to external", func(t *testing.T) {
		tmp := t.TempDir()
		origHome := os.Getenv("HOME")
		t.Setenv("HOME", tmp)
		_ = origHome
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0755)
		os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(`{}`), 0644)

		var cfg Config
		err := LoadOcodeConfig(&cfg)
		if err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg.Ocode.EditorMode != EditorModeExternal {
			t.Fatalf("want EditorModeExternal, got %q", cfg.Ocode.EditorMode)
		}
	})
}

func TestEditorModeLoadSave(t *testing.T) {
	chdirTempForConfigTest(t)

	t.Run("load tmux-split", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0755)
		val := `{"editor_mode":"tmux-split"}`
		os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(val), 0644)

		var cfg Config
		err := LoadOcodeConfig(&cfg)
		if err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg.Ocode.EditorMode != EditorModeTmuxSplit {
			t.Fatalf("want EditorModeTmuxSplit, got %q", cfg.Ocode.EditorMode)
		}
	})

	t.Run("load tmux-window", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0755)
		val := `{"editor_mode":"tmux-window"}`
		os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(val), 0644)

		var cfg Config
		err := LoadOcodeConfig(&cfg)
		if err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg.Ocode.EditorMode != EditorModeTmuxWindow {
			t.Fatalf("want EditorModeTmuxWindow, got %q", cfg.Ocode.EditorMode)
		}
	})

	t.Run("save editor_mode", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0755)

		cfg := defaultOcodeConfig()
		cfg.EditorMode = EditorModeTmuxSplit
		err := SaveOcodeConfig(&cfg)
		if err != nil {
			t.Fatalf("SaveOcodeConfig failed: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(configDir, "ocodeconfig.json"))
		if err != nil {
			t.Fatalf("read config failed: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("parse config failed: %v", err)
		}
		mode, ok := parsed["editor_mode"].(string)
		if !ok {
			t.Fatal("editor_mode not found in saved config")
		}
		if mode != EditorModeTmuxSplit {
			t.Fatalf("want tmux-split, got %q", mode)
		}

		if _, ok := parsed["editor"]; ok {
			t.Fatal("editor should not be saved when empty")
		}
	})

	t.Run("save editor_mode external is omitted", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0755)

		cfg := defaultOcodeConfig()
		cfg.EditorMode = EditorModeExternal
		err := SaveOcodeConfig(&cfg)
		if err != nil {
			t.Fatalf("SaveOcodeConfig failed: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(configDir, "ocodeconfig.json"))
		if err != nil {
			t.Fatalf("read config failed: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("parse config failed: %v", err)
		}
		if _, ok := parsed["editor_mode"]; ok {
			t.Fatal("editor_mode should not be saved when external")
		}
	})

	t.Run("save editor mode preserves editor", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0755)

		cfg := defaultOcodeConfig()
		cfg.Editor = "nvim"
		cfg.EditorMode = EditorModeTmuxWindow
		err := SaveOcodeConfig(&cfg)
		if err != nil {
			t.Fatalf("SaveOcodeConfig failed: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(configDir, "ocodeconfig.json"))
		if err != nil {
			t.Fatalf("read config failed: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("parse config failed: %v", err)
		}
		if parsed["editor"] != "nvim" {
			t.Fatalf("want editor nvim, got %v", parsed["editor"])
		}
		if parsed["editor_mode"] != EditorModeTmuxWindow {
			t.Fatalf("want editor_mode tmux-window, got %v", parsed["editor_mode"])
		}
	})
}

func TestSaveOcodeConfigUsesProjectPathWhenAvailable(t *testing.T) {
	projectDir := t.TempDir()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "opencode.json"), []byte(`{"model":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultOcodeConfig()
	cfg.Permissions.Tools["bash"] = "allow"
	if err := SaveOcodeConfig(&cfg); err != nil {
		t.Fatalf("SaveOcodeConfig failed: %v", err)
	}

	projectPath := filepath.Join(projectDir, "ocodeconfig.json")
	if _, err := os.Stat(projectPath); err != nil {
		t.Fatalf("expected project ocode config to be created: %v", err)
	}
	globalPath := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatalf("expected global ocode config to remain absent, got err=%v", err)
	}
	data, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	permissions, ok := parsed["permissions"].(map[string]any)
	if !ok {
		t.Fatal("permissions not found in saved project config")
	}
	tools, ok := permissions["tools"].(map[string]any)
	if !ok {
		t.Fatal("permissions.tools not found in saved project config")
	}
	if tools["bash"] != "allow" {
		t.Fatalf("want bash allow, got %v", tools["bash"])
	}
}

func TestSaveOcodePermissionsUsesProjectPathWhenProjectExists(t *testing.T) {
	projectDir := t.TempDir()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "opencode.json"), []byte(`{"model":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	permissions := defaultPermissionConfig()
	permissions.Tools["webfetch"] = "allow"
	if err := SaveOcodePermissions(permissions); err != nil {
		t.Fatalf("SaveOcodePermissions failed: %v", err)
	}

	projectPath := filepath.Join(projectDir, "ocodeconfig.json")
	data, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project ocode config failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	permissionsRaw, ok := parsed["permissions"].(map[string]any)
	if !ok {
		t.Fatal("permissions not found in saved project config")
	}
	tools, ok := permissionsRaw["tools"].(map[string]any)
	if !ok {
		t.Fatal("permissions.tools not found in saved project config")
	}
	if tools["webfetch"] != "allow" {
		t.Fatalf("want webfetch allow, got %v", tools["webfetch"])
	}
	globalPath := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatalf("expected global ocode config to remain absent, got err=%v", err)
	}
}

func TestSaveOcodePermissionsPersistsAcrossNextSession(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	chdirTempForConfigTest(t)

	permissions := defaultPermissionConfig()
	permissions.Tools["bash"] = "allow"
	permissions.Bash.AutoAllowPrefixes = []string{"jq"}
	permissions.Bash.PrefixModes = map[string]string{"jq": "read_only", "sed": "mutating"}
	if err := SaveOcodePermissions(permissions); err != nil {
		t.Fatalf("SaveOcodePermissions failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if got := cfg.Ocode.Permissions.Tools["bash"]; got != "allow" {
		t.Fatalf("want persisted bash allow, got %q", got)
	}
	if len(cfg.Ocode.Permissions.Bash.AutoAllowPrefixes) != 1 || cfg.Ocode.Permissions.Bash.AutoAllowPrefixes[0] != "jq" {
		t.Fatalf("want persisted auto_allow_prefixes [jq], got %#v", cfg.Ocode.Permissions.Bash.AutoAllowPrefixes)
	}
	if got := cfg.Ocode.Permissions.Bash.PrefixModes["sed"]; got != "mutating" {
		t.Fatalf("want persisted sed mode mutating, got %q", got)
	}
}

func TestSaveEditorMode(t *testing.T) {
	chdirTempForConfigTest(t)

	t.Run("valid modes save", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0755)

		for _, mode := range []string{EditorModeExternal, EditorModeTmuxSplit, EditorModeTmuxWindow} {
			err := SaveEditorMode(mode)
			if err != nil {
				t.Fatalf("SaveEditorMode(%q) failed: %v", mode, err)
			}
		}
	})

	t.Run("invalid mode returns error", func(t *testing.T) {
		err := SaveEditorMode("bogus")
		if err == nil {
			t.Fatal("expected error for bogus mode")
		}
	})
}

func TestSaveAndGetLastThinkingBudget(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := SaveLastThinkingBudget(8000); err != nil {
		t.Fatalf("SaveLastThinkingBudget failed: %v", err)
	}
	if got := GetLastThinkingBudget(); got != 8000 {
		t.Fatalf("want 8000, got %d", got)
	}

	data, err := os.ReadFile(filepath.Join(configDir, "ocodeconfig.json"))
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}
	if got, ok := parsed["last_thinking_budget"].(float64); !ok || int(got) != 8000 {
		t.Fatalf("want last_thinking_budget 8000, got %v", parsed["last_thinking_budget"])
	}
}

func TestExtraAllowedPathsLoadAndSave(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initial := `{"extra_allowed_paths":["/tmp/a","/tmp/b"]}`
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if len(cfg.Ocode.ExtraAllowedPaths) != 2 {
		t.Fatalf("want 2 extra paths, got %d", len(cfg.Ocode.ExtraAllowedPaths))
	}

	cfg.Ocode.ExtraAllowedPaths = []string{"/tmp/c"}
	if err := SaveOcodeConfig(&cfg.Ocode); err != nil {
		t.Fatalf("SaveOcodeConfig failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, "ocodeconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	raw, ok := parsed["extra_allowed_paths"].([]any)
	if !ok || len(raw) != 1 || raw[0] != "/tmp/c" {
		t.Fatalf("unexpected extra_allowed_paths: %v", parsed["extra_allowed_paths"])
	}
}

func TestAdvisorConfigLoadPreservesDefaultEnabledWhenOmitted(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{"advisor":{"provider":"anthropic","model":"claude-sonnet-4-6"}}`
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if !cfg.Ocode.Advisor.Enabled {
		t.Fatal("expected advisor.enabled to remain true default when omitted")
	}
	if cfg.Ocode.Advisor.Provider != "anthropic" || cfg.Ocode.Advisor.Model != "claude-sonnet-4-6" {
		t.Fatalf("unexpected advisor config: %#v", cfg.Ocode.Advisor)
	}
}

func TestAdvisorConfigLoadAppliesExplicitEnabledFalse(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{"advisor":{"enabled":false,"provider":"anthropic","model":"claude-sonnet-4-6"}}`
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.Advisor.Enabled {
		t.Fatal("expected advisor.enabled to be false when explicitly configured")
	}
}

func TestSaveAdvisorModel_RequiresProviderPrefix(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := SaveAdvisorModel("claude-sonnet-4-6"); err == nil {
		t.Fatal("expected error for advisor model without provider prefix")
	}
	if err := SaveAdvisorModel("anthropic/claude-sonnet-4-6"); err != nil {
		t.Fatalf("SaveAdvisorModel(provider/model) failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.Advisor.Provider != "anthropic" || cfg.Ocode.Advisor.Model != "claude-sonnet-4-6" {
		t.Fatalf("unexpected saved advisor config: %#v", cfg.Ocode.Advisor)
	}
}
