package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectBundleMissingDocsDir(t *testing.T) {
	td := t.TempDir()
	b, ok := DetectBundle(td)
	if ok {
		t.Fatal("DetectBundle should return false when docs/ directory is missing")
	}
	if b != nil {
		t.Fatalf("DetectBundle should return nil bundle, got %+v", b)
	}
}

func TestDetectBundleNoMarkerInIndex(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.Mkdir(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// index.md without okf_version marker
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# Docs\n\nJust a regular index.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b, ok := DetectBundle(td)
	if ok {
		t.Fatal("DetectBundle should return false when index.md lacks okf_version")
	}
	if b != nil {
		t.Fatalf("DetectBundle should return nil bundle, got %+v", b)
	}
}

func TestDetectBundleWithMarker(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.Mkdir(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	indexContent := `---
okf_version: "0.1"
---
# Knowledge Bundle
`
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte(indexContent), 0644); err != nil {
		t.Fatal(err)
	}
	b, ok := DetectBundle(td)
	if !ok {
		t.Fatal("DetectBundle should return true when docs/index.md has okf_version")
	}
	if b == nil {
		t.Fatal("DetectBundle should return a Bundle")
	}
	if b.OKFVersion != "0.1" {
		t.Errorf("OKFVersion = %q, want %q", b.OKFVersion, "0.1")
	}
	// Root should be the absolute path of docs/
	if b.Root != docsDir {
		t.Errorf("Root = %q, want %q", b.Root, docsDir)
	}
}

func TestDetectBundleEmptyOKFVersion(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.Mkdir(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// okf_version present but empty
	indexContent := `---
okf_version: ""
---
`
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte(indexContent), 0644); err != nil {
		t.Fatal(err)
	}
	b, ok := DetectBundle(td)
	if ok {
		t.Fatal("DetectBundle should return false when okf_version is empty")
	}
	if b != nil {
		t.Fatal("DetectBundle should return nil when okf_version is empty")
	}
}

func TestDocsSkipsReservedFilesAndReturnsSorted(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	subDir := filepath.Join(docsDir, "playbooks")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create index.md (reserved — should be skipped)
	os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("---\nokf_version: \"0.1\"\n---\n# Index\n"), 0644)
	// Create log.md (reserved — should be skipped)
	os.WriteFile(filepath.Join(docsDir, "log.md"), []byte("---\n---\n# Log\n"), 0644)
	// Create a hidden file (should be skipped)
	os.WriteFile(filepath.Join(docsDir, ".hidden.md"), []byte("# hidden\n"), 0644)

	// Create concept docs (not reserved — should be included)
	os.WriteFile(filepath.Join(docsDir, "alpha.md"), []byte("---\ntype: concept\ntitle: Alpha\n---\nAlpha body.\n"), 0644)
	os.WriteFile(filepath.Join(docsDir, "beta.md"), []byte("---\ntype: guide\ntitle: Beta\n---\nBeta body.\n"), 0644)
	os.WriteFile(filepath.Join(subDir, "gamma.md"), []byte("---\ntype: playbook\ntitle: Gamma\n---\nGamma body.\n"), 0644)

	b := &Bundle{
		Root:       docsDir,
		OKFVersion: "0.1",
	}
	docs, err := b.Docs()
	if err != nil {
		t.Fatalf("Docs(): %v", err)
	}

	// Should skip index.md, log.md, .hidden.md
	// Should include alpha.md, beta.md, playbooks/gamma.md — sorted by path
	if len(docs) != 3 {
		t.Fatalf("Docs() returned %d docs, want 3: %+v", len(docs), docs)
	}

	// Check sorted order: alpha.md < beta.md < playbooks/gamma.md (by path)
	want := []string{"alpha.md", "beta.md", "playbooks/gamma.md"}
	for i, w := range want {
		if docs[i].Path != w {
			t.Errorf("docs[%d].Path = %q, want %q", i, docs[i].Path, w)
		}
	}
}

