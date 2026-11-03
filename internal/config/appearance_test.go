package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestAppearanceSettingsRoundTrip persists a selection, reloads the config, and
// checks the values survive a save that also carries an unknown key from a newer
// (or older) binary.
func TestAppearanceSettingsRoundTrip(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	setHomeTree(t, tmp)

	dir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	path := filepath.Join(dir, "ocodeconfig.json")
	// A key this binary does not know about must survive the save (the raw-map
	// pattern every other block relies on).
	if err := os.WriteFile(path, []byte(`{"unknown_future_key":{"nested":1}}`), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	want := AppearanceConfig{
		ColorTheme:         "vscode:catppuccin.catppuccin-vsc/mocha",
		IconTheme:          "vscode:PKief.material-icon-theme/material-icon-theme",
		ExtraExtensionDirs: []string{"/opt/themes", "/home/james/portable/extensions"},
	}
	if err := SaveOcodeAppearanceConfig(want); err != nil {
		t.Fatalf("SaveOcodeAppearanceConfig: %v", err)
	}

	loaded, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if loaded.Appearance.ColorTheme != want.ColorTheme {
		t.Errorf("ColorTheme = %q, want %q", loaded.Appearance.ColorTheme, want.ColorTheme)
	}
	if loaded.Appearance.IconTheme != want.IconTheme {
		t.Errorf("IconTheme = %q, want %q", loaded.Appearance.IconTheme, want.IconTheme)
	}
	if !reflect.DeepEqual(loaded.Appearance.ExtraExtensionDirs, want.ExtraExtensionDirs) {
		t.Errorf("ExtraExtensionDirs = %v, want %v", loaded.Appearance.ExtraExtensionDirs, want.ExtraExtensionDirs)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"unknown_future_key"`) {
		t.Fatalf("unknown key dropped on save: %s", data)
	}
	if !strings.Contains(string(data), `"appearance"`) {
		t.Fatalf("appearance block missing from disk: %s", data)
	}
}

// An untouched install must not gain an "appearance" key: a written default
// would pin it, so a later change to the fallback could never reach the user.
func TestAppearanceBlockOmittedWhenUnset(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	setHomeTree(t, tmp)

	cfg := defaultOcodeConfig()
	if err := SaveOcodeConfig(&cfg); err != nil {
		t.Fatalf("SaveOcodeConfig: %v", err)
	}
	path := filepath.Join(tmp, ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), `"appearance"`) {
		t.Fatalf("unset appearance block was written: %s", data)
	}

	// Clearing a selection removes the block again.
	if err := SaveOcodeAppearanceConfig(AppearanceConfig{ColorTheme: "vscode:acme.themes/dark"}); err != nil {
		t.Fatalf("SaveOcodeAppearanceConfig(select): %v", err)
	}
	if err := SaveOcodeAppearanceConfig(AppearanceConfig{}); err != nil {
		t.Fatalf("SaveOcodeAppearanceConfig(clear): %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after clear: %v", err)
	}
	if strings.Contains(string(data), `"appearance"`) {
		t.Fatalf("cleared appearance block still on disk: %s", data)
	}
}

// An unknown configured theme must load, not fail the whole config: spec §13
// degrades it to a warning and a fallback at render time.
func TestAppearanceUnknownThemeLoads(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	setHomeTree(t, tmp)

	dir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	body := `{"appearance":{"color_theme":"vscode:not.installed/gone","icon_theme":"","extra_extension_dirs":[]}}`
	if err := os.WriteFile(filepath.Join(dir, "ocodeconfig.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	loaded, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if loaded.Appearance.ColorTheme != "vscode:not.installed/gone" {
		t.Fatalf("ColorTheme = %q, want the stored (unresolvable) id", loaded.Appearance.ColorTheme)
	}
}

func TestNormalizeAppearanceConfig(t *testing.T) {
	got := NormalizeAppearanceConfig(AppearanceConfig{
		ColorTheme:         "  vscode:acme.themes/dark  ",
		IconTheme:          "\t\n",
		ExtraExtensionDirs: []string{" /opt/a ", "", "/opt/a", "/opt/b", "   "},
	})
	want := AppearanceConfig{
		ColorTheme:         "vscode:acme.themes/dark",
		IconTheme:          "",
		ExtraExtensionDirs: []string{"/opt/a", "/opt/b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAppearanceConfig = %+v, want %+v", got, want)
	}
	if got := NormalizeAppearanceConfig(AppearanceConfig{ExtraExtensionDirs: []string{}}); got.ExtraExtensionDirs != nil {
		t.Fatalf("empty slice not normalized to nil: %#v", got.ExtraExtensionDirs)
	}
	if !(AppearanceConfig{}).IsZero() {
		t.Fatal("zero AppearanceConfig is not IsZero")
	}
	if (AppearanceConfig{ColorTheme: "vscode:acme.themes/dark"}).IsZero() {
		t.Fatal("a selection reported IsZero")
	}
}
