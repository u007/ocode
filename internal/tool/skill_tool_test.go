package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/skill"
)

func writeSkillToolFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// skillToolEnv isolates HOME, cwd and the bundled dirs so only the fixtures a
// test writes are discovered. It returns the fake home.
func skillToolEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Chdir(t.TempDir())
	prevSkills, prevPlugins := bundled.SkillsDir, bundled.PluginsDir
	bundled.SkillsDir, bundled.PluginsDir = "", ""
	t.Cleanup(func() { bundled.SkillsDir, bundled.PluginsDir = prevSkills, prevPlugins })
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)
	return home
}

func runSkillTool(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"name": name})
	out, err := SkillTool{}.ExecuteCtx(ctx, args)
	if err != nil {
		t.Fatalf("skill %q: %v", name, err)
	}
	return out
}

// TestSkillToolReportsBaseDirectory pins Claude Code parity: a skill's own
// files (scripts/, references/, templates/, sibling .md files) are referenced
// by relative path, so the tool must say where the skill lives. For a plugin
// skill it also names the plugin root and expands ${CLAUDE_PLUGIN_ROOT}.
func TestSkillToolReportsBaseDirectory(t *testing.T) {
	home := skillToolEnv(t)
	pdir := filepath.Join(home, ".config", "opencode", "plugins", "superpowers")
	writeSkillToolFixture(t, filepath.Join(pdir, ".claude-plugin", "plugin.json"), `{"name":"superpowers"}`)
	skillDir := filepath.Join(pdir, "skills", "brainstorming")
	writeSkillToolFixture(t, filepath.Join(skillDir, "SKILL.md"),
		"---\nname: brainstorming\ndescription: d\n---\nRead `visual-companion.md`; run ${CLAUDE_PLUGIN_ROOT}/hooks/x.\n")

	out := runSkillTool(t, context.Background(), "superpowers:brainstorming")
	for _, want := range []string{
		"Base directory for this skill: " + skillDir,
		"Plugin root: " + pdir,
		"run " + pdir + "/hooks/x.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "${CLAUDE_PLUGIN_ROOT}") {
		t.Errorf("${CLAUDE_PLUGIN_ROOT} not expanded:\n%s", out)
	}
}

// TestSkillToolResolvesNamespacedNameToPlainSkill covers skills copied into a
// skills dir instead of installed as a plugin: they are named "writing-plans",
// but superpowers skills reference each other as "superpowers:writing-plans".
func TestSkillToolResolvesNamespacedNameToPlainSkill(t *testing.T) {
	home := skillToolEnv(t)
	writeSkillToolFixture(t, filepath.Join(home, ".claude", "skills", "writing-plans", "SKILL.md"),
		"---\nname: writing-plans\ndescription: plain copy\n---\nbody\n")

	out := runSkillTool(t, context.Background(), "superpowers:writing-plans")
	if !strings.Contains(out, "plain copy") {
		t.Fatalf("namespaced name did not resolve to the plain skill:\n%s", out)
	}
}

// TestSkillToolUsesSessionProjectRoot pins that project-local skills resolve
// against the session's project, not the process cwd (the desktop app starts
// with cwd "/").
func TestSkillToolUsesSessionProjectRoot(t *testing.T) {
	skillToolEnv(t)
	proj := t.TempDir()
	writeSkillToolFixture(t, filepath.Join(proj, ".claude", "skills", "local-only", "SKILL.md"),
		"---\nname: local-only\ndescription: project skill\n---\nbody\n")

	out := runSkillTool(t, WithWorkDir(context.Background(), proj), "local-only")
	if !strings.Contains(out, "project skill") {
		t.Fatalf("project-local skill not resolved from the session root:\n%s", out)
	}
}
