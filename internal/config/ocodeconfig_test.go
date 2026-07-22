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
	permissions.Bash.Prefixes["grep -n"] = "deny"
	permissions.Bash.Prefixes["sed"] = "deny"
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
	if got := cfg.Ocode.Permissions.Bash.Prefixes["grep -n"]; got != "deny" {
		t.Fatalf("want persisted grep -n ban deny, got %q", got)
	}
	if got := cfg.Ocode.Permissions.Bash.Prefixes["sed"]; got != "deny" {
		t.Fatalf("want persisted sed ban deny, got %q", got)
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

	onDisk := defaultPermissionConfig()
	if err := SaveOcodePermissions(onDisk); err != nil {
		t.Fatalf("seed SaveOcodePermissions failed: %v", err)
	}
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

// TestAdvisorCheckpointsRoundTrip verifies that the checkpoints field
// round-trips through save and load correctly:
//   - omitted key → default ["plan","done"]
//   - explicit []   → [] (all checkpoints disabled)
//   - explicit list → that exact list
func TestAdvisorCheckpointsRoundTrip(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write an initial config with explicit empty checkpoints.
	initial := `{"advisor":{"checkpoints":[]}}`
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	// Load: explicit [] must be preserved.
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.Advisor.Checkpoints == nil {
		t.Fatal("Checkpoints is nil after loading [], want non-nil empty slice")
	}
	if len(cfg.Ocode.Advisor.Checkpoints) != 0 {
		t.Fatalf("Checkpoints = %v after loading [], want []", cfg.Ocode.Advisor.Checkpoints)
	}

	// Save and reload: empty must still be empty.
	if err := SaveOcodeConfig(&cfg.Ocode); err != nil {
		t.Fatalf("SaveOcodeConfig failed: %v", err)
	}
	var cfg2 Config
	if err := LoadOcodeConfig(&cfg2); err != nil {
		t.Fatalf("LoadOcodeConfig (reload) failed: %v", err)
	}
	if cfg2.Ocode.Advisor.Checkpoints == nil {
		t.Fatal("Checkpoints is nil after save/reload of [], want non-nil empty slice")
	}
	if len(cfg2.Ocode.Advisor.Checkpoints) != 0 {
		t.Fatalf("Checkpoints = %v after save/reload of [], want []", cfg2.Ocode.Advisor.Checkpoints)
	}

	// Now set non-empty checkpoints and verify round-trip.
	cfg2.Ocode.Advisor.Checkpoints = []string{"plan", "done"}
	if err := SaveOcodeConfig(&cfg2.Ocode); err != nil {
		t.Fatalf("SaveOcodeConfig (plan,done) failed: %v", err)
	}
	var cfg3 Config
	if err := LoadOcodeConfig(&cfg3); err != nil {
		t.Fatalf("LoadOcodeConfig (reload plan,done) failed: %v", err)
	}
	if len(cfg3.Ocode.Advisor.Checkpoints) != 2 ||
		cfg3.Ocode.Advisor.Checkpoints[0] != "plan" ||
		cfg3.Ocode.Advisor.Checkpoints[1] != "done" {
		t.Fatalf("Checkpoints = %v after save/reload of [plan,done], want [plan,done]",
			cfg3.Ocode.Advisor.Checkpoints)
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

	// Verify that clearing the model (empty string) only resets
	// provider/model, not the enabled or checkpoints fields.
	cfg.Ocode.Advisor.Enabled = false
	cfg.Ocode.Advisor.Checkpoints = []string{}
	if err := SaveOcodeConfig(&cfg.Ocode); err != nil {
		t.Fatalf("SaveOcodeConfig before clear failed: %v", err)
	}

	if err := SaveAdvisorModel(""); err != nil {
		t.Fatalf("SaveAdvisorModel('') (clear) failed: %v", err)
	}

	var cfg2 Config
	if err := LoadOcodeConfig(&cfg2); err != nil {
		t.Fatalf("LoadOcodeConfig after clear failed: %v", err)
	}

	// Provider/model should be back to defaults.
	if cfg2.Ocode.Advisor.Provider != defaultAdvisorConfig().Provider {
		t.Fatalf("after clear, Provider = %q, want default %q",
			cfg2.Ocode.Advisor.Provider, defaultAdvisorConfig().Provider)
	}
	if cfg2.Ocode.Advisor.Model != defaultAdvisorConfig().Model {
		t.Fatalf("after clear, Model = %q, want default %q",
			cfg2.Ocode.Advisor.Model, defaultAdvisorConfig().Model)
	}

	// Enabled and Checkpoints must NOT be reset by clearing the model.
	if cfg2.Ocode.Advisor.Enabled {
		t.Fatal("after clear, Enabled was reset to true (should have stayed false)")
	}
	if cfg2.Ocode.Advisor.Checkpoints == nil || len(cfg2.Ocode.Advisor.Checkpoints) != 0 {
		t.Fatalf("after clear, Checkpoints = %v, want [] (should not be reset to defaults)",
			cfg2.Ocode.Advisor.Checkpoints)
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

func TestLoadProjectSettings(t *testing.T) {
	// Test: empty path returns empty slice
	paths, err := loadProjectSettings("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Error("expected empty paths for empty path")
	}

	// Test: valid settings file with extra_allowed_paths
	tmpFile := t.TempDir()
	settingsPath := filepath.Join(tmpFile, "settings.json")
	testData := `{"extra_allowed_paths": ["/foo", "/bar"], "future_field": "preserved"}`
	if err := os.WriteFile(settingsPath, []byte(testData), 0644); err != nil {
		t.Fatalf("write test settings: %v", err)
	}
	paths, err = loadProjectSettings(settingsPath)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(paths))
	}
	if paths[0] != "/foo" || paths[1] != "/bar" {
		t.Errorf("unexpected paths: %v", paths)
	}

	// Test: settings file without extra_allowed_paths
	testData = `{"other_field": "value"}`
	if err := os.WriteFile(settingsPath, []byte(testData), 0644); err != nil {
		t.Fatalf("write test settings: %v", err)
	}
	paths, err = loadProjectSettings(settingsPath)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected empty paths when field missing, got %v", paths)
	}

	// Test: non-existent file returns empty slice (not error)
	paths, err = loadProjectSettings(filepath.Join(tmpFile, "nonexistent.json"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Error("expected empty paths for non-existent file")
	}
}

func TestLoadOcodeConfigReturnsErrorForInvalidProjectSettings(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("create .git: %v", err)
	}

	ocodeDir := filepath.Join(projectDir, ".ocode")
	if err := os.Mkdir(ocodeDir, 0755); err != nil {
		t.Fatalf("create .ocode: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ocodeDir, "settings.json"), []byte(`{"extra_allowed_paths": [`), 0644); err != nil {
		t.Fatalf("write invalid settings: %v", err)
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	globalConfigDir := filepath.Join(homeDir, ".config", "opencode")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("create global config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalConfigDir, "ocodeconfig.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	}()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err == nil {
		t.Fatal("expected LoadOcodeConfig to fail for invalid project settings")
	}
}

func TestLoadOcodeConfigMergesProjectSettings(t *testing.T) {
	// Create project directory with .git so FindProjectRoot finds it
	projectDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("create .git: %v", err)
	}

	// Create .ocode/settings.json with extra_allowed_paths
	ocodeDir := filepath.Join(projectDir, ".ocode")
	if err := os.Mkdir(ocodeDir, 0755); err != nil {
		t.Fatalf("create .ocode: %v", err)
	}
	settingsData := `{"extra_allowed_paths": ["/project/path1", "/project/path2"]}`
	if err := os.WriteFile(filepath.Join(ocodeDir, "settings.json"), []byte(settingsData), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// Isolate HOME so we don't load real global config
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	globalConfigDir := filepath.Join(homeDir, ".config", "opencode")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("create global config dir: %v", err)
	}
	// Empty global config (so getGlobalOcodeConfigPath succeeds, no extra paths)
	if err := os.WriteFile(filepath.Join(globalConfigDir, "ocodeconfig.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	// Chdir to project dir so FindProjectRoot picks it up
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	}()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}

	// Verify project paths are present in ExtraAllowedPaths
	found1, found2 := false, false
	for _, p := range cfg.Ocode.ExtraAllowedPaths {
		if p == "/project/path1" {
			found1 = true
		}
		if p == "/project/path2" {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("expected /project/path1 and /project/path2 in ExtraAllowedPaths, got %v", cfg.Ocode.ExtraAllowedPaths)
	}
}

// Test project-scoped save via saveProjectSettings with reload verification.
func TestSaveProjectSettings_BasicSaveAndReload(t *testing.T) {
	projectDir := t.TempDir()
	ocodeDir := filepath.Join(projectDir, ".ocode")
	if err := os.Mkdir(ocodeDir, 0755); err != nil {
		t.Fatalf("create .ocode: %v", err)
	}
	settingsPath := filepath.Join(ocodeDir, "settings.json")

	// Save first path
	if err := saveProjectSettings(settingsPath, []string{"/project/path1"}); err != nil {
		t.Fatalf("save /project/path1: %v", err)
	}
	paths, err := loadProjectSettings(settingsPath)
	if err != nil {
		t.Fatalf("load after save: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/project/path1" {
		t.Errorf("expected [/project/path1], got %v", paths)
	}

	// Save additional path (load-modify-write)
	paths, _ = loadProjectSettings(settingsPath)
	paths = append(paths, "/project/path2")
	if err := saveProjectSettings(settingsPath, paths); err != nil {
		t.Fatalf("save paths: %v", err)
	}
	paths, _ = loadProjectSettings(settingsPath)
	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d: %v", len(paths), paths)
	}
}

// Test key preservation: unknown fields in .ocode/settings.json survive saves.
func TestSaveProjectSettings_KeyPreservation(t *testing.T) {
	projectDir := t.TempDir()
	ocodeDir := filepath.Join(projectDir, ".ocode")
	if err := os.MkdirAll(ocodeDir, 0755); err != nil {
		t.Fatalf("create .ocode: %v", err)
	}
	settingsPath := filepath.Join(ocodeDir, "settings.json")

	// Pre-populate with an unknown field + existing paths
	rawSettings := map[string]json.RawMessage{
		"extra_allowed_paths": json.RawMessage(`["/old/path"]`),
		"future_setting":      json.RawMessage(`"preserved_value"`),
	}
	data, _ := json.Marshal(rawSettings)
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("write pre-populated settings: %v", err)
	}

	// Save new paths (this should preserve future_setting)
	if err := saveProjectSettings(settingsPath, []string{"/old/path", "/new/path"}); err != nil {
		t.Fatalf("save with merge: %v", err)
	}

	// Verify both updated paths and unknown field are preserved
	savedData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(savedData, &result); err != nil {
		t.Fatalf("unmarshal saved: %v", err)
	}
	if _, ok := result["future_setting"]; !ok {
		t.Error("expected future_setting to be preserved, but it was lost")
	}
	var savedPaths []string
	if err := json.Unmarshal(result["extra_allowed_paths"], &savedPaths); err != nil {
		t.Fatalf("unmarshal saved paths: %v", err)
	}
	if len(savedPaths) != 2 {
		t.Errorf("expected 2 paths, got %d: %v", len(savedPaths), savedPaths)
	}
	if savedPaths[0] != "/old/path" || savedPaths[1] != "/new/path" {
		t.Errorf("unexpected paths: %v", savedPaths)
	}
}

// Test SaveExtraAllowedPath writes to project config when in a project.
func TestSaveExtraAllowedPath_SavesToProjectConfig(t *testing.T) {
	projectDir := t.TempDir()

	// Create .git to make it a project
	gitDir := filepath.Join(projectDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("create .git: %v", err)
	}

	// Create .ocode directory
	ocodeDir := filepath.Join(projectDir, ".ocode")
	if err := os.Mkdir(ocodeDir, 0755); err != nil {
		t.Fatalf("create .ocode: %v", err)
	}
	settingsPath := filepath.Join(ocodeDir, "settings.json")

	// Save working directory and chdir into project so FindProjectRoot picks it up
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	}()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Isolate HOME so we don't load real global config
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	globalConfigDir := filepath.Join(homeDir, ".config", "opencode")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("create global config dir: %v", err)
	}
	// Empty global config
	if err := os.WriteFile(filepath.Join(globalConfigDir, "ocodeconfig.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	// Save a path — should go to .ocode/settings.json
	if err := SaveExtraAllowedPath("/project/path1"); err != nil {
		t.Fatalf("SaveExtraAllowedPath: %v", err)
	}
	paths, err := loadProjectSettings(settingsPath)
	if err != nil {
		t.Fatalf("load project settings: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/project/path1" {
		t.Errorf("expected [/project/path1], got %v", paths)
	}

	// Attempt duplicate — should be no-op
	if err := SaveExtraAllowedPath("/project/path1"); err != nil {
		t.Fatalf("save duplicate: %v", err)
	}
	paths, _ = loadProjectSettings(settingsPath)
	if len(paths) != 1 {
		t.Errorf("expected 1 path after dedup, got %d: %v", len(paths), paths)
	}
}

// Test SaveExtraAllowedPath falls back to global config when not in a project.
func TestSaveExtraAllowedPath_FallsBackToGlobal(t *testing.T) {
	// Isolate in a temp dir with HOME isolated so no project is found
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	globalConfigDir := filepath.Join(homeDir, ".config", "opencode")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("create global config dir: %v", err)
	}
	// Empty global config
	if err := os.WriteFile(filepath.Join(globalConfigDir, "ocodeconfig.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	isolatedDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	}()
	if err := os.Chdir(isolatedDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Save a path — should fall back to global config
	if err := SaveExtraAllowedPath("/global/path"); err != nil {
		t.Fatalf("SaveExtraAllowedPath: %v", err)
	}

	// Verify it was saved to global config
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		t.Fatalf("loadFullOcodeConfig: %v", err)
	}
	var found bool
	for _, p := range cfg.ExtraAllowedPaths {
		if p == "/global/path" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected /global/path in global config, got %v", cfg.ExtraAllowedPaths)
	}
}

// TestLoadFullOcodeConfig_DoesNotMergeProjectPaths verifies that loadFullOcodeConfig
// (used by Save* helpers) does NOT merge project paths. If it did, every save operation
// (theme change, model change, etc.) would leak per-project paths into the global config.
func TestLoadFullOcodeConfig_DoesNotMergeProjectPaths(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	ocodeDir := filepath.Join(projectDir, ".ocode")
	if err := os.Mkdir(ocodeDir, 0755); err != nil {
		t.Fatalf("create .ocode: %v", err)
	}
	settingsPath := filepath.Join(ocodeDir, "settings.json")
	testData := `{"extra_allowed_paths": ["/project/path"]}`
	if err := os.WriteFile(settingsPath, []byte(testData), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(projectDir)

	cfg, err := loadFullOcodeConfig()
	if err != nil {
		t.Fatalf("loadFullOcodeConfig failed: %v", err)
	}
	for _, p := range cfg.ExtraAllowedPaths {
		if p == "/project/path" {
			t.Errorf("loadFullOcodeConfig must not merge project paths into global config (would leak on next Save* call); got %v", cfg.ExtraAllowedPaths)
		}
	}
}

// TestSavePinnedSkillsRoundTrip proves the SavePinnedSkills / load cycle uses
// Discovery.PinnedSkills as a single source of truth. Regression test for the
// duplicate-field bug where SavePinnedSkills wrote to a stale top-level field
// that loadOcodeConfigFile never read back, so pin/unpin changes were lost on
// restart.
func TestSavePinnedSkillsRoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Pin two skills.
	want := []string{"brainstorming", "review-changes"}
	if err := SavePinnedSkills(want); err != nil {
		t.Fatalf("SavePinnedSkills: %v", err)
	}

	// Reload and confirm the discovery field has them.
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		t.Fatalf("loadFullOcodeConfig: %v", err)
	}
	if got := cfg.Discovery.PinnedSkills; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Discovery.PinnedSkills = %v, want %v", got, want)
	}

	// Unpin one.
	if err := SavePinnedSkills([]string{"review-changes"}); err != nil {
		t.Fatalf("SavePinnedSkills (unpin): %v", err)
	}
	cfg, err = loadFullOcodeConfig()
	if err != nil {
		t.Fatalf("loadFullOcodeConfig after unpin: %v", err)
	}
	if got := cfg.Discovery.PinnedSkills; len(got) != 1 || got[0] != "review-changes" {
		t.Fatalf("Discovery.PinnedSkills = %v, want [review-changes]", got)
	}
}
