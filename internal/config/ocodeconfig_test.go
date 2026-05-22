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
