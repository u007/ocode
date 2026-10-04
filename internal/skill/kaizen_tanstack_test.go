package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

// tanstack is a DETECTED stack (internal/stackdetect: a `@tanstack/react-query` /
// `@tanstack/react-router` dep in <root>/package.json) and is deliberately NOT in
// universalStacks. So this skill has a TWO-AXIS gate: the provider-stripped model id
// must equal tuned_for AND the session's repo must actually be a TanStack project.
//
// That second axis is the one that silently voids delivery — the ocode repo itself has
// no TanStack dependency, so asserting against repoRoot() proves nothing. These tests
// therefore stand up throwaway roots on both sides of the detector.
//
// Note on provenance: this is NOT a scorecard derivation. The closed-book tanstack
// scorecard for space-bunny-free is 92.6% with no tag below the 0.75 threshold (see
// docs/okf/tanstack/scores/space-bunny-free.md, "Derivation targets: none"). The skill
// is a requested stack-hygiene correction plus the one item the benchmark did flag —
// suspense, via tanstack-suspense-02 at 0.50.
//
// These tests pin the DELIVERY contract only. They assert nothing about TanStack
// correctness; that is the scorecard's job, not a Go test's.

const tanstackSkillName = "tanstack-tuning-space-bunny-free"

// useBundledRepoSkills points skill discovery at the repo's own skills/ tree (standing
// in for main's EnsureExtracted, as kaizen_pdf_test.go does) so a temp root can borrow
// the real skill files.
func useBundledRepoSkills(t *testing.T) {
	t.Helper()
	prev := bundled.SkillsDir
	bundled.SkillsDir = filepath.Join(repoRoot(), "skills")
	t.Cleanup(func() { bundled.SkillsDir = prev; InvalidateSkillCache() })
	InvalidateSkillCache()
}

// tanstackRoot returns a temp project root that stackdetect reads as a TanStack repo.
func tanstackRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pkg := `{"name":"x","dependencies":{"react":"^19.0.0","@tanstack/react-router":"^1"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func kaizenSkillNames(skills []Skill) []string {
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return names
}

func TestKaizenDelivery_spacebunny_tanstack(t *testing.T) {
	useBundledRepoSkills(t)
	const model = "opencode-go/space-bunny-free"

	// 1. Load path: model matches AND the stack is detected.
	names := kaizenSkillNames(KaizenSkillsForModel(tanstackRoot(t), model))
	t.Logf("KaizenSkillsForModel(tanstackRoot, %s) = %v", model, names)
	if !contains(names, tanstackSkillName) {
		t.Fatalf("%s NOT admitted for %s in a TanStack repo; got %v", tanstackSkillName, model, names)
	}

	// 2. Catalog: the model-aware catalog advertises it.
	if !strings.Contains(BuildCatalogForModel(tanstackRoot(t), model), tanstackSkillName) {
		t.Fatalf("BuildCatalogForModel does not advertise %s", tanstackSkillName)
	}

	// 3. Ungated (excludeKaizen) set must NOT contain it — a caller that does not know
	// the active model must never see a model-specific correction.
	for _, s := range excludeKaizen(LoadSkillsForRoot(tanstackRoot(t))) {
		if s.Name == tanstackSkillName {
			t.Fatalf("excludeKaizen leaked the Kaizen skill %s", tanstackSkillName)
		}
	}

	// 4. Wrong model is refused even though the stack IS detected. tuned_for is the
	// gate, so a typo there would silently widen delivery to every model.
	for _, other := range []string{"anthropic/claude-opus-4-8", "opencode-go/mimo-v2.5", "mimo-v2.5"} {
		for _, s := range KaizenSkillsForModel(tanstackRoot(t), other) {
			if s.Name == tanstackSkillName {
				t.Fatalf("wrong model %s admitted %s", other, tanstackSkillName)
			}
		}
	}

	// 5. Right model, WRONG repo: tanstack is not a universal stack, so a repo with no
	// TanStack dep must not receive it. This is the axis that silently voids delivery,
	// and the reason a repoRoot() assertion would have passed vacuously.
	for _, s := range KaizenSkillsForModel(t.TempDir(), model) {
		if s.Name == tanstackSkillName {
			t.Fatalf("%s admitted in a NON-TanStack repo; the stack gate is not working", tanstackSkillName)
		}
	}
}

func TestKaizenDigest_spacebunny_tanstack(t *testing.T) {
	useBundledRepoSkills(t)
	const model = "opencode-go/space-bunny-free"

	block := KaizenDigestBlock(tanstackRoot(t), model)
	if strings.TrimSpace(block) == "" {
		t.Fatal("expected a digest block for space-bunny-free in a TanStack repo, got empty")
	}
	// Both load-bearing cruxes must survive compression into the cached prompt prefix.
	// These are the whole point of the skill, and the digest is the part guaranteed to
	// reach the model.
	for _, crux := range []string{
		"The route tree is GENERATED",    // generated-file hygiene (section 1)
		"Suspense needs BOTH boundaries", // the scored-0.50 pairing gap
		"queryFn` must THROW",            // the scored-0.50 throw contract
		"never report its formatting as a defect",
	} {
		if !strings.Contains(block, crux) {
			t.Errorf("digest block dropped a required crux: %q", crux)
		}
	}

	// The parsed skill must actually carry a Digest (guards the sync + marker
	// propagation, not just the renderer).
	for _, s := range KaizenSkillsForModel(tanstackRoot(t), model) {
		if s.Name == tanstackSkillName && s.Digest == "" {
			t.Fatal("embedded tanstack skill parsed with an empty Digest (markers lost in sync?)")
		}
	}

	// Negative controls, so "admitted for the right model in the right repo" stays
	// distinguishable from "injected unconditionally":
	//  - wrong model in a TanStack repo
	if got := KaizenDigestBlock(tanstackRoot(t), "anthropic/claude-opus-4-8"); strings.Contains(got, "The route tree is GENERATED") {
		t.Fatalf("tanstack digest injected for a NON-tuned model: %q", got)
	}
	//  - right model, non-TanStack repo
	if got := KaizenDigestBlock(t.TempDir(), model); strings.Contains(got, "The route tree is GENERATED") {
		t.Fatalf("tanstack digest injected into a NON-TanStack repo: %q", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
