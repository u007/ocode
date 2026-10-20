package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestNormalizeSkillNames(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, nil},
		{"trims", []string{"  b  ", "a"}, []string{"a", "b"}},
		{"drops empties", []string{"a", "", "   ", "b"}, []string{"a", "b"}},
		{"dedupes", []string{"a", "a", "a"}, []string{"a"}},
		{"sorts", []string{"z", "m", "a"}, []string{"a", "m", "z"}},
		{"keeps namespace colon", []string{"plugin:skill"}, []string{"plugin:skill"}},
		{"case sensitive", []string{"A", "a"}, []string{"A", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeSkillNames(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("NormalizeSkillNames(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestResolveDisabledSet pins the layering algebra. This is the single place
// the precedence lives, so a regression here silently changes every consumer.
func TestResolveDisabledSet(t *testing.T) {
	cases := []struct {
		name    string
		global  SkillsConfig
		project SkillsConfig
		want    []string
	}{
		{
			name: "empty defaults disable nothing",
			want: nil,
		},
		{
			name:   "global disabled",
			global: SkillsConfig{Disabled: []string{"docx"}},
			want:   []string{"docx"},
		},
		{
			name:    "project adds to global",
			global:  SkillsConfig{Disabled: []string{"docx"}},
			project: SkillsConfig{Disabled: []string{"pdf"}},
			want:    []string{"docx", "pdf"},
		},
		{
			name:    "project enabled rescues a globally disabled skill",
			global:  SkillsConfig{Disabled: []string{"docx", "pdf"}},
			project: SkillsConfig{Enabled: []string{"docx"}},
			want:    []string{"pdf"},
		},
		{
			name:    "global enabled rescues a project disabled skill",
			project: SkillsConfig{Disabled: []string{"docx"}},
			global:  SkillsConfig{Enabled: []string{"docx"}},
			want:    nil,
		},
		{
			name:    "enabled wins over disabled in the same project",
			project: SkillsConfig{Disabled: []string{"docx"}, Enabled: []string{"docx"}},
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := setToSortedSlice(resolveDisabledSet(tc.global, tc.project))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolveDisabledSet(%+v, %+v) = %v, want %v", tc.global, tc.project, got, tc.want)
			}
		})
	}
}

func setToSortedSlice(m map[string]bool) []string {
	var out []string
	for name, disabled := range m {
		if disabled {
			out = append(out, name)
		}
	}
	// Small N; insertion sort keeps the test dependency-free.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func writeTestGlobalSkills(t *testing.T, prefs SkillsConfig) string {
	t.Helper()
	path, err := ActiveOcodeConfigPath()
	if err != nil {
		t.Fatalf("ActiveOcodeConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	body := map[string]any{"skills": prefs}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func writeTestProjectSettings(t *testing.T, root string, body map[string]any) {
	t.Helper()
	path := ProjectSettingsPathForRoot(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir .ocode: %v", err)
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func TestEffectiveDisabledSkillsLayering(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	root := t.TempDir()

	writeTestGlobalSkills(t, SkillsConfig{Disabled: []string{"docx", "pdf"}})
	writeTestProjectSettings(t, root, map[string]any{
		"skills": map[string]any{
			"disabled": []string{"stt"},
			"enabled":  []string{"docx"},
		},
	})

	got, err := EffectiveDisabledSkills(root)
	if err != nil {
		t.Fatalf("EffectiveDisabledSkills: %v", err)
	}
	want := []string{"pdf", "stt"}
	if names := setToSortedSlice(got); !reflect.DeepEqual(names, want) {
		t.Fatalf("effective disabled = %v, want %v", names, want)
	}
}

func TestEffectiveDisabledSkillsNoProjectRoot(t *testing.T) {
	setHomeTree(t, t.TempDir())
	writeTestGlobalSkills(t, SkillsConfig{Disabled: []string{"docx"}})

	got, err := EffectiveDisabledSkills("")
	if err != nil {
		t.Fatalf("EffectiveDisabledSkills: %v", err)
	}
	if !got["docx"] {
		t.Fatalf("global disable missing for empty root: %v", got)
	}
}

// TestEffectiveDisabledSkillsUnreadableProjectDegradesToGlobal pins the
// documented degradation: a malformed project settings file must not empty the
// skill catalog. It reports the error AND keeps the global layer.
func TestEffectiveDisabledSkillsUnreadableProjectDegradesToGlobal(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()
	writeTestGlobalSkills(t, SkillsConfig{Disabled: []string{"docx"}})

	path := ProjectSettingsPathForRoot(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := EffectiveDisabledSkills(root)
	if err == nil {
		t.Fatal("expected an error for a malformed project settings file")
	}
	if !got["docx"] {
		t.Fatalf("global layer lost after project parse failure: %v", got)
	}
}

func TestSaveSkillEnabledProjectScopePreservesOtherKeys(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()
	writeTestProjectSettings(t, root, map[string]any{
		"extra_allowed_paths": []string{"/tmp/x"},
	})

	if err := SaveSkillEnabled(root, "docx", false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	path := ProjectSettingsPathForRoot(root)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if _, ok := raw["extra_allowed_paths"]; !ok {
		t.Fatalf("extra_allowed_paths was dropped: %s", data)
	}
	var block skillsConfigFile
	if err := json.Unmarshal(raw["skills"], &block); err != nil {
		t.Fatalf("unmarshal skills block: %v", err)
	}
	if !reflect.DeepEqual(block.Disabled, []string{"docx"}) {
		t.Fatalf("disabled = %v, want [docx]", block.Disabled)
	}
	if len(block.Enabled) != 0 {
		t.Fatalf("enabled should stay empty, got %v", block.Enabled)
	}
}

func TestSaveSkillEnabledRoundTripAndOverride(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()

	if err := SaveSkillEnabled(root, "docx", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := EffectiveDisabledSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if !got["docx"] {
		t.Fatalf("docx should be disabled, got %v", got)
	}

	// Re-enable: the name must move from Disabled to Enabled, not just be
	// removed, so it can override a GLOBAL disable too.
	if err := SaveSkillEnabled(root, "docx", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	prefs, err := loadProjectSkillPrefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prefs.Enabled, []string{"docx"}) {
		t.Fatalf("enabled = %v, want [docx]", prefs.Enabled)
	}
	if len(prefs.Disabled) != 0 {
		t.Fatalf("disabled should be empty, got %v", prefs.Disabled)
	}
}

// TestSaveSkillEnabledProjectOverridesGlobalDisable is the end-to-end layering
// case the sidebar produces: disable globally (by hand), then enable in the
// project through the UI.
func TestSaveSkillEnabledProjectOverridesGlobalDisable(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()
	writeTestGlobalSkills(t, SkillsConfig{Disabled: []string{"docx"}})

	before, err := EffectiveDisabledSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if !before["docx"] {
		t.Fatal("precondition: docx should be globally disabled")
	}

	if err := SaveSkillEnabled(root, "docx", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	after, err := EffectiveDisabledSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if after["docx"] {
		t.Fatalf("project enable did not override the global disable: %v", after)
	}
}

func TestSaveSkillEnabledGlobalScopeWhenRootEmpty(t *testing.T) {
	setHomeTree(t, t.TempDir())

	if err := SaveSkillEnabled("", "docx", false); err != nil {
		t.Fatalf("disable globally: %v", err)
	}
	loaded, err := loadFullOcodeConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Skills.Disabled, []string{"docx"}) {
		t.Fatalf("global disabled = %v, want [docx]", loaded.Skills.Disabled)
	}

	if err := SaveSkillEnabled("", "docx", true); err != nil {
		t.Fatalf("enable globally: %v", err)
	}
	loaded, err = loadFullOcodeConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Skills.Disabled) != 0 || !reflect.DeepEqual(loaded.Skills.Enabled, []string{"docx"}) {
		t.Fatalf("global after enable = %+v", loaded.Skills)
	}
}

func TestSaveProjectSkillPrefsDeletesEmptyBlock(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()
	writeTestProjectSettings(t, root, map[string]any{
		"extra_allowed_paths": []string{"/tmp/x"},
		"skills":              map[string]any{"disabled": []string{"docx"}},
	})

	if err := saveProjectSkillPrefs(root, SkillsConfig{}); err != nil {
		t.Fatalf("save empty: %v", err)
	}
	data, err := os.ReadFile(ProjectSettingsPathForRoot(root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["skills"]; ok {
		t.Fatalf("empty skills block should be deleted, got %s", data)
	}
	if _, ok := raw["extra_allowed_paths"]; !ok {
		t.Fatalf("extra_allowed_paths should survive, got %s", data)
	}
}

func TestSaveSkillEnabledConcurrentTogglesAllSurvive(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()

	names := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	var wg sync.WaitGroup
	errs := make(chan error, len(names))
	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			errs <- SaveSkillEnabled(root, n, false)
		}(name)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent toggle: %v", err)
		}
	}

	got, err := EffectiveDisabledSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(names) {
		t.Fatalf("effective disabled = %v, want all of %v (a lost update means the lock failed)", got, names)
	}
	for _, name := range names {
		if !got[name] {
			t.Fatalf("lost concurrent toggle for %q: %v", name, got)
		}
	}
}

// A global config that cannot be loaded must not hide a readable project layer:
// the project's disables stay in force, the set is non-nil, and the error is
// still reported so callers can tell it is degraded.
func TestEffectiveDisabledSkillsUnreadableGlobalKeepsProjectLayer(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()
	cfgPath := writeTestGlobalSkills(t, SkillsConfig{Disabled: []string{"docx"}})
	if err := os.WriteFile(cfgPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestProjectSettings(t, root, map[string]any{
		"skills": map[string]any{"disabled": []string{"stt"}},
	})

	got, err := EffectiveDisabledSkills(root)
	if err == nil {
		t.Fatal("expected an error for an unreadable global config")
	}
	if got == nil || !got["stt"] {
		t.Fatalf("project layer lost after global load failure: %v", got)
	}
	if got["docx"] {
		t.Fatalf("unreadable global layer must contribute nothing: %v", got)
	}
}