func TestDocsFailsForMissingRoot(t *testing.T) {
	b := &Bundle{
		Root:       "/nonexistent/path/docs",
		OKFVersion: "0.1",
	}
	_, err := b.Docs()
	if err == nil {
		t.Fatal("Docs() should return an error for nonexistent root")
	}
}

func TestDetectBundleIndexWithoutFrontmatter(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.Mkdir(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// index.md with no frontmatter at all
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# Just a heading\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b, ok := DetectBundle(td)
	if ok {
		t.Fatal("DetectBundle should return false for frontmatter-less index.md")
	}
	if b != nil {
		t.Fatal("DetectBundle should return nil for frontmatter-less index.md")
	}
}

func TestInitBundleCreatesMarkerAndLog(t *testing.T) {
	td := t.TempDir()
	if err := InitBundle(td); err != nil {
		t.Fatalf("InitBundle failed: %v", err)
	}
	// Verify DetectBundle now succeeds.
	bundle, ok := DetectBundle(td)
	if !ok {
		t.Fatal("InitBundle: DetectBundle should succeed after init")
	}
	if bundle.OKFVersion != "0.1" {
		t.Fatalf("InitBundle: OKFVersion = %q, want %q", bundle.OKFVersion, "0.1")
	}
	// Verify log.md exists.
	logPath := filepath.Join(td, "docs", "log.md")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("InitBundle: log.md missing: %v", err)
	}
	// Verify index.md content.
	indexPath := filepath.Join(td, "docs", "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("InitBundle: index.md not readable: %v", err)
	}
	if !strings.Contains(string(data), `okf_version: "0.1"`) {
		t.Fatal("InitBundle: index.md missing okf_version marker")
	}
}

func TestInitBundleRefusesDoubleInit(t *testing.T) {
	td := t.TempDir()
	if err := InitBundle(td); err != nil {
		t.Fatalf("first InitBundle failed: %v", err)
	}
	if err := InitBundle(td); err == nil {
		t.Fatal("InitBundle should error when bundle already exists")
	}
}

// TestInitBundleRefusesNonOKFOverwrite verifies that InitBundle does not
// destroy pre-existing non-OKF files (C3). A mkdocs-style index.md without
// okf_version must be preserved.
func TestInitBundleRefusesNonOKFOverwrite(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a non-OKF index.md (mkdocs-style).
	mkdocsIndex := []byte("# My Project\n\nWelcome to the docs.\n")
	indexPath := filepath.Join(docsDir, "index.md")
	if err := os.WriteFile(indexPath, mkdocsIndex, 0644); err != nil {
		t.Fatal(err)
	}

	// InitBundle must refuse to overwrite.
	if err := InitBundle(td); err == nil {
		t.Fatal("InitBundle should refuse to overwrite existing non-OKF index.md")
	}

	// Verify the original content is preserved.
	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(mkdocsIndex) {
		t.Fatalf("InitBundle modified the pre-existing index.md: got %q, want %q", string(after), string(mkdocsIndex))
	}
}

// TestInitBundleRefusesNonOKFLogOverwrite verifies that a pre-existing
// non-OKF log.md is also protected.
func TestInitBundleRefusesNonOKFLogOverwrite(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a non-OKF log.md.
	nonOKFLog := []byte("# Build Log\n\n- Step 1: done\n")
	logPath := filepath.Join(docsDir, "log.md")
	if err := os.WriteFile(logPath, nonOKFLog, 0644); err != nil {
		t.Fatal(err)
	}

	if err := InitBundle(td); err == nil {
		t.Fatal("InitBundle should refuse to overwrite existing non-OKF log.md")
	}

	// Verify log.md is preserved.
	after, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(nonOKFLog) {
		t.Fatalf("InitBundle modified pre-existing log.md: got %q, want %q", string(after), string(nonOKFLog))
	}
}

