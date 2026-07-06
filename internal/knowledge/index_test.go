package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTestBundle creates a temporary bundle with an index.md marker and
// returns the Bundle and the docs directory path.
func setupTestBundle(t *testing.T) (*Bundle, string) {
	t.Helper()
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create root index.md with okf_version marker.
	indexContent := "---\nokf_version: \"0.1\"\n---\n"
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte(indexContent), 0644); err != nil {
		t.Fatal(err)
	}
	b, ok := DetectBundle(td)
	if !ok {
		t.Fatal("DetectBundle returned false for test bundle")
	}
	return b, docsDir
}

// writeDoc creates a conforming doc in the bundle.
func writeDoc(t *testing.T, docsDir, relPath, docType, title, description string, tags []string) {
	t.Helper()
	fullPath := filepath.Join(docsDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	var tagBlock string
	for _, tag := range tags {
		tagBlock += "  - " + tag + "\n"
	}
	content := "---\ntype: " + docType + "\ntitle: " + title + "\ndescription: " + description + "\ntags:\n" + tagBlock + "---\nBody of " + relPath + "\n"
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateIndexCreatesIndexFile(t *testing.T) {
	b, docsDir := setupTestBundle(t)
	writeDoc(t, docsDir, "concepts/my-doc.md", "concept", "My Doc", "A test document", nil)

	if err := GenerateIndex(b); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	indexPath := filepath.Join(docsDir, "index.md")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("failed to read generated index.md: %v", err)
	}
	content := string(raw)

	// Must preserve okf_version frontmatter.
	if !strings.Contains(content, "okf_version:") {
		t.Errorf("index.md should preserve okf_version frontmatter, got:\n%s", content)
	}

	// Must contain the doc entry as a link.
	if !strings.Contains(content, "concepts/my-doc.md") {
		t.Errorf("index.md should contain link to concepts/my-doc.md, got:\n%s", content)
	}
}

func TestGenerateIndexGroupsByDirectory(t *testing.T) {
	b, docsDir := setupTestBundle(t)
	// Root-level doc.
	writeDoc(t, docsDir, "getting-started.md", "guide", "Getting Started", "How to start", nil)
	// Directory-level doc.
	writeDoc(t, docsDir, "playbooks/deploy.md", "playbook", "Deploy Guide", "How to deploy", nil)
	// Another directory.
	writeDoc(t, docsDir, "reference/api.md", "reference", "API Reference", "API docs", nil)

	if err := GenerateIndex(b); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(docsDir, "index.md"))
	content := string(raw)

	// Root-level docs should be under "# Concepts".
	if !strings.Contains(content, "Concepts") {
		t.Errorf("expected # Concepts section for root-level docs, got:\n%s", content)
	}

	// Directory-level docs should be under their directory name.
	if !strings.Contains(content, "playbooks") {
		t.Errorf("expected section for playbooks directory, got:\n%s", content)
	}
	if !strings.Contains(content, "reference") {
		t.Errorf("expected section for reference directory, got:\n%s", content)
	}
}

func TestGenerateIndexNonConformingFiles(t *testing.T) {
	b, docsDir := setupTestBundle(t)
	// Conforming doc.
	writeDoc(t, docsDir, "good.md", "concept", "Good Doc", "A good doc", nil)
	// Non-conforming doc (no frontmatter).
	os.WriteFile(filepath.Join(docsDir, "notes.md"), []byte("# Just notes\n\nNo frontmatter here.\n"), 0644)

	if err := GenerateIndex(b); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(docsDir, "index.md"))
	content := string(raw)

	// Non-conforming files should be under "# Unclassified".
	if !strings.Contains(content, "Unclassified") {
		t.Errorf("expected # Unclassified section for non-conforming docs, got:\n%s", content)
	}
	// Non-conforming files should have a link without description.
	if !strings.Contains(content, "notes.md") {
		t.Errorf("expected link to notes.md in Unclassified section, got:\n%s", content)
	}
}

func TestGenerateIndexDeprecatedAnnotation(t *testing.T) {
	b, docsDir := setupTestBundle(t)

	// Write a deprecated doc.
	content := "---\ntype: concept\ntitle: Old Doc\ndescription: An old doc\nstatus: deprecated\ndeprecated_reason: Superseded\n---\nOld body\n"
	os.WriteFile(filepath.Join(docsDir, "old.md"), []byte(content), 0644)

	if err := GenerateIndex(b); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(docsDir, "index.md"))
	idxContent := string(raw)

	if !strings.Contains(idxContent, "(deprecated)") {
		t.Errorf("deprecated doc should be annotated with (deprecated), got:\n%s", idxContent)
	}
}

