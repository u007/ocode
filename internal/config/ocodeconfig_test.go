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

func TestIDEModeLoadSave(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := SaveIDEMode(IDEModeOff); err != nil {
		t.Fatalf("SaveIDEMode(off) failed: %v", err)
	}

	configPath := filepath.Join(tmp, ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}
	if got, ok := parsed["ide_mode"].(string); !ok || got != IDEModeOff {
		t.Fatalf("saved ide_mode = %v, want %q", parsed["ide_mode"], IDEModeOff)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.IDEMode != IDEModeOff {
		t.Fatalf("loaded IDEMode = %q, want %q", cfg.Ocode.IDEMode, IDEModeOff)
	}
}

func TestSaveOcodeConfigWritesToGlobalPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cfg := defaultOcodeConfig()
	cfg.Permissions.Tools["bash"] = "allow"
	if err := SaveOcodeConfig(&cfg); err != nil {
		t.Fatalf("SaveOcodeConfig failed: %v", err)
	}

	globalPath := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
	if _, err := os.Stat(globalPath); err != nil {
		t.Fatalf("expected global ocode config to be created: %v", err)
	}
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	permissions, ok := parsed["permissions"].(map[string]any)
	if !ok {
		t.Fatal("permissions not found in saved config")
	}
	tools, ok := permissions["tools"].(map[string]any)
	if !ok {
		t.Fatal("permissions.tools not found in saved config")
	}
	if tools["bash"] != "allow" {
		t.Fatalf("want bash allow, got %v", tools["bash"])
	}
}

