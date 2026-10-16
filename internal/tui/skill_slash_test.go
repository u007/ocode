package tui

import (
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
