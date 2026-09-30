package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

func writeSkillFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPluginSkillsAreNamespaced verifies a plugin's skills load as
// "<plugin>:<skill>" alongside (not shadowing) a same-named user skill, and
// that LoadSkill resolves a bare name to the user skill first, falling back
// to the plugin skill only when no ordinary skill has that name.
func TestPluginSkillsAreNamespaced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Chdir(t.TempDir())
	prevSkills, prevPlugins := bundled.SkillsDir, bundled.PluginsDir
	bundled.SkillsDir, bundled.PluginsDir = "", ""
	t.Cleanup(func() { bundled.SkillsDir, bundled.PluginsDir = prevSkills, prevPlugins })
	InvalidateSkillCache()
	t.Cleanup(InvalidateSkillCache)

	// A Claude Code-format plugin in ocode's global plugin dir.
	pdir := filepath.Join(home, ".config", "opencode", "plugins", "superpowers")
	writeSkillFixture(t, filepath.Join(pdir, ".claude-plugin", "plugin.json"), `{"name":"superpowers"}`)
	writeSkillFixture(t, filepath.Join(pdir, "skills", "brainstorming", "SKILL.md"),
		"---\nname: brainstorming\ndescription: plugin brainstorming\n---\n")
	writeSkillFixture(t, filepath.Join(pdir, "skills", "writing-plans", "SKILL.md"),
		"---\nname: writing-plans\ndescription: plugin plans\n---\n")
	// A user skill with the same bare name.
	writeSkillFixture(t, filepath.Join(home, ".claude", "skills", "brainstorming", "SKILL.md"),
		"---\nname: brainstorming\ndescription: user brainstorming\n---\n")

	byName := map[string]Skill{}
	for _, s := range LoadSkillsForRoot("") {
		byName[s.Name] = s
	}
	if s, ok := byName["superpowers:brainstorming"]; !ok || s.Plugin != "superpowers" || s.Description != "plugin brainstorming" {
		t.Fatalf("namespaced plugin skill missing or wrong: %+v", s)
	}
	if s, ok := byName["brainstorming"]; !ok || s.Plugin != "" || s.Description != "user brainstorming" {
		t.Fatalf("user skill shadowed or wrong: %+v", s)
	}

	for name, wantDesc := range map[string]string{
		"brainstorming":             "user brainstorming",
		"superpowers:brainstorming": "plugin brainstorming",
		"writing-plans":             "plugin plans",
	} {
		s, err := LoadSkill(name)
		if err != nil || s == nil {
			t.Fatalf("LoadSkill(%q) = %v, %v", name, s, err)
		}
		if s.Description != wantDesc {
			t.Errorf("LoadSkill(%q) description = %q, want %q", name, s.Description, wantDesc)
		}
	}
}
