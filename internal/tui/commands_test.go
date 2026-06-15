package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/memory"
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

func TestRunMemCmdTogglesAndShowsStatus(t *testing.T) {
	wd := t.TempDir()
	home := filepath.Join(wd, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll home: %v", err)
	}
	t.Setenv("HOME", home)

	snap, err := memory.Status(wd)
	if err != nil {
		t.Fatalf("memory.Status: %v", err)
	}
	for path, body := range map[string]string{
		snap.User.Path:    "remember user prefs\nsecond line\nthird line\nmore\n",
		snap.Project.Path: "remember project decisions\n",
		snap.Global.Path:  "remember global lessons\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}

	m := model{workDir: wd, config: &config.Config{Ocode: config.OcodeConfig{MemoryEnabled: false}}}

	runMemCmd(&m, nil)
	if len(m.messages) == 0 {
		t.Fatal("expected /mem to append a status message")
	}
	got := m.messages[len(m.messages)-1].text
	for _, want := range []string{"Memory context injection: disabled", "Project memory", "User memory", "remember user prefs", "remember project decisions", "remember global lessons", "internal/memory/memory.go", "skills/ocode-mem/SKILL.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q: %s", want, got)
		}
	}

	runMemCmd(&m, []string{"on"})
	if !m.config.Ocode.MemoryEnabled {
		t.Fatal("expected /mem on to persist enabled state in config")
	}
	got = m.messages[len(m.messages)-1].text
	if !strings.Contains(got, "enabled") {
		t.Fatalf("expected /mem on confirmation, got %s", got)
	}

	runMemCmd(&m, []string{"status"})
	got = m.messages[len(m.messages)-1].text
	if !strings.Contains(got, "Memory context injection: enabled") {
		t.Fatalf("status output missing enabled state: %s", got)
	}
	for _, want := range []string{snap.User.Path, snap.Project.Path, snap.Global.Path} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing memory path %q: %s", want, got)
		}
	}
	for _, want := range []string{"internal/memory/memory.go", "internal/tui/memory.go", "internal/tui/commands.go", "internal/config/ocodeconfig.go", "skills/ocode-mem/SKILL.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing related file path %q: %s", want, got)
		}
	}
}
