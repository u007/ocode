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
	if got := KaizenDigestBlock(t.TempDir(), "ollama-cloud/glm-5.3-flash"); strings.Contains(got, "apply_redactions") {
		t.Fatalf("pdf digest injected in a repo without PDFs: %q", got)
	}
}
