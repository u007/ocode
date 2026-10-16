package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/skill"
)

// TestRepoSkillsRegisterAsSlashCommands: /changes and /standup are bundled
// skills, not built-in commands, so they must reach the slash popup through
// the skill-as-slash path. A built-in of the same name would shadow the skill.
func TestRepoSkillsRegisterAsSlashCommands(t *testing.T) {
	prev := bundled.SkillsDir
	bundled.SkillsDir = filepath.Join("..", "..", "skills")
	t.Cleanup(func() {
		bundled.SkillsDir = prev
		skill.InvalidateSkillCache()
	})
	skill.InvalidateSkillCache()

	refreshCustomCommands(nil, t.TempDir(), "")

	for _, key := range []string{"/changes", "/standup"} {
		if _, ok := customCommandLookup[key]; !ok {
			t.Fatalf("%s not registered as a slash skill", key)
		}
		if _, builtin := commandLookup[key]; builtin {
			t.Fatalf("%s is still a built-in command; it would shadow the skill", key)
		}
	}
}

// TestSkillsOutputRefreshesSlashCommands: a successful skill install writes a
// new SKILL.md and invalidates the skill cache. The skillsOutputMsg handler
// must rebuild the slash lookup, or the new /<name> stays unreachable until an
// unrelated refresh runs.
func TestSkillsOutputRefreshesSlashCommands(t *testing.T) {
	t.Cleanup(func() {
		skill.InvalidateSkillCache()
		refreshCustomCommands(nil, "", "")
	})
	root := t.TempDir()
	const name = "zz-refresh-probe-skill"
	m := newTestModel()
	m.workDir = root
	skill.InvalidateSkillCache()
	m.refreshCustomCommands()
	if _, ok := customCommandLookup["/"+name]; ok {
		t.Fatalf("precondition: /%s already registered", name)
	}

	dir := filepath.Join(root, ".claude", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: probe\n---\nprobe body\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	skill.InvalidateSkillCache()

	updated, _ := m.Update(skillsOutputMsg{text: "installed " + name})
	derefTestModel(t, updated)
	if _, ok := customCommandLookup["/"+name]; !ok {
		t.Fatalf("/%s not registered after skillsOutputMsg; handler did not refresh slash commands", name)
	}
}
