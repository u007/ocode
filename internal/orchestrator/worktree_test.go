package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These are integration tests — they run real git commands.
// Skip if not in a git repo.
func repoRoot(t *testing.T) string {
	t.Helper()
	// Walk up from the test file's location to find .git
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not in a git repo")
		}
		dir = parent
	}
}

func TestWorktreeSetupTeardown(t *testing.T) {
	root := repoRoot(t)
	wm := NewWorktreeManager(root)
	path, err := wm.Setup("test-run-001")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if path == "" {
		t.Fatal("Setup returned empty path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree path does not exist: %v", err)
	}
	if err := wm.Teardown(false); err != nil {
		t.Fatalf("Teardown failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree should be removed after Teardown(preserve=false)")
	}
}

func TestWorktreePreserveOnHalt(t *testing.T) {
	root := repoRoot(t)
	// Clean up any stale worktree from a prior failed test run.
	// (git worktree leaves locked entries when tests crash mid-flight.)
	stalePath := filepath.Join(root, ".worktrees", "orchestrator-test-run-002")
	_ = os.RemoveAll(stalePath)
	_ = exec.Command("git", "-C", root, "worktree", "prune").Run()
	wm := NewWorktreeManager(root)
	path, err := wm.Setup("test-run-002")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	t.Cleanup(func() { wm.Teardown(false) }) // always clean up
	if err := wm.Teardown(true); err != nil {
		t.Fatalf("Teardown(preserve=true) failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("worktree should be preserved: %v", err)
	}
}
