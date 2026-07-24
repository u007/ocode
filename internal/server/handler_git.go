package server

import (
	"net/http"
	"os/exec"
	"strings"
)

// GitDiffFile represents a single file's diff in the working tree.
type GitDiffFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "modified", "added", "deleted", "renamed", "untracked"
	Patch  string `json:"patch"`
}

// gitRun runs a git command in the handler's work directory and returns stdout.
func (h *Handler) gitRun(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if h.workDir != "" {
		cmd.Dir = h.workDir
	}
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

type GitStatus struct {
	Branch       string   `json:"branch"`
	StagedFiles  []string `json:"staged_files"`
	ChangedFiles []string `json:"changed_files"`
	HasChanges   bool     `json:"has_changes"`
}

func (h *Handler) HandleGitStatus(w http.ResponseWriter, r *http.Request) {
	branch := ""
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}

	staged := []string{}
	changed := []string{}
	if out, err := exec.Command("git", "diff", "--name-only", "--cached").Output(); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f != "" {
				staged = append(staged, f)
			}
		}
	}
	if out, err := exec.Command("git", "diff", "--name-only").Output(); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f != "" {
				changed = append(changed, f)
			}
		}
	}

	writeJSON(w, http.StatusOK, GitStatus{
		Branch:       branch,
		StagedFiles:  staged,
		ChangedFiles: changed,
		HasChanges:   len(staged) > 0 || len(changed) > 0,
	})
}

// HandleGitDiff returns the unified diff for the working tree.
// Supports ?path= filter for a single file.
func (h *Handler) HandleGitDiff(w http.ResponseWriter, r *http.Request) {
	pathFilter := r.URL.Query().Get("path")

	// Check if we're in a git repo
	if _, err := h.gitRun("rev-parse", "--git-dir"); err != nil {
		writeJSON(w, http.StatusOK, []GitDiffFile{})
		return
	}

	files := make([]GitDiffFile, 0)

	// Get modified/added/deleted files from git diff
	diffArgs := []string{"diff", "--no-color", "-u"}
	if pathFilter != "" {
		diffArgs = append(diffArgs, "--", pathFilter)
	}
	if diffOut, err := h.gitRun(diffArgs...); err == nil && diffOut != "" {
		files = append(files, parseUnifiedDiff(diffOut)...)
	}

	// Get untracked files
	statusArgs := []string{"status", "--porcelain", "-u"}
	if pathFilter != "" {
		statusArgs = append(statusArgs, "--", pathFilter)
	}
	if statusOut, err := h.gitRun(statusArgs...); err == nil {
		for _, line := range strings.Split(statusOut, "\n") {
			if len(line) < 4 {
				continue
			}
			statusCode := line[:2]
			filePath := line[3:]
			if strings.Contains(statusCode, "?") {
				// Untracked file — get its content as patch
				patch := ""
				if content, err := h.gitRun("diff", "--no-index", "/dev/null", filePath); err != nil {
					patch = content
				}
				files = append(files, GitDiffFile{
					Path:   filePath,
					Status: "untracked",
					Patch:  patch,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, files)
}

// parseUnifiedDiff parses a unified diff output into GitDiffFile entries.
func parseUnifiedDiff(diff string) []GitDiffFile {
	var files []GitDiffFile
	var current *GitDiffFile
	var patchLines []string

	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			// Save previous file
			if current != nil {
				current.Patch = strings.Join(patchLines, "\n")
				files = append(files, *current)
			}
			// Parse "diff --git a/path b/path"
			parts := strings.Split(line, " b/")
			if len(parts) >= 2 {
				current = &GitDiffFile{
					Path:   parts[len(parts)-1],
					Status: "modified",
				}
			}
			patchLines = nil
		} else if strings.HasPrefix(line, "new file") {
			if current != nil {
				current.Status = "added"
			}
		} else if strings.HasPrefix(line, "deleted file") {
			if current != nil {
				current.Status = "deleted"
			}
		} else if strings.HasPrefix(line, "rename from") {
			if current != nil {
				current.Status = "renamed"
			}
		} else if current != nil {
			patchLines = append(patchLines, line)
		}
	}

	// Save last file
	if current != nil {
		current.Patch = strings.Join(patchLines, "\n")
		files = append(files, *current)
	}

	return files
}
