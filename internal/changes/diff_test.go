package changes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderDiffIdentical verifies that two identical files produce the
// "unchanged" message.
func TestRenderDiffIdentical(t *testing.T) {
	dir := t.TempDir()

	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := RenderDiff(a, b)
	if err != nil {
		t.Fatalf("RenderDiff identical: unexpected error: %v", err)
	}
	if !strings.Contains(out, "unchanged") {
		t.Fatalf("expected 'unchanged' message, got: %q", out)
	}
}

// TestRenderDiffDifferent verifies that two different files produce a
// unified diff with --- and +++ markers.
func TestRenderDiffDifferent(t *testing.T) {
	dir := t.TempDir()

	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("hello\nuniverse\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := RenderDiff(a, b)
	if err != nil {
		t.Fatalf("RenderDiff different: unexpected error: %v", err)
	}
	if !strings.Contains(out, "---") || !strings.Contains(out, "+++") {
		t.Fatalf("expected unified diff with ---/+++ markers, got:\n%s", out)
	}
	if !strings.Contains(out, "-world") {
		t.Fatalf("expected -world in diff output, got:\n%s", out)
	}
	if !strings.Contains(out, "+universe") {
		t.Fatalf("expected +universe in diff output, got:\n%s", out)
	}
}

// TestRenderDiffAdded verifies that a file with no backup (FirstBackupPath == "")
// returns the "new file" message.
func TestRenderDiffAdded(t *testing.T) {
	dir := t.TempDir()

	current := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(current, []byte("new file content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := RenderDiff("", current)
	if err != nil {
		t.Fatalf("RenderDiff added: unexpected error: %v", err)
	}
	if !strings.Contains(out, "new file") {
		t.Fatalf("expected 'new file' message, got: %q", out)
	}
}

// TestRenderDiffBackupMissing verifies that a missing backup path (non-existent)
// returns the "new file" message.
func TestRenderDiffBackupMissing(t *testing.T) {
	dir := t.TempDir()

	backup := filepath.Join(dir, "nonexistent_backup.txt")
	current := filepath.Join(dir, "current.txt")
	if err := os.WriteFile(current, []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := RenderDiff(backup, current)
	if err != nil {
		t.Fatalf("RenderDiff backup missing: unexpected error: %v", err)
	}
	if !strings.Contains(out, "new file") {
		t.Fatalf("expected 'new file' message, got: %q", out)
	}
}

// TestRenderDiffCurrentDeleted verifies that a missing current file returns
// the "deleted" message.
func TestRenderDiffCurrentDeleted(t *testing.T) {
	dir := t.TempDir()

	backup := filepath.Join(dir, "backup.txt")
	current := filepath.Join(dir, "deleted.txt")
	if err := os.WriteFile(backup, []byte("old content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Don't create current — simulate deleted file.

	out, err := RenderDiff(backup, current)
	if err != nil {
		t.Fatalf("RenderDiff deleted: unexpected error: %v", err)
	}
	if !strings.Contains(out, "deleted") {
		t.Fatalf("expected 'deleted' message, got: %q", out)
	}
}
