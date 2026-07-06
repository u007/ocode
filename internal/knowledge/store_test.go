package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupStore creates a test bundle and returns a Store.
func setupStore(t *testing.T) (*Store, *Bundle, string) {
	t.Helper()
	b, docsDir := setupTestBundle(t)
	s := NewStore(b)
	return s, b, docsDir
}

func TestNewStore(t *testing.T) {
	b, _ := setupTestBundle(t)
	s := NewStore(b)
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
}

func TestStoreGetReturnsDoc(t *testing.T) {
	_, b, docsDir := setupStore(t)
	writeDoc(t, docsDir, "test.md", "concept", "Test Doc", "A test document", []string{"go"})

	s := NewStore(b)
	doc, err := s.Get("test.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doc == nil {
		t.Fatal("Get returned nil doc")
	}
	if doc.Title != "Test Doc" {
		t.Errorf("Title = %q, want %q", doc.Title, "Test Doc")
	}
	if doc.Type != "concept" {
		t.Errorf("Type = %q, want %q", doc.Type, "concept")
	}
}

func TestStoreGetMissingDoc(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)
	_, err := s.Get("nonexistent.md")
	if err == nil {
		t.Fatal("expected error for missing doc")
	}
}

func TestStoreGetNestedDoc(t *testing.T) {
	_, b, docsDir := setupStore(t)
	writeDoc(t, docsDir, "playbooks/deploy.md", "playbook", "Deploy Guide", "How to deploy", nil)

	s := NewStore(b)
	doc, err := s.Get("playbooks/deploy.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doc == nil || doc.Title != "Deploy Guide" {
		t.Errorf("got title %q, want %q", doc.Title, "Deploy Guide")
	}
}

func TestStoreSearchByTitle(t *testing.T) {
	_, b, docsDir := setupStore(t)
	writeDoc(t, docsDir, "doc1.md", "concept", "Alpha Protocol", "About alpha", nil)
	writeDoc(t, docsDir, "doc2.md", "concept", "Beta System", "About beta", nil)

	s := NewStore(b)
	results, total, err := s.Search("alpha", nil, "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Title != "Alpha Protocol" {
		t.Errorf("title = %q, want %q", results[0].Title, "Alpha Protocol")
	}
}

