package tui

import (
	"strings"
	"testing"
)

// TestPluginInstallBareNameIsSkillNotGitHub: `/plugin install <name>` with no
// path separator is a bundled-skill install. An unknown name must come back as
// an error message naming the input. It must not reach the GitHub installer
// (pluginInstallMsg), which would try to clone "nosuch" as a repo.
func TestPluginInstallBareNameIsSkillNotGitHub(t *testing.T) {
	m := newTestModel()

	cmd := runPluginCmd(&m, []string{"install", "nosuch-skill"})
	if cmd == nil {
		t.Fatal("bare-name install returned no command")
	}
	msg, ok := cmd().(skillsOutputMsg)
	if !ok {
		t.Fatalf("bare-name install produced %T, want skillsOutputMsg (never pluginInstallMsg)", cmd())
	}
	if !strings.Contains(msg.text, "nosuch-skill") || !strings.Contains(msg.text, "no bundled skill") {
		t.Fatalf("unknown bare name should report no bundled skill, got %q", msg.text)
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
