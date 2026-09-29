package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

// Word and PowerPoint repos (including legacy .doc/.ppt) get the embedded
// docx/pptx tuning digests for a tuned model, and only for the matching stack.
func TestKaizenDigestBlock_officeRepos(t *testing.T) {
	prev := bundled.SkillsDir
	bundled.SkillsDir = filepath.Join(repoRoot(), "skills")
	t.Cleanup(func() { bundled.SkillsDir = prev; InvalidateSkillCache() })
	InvalidateSkillCache()

	const (
		docxMimo = "Word edits (python-docx): cached fields"
		pptxGLM  = "PowerPoint edits (python-pptx): find by content, sync the frame"
		pdfGLM   = "PDF edits (PyMuPDF): redaction defaults"
	)
	cases := []struct {
		name    string
		files   []string
		model   string
		want    []string
		notWant []string
	}{
		{"legacy doc → docx digest", []string{"old/report.doc"}, "opencode-go/mimo-v2.6-flash", []string{docxMimo}, nil},
		// pdfGLM is now WANTED, not forbidden: the pdf corpus is model-gated only and
		// glm-5.3-flash is pdf-tuned, so a docx repo legitimately receives the pdf
		// digest. This case guards cross-stack isolation (no pptx), as its name says —
		// pptx is still marker-gated, so a docx-only repo must not get it.
		{"docx repo gets no pptx digest", []string{"invoice.docx"}, "ollama-cloud/glm-5.3-flash", []string{pdfGLM}, []string{pptxGLM}},
		{"pptx + pdf repo → both digests", []string{"decks/q3.pptx", "invoice.pdf"}, "ollama-cloud/glm-5.3-flash", []string{pptxGLM, pdfGLM}, nil},
		{"untuned model gets none", []string{"deck.ppt"}, "opencode-go/some-other-model", nil, []string{"python-pptx"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, f := range tc.files {
				p := filepath.Join(root, f)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			block := KaizenDigestBlock(root, tc.model)
			for _, w := range tc.want {
				if !strings.Contains(block, w) {
					t.Errorf("missing %q in digest block %q", w, block)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(block, w) {
					t.Errorf("unexpected %q in digest block %q", w, block)
				}
			}
		})
	}
}
