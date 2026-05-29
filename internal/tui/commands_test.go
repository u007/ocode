package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/config"
)

func TestRefreshCustomCommandsRespectsEnabledPlugins(t *testing.T) {
	wd := t.TempDir()
	home := filepath.Join(wd, "home")
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("MkdirAll home: %v", err)
	}
	t.Setenv("HOME", home)
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(wd, "opencode.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile opencode.json: %v", err)
	}
	pluginDir := filepath.Join(wd, ".opencode", "plugins", "sample")
	if err := os.MkdirAll(filepath.Join(pluginDir, "commands"), 0755); err != nil {
		t.Fatalf("MkdirAll plugin commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"sample"}`), 0644); err != nil {
		t.Fatalf("WriteFile plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "commands", "hello.md"), []byte("---\nname: hello\n---\nHello from plugin"), 0644); err != nil {
		t.Fatalf("WriteFile command: %v", err)
	}

	refreshCustomCommands(nil)
	if _, ok := customCommandLookup["/hello"]; !ok {
		t.Fatalf("expected plugin command to be loaded without filter")
	}

	refreshCustomCommands(&config.Config{Plugins: map[string]config.PluginConfig{"sample": {Enabled: false}}})
	if _, ok := customCommandLookup["/hello"]; ok {
		t.Fatalf("expected disabled plugin command to be hidden")
	}

	refreshCustomCommands(&config.Config{Plugins: map[string]config.PluginConfig{"sample": {Enabled: true}}})
	if _, ok := customCommandLookup["/hello"]; !ok {
		t.Fatalf("expected enabled plugin command to be loaded")
	}
}