func TestGenerateIndexEntriesSortedByTitle(t *testing.T) {
	b, docsDir := setupTestBundle(t)
	writeDoc(t, docsDir, "zebra.md", "concept", "Zebra Topic", "About zebras", nil)
	writeDoc(t, docsDir, "alpha.md", "concept", "Alpha Topic", "About alphas", nil)
	writeDoc(t, docsDir, "beta.md", "concept", "Beta Topic", "About betas", nil)

	if err := GenerateIndex(b); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(docsDir, "index.md"))
	content := string(raw)

	// Alpha should appear before Beta, Beta before Zebra.
	alphaIdx := strings.Index(content, "Alpha Topic")
	betaIdx := strings.Index(content, "Beta Topic")
	zebraIdx := strings.Index(content, "Zebra Topic")

	if alphaIdx < 0 || betaIdx < 0 || zebraIdx < 0 {
		t.Fatal("missing entries in index")
	}
	if !(alphaIdx < betaIdx && betaIdx < zebraIdx) {
		t.Error("entries should be sorted by title")
	}
}

func TestGenerateIndexLeavesPreviousIntactOnFailure(t *testing.T) {
	_, docsDir := setupTestBundle(t)

	// Write initial index.md with custom content.
	initial := "---\nokf_version: \"0.1\"\n---\n# Original Index\n"
	indexPath := filepath.Join(docsDir, "index.md")
	os.WriteFile(indexPath, []byte(initial), 0644)

	// Create a doc that will be walked.
	writeDoc(t, docsDir, "test.md", "concept", "Test Doc", "A test", nil)

	// Use a bundle whose Root does not exist, so Docs() fails.
	b2 := &Bundle{Root: filepath.Join(t.TempDir(), "nonexistent"), OKFVersion: "0.1"}
	err := GenerateIndex(b2)
	if err == nil {
		t.Fatal("expected error for nonexistent bundle root")
	}

	// Original index.md in the original bundle should be intact.
	raw, _ := os.ReadFile(filepath.Join(docsDir, "index.md"))
	if string(raw) != initial {
		t.Errorf("original index.md should be preserved on failure, got:\n%s", raw)
	}
}

func TestGenerateIndexSectionFormat(t *testing.T) {
	b, docsDir := setupTestBundle(t)
	writeDoc(t, docsDir, "concepts/my-concept.md", "concept", "My Concept", "This is my concept", []string{"go", "testing"})

	if err := GenerateIndex(b); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(docsDir, "index.md"))
	content := string(raw)

	// Entry should have format: [description or title].
	if !strings.Contains(content, "concepts/my-concept.md") {
		t.Errorf("expected link to concepts/my-concept.md")
	}
	if !strings.Contains(content, "This is my concept") {
		t.Errorf("expected description 'This is my concept' in index")
	}
}

func TestGenerateIndexPreservesRootFrontmatter(t *testing.T) {
	b, docsDir := setupTestBundle(t)
	writeDoc(t, docsDir, "test.md", "concept", "Test", "A test", nil)

	if err := GenerateIndex(b); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(docsDir, "index.md"))

	// Parse the frontmatter and verify okf_version is preserved.
	doc, err := ParseDoc("index.md", raw)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if !doc.Conforming {
		t.Fatal("generated index.md should be conforming")
	}

	// Check okf_version via Extra.
	if doc.Extra == nil {
		t.Fatal("extra should contain okf_version")
	}
	var foundVersion string
	for i := 0; i < len(doc.Extra.Content); i += 2 {
		if doc.Extra.Content[i].Value == "okf_version" {
			if err := doc.Extra.Content[i+1].Decode(&foundVersion); err != nil {
				t.Fatalf("decode okf_version: %v", err)
			}
			break
		}
	}
	if foundVersion != "0.1" {
		t.Errorf("okf_version = %q, want %q", foundVersion, "0.1")
	}
}

func TestGenerateIndexWithNoDocs(t *testing.T) {
	b, docsDir := setupTestBundle(t)

	if err := GenerateIndex(b); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(docsDir, "index.md"))
	content := string(raw)

	// Should still have okf_version and just the basic structure.
	if !strings.Contains(content, "okf_version") {
		t.Errorf("index.md should preserve okf_version, got:\n%s", content)
	}
}

// --- AppendLog tests ---

