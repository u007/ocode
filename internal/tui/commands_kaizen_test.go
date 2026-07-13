package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/skill"
)

// TestRefreshCustomCommandsKaizenAdmit reproduces the exact user-reported
// scenario: on model "openrouter/tencent/hy3:free" the per-model conduct Kaizen
// skill must appear in the / slash autocomplete as /conduct-tuning-tencent-hy3,
// and must NOT appear for an unrelated model. This guards the two fixes that
// make that true:
//  1. modelMatchesTuned strips the OpenRouter ":free" variant before compare.
//  2. refreshCustomCommands uses the model-aware LoadSkillsForModel (not the
//     ungated LoadSkills), so Kaizen skills are gated into the slash catalog
//     only when the active model matches.
func TestRefreshCustomCommandsKaizenAdmit(t *testing.T) {
	// Isolate HOME + bundled skills dir so the test only sees the skill we
	// write under the temp workDir.
	t.Setenv("HOME", t.TempDir())
	prevBundled := bundled.SkillsDir
	bundled.SkillsDir = ""
	t.Cleanup(func() { bundled.SkillsDir = prevBundled })
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)

	root := t.TempDir()
	dir := filepath.Join(root, "skills", "kaizen", "conduct-tuning-tencent-hy3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: conduct-tuning-tencent-hy3\ndescription: conduct tuning for tencent/hy3\ntuned_for: tencent/hy3\nstack: conduct\n---\n\n# conduct-tuning-tencent-hy3\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Matching model (with OpenRouter :free variant) -> command registered.
	refreshCustomCommands(nil, root, "openrouter/tencent/hy3:free")
	if _, ok := customCommandLookup["/conduct-tuning-tencent-hy3"]; !ok {
		t.Fatalf("expected /conduct-tuning-tencent-hy3 in slash catalog for openrouter/tencent/hy3:free")
	}

	// Unrelated model -> command must NOT be registered (Kaizen stays gated).
	refreshCustomCommands(nil, root, "anthropic/claude-opus-4-8")
	if _, ok := customCommandLookup["/conduct-tuning-tencent-hy3"]; ok {
		t.Fatalf("/conduct-tuning-tencent-hy3 must NOT appear for an unrelated model")
	}

	// Empty active model -> no Kaizen admitted (ungated default).
	refreshCustomCommands(nil, root, "")
	if _, ok := customCommandLookup["/conduct-tuning-tencent-hy3"]; ok {
		t.Fatalf("/conduct-tuning-tencent-hy3 must NOT appear with empty active model")
	}
}