func TestInitBundleCreatesDocsDir(t *testing.T) {
	td := t.TempDir()
	// Remove docs/ if it was auto-created by TempDir? No, TempDir is empty.
	if err := InitBundle(td); err != nil {
		t.Fatalf("InitBundle failed: %v", err)
	}
	docsDir := filepath.Join(td, "docs")
	if _, err := os.Stat(docsDir); err != nil {
		t.Fatalf("InitBundle: docs/ not created: %v", err)
	}
}

func TestDefaultExcludedPatternsSkipPlanFiles(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create regular docs (should be included).
	writeDoc(t, docsDir, "intro.md", "concept", "Intro", "Introduction", nil)
	writeDoc(t, docsDir, "guide.md", "guide", "Guide", "User guide", nil)

	// Create PLAN-*.md files (should be excluded by default).
	writeDoc(t, docsDir, "PLAN-feature-x.md", "concept", "Plan", "A feature plan", nil)
	writeDoc(t, docsDir, "PLAN-refactor.md", "concept", "Refactor", "A refactoring plan", nil)

	// Create *.OCODE.md files (should be excluded by default).
	writeDoc(t, docsDir, "custom-model.OCODE.md", "concept", "Custom Model", "Model config", nil)
	writeDoc(t, docsDir, "agent-behavior.OCODE.md", "concept", "Agent", "Agent config", nil)

	// Create PLAN.md (no hyphen — should NOT be excluded, the pattern is PLAN-*)
	writeDoc(t, docsDir, "PLAN.md", "concept", "Plan Root", "Root plan doc", nil)

	// Create .OCODE.md without prefix (should be excluded — *.OCODE.md)
	writeDoc(t, docsDir, "test.OCODE.md", "concept", "Test Model", "Test config", nil)

	b := &Bundle{Root: docsDir, OKFVersion: "0.1"}
	docs, err := b.Docs()
	if err != nil {
		t.Fatalf("Docs(): %v", err)
	}

	// Expected: intro.md, guide.md, PLAN.md (3 docs total)
	if len(docs) != 3 {
		t.Fatalf("Docs() returned %d docs, want 3. Got: %+v", len(docs), docs)
	}

	var paths []string
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	t.Logf("Returned paths: %v", paths)

	for _, p := range paths {
		if strings.HasPrefix(p, "PLAN-") {
			t.Errorf("Docs() returned excluded PLAN-* file: %s", p)
		}
		if strings.HasSuffix(p, ".OCODE.md") {
			t.Errorf("Docs() returned excluded *.OCODE.md file: %s", p)
		}
	}
}

func TestOKFIgnoreExcludesByPattern(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create docs.
	writeDoc(t, docsDir, "intro.md", "concept", "Intro", "Introduction", nil)
	writeDoc(t, docsDir, "guide.md", "guide", "Guide", "User guide", nil)
	writeDoc(t, docsDir, "TODO-checklist.md", "concept", "TODO", "Task list", nil)
	writeDoc(t, docsDir, "DRAFT-proposal.md", "concept", "Draft", "Draft proposal", nil)
	writeDoc(t, docsDir, "plans/SPEC-api.md", "concept", "Spec", "API spec", nil)
	writeDoc(t, docsDir, "plans/ROADMAP.md", "concept", "Roadmap", "Project roadmap", nil)
	writeDoc(t, docsDir, "notes/scratch.md", "concept", "Scratch", "Scratch notes", nil)

	// Create .okfignore with patterns.
	okfignoreContent := []byte(`# Files to exclude
TODO-*.md
DRAFT-*.md
plans/*
`)
	if err := os.WriteFile(filepath.Join(docsDir, ".okfignore"), okfignoreContent, 0644); err != nil {
		t.Fatal(err)
	}

	b := &Bundle{Root: docsDir, OKFVersion: "0.1"}
	docs, err := b.Docs()
	if err != nil {
		t.Fatalf("Docs(): %v", err)
	}

	// Expected: intro.md, guide.md, notes/scratch.md (3 docs)
	if len(docs) != 3 {
		t.Fatalf("Docs() returned %d docs, want 3. Got: %+v", len(docs), docs)
	}

	var paths []string
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	t.Logf("Returned paths: %v", paths)

	// Check excluded files are not present.
	for _, p := range paths {
		if p == "TODO-checklist.md" {
			t.Error("Docs() returned excluded TODO-checklist.md")
		}
		if p == "DRAFT-proposal.md" {
			t.Error("Docs() returned excluded DRAFT-proposal.md")
		}
		if strings.HasPrefix(p, "plans/") {
			t.Errorf("Docs() returned excluded plans/ file: %s", p)
		}
	}

	// Check included files are present.
	got := make(map[string]bool)
	for _, d := range docs {
		got[d.Path] = true
	}
	for _, want := range []string{"intro.md", "guide.md", "notes/scratch.md"} {
		if !got[want] {
			t.Errorf("Docs() missing expected file: %s", want)
		}
	}
}

