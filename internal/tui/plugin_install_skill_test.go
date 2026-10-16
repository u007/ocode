package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/u007/ocode/internal/skill"
)

const alphaSkillBody = "---\nname: alpha\nversion: 1.0.0\n---\n# alpha\n"

// registerTestBundledSkills injects a synthetic bundled FS so the skill
// installer resolves names here, as the real binary does at startup.
func registerTestBundledSkills(t *testing.T) {
	t.Helper()
	skill.SetBundledFS(fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{Data: []byte(alphaSkillBody)},
	})
	t.Cleanup(func() { skill.SetBundledFS(nil) })
}

// TestPluginInstallBareNameIsSkillNotGitHub: `/plugin install <name>` with no
// path separator is a bundled-skill install. An unknown name must come back as
// an error message naming the input. It must not reach the GitHub installer
// (pluginInstallMsg), which would try to clone "nope" as a repo.
func TestPluginInstallBareNameIsSkillNotGitHub(t *testing.T) {
	registerTestBundledSkills(t)
	m := newTestModel()

	cmd := runPluginCmd(&m, []string{"install", "nope"})
	if cmd == nil {
		t.Fatal("bare-name install returned no command")
	}
	msg, ok := cmd().(skillsOutputMsg)
	if !ok {
		t.Fatalf("bare-name install produced %T, want skillsOutputMsg (never pluginInstallMsg)", cmd())
	}
	if !strings.Contains(msg.text, `no bundled skill "nope"`) {
		t.Fatalf("unknown bare name should report no bundled skill \"nope\", got %q", msg.text)
	}
}

// TestPluginInstallBundledSkillWritesClaudeDir: a bare name that IS bundled
// installs into ~/.claude/skills under the temp HOME, never touching the real one.
func TestPluginInstallBundledSkillWritesClaudeDir(t *testing.T) {
	registerTestBundledSkills(t)
	home := t.TempDir()
	setVoiceHomeTree(t, home)
	m := newTestModel()

	cmd := runPluginCmd(&m, []string{"install", "alpha"})
	if cmd == nil {
		t.Fatal("bare-name install returned no command")
	}
	msg, ok := cmd().(skillsOutputMsg)
	if !ok {
		t.Fatalf("bare-name install produced %T, want skillsOutputMsg", cmd())
	}
	if !strings.Contains(msg.text, "installed alpha") {
		t.Fatalf("report should name the install, got %q", msg.text)
	}
	got, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "alpha", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if string(got) != alphaSkillBody {
		t.Fatalf("installed body mismatch:\nwant %q\ngot  %q", alphaSkillBody, got)
	}
}

// TestPluginInstallURLStillUsesGitHub: anything with a path separator keeps the
// GitHub installer path.
func TestPluginInstallURLStillUsesGitHub(t *testing.T) {
	m := newTestModel()

	cmd := runPluginCmd(&m, []string{"install", "github.com/user/repo@v1"})
	if cmd == nil {
		t.Fatal("URL install returned no command")
	}
	msg, ok := cmd().(pluginInstallMsg)
	if !ok {
		t.Fatalf("URL install produced %T, want pluginInstallMsg", cmd())
	}
	if msg.source != "github.com/user/repo" || msg.ref != "v1" {
		t.Fatalf("source/ref = %q/%q, want github.com/user/repo/v1", msg.source, msg.ref)
	}
}
