package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// External plugin state (source/dir/ref/enabled) is owned by ocodeconfig.json,
// not opencode.json — see LoadOcodeConfig / SavePlugin in ocodeconfig.go.

func TestSavePluginEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	ocodeCfgDir := filepath.Join(tmpHome, ".config", "opencode")
	if err := os.MkdirAll(ocodeCfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ocodeCfgPath := filepath.Join(ocodeCfgDir, "ocodeconfig.json")
	if err := os.WriteFile(ocodeCfgPath, []byte(`{"external_plugins":{"myplugin":{"source":"github.com/x/y","dir":"/tmp/x-y","enabled":true}}}`), 0o600); err != nil {
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

	ocodeCfgPath := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(ocodeCfgPath)
	if err != nil {
		t.Fatalf("read ocodeconfig.json: %v", err)
	}
	if !strings.Contains(string(data), "acme-plugin") {
		t.Errorf("expected acme-plugin persisted in ocodeconfig.json, got: %s", data)
	}
}

func TestRemovePlugin(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	ocodeCfgDir := filepath.Join(tmpHome, ".config", "opencode")
	if err := os.MkdirAll(ocodeCfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ocodeCfgPath := filepath.Join(ocodeCfgDir, "ocodeconfig.json")
	if err := os.WriteFile(ocodeCfgPath, []byte(`{"external_plugins":{"gone":{"source":"x","dir":"/tmp/x","enabled":true}}}`), 0o600); err != nil {
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

func TestRemoveMCPServer(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpHome)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	cfgPath := filepath.Join(tmpDir, "opencode.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcp":{"gone":{"type":"local","command":["echo","hi"],"enabled":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := RemoveMCPServer("gone"); err != nil {
		t.Fatalf("RemoveMCPServer: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.MCP["gone"]; ok {
		t.Error("mcp server still present after remove")
	}
}
