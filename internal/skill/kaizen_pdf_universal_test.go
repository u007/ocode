package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/stackdetect"
)

// The pdf corpus is UNIVERSAL: its tuning skills are admitted on an exact
// model-id match alone, with no stackdetect gate (see universalStacks in
// loader.go). The marker-file gate could not see the tasks these corrections
// exist for — generating a PDF from scratch, a PDF attached from outside the
// repo, a PDF deeper than the glob limit, or the session that first writes one.

// writeDigestKaizenSkill writes a digest-bearing Kaizen skill at the REAL nested
// layout <root>/skills/kaizen/<name>/SKILL.md (what sync-derived-skills.py
// produces and //go:embed ships). The digest carries marker so the test can
// assert the exact injected text rather than "something was injected".
func writeDigestKaizenSkill(t *testing.T, root, name, tunedFor, stack, marker string) {
	t.Helper()
	dir := filepath.Join(root, "skills", "kaizen", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\n" +
		"description: tuned skill for tests.\n" +
		"tuned_for: " + tunedFor + "\n" +
		"stack: " + stack + "\n---\n\n" +
		"# " + name + "\n\n<!-- kaizen:digest -->\nDIGEST-" + marker + "\n<!-- /kaizen:digest -->\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hasDetected reports whether stackdetect would report the given stack for root.
func hasDetected(root, stack string) bool {
	for _, d := range stackdetect.Detect(root) {
		if strings.EqualFold(d, stack) {
			return true
		}
	}
	return false
}

// TestKaizenPDFIsModelGatedOnly is the hermetic regression guard for the
// create-a-PDF hole: a pdf-stack tuning skill must be admitted and its digest
// force-injected in a root that has NO pdf marker for the detector to find.
//
// The `stackdetect` precondition assertion is the load-bearing part. Without it
// this test could pass for the wrong reason — i.e. because a stray *.pdf happened
// to exist in root, which is the behaviour the marker gate already gave us.
func TestKaizenPDFIsModelGatedOnly(t *testing.T) {
	const model = "some-provider/space-bunny-free"
	const marker = "PDFONLY42"

	root := t.TempDir()
	writeDigestKaizenSkill(t, root, "pdf-tuning-space-bunny-free", "space-bunny-free", "pdf", marker)

	// Precondition: there is no *.pdf here, so admission cannot come from detection.
	if hasDetected(root, "pdf") {
		t.Fatalf("test root unexpectedly detects the pdf stack (%v); the assertion below would be vacuous",
			stackdetect.Detect(root))
	}

	// 1. Admitted on model match alone.
	var names []string
	for _, s := range KaizenSkillsForModel(root, model) {
		names = append(names, s.Name)
	}
	if !containsName(names, "pdf-tuning-space-bunny-free") {
		t.Fatalf("pdf tuning skill not admitted with no pdf marker present; got %v", names)
	}

	// 2. Its digest is force-injected into the base prompt — no tool call needed.
	block := KaizenDigestBlock(root, model)
	if !strings.Contains(block, "DIGEST-"+marker) {
		t.Fatalf("pdf digest not injected for a pdf-free root; got:\n%s", block)
	}
	if !strings.Contains(block, "Model-Specific Directives") {
		t.Fatalf("digest missing the directives header; got:\n%s", block)
	}

	// 3. Still model-gated: a non-matching model gets nothing.
	if got := KaizenDigestBlock(root, "anthropic/claude-opus-4-8"); strings.Contains(got, marker) {
		t.Fatalf("wrong model received the pdf digest:\n%s", got)
	}

	// 4. Widening must NOT have leaked into other stacks: a marker-gated stack in
	// the same pdf-free root is still refused (no fail-open regression).
	writeDigestKaizenSkill(t, root, "golang-tuning-space-bunny-free", "space-bunny-free", "golang", "GOLANGONLY")
	if got := KaizenDigestBlock(root, model); strings.Contains(got, "GOLANGONLY") {
		t.Fatalf("marker-gated golang skill leaked into a root with no detection:\n%s", got)
	}
}

// TestKaizenPDFShippedSkillIsAdmittedWithoutAPDFMarker pins the REAL shipped
// corpus, not a synthetic one: the pdf tuning skill for space-bunny-free must be
// admitted in a repository with no PDFs at all. This is the assertion that failed
// before the gate change — root contains zero *.pdf files, so the marker gate
// returned false and neither the catalogue line nor the digest was produced.
func TestKaizenPDFShippedSkillIsAdmittedWithoutAPDFMarker(t *testing.T) {
	const model = "opencode-go/space-bunny-free"
	root := repoRoot()

	if !hasDetected(root, "pdf") {
		t.Logf("NOTE: repo root detects no pdf stack — this test is exercising the ungated path")
	}

	block := KaizenDigestBlock(root, model)
	// A phrase unique to the pdf digest (not present in the conduct/hallucination ones).
	if !strings.Contains(block, "rasterising it") {
		t.Fatalf("shipped pdf digest not force-injected for %s; block was:\n%s", model, block)
	}

	// The catalogue must advertise it too, so the model can load the full body.
	if cat := BuildCatalogForModel(root, model); !strings.Contains(cat, "pdf-tuning-space-bunny-free") {
		t.Fatal("BuildCatalogForModel does not advertise the pdf tuning skill")
	}
	// The catalogue line must not tell the model to stand down when no PDF exists —
	// that prose was the model-visible half of the same bug.
	if cat := BuildCatalogForModel(root, model); strings.Contains(cat, "repository contains a PDF") {
		t.Fatalf("catalogue still carries the stale repo-marker gate prose:\n%s", cat)
	}
}

// TestPDFIsUniversalButIsNotGloballyWildcarded guards the shape of the allowlist:
// "pdf" is universal, and the lookup is a set membership test rather than a
// substring/prefix match that would accidentally admit sibling names.
func TestPDFIsUniversalButNotGloballyWildcarded(t *testing.T) {
	if !stackActive("pdf", nil) {
		t.Fatal("pdf must be a universal stack")
	}
	if !stackActive("PDF", nil) {
		t.Fatal("universal stack lookup must be case-insensitive, as EqualFold was")
	}
	if stackActive("pd", nil) || stackActive("pdfx", nil) || stackActive("pdf-toolkit", nil) {
		t.Fatal("universal lookup must be exact-set membership, not a prefix/substring match")
	}
	// Conduct/hallucination behaviour is preserved, and marker gating still applies
	// to everything else.
	if !stackActive("conduct", nil) || !stackActive("hallucination", nil) || !stackActive("", nil) {
		t.Fatal("pre-existing universal corpora regressed")
	}
	if stackActive("golang", nil) || stackActive("docx", nil) || stackActive("pptx", nil) {
		t.Fatal("marker-gated stacks must stay gated while no detection is present")
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
