package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavePluginEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	cfgPath := filepath.Join(tmpDir, "opencode.json")
	if err := os.WriteFile(cfgPath, []byte(`{"plugins":{"myplugin":{"source":"github.com/x/y","dir":"/tmp/x-y","enabled":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := SavePluginEnabled("myplugin", false); err != nil {
		t.Fatalf("SavePluginEnabled: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins["myplugin"].Enabled {
		t.Error("expected plugin to be disabled")
	}
}

func TestSavePlugin(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	if err := os.WriteFile(filepath.Join(tmpDir, "opencode.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	p := PluginConfig{Source: "github.com/acme/plugin", Dir: "/home/user/.config/opencode/plugins/acme-plugin", Enabled: true}
	if err := SavePlugin("acme-plugin", p); err != nil {
		t.Fatalf("SavePlugin: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Plugins["acme-plugin"]
	if got.Source != p.Source || got.Dir != p.Dir || !got.Enabled {
		t.Errorf("got %+v, want %+v", got, p)
	}
}

func TestRemovePlugin(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	cfgPath := filepath.Join(tmpDir, "opencode.json")
	if err := os.WriteFile(cfgPath, []byte(`{"plugins":{"gone":{"source":"x","dir":"/tmp/x","enabled":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := RemovePlugin("gone"); err != nil {
		t.Fatalf("RemovePlugin: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Plugins["gone"]; ok {
		t.Error("plugin still present after remove")
	}
}