func TestSaveOcodePermissionsWritesToGlobalPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	permissions := defaultPermissionConfig()
	permissions.Tools["webfetch"] = "allow"
	if err := SaveOcodePermissions(permissions); err != nil {
		t.Fatalf("SaveOcodePermissions failed: %v", err)
	}

	globalPath := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read global ocode config failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	permissionsRaw, ok := parsed["permissions"].(map[string]any)
	if !ok {
		t.Fatal("permissions not found in saved config")
	}
	tools, ok := permissionsRaw["tools"].(map[string]any)
	if !ok {
		t.Fatal("permissions.tools not found in saved config")
	}
	if tools["webfetch"] != "allow" {
		t.Fatalf("want webfetch allow, got %v", tools["webfetch"])
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

// TestSaveOcodePermissionsPreservesAutoModelAndGrants reproduces the
// "permission model keeps getting erased" bug: a session whose in-memory
// permissions (e.g. from ExportConfig) carry no auto.model/grants must not
// erase those fields already on disk when it persists.
func TestSaveOcodePermissionsPreservesAutoModelAndGrants(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	chdirTempForConfigTest(t)

	// Disk already has a configured model + a grant (set by another session).
	// The model is written via SavePermissionModel — its only owner.
	onDisk := defaultPermissionConfig()
	onDisk.Auto.Grants = []AutoGrant{{Kind: "tool", Tool: "bash"}}
	if err := SaveOcodePermissions(onDisk); err != nil {
		t.Fatalf("seed SaveOcodePermissions failed: %v", err)
	}
	if err := SavePermissionModel("opencode/deepseek-v4-flash-free"); err != nil {
		t.Fatalf("seed SavePermissionModel failed: %v", err)
	}

	// A stale session persists permissions carrying only enabled (model empty,
	// grants nil) — exactly what ExportConfig produces.
	stale := defaultPermissionConfig()
	stale.Auto = &AutoPermissionConfig{Enabled: true}
	if err := SaveOcodePermissions(stale); err != nil {
		t.Fatalf("stale SaveOcodePermissions failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if got := cfg.Ocode.Permissions.Auto.Model; got != "opencode/deepseek-v4-flash-free" {
		t.Fatalf("auto.model erased: got %q", got)
	}
	if len(cfg.Ocode.Permissions.Auto.Grants) != 1 {
		t.Fatalf("auto.grants erased: got %#v", cfg.Ocode.Permissions.Auto.Grants)
	}
	if !cfg.Ocode.Permissions.Auto.Enabled {
		t.Fatalf("caller's enabled=true not applied")
	}
}

// TestSaveOcodePermissionsNeverClobbersModelFromStaleSnapshot reproduces the
// multi-session bug the user reported: a session that started with model "Y"
// persists a tool-rule change while carrying a STALE non-empty model in its
// in-memory snapshot, after another session selected model "Z" on disk via
// /permissions model. The permissions write must not overwrite the on-disk
// model — only SavePermissionModel owns it.
func TestSaveOcodePermissionsNeverClobbersModelFromStaleSnapshot(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	chdirTempForConfigTest(t)

	// Another session selected model "Z" on disk via /permissions model.
	if err := SavePermissionModel("opencode/model-z"); err != nil {
		t.Fatalf("seed SavePermissionModel failed: %v", err)
	}

	// This (older) session toggles a tool rule; its snapshot still carries the
	// previously-loaded model "Y". It did NOT run /permissions model.
	stale := defaultPermissionConfig()
	stale.Tools["bash"] = "allow"
	stale.Auto = &AutoPermissionConfig{Enabled: true, Model: "opencode/model-y"}
	if err := SaveOcodePermissions(stale); err != nil {
		t.Fatalf("stale SaveOcodePermissions failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if got := cfg.Ocode.Permissions.Auto.Model; got != "opencode/model-z" {
		t.Fatalf("on-disk model clobbered by stale snapshot: got %q, want %q", got, "opencode/model-z")
	}
	if got := cfg.Ocode.Permissions.Tools["bash"]; got != "allow" {
		t.Fatalf("caller's tool rule not applied: got %q", got)
	}
}

// TestSaveOcodePermissionsPreservesDiskAutoWhenCallerHasNone covers branch B:
// this session never had an auto block, but a concurrent session wrote one
// (model + enabled) to disk. The whole disk block must survive verbatim.
func TestSaveOcodePermissionsPreservesDiskAutoWhenCallerHasNone(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	chdirTempForConfigTest(t)

	onDisk := defaultPermissionConfig()
	onDisk.Auto = &AutoPermissionConfig{Enabled: true}
	if err := SaveOcodePermissions(onDisk); err != nil {
		t.Fatalf("seed SaveOcodePermissions failed: %v", err)
	}
	if err := SavePermissionModel("opencode/model-z"); err != nil {
		t.Fatalf("seed SavePermissionModel failed: %v", err)
	}

	// Caller carries no auto block at all.
	noAuto := defaultPermissionConfig()
	noAuto.Auto = nil
	noAuto.Tools["bash"] = "allow"
	if err := SaveOcodePermissions(noAuto); err != nil {
		t.Fatalf("noAuto SaveOcodePermissions failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.Permissions.Auto == nil {
		t.Fatal("disk auto block erased when caller had none")
	}
	if got := cfg.Ocode.Permissions.Auto.Model; got != "opencode/model-z" {
		t.Fatalf("disk model erased: got %q", got)
	}
	if !cfg.Ocode.Permissions.Auto.Enabled {
		t.Fatal("disk enabled flag overridden when caller had no opinion")
	}
}

// TestSaveAutoPermissionEnabledKeepsOtherFields proves the targeted enabled
// writer used by --permission-mode does not clobber model/grants/tool rules.
func TestSaveAutoPermissionEnabledKeepsOtherFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	chdirTempForConfigTest(t)

	seed := defaultPermissionConfig()
	seed.Tools["bash"] = "allow"
	if err := SaveOcodePermissions(seed); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if err := SavePermissionModel("opencode/deepseek-v4-flash-free"); err != nil {
		t.Fatalf("seed SavePermissionModel failed: %v", err)
	}

	if err := SaveAutoPermissionEnabled(true); err != nil {
		t.Fatalf("SaveAutoPermissionEnabled failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if !cfg.Ocode.Permissions.Auto.Enabled {
		t.Fatalf("enabled not persisted")
	}
	if got := cfg.Ocode.Permissions.Auto.Model; got != "opencode/deepseek-v4-flash-free" {
		t.Fatalf("model erased by enabled write: got %q", got)
	}
	if got := cfg.Ocode.Permissions.Tools["bash"]; got != "allow" {
		t.Fatalf("tool rule erased by enabled write: got %q", got)
	}
}

// TestSaveAutoGrantRoundTrip proves the targeted grant saver persists an
// interpreter grant (with min_confidence) and round-trips all fields, without
// clobbering the model or other fields, and that it de-dupes only truly
// identical grants.
func TestSaveAutoGrantRoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	chdirTempForConfigTest(t)

	// Seed an on-disk config carrying a non-default min_confidence + model.
	full, err := loadFullOcodeConfig()
	if err != nil {
		t.Fatalf("loadFullOcodeConfig failed: %v", err)
	}
	if full.Permissions.Auto == nil {
		full.Permissions.Auto = &AutoPermissionConfig{}
	}
	full.Permissions.Auto.Enabled = true
	full.Permissions.Auto.MinConfidence = 0.9
	full.Permissions.Auto.Model = "opencode/deepseek-v4-flash-free"
	if err := SaveOcodeConfig(full); err != nil {
		t.Fatalf("seed SaveOcodeConfig failed: %v", err)
	}

	grant := AutoGrant{
		Kind:              "interpreter_exact",
		Language:          "python",
		SourceMode:        "script_file",
		NormalizedCommand: "python job.py",
		EntrypointPath:    "job.py",
		EntrypointSHA256:  "deadbeef",
		CWD:               "/work",
		Destructive:       true,
	}
	if err := SaveAutoGrant(grant); err != nil {
		t.Fatalf("SaveAutoGrant failed: %v", err)
	}
	// Identical grant is a no-op (dedup).
	if err := SaveAutoGrant(grant); err != nil {
		t.Fatalf("SaveAutoGrant (dedup) failed: %v", err)
	}
	// Same source hash but different path/cwd is a distinct exact grant now.
	sameSourceDifferentPath := grant
	sameSourceDifferentPath.EntrypointPath = "nested/job.py"
	sameSourceDifferentPath.CWD = "/other"
	if err := SaveAutoGrant(sameSourceDifferentPath); err != nil {
		t.Fatalf("SaveAutoGrant (same source, different path) failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	auto := cfg.Ocode.Permissions.Auto
	if auto == nil {
		t.Fatal("auto config missing")
	}
	if auto.MinConfidence != 0.9 {
		t.Fatalf("min_confidence not round-tripped: got %v", auto.MinConfidence)
	}
	if auto.Model != "opencode/deepseek-v4-flash-free" {
		t.Fatalf("model erased by grant write: got %q", auto.Model)
	}
	if len(auto.Grants) != 2 {
		t.Fatalf("expected 2 grants after dedup, got %d", len(auto.Grants))
	}
	g := auto.Grants[0]
	if g.Kind != "interpreter_exact" || g.Language != "python" || g.SourceMode != "script_file" ||
		g.NormalizedCommand != "python job.py" || g.EntrypointSHA256 != "deadbeef" || !g.Destructive || g.CWD != "/work" {
		t.Fatalf("grant fields not round-tripped: %+v", g)
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

func TestAutoPermissionConfigLoadAppliesExplicitFalseOverrides(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	globalDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(globalDir, "ocodeconfig.json")
	if err := os.WriteFile(globalPath, []byte(`{"permissions":{"auto":{"enabled":false,"allow_destructive":false}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.Permissions.Auto == nil {
		t.Fatal("expected permissions.auto block to load")
	}
	if cfg.Ocode.Permissions.Auto.Enabled {
		t.Fatal("expected permissions.auto.enabled=false from global config")
	}
	if cfg.Ocode.Permissions.Auto.AllowDestructive {
		t.Fatal("expected permissions.auto.allow_destructive=false from global config")
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

func TestASTPluginLoadSave(t *testing.T) {
	chdirTempForConfigTest(t)

	t.Run("default is disabled", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg.Ocode.Plugins.AST {
			t.Fatal("ast plugin should default to disabled")
		}
	})

	t.Run("load enabled from file", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0o755)
		os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(`{"plugins":{"ast":true}}`), 0o644)

		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if !cfg.Ocode.Plugins.AST {
			t.Fatal("want ast plugin enabled from file")
		}
	})

	t.Run("save round-trips", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0o755)

		if err := SaveOcodeASTPlugin(true); err != nil {
			t.Fatalf("SaveOcodeASTPlugin failed: %v", err)
		}
		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if !cfg.Ocode.Plugins.AST {
			t.Fatal("ast plugin should persist as enabled after save")
		}

		if err := SaveOcodeASTPlugin(false); err != nil {
			t.Fatalf("SaveOcodeASTPlugin(false) failed: %v", err)
		}
		var cfg2 Config
		if err := LoadOcodeConfig(&cfg2); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg2.Ocode.Plugins.AST {
			t.Fatal("ast plugin should persist as disabled after save")
		}
	})
}

func TestMemoryEnabledLoadSave(t *testing.T) {
	chdirTempForConfigTest(t)

	t.Run("default is enabled", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if !cfg.Ocode.MemoryEnabled {
			t.Fatal("memory context should default to enabled")
		}
	})

	t.Run("load disabled from file", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0o755)
		os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(`{"memory_enabled":false}`), 0o644)

		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg.Ocode.MemoryEnabled {
			t.Fatal("want memory context disabled from file")
		}
	})

	t.Run("save round-trips", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0o755)

		if err := SaveMemoryEnabled(true); err != nil {
			t.Fatalf("SaveMemoryEnabled(true) failed: %v", err)
		}
		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if !cfg.Ocode.MemoryEnabled {
			t.Fatal("memory context should persist as enabled after save")
		}

		if err := SaveMemoryEnabled(false); err != nil {
			t.Fatalf("SaveMemoryEnabled(false) failed: %v", err)
		}
		var cfg2 Config
		if err := LoadOcodeConfig(&cfg2); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg2.Ocode.MemoryEnabled {
			t.Fatal("memory context should persist as disabled after save")
		}
	})
}

func TestDocPromptEnabledLoadSave(t *testing.T) {
	chdirTempForConfigTest(t)

	t.Run("default is disabled", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg.Ocode.DocPromptEnabled {
			t.Fatal("doc prompt should default to disabled")
		}
	})

	t.Run("load enabled from file", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0o755)
		os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(`{"doc_prompt_enabled":true}`), 0o644)

		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if !cfg.Ocode.DocPromptEnabled {
			t.Fatal("want doc prompt enabled from file")
		}
	})

	t.Run("save round-trips", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		configDir := filepath.Join(tmp, ".config", "opencode")
		os.MkdirAll(configDir, 0o755)

		if err := SaveDocPromptEnabled(true); err != nil {
			t.Fatalf("SaveDocPromptEnabled(true) failed: %v", err)
		}
		var cfg Config
		if err := LoadOcodeConfig(&cfg); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if !cfg.Ocode.DocPromptEnabled {
			t.Fatal("doc prompt should persist as enabled after save")
		}

		if err := SaveDocPromptEnabled(false); err != nil {
			t.Fatalf("SaveDocPromptEnabled(false) failed: %v", err)
		}
		var cfg2 Config
		if err := LoadOcodeConfig(&cfg2); err != nil {
			t.Fatalf("LoadOcodeConfig failed: %v", err)
		}
		if cfg2.Ocode.DocPromptEnabled {
			t.Fatal("doc prompt should persist as disabled after save")
		}
	})
}