func TestStoreSearchByDescription(t *testing.T) {
	_, b, docsDir := setupStore(t)
	writeDoc(t, docsDir, "doc1.md", "concept", "Something", "This describes beta features", nil)
	writeDoc(t, docsDir, "doc2.md", "concept", "Other", "No match here", nil)

	s := NewStore(b)
	results, total, err := s.Search("beta", nil, "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestStoreSearchByBody(t *testing.T) {
	_, b, docsDir := setupStore(t)
	// Write a doc with body containing the search term.
	content := "---\ntype: concept\ntitle: Background\ndescription: No mention\n---\nThis document discusses zebra patterns in detail.\n"
	os.WriteFile(filepath.Join(docsDir, "zebra.md"), []byte(content), 0644)

	writeDoc(t, docsDir, "other.md", "concept", "Other", "Nothing here", nil)

	s := NewStore(b)
	results, total, err := s.Search("zebra", nil, "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestStoreSearchRelevanceRanking(t *testing.T) {
	_, b, docsDir := setupStore(t)
	// body match only
	bodyOnly := "---\ntype: concept\ntitle: Irrelevant\ndescription: No mention\n---\nThe query term appears only in the body.\n"
	os.WriteFile(filepath.Join(docsDir, "body.md"), []byte(bodyOnly), 0644)
	// description match
	descOnly := "---\ntype: concept\ntitle: Not Matching\ndescription: This mentions the query term in description\n---\nBody\n"
	os.WriteFile(filepath.Join(docsDir, "desc.md"), []byte(descOnly), 0644)
	// title match
	titleOnly := "---\ntype: concept\ntitle: Query Term Is Here\ndescription: Just desc\n---\nBody\n"
	os.WriteFile(filepath.Join(docsDir, "title.md"), []byte(titleOnly), 0644)

	s := NewStore(b)
	results, total, err := s.Search("query term", nil, "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	// First result should be the title match.
	if !strings.Contains(results[0].Title, "Query Term") {
		t.Errorf("expected title match first, got %q", results[0].Title)
	}
}

func TestStoreSearchWithTagsFilter(t *testing.T) {
	_, b, docsDir := setupStore(t)
	writeDoc(t, docsDir, "doc1.md", "concept", "Go Doc", "About Go", []string{"go", "programming"})
	writeDoc(t, docsDir, "doc2.md", "concept", "Rust Doc", "About Rust", []string{"rust"})

	s := NewStore(b)
	results, total, err := s.Search("", []string{"go"}, "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Title != "Go Doc" {
		t.Errorf("title = %q, want %q", results[0].Title, "Go Doc")
	}
}

func TestStoreSearchWithDocTypeFilter(t *testing.T) {
	_, b, docsDir := setupStore(t)
	writeDoc(t, docsDir, "concept1.md", "concept", "Concept One", "A concept", nil)
	writeDoc(t, docsDir, "guide1.md", "guide", "Guide One", "A guide", nil)

	s := NewStore(b)
	results, total, err := s.Search("", nil, "guide", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Type != "guide" {
		t.Errorf("type = %q, want %q", results[0].Type, "guide")
	}
}

func TestStoreSearchPagination(t *testing.T) {
	_, b, docsDir := setupStore(t)
	for i := 0; i < 5; i++ {
		title := string(rune('A'+i)) + " Doc"
		writeDoc(t, docsDir, "doc"+string(rune('a'+i))+".md", "concept", title, "Description", nil)
	}

	s := NewStore(b)
	// Page 1: first 2 results
	results, total, err := s.Search("Doc", nil, "", 0, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(results) != 2 {
		t.Fatalf("page 1: got %d results, want 2", len(results))
	}

	// Page 2: next 2 results
	results, total, err = s.Search("Doc", nil, "", 1, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(results) != 2 {
		t.Fatalf("page 2: got %d results, want 2", len(results))
	}
}

func TestStoreSearchCaseInsensitive(t *testing.T) {
	_, b, docsDir := setupStore(t)
	writeDoc(t, docsDir, "doc.md", "concept", "UpperCase", "UPPERCASE DESCRIPTION", nil)

	s := NewStore(b)
	results, total, err := s.Search("uppercase", nil, "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1 (case-insensitive)", total)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestStoreSearchEmptyQuery(t *testing.T) {
	_, b, docsDir := setupStore(t)
	writeDoc(t, docsDir, "doc1.md", "concept", "Doc One", "First doc", nil)
	writeDoc(t, docsDir, "doc2.md", "guide", "Doc Two", "Second doc", nil)

	s := NewStore(b)
	// Empty query with no filters should return all docs.
	results, total, err := s.Search("", nil, "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

func TestStoreWriteCreatesDoc(t *testing.T) {
	_, b, docsDir := setupStore(t)
	s := NewStore(b)

	err := s.Write("new-doc.md", "concept", "New Doc", "A brand new doc", "https://example.com", []string{"new"}, "Body content here.")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify file was created.
	raw, err := os.ReadFile(filepath.Join(docsDir, "new-doc.md"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Verify it parses correctly.
	doc, err := ParseDoc("new-doc.md", raw)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if !doc.Conforming {
		t.Fatal("written doc should be conforming")
	}
	if doc.Type != "concept" {
		t.Errorf("Type = %q, want %q", doc.Type, "concept")
	}
	if doc.Title != "New Doc" {
		t.Errorf("Title = %q, want %q", doc.Title, "New Doc")
	}
	if doc.Description != "A brand new doc" {
		t.Errorf("Description = %q, want %q", doc.Description, "A brand new doc")
	}
	if doc.Body != "Body content here." {
		t.Errorf("Body = %q, want %q", doc.Body, "Body content here.")
	}
	if len(doc.Tags) != 1 || doc.Tags[0] != "new" {
		t.Errorf("Tags = %v, want [new]", doc.Tags)
	}
	if doc.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestStoreWriteCreatesIntermediateDirs(t *testing.T) {
	_, b, docsDir := setupStore(t)
	s := NewStore(b)

	err := s.Write("deep/nested/path/doc.md", "concept", "Nested", "Nested doc", "", nil, "Body")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := os.Stat(filepath.Join(docsDir, "deep/nested/path/doc.md")); os.IsNotExist(err) {
		t.Fatal("intermediate directories were not created")
	}
}

func TestStoreWriteUpdatesExistingDoc(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	// Create initial doc.
	err := s.Write("update.md", "concept", "Original Title", "Original description", "", nil, "Original body.")
	if err != nil {
		t.Fatalf("Write (create): %v", err)
	}

	// Update the doc.
	err = s.Write("update.md", "concept", "Updated Title", "Updated description", "https://example.com", []string{"updated"}, "Updated body.")
	if err != nil {
		t.Fatalf("Write (update): %v", err)
	}

	// Read back and verify.
	doc, err := s.Get("update.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doc.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", doc.Title, "Updated Title")
	}
	if doc.Description != "Updated description" {
		t.Errorf("Description = %q, want %q", doc.Description, "Updated description")
	}
	// The original had no resource, so updated should have it.
	if doc.Resource != "https://example.com" {
		t.Errorf("Resource = %q, want %q", doc.Resource, "https://example.com")
	}
}

func TestStoreWritePreservesUnknownKeys(t *testing.T) {
	_, b, docsDir := setupStore(t)
	s := NewStore(b)

	// Create doc with unknown keys.
	content := "---\ntype: concept\ntitle: Original\ndescription: Original\ncustom_field: keep-me\nanother_unknown:\n  nested: true\nversion: 2\n---\nBody\n"
	os.WriteFile(filepath.Join(docsDir, "preserve.md"), []byte(content), 0644)

	// Update through store (should preserve unknown keys via merge).
	err := s.Write("preserve.md", "concept", "Updated", "Updated desc", "", nil, "New body")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	doc, err := s.Get("preserve.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !doc.Conforming {
		t.Fatal("doc should be conforming")
	}

	// Verify unknown keys survived.
	if doc.Extra == nil {
		t.Fatal("Extra should not be nil, unknown keys should be preserved")
	}
	unknownKeys := make(map[string]bool)
	for i := 0; i < len(doc.Extra.Content); i += 2 {
		unknownKeys[doc.Extra.Content[i].Value] = true
	}
	for _, k := range []string{"custom_field", "another_unknown", "version"} {
		if !unknownKeys[k] {
			t.Errorf("unknown key %q was not preserved", k)
		}
	}
}

func TestStoreWriteRejectsEmptyType(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	err := s.Write("no-type.md", "", "No Type", "Missing type", "", nil, "Body")
	if err == nil {
		t.Fatal("expected error for empty type")
	}
}

func TestStoreWriteRejectsReservedPathIndex(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	err := s.Write("index.md", "concept", "Index", "", "", nil, "Body")
	if err == nil {
		t.Fatal("expected error for writing to index.md")
	}
}

func TestStoreWriteRejectsReservedPathLog(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	err := s.Write("log.md", "concept", "Log", "", "", nil, "Body")
	if err == nil {
		t.Fatal("expected error for writing to log.md")
	}
}

func TestStoreWriteRejectsPathTraversal(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	err := s.Write("../outside.md", "concept", "Outside", "", "", nil, "Body")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestStoreWriteCreatesLogEntry(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	err := s.Write("logged.md", "concept", "Logged Doc", "A logged doc", "", nil, "Body")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify log entry exists.
	logPath := filepath.Join(b.Root, "log.md")
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log.md not found: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "**Creation**") || !strings.Contains(content, "logged.md") {
		t.Errorf("log.md should contain Creation entry for logged.md, got:\n%s", content)
	}
}

func TestStoreWriteUpdatesIndex(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	err := s.Write("indexed.md", "concept", "Indexed Doc", "An indexed doc", "", nil, "Body")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(b.Root, "index.md"))
	if err != nil {
		t.Fatalf("index.md not found: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "Indexed Doc") {
		t.Errorf("index.md should contain 'Indexed Doc', got:\n%s", content)
	}
}

func TestStoreDeprecateSetsStatus(t *testing.T) {
	_, b, docsDir := setupStore(t)
	s := NewStore(b)

	writeDoc(t, docsDir, "old.md", "concept", "Old Doc", "An old doc", nil)

	err := s.Deprecate("old.md", "Superseded by new doc")
	if err != nil {
		t.Fatalf("Deprecate: %v", err)
	}

	doc, err := s.Get("old.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doc.Status != "deprecated" {
		t.Errorf("Status = %q, want %q", doc.Status, "deprecated")
	}
	if doc.DeprecatedReason != "Superseded by new doc" {
		t.Errorf("DeprecatedReason = %q, want %q", doc.DeprecatedReason, "Superseded by new doc")
	}
}

func TestStoreDeprecateCreatesLogEntry(t *testing.T) {
	_, b, docsDir := setupStore(t)
	s := NewStore(b)

	writeDoc(t, docsDir, "dep.md", "concept", "Dep Doc", "A dep doc", nil)

	err := s.Deprecate("dep.md", "No longer relevant")
	if err != nil {
		t.Fatalf("Deprecate: %v", err)
	}

	logPath := filepath.Join(b.Root, "log.md")
	raw, _ := os.ReadFile(logPath)
	content := string(raw)
	if !strings.Contains(content, "**Deprecation**") || !strings.Contains(content, "dep.md") {
		t.Errorf("log.md should contain Deprecation entry, got:\n%s", content)
	}
}

func TestStoreDeprecateUpdatesIndex(t *testing.T) {
	_, b, docsDir := setupStore(t)
	s := NewStore(b)

	writeDoc(t, docsDir, "deptest.md", "concept", "Dep Test", "A test for deprecation", nil)

	err := s.Deprecate("deptest.md", "Obsolete")
	if err != nil {
		t.Fatalf("Deprecate: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(b.Root, "index.md"))
	content := string(raw)
	if !strings.Contains(content, "(deprecated)") {
		t.Errorf("index.md should annotate deprecated doc, got:\n%s", content)
	}
}

func TestStoreWriteUpdatesTimestamp(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	// Create doc, then immediately update.
	err := s.Write("ts.md", "concept", "TS Doc", "Testing timestamp", "", nil, "Body")
	if err != nil {
		t.Fatalf("Write (create): %v", err)
	}

	// Give the clock a tiny tick.
	time.Sleep(time.Millisecond)

	err = s.Write("ts.md", "concept", "TS Doc Updated", "Updated", "", nil, "New body")
	if err != nil {
		t.Fatalf("Write (update): %v", err)
	}

	doc, err := s.Get("ts.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doc.Title != "TS Doc Updated" {
		t.Errorf("Title = %q, want %q", doc.Title, "TS Doc Updated")
	}
	if doc.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestStoreDeprecateErrorForMissingDoc(t *testing.T) {
	_, b, _ := setupStore(t)
	s := NewStore(b)

	err := s.Deprecate("missing.md", "Reason")
	if err == nil {
		t.Fatal("expected error for deprecating missing doc")
	}
}
