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

	refreshCustomCommands(nil, "", "")
	if _, ok := customCommandLookup["/hello"]; !ok {
		t.Fatalf("expected plugin command to be loaded without filter")
	}

	refreshCustomCommands(&config.Config{Plugins: map[string]config.PluginConfig{"sample": {Enabled: false}}}, "", "")
	if _, ok := customCommandLookup["/hello"]; ok {
		t.Fatalf("expected disabled plugin command to be hidden")
	}

	refreshCustomCommands(&config.Config{Plugins: map[string]config.PluginConfig{"sample": {Enabled: true}}}, "", "")
	if _, ok := customCommandLookup["/hello"]; !ok {
		t.Fatalf("expected enabled plugin command to be loaded")
	}
}

// TestRefreshCustomCommandsKaizenSlashAdmit: Kaizen skills appear as slash
// commands only when activeModel matches (incl. OpenRouter :free variant).
func TestRefreshCustomCommandsKaizenSlashAdmit(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	// Isolate from real bundled + project skills.
	// skill package uses HOME + ProjectLocalSkillDirs(root) + bundled.
	// Write a Kaizen conduct skill under root/skills/kaizen/<name>/.
	skillDir := filepath.Join(root, "skills", "kaizen", "conduct-tuning-tencent-hy3")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: conduct-tuning-tencent-hy3\n" +
		"description: kaizen conduct for hy3.\n" +
		"tuned_for: tencent/hy3\nstack: conduct\n---\n\n# body\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Normal skill always admitted.
	normalDir := filepath.Join(root, "skills", "normal-skill")
	if err := os.MkdirAll(normalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(normalDir, "SKILL.md"),
		[]byte("---\nname: normal-skill\ndescription: always on.\n---\n\n# n\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wrong model: Kaizen hidden, normal present.
	refreshCustomCommands(nil, root, "anthropic/claude-opus-4-8")
	if _, ok := customCommandLookup["/conduct-tuning-tencent-hy3"]; ok {
		t.Fatal("Kaizen skill must not appear for non-matching model")
	}
	if _, ok := customCommandLookup["/normal-skill"]; !ok {
		t.Fatal("normal skill must appear regardless of model")
	}

	// OpenRouter free variant: Kaizen admitted.
	refreshCustomCommands(nil, root, "openrouter/tencent/hy3:free")
	if _, ok := customCommandLookup["/conduct-tuning-tencent-hy3"]; !ok {
		t.Fatal("expected /conduct-tuning-tencent-hy3 for openrouter/tencent/hy3:free")
	}
	// Slash popup must surface it on /conduct prefix.
	found := false
	for _, s := range slashSuggestions("/conduct") {
		if s.name == "/conduct-tuning-tencent-hy3" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("slashSuggestions(/conduct) missing conduct-tuning-tencent-hy3")
	}

	// Empty model: Kaizen hidden again.
	refreshCustomCommands(nil, root, "")
	if _, ok := customCommandLookup["/conduct-tuning-tencent-hy3"]; ok {
		t.Fatal("Kaizen skill must not appear when activeModel empty")
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
