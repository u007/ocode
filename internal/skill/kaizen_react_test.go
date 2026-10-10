package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// react-tuning-claude-opus-4-8 shipped on 2026-10-02 when a stale
// `illustrative: true` flag was removed from its docs/okf source. While that flag
// stood, sync-derived-skills.py refused to promote it and claude-opus-4-8 ran with
// ZERO React corrections, despite docs/okf/react/scores/claude-opus-4-8.md scoring 84%
// with three tags below the 0.75 threshold (rsc 0.58, suspense 0.55, refs 0.67).
//
// These tests pin the DELIVERY contract: `react` is a DETECTED stack (internal/
// stackdetect: a `react` dep in <root>/package.json, or a .jsx/.tsx file importing
// react), so admission needs BOTH axes — the provider-stripped model id equal to
// tuned_for AND a React repo. Asserting against repoRoot() would prove nothing, since
// the ocode repo itself is not a React project.
func TestKaizenDelivery_opus48_react(t *testing.T) {
	useBundledRepoSkills(t)
	const model = "anthropic/claude-opus-4-8"
	const name = "react-tuning-claude-opus-4-8"

	names := kaizenSkillNames(KaizenSkillsForModel(reactRoot(t), model))
	t.Logf("KaizenSkillsForModel(reactRoot, %s) = %v", model, names)
	if !contains(names, name) {
		t.Fatalf("%s NOT admitted for %s in a React repo; got %v", name, model, names)
	}

	if !strings.Contains(BuildCatalogForModel(reactRoot(t), model), name) {
		t.Fatalf("BuildCatalogForModel does not advertise %s", name)
	}

	// Ungated callers must never see a model-specific correction.
	for _, s := range excludeKaizen(LoadSkillsForRoot(reactRoot(t))) {
		if s.Name == name {
			t.Fatalf("excludeKaizen leaked the Kaizen skill %s", name)
		}
	}

	// Wrong model: tuned_for is the gate, so a typo would widen delivery to everyone.
	for _, other := range []string{"anthropic/claude-sonnet-4-5", "opencode-go/space-bunny-free", "space-bunny-free"} {
		for _, s := range KaizenSkillsForModel(reactRoot(t), other) {
			if s.Name == name {
				t.Fatalf("wrong model %s admitted %s", other, name)
			}
		}
	}

	// Right model, wrong repo: react is not a universal stack.
	for _, s := range KaizenSkillsForModel(t.TempDir(), model) {
		if s.Name == name {
			t.Fatalf("%s admitted in a NON-React repo; the stack gate is not working", name)
		}
	}
}

// reactRoot returns a temp project root stackdetect reads as a React repo.
func reactRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pkg := `{"name":"x","dependencies":{"react":"^19.0.0"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
