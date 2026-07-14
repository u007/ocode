package skill

import (
	"testing"
)

func TestKaizenReviewEmbedded(t *testing.T) {
	// Repo root is the parent of this package dir.
	root := "../.."
	all := LoadSkillsForRoot(root)

	var found *Skill
	for i := range all {
		if all[i].Name == "kaizen-review" {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("kaizen-review skill NOT discovered under repo skills/")
	}
	if found.TunedFor != "" {
		t.Fatalf("kaizen-review must be a NORMAL skill (empty TunedFor), got %q", found.TunedFor)
	}
	if found.Content == "" {
		t.Fatalf("kaizen-review has empty content")
	}
	t.Logf("OK: kaizen-review discovered; source=%s; bytes=%d", found.Source, len(found.Content))

	// It must be admitted for every model as a normal skill (no Kaizen gate).
	if found.TunedFor != "" {
		t.Fatalf("normal skill must have empty TunedFor to be universally admitted")
	}

	// And it must appear in the ungated catalog for the repo root. LoadSkills()
	// resolves from the process cwd, so we check at the explicit root via
	// LoadSkillsForModel (which admits every normal skill regardless of model).
	ungated := LoadSkillsForModel(root, "some-model")
	present := false
	for _, s := range ungated {
		if s.Name == "kaizen-review" {
			present = true
			break
		}
	}
	if !present {
		t.Fatalf("kaizen-review should appear in the ungated catalog for the repo root")
	}
}
