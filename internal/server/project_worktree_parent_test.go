package server

import (
	"path/filepath"
	"testing"
)

func TestProbeWorktreeParent(t *testing.T) {
	main := worktreeFixture(t)
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, main, "git", "worktree", "add", "-b", "feat", linked)
	linked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatalf("resolve linked dir: %v", err)
	}

	if got := probeWorktreeParent(linked); got != main {
		t.Fatalf("linked worktree parent = %q, want %q", got, main)
	}
	if got := probeWorktreeParent(main); got != "" {
		t.Fatalf("main checkout parent = %q, want empty", got)
	}
	if got := probeWorktreeParent(t.TempDir()); got != "" {
		t.Fatalf("non-repo parent = %q, want empty", got)
	}
}
