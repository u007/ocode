package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// gitHunkRequest is the body of POST /api/git/hunk. Path may be project-
// relative or absolute inside the project root (the same convention as the
// file-level git actions). Hunk is the zero-based index of the hunk in the
// file's derived diff. Action is one of "stage" | "unstage" | "discard";
// which actions are legal depends on which diff the hunk came from:
//
//	unstaged diff:  stage   (apply --cached),  discard (apply --reverse)
//	staged diff:    unstage (apply --cached --reverse)
//
// Staged is true when the hunk came from the staged (index) diff.
type gitHunkRequest struct {
	Path   string `json:"path"`
	Hunk   int    `json:"hunk_index"`
	Action string `json:"action"`
	Staged bool   `json:"staged"`
}

// HandleGitHunk stages, unstages, or discards a single hunk of a file's diff.
// The server re-derives the file's diff itself instead of trusting client
// patch text (which could smuggle arbitrary paths/context through git apply),
// then rebuilds a minimal patch containing only the selected hunk and pipes it
// into `git apply`. Untracked files have a single synthetic hunk (the whole
// file): stage adds the file, discard removes it. The response is the full
// refreshed workspace so the UI re-renders from one authoritative snapshot.
func (h *Handler) HandleGitHunk(w http.ResponseWriter, r *http.Request) {
	var req gitHunkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	dir, ok := h.mutationProjectDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	// Detect a repository via git itself so worktrees and bare setups are
	// recognised (same check as the file-level git actions).
	if out, err := gitRunInDir(dir, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		writeError(w, http.StatusBadRequest, "not a git repository")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if req.Hunk < 0 {
		writeError(w, http.StatusBadRequest, "invalid hunk index")
		return
	}
	switch req.Action {
	case "stage", "unstage", "discard":
	default:
		writeError(w, http.StatusBadRequest, "action must be stage, unstage or discard")
		return
	}

	// Validate the path through the same resolver the other git mutations use,
	// then hand git the repo-relative pathspec.
	_, spec, err := resolveRepoPath(dir, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := applyGitHunk(dir, spec, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gitWorkspaceForDir(dir))
}

// applyGitHunk performs the requested hunk operation for one file.
func applyGitHunk(dir, spec string, req gitHunkRequest) error {
	// Untracked files get a single synthetic hunk covering the whole file.
	// The index can never hold an untracked entry, so this only applies to
	// unstaged requests.
	if !req.Staged && isUntrackedPath(dir, spec) {
		if req.Hunk != 0 {
			return fmt.Errorf("untracked files have a single hunk (index 0)")
		}
		switch req.Action {
		case "stage":
			if _, err := gitRunInDir(dir, "add", "--", spec); err != nil {
				return fmt.Errorf("git add failed: %w", err)
			}
			return nil
		case "discard":
			abs, _, err := resolveRepoPath(dir, spec)
			if err != nil {
				return err
			}
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove %s: %w", spec, err)
			}
			return nil
		default:
			return fmt.Errorf("cannot %s an untracked file", req.Action)
		}
	}

	// Re-derive the file's diff (index or working tree) and split out hunks.
	diffArgs := []string{"diff", "--no-color", "-u"}
	if req.Staged {
		diffArgs = append(diffArgs, "--cached")
	}
	diffArgs = append(diffArgs, "--", spec)
	out, err := gitRunInDir(dir, diffArgs...)
	if err != nil || out == "" {
		return fmt.Errorf("no diff found for %s", spec)
	}
	header, hunks := splitDiffHunks(out)
	if req.Hunk >= len(hunks) {
		return fmt.Errorf("hunk index %d out of range (file has %d hunks)", req.Hunk, len(hunks))
	}

	// Action → git apply flags. The matrix deliberately rejects ambiguous
	// pairings (stage on a staged hunk, unstage on an unstaged hunk, discard
	// on a staged hunk) instead of silently doing two applies.
	var applyArgs []string
	switch req.Action {
	case "stage":
		if req.Staged {
			return fmt.Errorf("cannot stage a hunk from the staged diff")
		}
		applyArgs = []string{"--cached"}
	case "unstage":
		if !req.Staged {
			return fmt.Errorf("cannot unstage a hunk from the unstaged diff")
		}
		applyArgs = []string{"--cached", "--reverse"}
	case "discard":
		if req.Staged {
			return fmt.Errorf("discarding a staged hunk is not supported — unstage it first")
		}
		applyArgs = []string{"--reverse"}
	}

	patch := strings.TrimRight(header, "\n") + "\n" + hunks[req.Hunk] + "\n"
	if err := gitApplyInDir(dir, applyArgs, patch); err != nil {
		return fmt.Errorf("git apply failed (the file may have changed): %w", err)
	}
	return nil
}

// splitDiffHunks splits a unified diff into its preamble (the diff --git /
// index / --- / +++ lines) and one string per hunk, each starting at its
// @@ line. Hunks are identified purely by a line that starts with "@@",
// which in unified diffs only occurs as a hunk header.
func splitDiffHunks(diff string) (header string, hunks []string) {
	lines := strings.Split(diff, "\n")
	var headerLines []string
	var cur []string
	inHunk := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "@@") {
			if inHunk {
				hunks = append(hunks, strings.Join(cur, "\n"))
			}
			cur = []string{ln}
			inHunk = true
			continue
		}
		if inHunk {
			cur = append(cur, ln)
		} else {
			headerLines = append(headerLines, ln)
		}
	}
	if inHunk {
		hunks = append(hunks, strings.Join(cur, "\n"))
	}
	return strings.Join(headerLines, "\n"), hunks
}

// gitApplyInDir runs `git apply` with the patch supplied on stdin. Options
// (--cached, --reverse, ...) are appended before the "-" that reads stdin.
// --whitespace=nowarn keeps a user's whitespace policy from rejecting hunks
// the UI is deliberately applying.
func gitApplyInDir(dir string, opts []string, patch string) error {
	args := append([]string{"apply", "--whitespace=nowarn"}, opts...)
	args = append(args, "-")
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = strings.NewReader(patch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v:\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// isUntrackedPath reports whether the repo-relative spec is an untracked
// (?? in porcelain) path. It checks the narrowest matching status line.
func isUntrackedPath(dir, spec string) bool {
	out, err := gitRunInDir(dir, "status", "--porcelain", "-u", "--", spec)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) >= 2 && strings.HasPrefix(line[:2], "??") {
			return true
		}
	}
	return false
}
