package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

// A repo that is only a folder of PDFs (no ocode checkout) gets the pdf tuning
// skill ADVERTISED for a tuned model (the skill ships in the binary), but its
// digest is NOT force-injected: pdf is a catalog-only stack (catalogOnlyStacks),
// so the model decides whether to load the full body.
func TestKaizenDigestBlock_pdfRepoOutsideCheckout(t *testing.T) {
	// Stand in for main's EnsureExtracted: the extracted tree mirrors skills/.
	prev := bundled.SkillsDir
	bundled.SkillsDir = filepath.Join(repoRoot(), "skills")
	t.Cleanup(func() { bundled.SkillsDir = prev; InvalidateSkillCache() })
	InvalidateSkillCache()

	const model = "ollama-cloud/glm-5.3-flash"
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "invoice.pdf"), []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if block := KaizenDigestBlock(root, model); strings.Contains(block, "PDF_REDACT_LINE_ART_REMOVE_IF_COVERED") {
		t.Fatalf("pdf digest force-injected for glm-5.3-flash in a PDF repo; block=%q", block)
	}
	if cat := BuildCatalogForModel(root, model); !strings.Contains(cat, "pdf-tuning-glm-5.3-flash") {
		t.Fatalf("pdf tuning skill not advertised in the catalog for glm-5.3-flash in a PDF repo")
	}
	// Positive control against the shipped skills: a universal, non-catalog-only
	// digest (conduct) is still force-injected for this model.
	if block := KaizenDigestBlock(root, model); !strings.Contains(block, "Three safety limits") {
		t.Fatalf("shipped conduct digest not force-injected for glm-5.3-flash; block=%q", block)
	}
	// Negative control: a non-tuned model is not advertised the pdf tuning skill.
	if cat := BuildCatalogForModel(t.TempDir(), "anthropic/claude-opus-4-8"); strings.Contains(cat, "pdf-tuning-") {
		t.Fatalf("pdf tuning skill advertised to a NON-tuned model")
	}
}
