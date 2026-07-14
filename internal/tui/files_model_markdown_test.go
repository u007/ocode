package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFilesPreviewRendersMarkdownTable verifies that a markdown file preview
// renders GitHub-style tables as real tables (box-drawing separators) instead
// of raw "|" pipes, and that previewRawLines stays line-aligned with the
// rendered previewLines so selection, in-file search, and mouse hit-testing
// remain correct.
func TestFilesPreviewRendersMarkdownTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	content := "# Title\n\n| Name | Age |\n| --- | --- |\n| Alice | 30 |\n| Bob | 25 |\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	msg, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg)
	if !ok {
		t.Fatal("expected filesPreviewMsg from preview load")
	}
	m.applyPreview(msg)

	// previewRawLines must mirror the rendered lines so selection/search align.
	if len(m.previewRawLines) != len(m.previewLines) {
		t.Fatalf("previewRawLines/previewLines length mismatch: %d vs %d", len(m.previewRawLines), len(m.previewLines))
	}

	joined := strings.Join(m.previewLines, "\n")
	// Rendered table uses box-drawing separators, not raw markdown pipes.
	if !strings.Contains(joined, "\u2502") {
		t.Fatalf("expected rendered markdown table with box-drawing char, got:\n%s", joined)
	}
	if !strings.Contains(joined, "\u2500") {
		t.Fatalf("expected rendered markdown table separator, got:\n%s", joined)
	}
	// Header cell content must survive rendering.
	if !strings.Contains(joined, "Alice") || !strings.Contains(joined, "Bob") {
		t.Fatalf("expected table cell content in rendered preview, got:\n%s", joined)
	}
	// previewRawLines is the ANSI-stripped rendered text, not raw markdown.
	if strings.Contains(strings.Join(m.previewRawLines, "\n"), "| Name |") {
		t.Fatalf("previewRawLines should hold rendered text, not raw markdown; got:\n%s", strings.Join(m.previewRawLines, "\n"))
	}
	// Heading should be rendered without the leading "#".
	if strings.Contains(strings.Join(m.previewRawLines, "\n"), "# Title") {
		t.Fatalf("expected heading rendered without '#', got:\n%s", strings.Join(m.previewRawLines, "\n"))
	}

	// In-file search must find text in the rendered (visible) preview.
	m.inFileSearchQuery = "Alice"
	matches := m.performInFileSearch(m.inFileSearchQuery)
	if len(matches) == 0 {
		t.Fatalf("expected in-file search to find 'Alice' in rendered markdown preview")
	}
}
