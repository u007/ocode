package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

func TestParseAgentContent_HonorsColorField(t *testing.T) {
	src := "---\ndescription: test\nmode: primary\ncolor: \"#7AA2F7\"\n---\nbody"
	def, _ := parseAgentContent(src, "fake.md")
	if def == nil {
		t.Fatal("expected def")
	}
	if def.Color != "\"#7AA2F7\"" && def.Color != "#7AA2F7" {
		t.Errorf("Color = %q, want #7AA2F7 (with or without quotes)", def.Color)
	}
}

func TestParseAgentContent_HonorsModelField(t *testing.T) {
	src := "---\ndescription: test\nmode: primary\nmodel: anthropic/claude-haiku-4-5\n---\nbody"
	def, diags := parseAgentContent(src, "fake.md")
	if def == nil {
		t.Fatalf("expected def, got diags: %+v", diags)
	}
	if def.Model != "anthropic/claude-haiku-4-5" {
		t.Errorf("Model = %q, want anthropic/claude-haiku-4-5", def.Model)
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "model") {
			t.Errorf("unexpected diagnostic for model field: %+v", d)
		}
	}
}

// Replaced by the richer sampling_params_test.go suite. Temperature/top_p are
// now applied (not warned about) when valid; warnings only fire for invalid
// numeric values.

func writeAgentFile(t *testing.T, parts ...string) {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findDef(defs []AgentDefinition, name string) *AgentDefinition {
	for i := range defs {
		if defs[i].Name == name {
			return &defs[i]
		}
	}
	return nil
}

// TestBundledPluginAgentLoadsAndDiskWins verifies the embedded (bundled)
// plugin agent is served on the normal init path when no disk copy exists, and
// that a disk copy overrides it (bundled is prepended as the lowest-precedence
// source).
func TestBundledPluginAgentLoadsAndDiskWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const name = "embeddedfallback"

	// Embedded (bundled) plugin.
	bundledDir := t.TempDir()
	writeAgentFile(t, bundledDir, name, "plugin.json")
	if err := os.WriteFile(filepath.Join(bundledDir, name, "plugin.json"), []byte(`{"name":"`+name+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAgentFile(t, bundledDir, name, "agents", name+".md")
	if err := os.WriteFile(filepath.Join(bundledDir, name, "agents", name+".md"),
		[]byte("---\nname: "+name+"\ndescription: bundled test agent\n---\nBUNDLED_PROMPT"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := bundled.PluginsDir
	bundled.PluginsDir = bundledDir
	defer func() { bundled.PluginsDir = prev }()

	oldwd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)

	// No disk copy -> bundled agent served through the normal LoadMarkdownAgents
	// startup path.
	r := NewAgentRegistry()
	r.LoadMarkdownAgents()
	def := findDef(r.All(), name)
	if def == nil {
		t.Fatal("bundled agent not loaded")
	}
	if def.SystemPrompt != "BUNDLED_PROMPT" {
		t.Fatalf("expected bundled prompt, got %q", def.SystemPrompt)
	}

	// Disk project copy overrides the bundled copy.
	diskRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(diskRoot, "opencode.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAgentFile(t, diskRoot, ".opencode", "plugins", name, "plugin.json")
	if err := os.WriteFile(filepath.Join(diskRoot, ".opencode", "plugins", name, "plugin.json"), []byte(`{"name":"`+name+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAgentFile(t, diskRoot, ".opencode", "plugins", name, "agents", name+".md")
	if err := os.WriteFile(filepath.Join(diskRoot, ".opencode", "plugins", name, "agents", name+".md"),
		[]byte("---\nname: "+name+"\ndescription: disk test agent\n---\nDISK_PROMPT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(diskRoot); err != nil {
		t.Fatal(err)
	}

	r2 := NewAgentRegistry()
	r2.LoadMarkdownAgents()
	def2 := findDef(r2.All(), name)
	if def2 == nil {
		t.Fatal("agent not loaded with disk override")
	}
	if def2.SystemPrompt != "DISK_PROMPT" {
		t.Fatalf("disk should override bundled, got %q", def2.SystemPrompt)
	}
}
