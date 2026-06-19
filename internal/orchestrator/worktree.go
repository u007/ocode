package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// WorktreeManager creates and destroys a dedicated git worktree for the pipeline run.
// In worktree mode, all developer writes and compile commands execute inside the
// worktree so other concurrent agents cannot corrupt the pipeline's build state.
type WorktreeManager struct {
	repoRoot string
	path     string // set after Setup
}

// NewWorktreeManager returns a manager rooted at the given git repository root.
func NewWorktreeManager(repoRoot string) *WorktreeManager {
	return &WorktreeManager{repoRoot: repoRoot}
}

// Path returns the worktree path, or "" if Setup has not been called.
func (w *WorktreeManager) Path() string { return w.path }

// Setup creates a new git worktree at .worktrees/orchestrator-<runID>/.
// Returns the absolute path to the worktree.
func (w *WorktreeManager) Setup(runID string) (string, error) {
	if w.path != "" {
		return "", fmt.Errorf("worktree already set up at %s", w.path)
	}
	dest := filepath.Join(w.repoRoot, ".worktrees", "orchestrator-"+runID)
	cmd := exec.Command("git", "worktree", "add", "--detach", dest, "HEAD")
	cmd.Dir = w.repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %w\n%s", err, out)
	}
	w.path = dest
	return dest, nil
}

// Teardown removes the worktree. When preserve is true (HALTED state), the
// worktree directory is left in place so the user can inspect the work.
func (w *WorktreeManager) Teardown(preserve bool) error {
	if w.path == "" {
		return nil
	}
	if !preserve {
		cmd := exec.Command("git", "worktree", "remove", "--force", w.path)
		cmd.Dir = w.repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			// Best-effort remove the directory if git command fails
			_ = os.RemoveAll(w.path)
			return fmt.Errorf("git worktree remove failed: %w\n%s", err, out)
		}
	}
	w.path = ""
	return nil
}