func TestOKFIgnoreNoFileDoesNotError(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, docsDir, "doc1.md", "concept", "Doc1", "First doc", nil)
	writeDoc(t, docsDir, "doc2.md", "concept", "Doc2", "Second doc", nil)

	// No .okfignore file — should not error.
	b := &Bundle{Root: docsDir, OKFVersion: "0.1"}
	docs, err := b.Docs()
	if err != nil {
		t.Fatalf("Docs() errored without .okfignore: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("Docs() returned %d docs, want 2", len(docs))
	}
}

func TestOKFIgnoreEmptyFileDoesNotExclude(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, docsDir, "doc1.md", "concept", "Doc1", "First doc", nil)

	// Empty .okfignore file — should not exclude anything.
	if err := os.WriteFile(filepath.Join(docsDir, ".okfignore"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	b := &Bundle{Root: docsDir, OKFVersion: "0.1"}
	docs, err := b.Docs()
	if err != nil {
		t.Fatalf("Docs() errored: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("Docs() returned %d docs, want 1", len(docs))
	}
}

func TestOKFIgnoreDirectoryExclusion(t *testing.T) {
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create files in specific directories.
	writeDoc(t, docsDir, "intro.md", "concept", "Intro", "Introduction", nil)
	writeDoc(t, docsDir, "guide.md", "guide", "Guide", "User guide", nil)
	writeDoc(t, docsDir, "plans/SPEC-api.md", "concept", "Spec", "API spec", nil)
	writeDoc(t, docsDir, "plans/ROADMAP.md", "concept", "Roadmap", "Project roadmap", nil)
	writeDoc(t, docsDir, "archive/old-guide.md", "concept", "Old", "Old guide", nil)
	writeDoc(t, docsDir, "archive/notes/random.md", "concept", "Random", "Random note", nil)

	// .okfignore that excludes "plans" directory.
	okfignoreContent := []byte("plans\n")
	if err := os.WriteFile(filepath.Join(docsDir, ".okfignore"), okfignoreContent, 0644); err != nil {
		t.Fatal(err)
	}

	b := &Bundle{Root: docsDir, OKFVersion: "0.1"}
	docs, err := b.Docs()
	if err != nil {
		t.Fatalf("Docs(): %v", err)
	}

	// Expected: intro.md, guide.md, archive/old-guide.md, archive/notes/random.md (4 docs)
	if len(docs) != 4 {
		t.Fatalf("Docs() returned %d docs, want 4. Got: %+v", len(docs), docs)
	}

	var paths []string
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	t.Logf("Returned paths: %v", paths)

	// Check that plans/ directory files are excluded.
	for _, p := range paths {
		if strings.HasPrefix(p, "plans/") {
			t.Errorf("Docs() returned excluded plans/ file: %s", p)
		}
	}
}
