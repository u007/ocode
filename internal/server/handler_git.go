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
	return gitRunInDir(h.workDir, args...)
}

func gitRunInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
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

// HandleGitStatus returns the working-tree status. By default it reports the
// server's workdir; ?project=<path> selects a registered project root instead
// (unknown paths are rejected so the endpoint can't probe arbitrary dirs).
func (h *Handler) HandleGitStatus(w http.ResponseWriter, r *http.Request) {
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}
	writeJSON(w, http.StatusOK, gitStatusForDir(dir))
}

func (h *Handler) gitProjectDir(r *http.Request) (string, bool) {
	dir := h.workDir
	if p := r.URL.Query().Get("project"); p != "" && p != h.workDir {
		if !h.isRegisteredProjectRoot(p) {
			return "", false
		}
		dir = p
	}
	return dir, true
}

// isRegisteredProjectRoot reports whether p is one of the saved project roots.
func (h *Handler) isRegisteredProjectRoot(p string) bool {
	if h.projects == nil {
		return false
	}
	for _, proj := range h.projects.List() {
		if proj.Path == p {
			return true
		}
	}
	return false
}

// gitStatusForDir computes the working-tree status of the repo at dir. It is
// shared by the legacy GET endpoint (with the server's workdir) and the
// subscriber-aware server-push git watcher (per project root). A non-repo or
// erroring dir yields an empty, no-changes status.
func gitStatusForDir(dir string) GitStatus {
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		out, _ := cmd.Output()
		return strings.TrimSpace(string(out))
	}

	// Initialize slices so JSON serializes [] (not null) — the web UI reads
	// staged_files.length / changed_files.length unconditionally.
	status := GitStatus{
		Branch:       run("rev-parse", "--abbrev-ref", "HEAD"),
		StagedFiles:  []string{},
		ChangedFiles: []string{},
	}
	for _, f := range strings.Split(run("diff", "--name-only", "--cached"), "\n") {
		if f != "" {
			status.StagedFiles = append(status.StagedFiles, f)
		}
	}
	for _, f := range strings.Split(run("diff", "--name-only"), "\n") {
		if f != "" {
			status.ChangedFiles = append(status.ChangedFiles, f)
		}
	}
	status.HasChanges = len(status.StagedFiles) > 0 || len(status.ChangedFiles) > 0
	return status
}

// HandleGitDiff returns the unified diff for the working tree.
// Supports ?path= filter for a single file.
func (h *Handler) HandleGitDiff(w http.ResponseWriter, r *http.Request) {
	pathFilter := r.URL.Query().Get("path")
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}
	runGit := func(args ...string) (string, error) {
		return gitRunInDir(dir, args...)
	}

	// Check if we're in a git repo
	if _, err := runGit("rev-parse", "--git-dir"); err != nil {
		writeJSON(w, http.StatusOK, []GitDiffFile{})
		return
	}

	files := make([]GitDiffFile, 0)

	// Get modified/added/deleted files from git diff
	diffArgs := []string{"diff", "--no-color", "-u"}
	if pathFilter != "" {
		diffArgs = append(diffArgs, "--", pathFilter)
	}
	if diffOut, err := runGit(diffArgs...); err == nil && diffOut != "" {
		files = append(files, parseUnifiedDiff(diffOut)...)
	}

	// Get untracked files
	statusArgs := []string{"status", "--porcelain", "-u"}
	if pathFilter != "" {
		statusArgs = append(statusArgs, "--", pathFilter)
	}
	if statusOut, err := runGit(statusArgs...); err == nil {
		for _, line := range strings.Split(statusOut, "\n") {
			if len(line) < 4 {
				continue
			}
			statusCode := line[:2]
			filePath := line[3:]
			if strings.Contains(statusCode, "?") {
				// Untracked file — get its content as patch
				patch := ""
				if content, err := runGit("diff", "--no-index", "/dev/null", filePath); err != nil {
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
