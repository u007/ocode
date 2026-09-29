package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

// A repo that is only a folder of PDFs (no ocode checkout) must still get the
// embedded pdf tuning digest for a tuned model: the skill ships in the binary.
//
// CONTRACT CHANGE (pdf corpus is model-gated only, see universalStacks in
// loader.go): the pdf tuning digest is injected on an exact model-id match with
// NO stackdetect gate, because a *.pdf marker cannot detect a PDF that does not
// exist yet, one attached from outside the repo, or one deeper than the glob
// limit. So a PDF-free root is now SERVED, and the negative control that proves
// the digest is not unconditional is the MODEL gate, not the repo gate.
func TestKaizenDigestBlock_pdfRepoOutsideCheckout(t *testing.T) {
	// Stand in for main's EnsureExtracted: the extracted tree mirrors skills/.
	prev := bundled.SkillsDir
	bundled.SkillsDir = filepath.Join(repoRoot(), "skills")
	t.Cleanup(func() { bundled.SkillsDir = prev; InvalidateSkillCache() })
	InvalidateSkillCache()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "invoice.pdf"), []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	block := KaizenDigestBlock(root, "ollama-cloud/glm-5.3-flash")
	if !strings.Contains(block, "PDF_REDACT_LINE_ART_REMOVE_IF_COVERED") {
		t.Fatalf("pdf digest not injected for glm-5.3-flash in a PDF repo; block=%q", block)
	}
	// Negative control: a non-tuned model still gets nothing, so "universal for the
	// right model" stays distinguishable from "injected unconditionally".
	if got := KaizenDigestBlock(t.TempDir(), "anthropic/claude-opus-4-8"); strings.Contains(got, "apply_redactions") {
		t.Fatalf("pdf digest injected for a NON-tuned model: %q", got)
	}
	// The create-from-scratch case: no PDF on disk, tuned model, digest served.
	if got := KaizenDigestBlock(t.TempDir(), "ollama-cloud/glm-5.3-flash"); !strings.Contains(got, "apply_redactions") {
		t.Fatalf("pdf digest withheld from a PDF-free root for a tuned model: %q", got)
	}
}
