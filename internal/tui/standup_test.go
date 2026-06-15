package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newStandupRepo creates a temp git repo with one commit and returns its path.
func newStandupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRunForTest(t, dir, "init")
	gitRunForTest(t, dir, "config", "user.email", "test@example.com")
	gitRunForTest(t, dir, "config", "user.name", "Test")
	writeFileForGitTest(t, dir, "a.txt", "hello\n")
	gitRunForTest(t, dir, "add", "a.txt")
	gitRunForTest(t, dir, "commit", "-m", "feat: add a.txt")
	return dir
}

// On a clean tree, standup must still render the commit history rather than
// erroring — the commit-only summary is an in-scope case (e.g. catching up on
// yesterday's work with nothing currently staged).
func TestStandupContextCleanTreeStillHasCommits(t *testing.T) {
	dir := newStandupRepo(t)

	ctx, err := getStandupContext(dir, nil)
	if err != nil {
		t.Fatalf("clean tree should not error, got: %v", err)
	}
	if !strings.Contains(ctx, "Recent Commits") {
		t.Fatalf("expected commit section, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "feat: add a.txt") {
		t.Fatalf("expected commit subject in context, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "Clean tree") {
		t.Fatalf("expected clean-tree note for pending changes, got:\n%s", ctx)
	}
}

// A dirty tree should surface pending changes alongside the commit history.
func TestStandupContextDirtyTreeHasPendingChanges(t *testing.T) {
	dir := newStandupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new file\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, err := getStandupContext(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(ctx, "Pending Changes") {
		t.Fatalf("expected pending changes section, got:\n%s", ctx)
	}
	if strings.Contains(ctx, "Clean tree") {
		t.Fatalf("dirty tree should not report a clean tree, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "b.txt") {
		t.Fatalf("expected untracked file in context, got:\n%s", ctx)
	}
}