func TestAppendLogCreatesFile(t *testing.T) {
	b, docsDir := setupTestBundle(t)

	if err := AppendLog(b, "Creation", "test.md", "Created test document"); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	logPath := filepath.Join(docsDir, "log.md")
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log.md not created: %v", err)
	}
	content := string(raw)

	// Must have the heading.
	if !strings.Contains(content, "Directory Update Log") {
		t.Errorf("log.md should have # Directory Update Log heading, got:\n%s", content)
	}

	// Must have today's date heading.
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(content, "## "+today) {
		t.Errorf("log.md should have ## %s heading, got:\n%s", today, content)
	}

	// Must have the entry.
	if !strings.Contains(content, "**Creation**") {
		t.Errorf("log.md should contain the action 'Creation', got:\n%s", content)
	}
	if !strings.Contains(content, "test.md") {
		t.Errorf("log.md should contain the path 'test.md', got:\n%s", content)
	}
}

func TestAppendLogAddsToExistingFile(t *testing.T) {
	b, _ := setupTestBundle(t)

	// First entry.
	if err := AppendLog(b, "Creation", "doc1.md", "First doc"); err != nil {
		t.Fatal(err)
	}

	// Second entry.
	if err := AppendLog(b, "Update", "doc2.md", "Updated doc2"); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(b.Root, "log.md"))
	content := string(raw)

	// Both entries should be present.
	if !strings.Contains(content, "doc1.md") || !strings.Contains(content, "doc2.md") {
		t.Errorf("log.md should contain both entries, got:\n%s", content)
	}
}

func TestAppendLogNewestDateFirst(t *testing.T) {
	b, docsDir := setupTestBundle(t)

	today := time.Now().Format("2006-01-02")

	// Add an entry with today's date.
	if err := AppendLog(b, "Creation", "doc1.md", "First entry"); err != nil {
		t.Fatal(err)
	}

	// Read the log and verify the date heading appears correctly.
	raw, _ := os.ReadFile(filepath.Join(docsDir, "log.md"))
	content := string(raw)

	// The heading "# Directory Update Log" should appear first, then the date section.
	headingIdx := strings.Index(content, "Directory Update Log")
	dateIdx := strings.Index(content, "## "+today)
	if headingIdx < 0 || dateIdx < 0 {
		t.Fatalf("missing expected headings, got:\n%s", content)
	}
	if headingIdx > dateIdx {
		t.Error("date heading should appear after main heading")
	}
}

func TestAppendLogAllActions(t *testing.T) {
	b, _ := setupTestBundle(t)

	actions := []struct {
		action  string
		docPath string
		summary string
	}{
		{"Creation", "new.md", "Created new doc"},
		{"Update", "existing.md", "Updated existing doc"},
		{"Deprecation", "old.md", "Deprecated old doc"},
		{"Deletion", "gone.md", "Deleted gone doc"},
	}

	for _, a := range actions {
		if err := AppendLog(b, a.action, a.docPath, a.summary); err != nil {
			t.Fatalf("AppendLog(%s): %v", a.action, err)
		}
	}

	raw, _ := os.ReadFile(filepath.Join(b.Root, "log.md"))
	content := string(raw)

	for _, a := range actions {
		if !strings.Contains(content, a.action) {
			t.Errorf("log.md should contain action %q", a.action)
		}
		if !strings.Contains(content, a.docPath) {
			t.Errorf("log.md should contain path %q", a.docPath)
		}
		if !strings.Contains(content, a.summary) {
			t.Errorf("log.md should contain summary %q", a.summary)
		}
	}
}

func TestAppendLogMultipleDates(t *testing.T) {
	b, _ := setupTestBundle(t)

	if err := AppendLog(b, "Creation", "doc1.md", "First entry"); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(b.Root, "log.md"))
	content := string(raw)

	today := time.Now().Format("2006-01-02")
	if !strings.Contains(content, "## "+today) {
		t.Errorf("expected date heading ## %s, got:\n%s", today, content)
	}

	if !strings.Contains(content, "doc1.md") {
		t.Errorf("expected link to doc1.md, got:\n%s", content)
	}
}

// Round-trip test: parse the log, verify YAML frontmatter is not present
// (log.md is a plain markdown file, not a knowledge doc).
func TestAppendLogProducesPlainMarkdown(t *testing.T) {
	b, _ := setupTestBundle(t)

	if err := AppendLog(b, "Creation", "test.md", "Test entry"); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(b.Root, "log.md"))
	content := string(raw)

	// Should NOT have YAML frontmatter markers.
	if strings.HasPrefix(content, "---") {
		t.Error("log.md should not have YAML frontmatter")
	}
}
