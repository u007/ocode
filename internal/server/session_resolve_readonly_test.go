package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/session"
)

// writeClaudeSourceForResolveTest puts a Claude Code transcript for id under
// the Claude project dir that projectRoot maps to (~/.claude/projects/<slug>),
// using the same slug layout as session's claudeProjectSlug.
func writeClaudeSourceForResolveTest(t *testing.T, home, projectRoot, id string) {
	t.Helper()
	slug := strings.ReplaceAll(filepath.ToSlash(filepath.Clean(paths.ProjectRoot(projectRoot))), "/", "-")
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"user","message":{"role":"user","content":"hello"},"timestamp":"2026-05-16T10:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// storageNamesForTest lists the entry names in wd's ocode storage dir, or nil
// when the dir does not exist yet.
func storageNamesForTest(t *testing.T, wd string) []string {
	t.Helper()
	dir, err := session.GetStorageDirForPath(wd)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Resolve is a read: resolving a Claude session that exists only as a source
// transcript must register it without writing a clone into ocode storage.
func TestResolveClaudeSourceOnlyWritesNothing(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	projectRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeClaudeSourceForResolveTest(t, home, projectRoot, "claude-2")
	before := storageNamesForTest(t, projectRoot)

	mgr := NewSessionManager(30*time.Minute, func() []string { return []string{projectRoot} }, nil)
	e, err := mgr.Resolve("claude:claude-2")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if e.ProjectRoot != projectRoot {
		t.Fatalf("root = %q, want %q", e.ProjectRoot, projectRoot)
	}
	after := storageNamesForTest(t, projectRoot)
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Fatalf("Resolve wrote to ocode storage: before %v, after %v", before, after)
	}
}

// When the same transcript sits under two roots and one root already holds the
// ocode clone, Resolve must pick that root and must not clone into the other.
func TestResolvePrefersExistingCloneOverSourceRoot(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	rootA, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeClaudeSourceForResolveTest(t, home, rootA, "claude-2")
	// The ocode clone is stored under the canonical id of "claude:claude-2".
	saveSessionToDir(t, rootB, "claude-claude-2")

	mgr := NewSessionManager(30*time.Minute, func() []string { return []string{rootA, rootB} }, nil)
	e, err := mgr.Resolve("claude:claude-2")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if e.ProjectRoot != rootB {
		t.Fatalf("root = %q, want the root holding the clone %q", e.ProjectRoot, rootB)
	}
	if names := storageNamesForTest(t, rootA); len(names) != 0 {
		t.Fatalf("clone written under the source-only root: %v", names)
	}
}
