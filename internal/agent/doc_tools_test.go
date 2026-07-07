package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/knowledge"
)

// newTestDocSearchStore builds a minimal OKF bundle with one doc whose body
// contains a unique token, and returns a Store over it.
func newTestDocSearchStore(t *testing.T) *knowledge.Store {
	t.Helper()
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	index := "---\nokf_version: \"0.1\"\n---\n"
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte(index), 0644); err != nil {
		t.Fatal(err)
	}
	docBody := "zebrafish is a unique token used only in this document body for testing get_top."
	docContent := "---\ntype: concept\ntitle: Example Doc\ndescription: an example\n---\n" + docBody
	if err := os.MkdirAll(filepath.Join(docsDir, "guide"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "guide", "example.md"), []byte(docContent), 0644); err != nil {
		t.Fatal(err)
	}
	b, ok := knowledge.DetectBundle(td)
	if !ok {
		t.Fatal("DetectBundle returned false for test bundle")
	}
	return knowledge.NewStore(b)
}

func TestDocSearchGetTopDefaultMetadataOnly(t *testing.T) {
	store := newTestDocSearchStore(t)
	tool := &DocSearchTool{store: store}

	out, err := tool.Execute(json.RawMessage(`{"query":"zebrafish","get_top":0}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "guide/example.md") {
		t.Fatalf("expected doc path in output, got:\n%s", out)
	}
	// Body token must NOT appear when get_top is 0 (backward-compatible).
	if strings.Contains(out, "zebrafish") {
		t.Fatalf("body leaked into metadata-only output:\n%s", out)
	}
}

func TestDocSearchGetTopInlinesBody(t *testing.T) {
	store := newTestDocSearchStore(t)
	tool := &DocSearchTool{store: store}

	out, err := tool.Execute(json.RawMessage(`{"query":"zebrafish","get_top":1}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "zebrafish") {
		t.Fatalf("expected inline body content, got:\n%s", out)
	}
	if !strings.Contains(out, "Full content of top 1") {
		t.Fatalf("expected inline-content header, got:\n%s", out)
	}
}

func TestDocSearchGetTopBounded(t *testing.T) {
	store := newTestDocSearchStore(t)
	tool := &DocSearchTool{store: store}

	// Request a huge top; must be capped at docSearchMaxTop (5) without panic.
	out, err := tool.Execute(json.RawMessage(`{"query":"zebrafish","get_top":999}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "Full content of top 1") {
		// Only one doc exists, so header reports top 1 even though capped at 5.
		t.Fatalf("expected bounded top header, got:\n%s", out)
	}
}
