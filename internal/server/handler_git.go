package server

import (
	"net/http"
	"os/exec"
	"strconv"
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

// mutationProjectDir resolves the target project for the new file/git mutation
// endpoints. It accepts the server workDir, any registered project root, or any
// configured extra allowed path (snapshot under h.mu). Unlike fileTreeRootFor it
// requires an exact match to an allowed root — it does not accept arbitrary
// subpaths — and does not broaden the shared gitProjectDir used by uploads.
func (h *Handler) mutationProjectDir(r *http.Request) (string, bool) {
	dir := h.workDir
	if p := r.URL.Query().Get("project"); p != "" && p != h.workDir {
		if h.isRegisteredProjectRoot(p) {
			return p, true
		}
		// Snapshot extra paths under lock to avoid races on h.cfg.
		h.mu.Lock()
		var extras []string
		if h.cfg != nil {
			extras = append([]string(nil), h.cfg.Ocode.ExtraAllowedPaths...)
		}
		h.mu.Unlock()
		for _, extra := range extras {
			if extra != "" && p == extra {
				return p, true
			}
		}
		return "", false
	}
	return dir, true
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
// Supports ?path= filter for a single file and ?staged=true for the index.
func (h *Handler) HandleGitDiff(w http.ResponseWriter, r *http.Request) {
	pathFilter := r.URL.Query().Get("path")
	staged := r.URL.Query().Get("staged") == "true" || r.URL.Query().Get("staged") == "1"
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}

	// Check if we're in a git repo
	if _, err := gitRunInDir(dir, "rev-parse", "--git-dir"); err != nil {
		writeJSON(w, http.StatusOK, []GitDiffFile{})
		return
	}

	writeJSON(w, http.StatusOK, diffFilesForDir(dir, staged, pathFilter))
}

// GitWorkspace is the full SourceTree-style snapshot of a repo's uncommitted
// state: branch + status, the staged (index) file diffs, and the unstaged
// (working-tree + untracked) file diffs. Returned as one payload so the Git
// tab can render both panes and refresh atomically after every mutation.
type GitWorkspace struct {
	Status   GitStatus     `json:"status"`
	Staged   []GitDiffFile `json:"staged"`
	Unstaged []GitDiffFile `json:"unstaged"`
}

// HandleGitWorkspace returns status + staged + unstaged diffs in one request.
func (h *Handler) HandleGitWorkspace(w http.ResponseWriter, r *http.Request) {
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}
	writeJSON(w, http.StatusOK, gitWorkspaceForDir(dir))
}

// gitWorkspaceForDir computes the full workspace snapshot for dir. A non-repo
// dir yields an empty, no-changes snapshot (same contract as gitStatusForDir).
func gitWorkspaceForDir(dir string) GitWorkspace {
	ws := GitWorkspace{
		Status:   gitStatusForDir(dir),
		Staged:   []GitDiffFile{},
		Unstaged: []GitDiffFile{},
	}
	// A non-repo dir: gitStatusForDir already returned an empty status; bail
	// before the git diff calls emit errors.
	if ws.Status.Branch == "" && !ws.Status.HasChanges {
		if _, err := gitRunInDir(dir, "rev-parse", "--git-dir"); err != nil {
			return ws
		}
	}
	ws.Staged = diffFilesForDir(dir, true, "")
	ws.Unstaged = diffFilesForDir(dir, false, "")
	return ws
}

// diffFilesForDir returns the parsed unified diff of the repo at dir — the
// index (staged=true) or the working tree plus untracked files (staged=false).
// A pathFilter (repo-relative) narrows the result to one path. Non-repo or
// erroring dirs yield an empty slice, never null (the frontend reads .length).
func diffFilesForDir(dir string, staged bool, pathFilter string) []GitDiffFile {
	run := func(args ...string) (string, error) {
		return gitRunInDir(dir, args...)
	}

	files := make([]GitDiffFile, 0)

	// Get modified/added/deleted files from git diff
	diffArgs := []string{"diff", "--no-color", "-u"}
	if staged {
		diffArgs = append(diffArgs, "--cached")
	}
	if pathFilter != "" {
		diffArgs = append(diffArgs, "--", pathFilter)
	}
	if diffOut, err := run(diffArgs...); err == nil && diffOut != "" {
		files = append(files, parseUnifiedDiff(diffOut)...)
	}

	// The index cannot hold untracked files; only the working-tree diff needs
	// the untracked pass.
	if !staged {
		statusArgs := []string{"status", "--porcelain", "-u"}
		if pathFilter != "" {
			statusArgs = append(statusArgs, "--", pathFilter)
		}
		if statusOut, err := run(statusArgs...); err == nil {
			for _, line := range strings.Split(statusOut, "\n") {
				if len(line) < 4 {
					continue
				}
				statusCode := line[:2]
				filePath := line[3:]
				if strings.Contains(statusCode, "?") {
					// Untracked file — get its content as patch. `git diff
					// --no-index` exits 1 even on success, so use output only.
					patch := ""
					if content, _ := run("diff", "--no-index", "/dev/null", filePath); content != "" {
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
	}

	return files
}

// GitCommit describes a single commit for the SourceTree-style log view.
type GitCommit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	Date    string `json:"date"`
}

// HandleGitLog returns recent commit history, newest first. Supports
// ?limit= (clamped to 1..200, default 50) and ?project=.
func (h *Handler) HandleGitLog(w http.ResponseWriter, r *http.Request) {
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	out, err := gitRunInDir(dir, "log",
		"-n", strconv.Itoa(limit),
		"--pretty=format:%H%x00%h%x00%an%x00%ae%x00%aI%x00%s")
	if err != nil {
		// Not a repo or no commits yet → empty list (never null).
		writeJSON(w, http.StatusOK, []GitCommit{})
		return
	}
	commits := make([]GitCommit, 0, limit)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) < 6 {
			continue
		}
		commits = append(commits, GitCommit{
			Hash:    fields[0],
			Short:   fields[1],
			Author:  fields[2],
			Email:   fields[3],
			Date:    fields[4],
			Message: fields[5],
		})
	}
	writeJSON(w, http.StatusOK, commits)
}

// HandleGitShow returns the diff of a single commit (?commit=<rev>), parsed
// through the same unified-diff pipeline as the working-tree diff.
func (h *Handler) HandleGitShow(w http.ResponseWriter, r *http.Request) {
	rev := strings.TrimSpace(r.URL.Query().Get("commit"))
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}
	if rev == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit is required"})
		return
	}
	// Resolve to a concrete object id first so the subsequent `git show` can't
	// be aimed at refs, ranges, or ambiguous abbreviations.
	resolved, err := gitRunInDir(dir, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil || resolved == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown commit"})
		return
	}
	out, err := gitRunInDir(dir, "show", "--no-color", "--format=", resolved)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if out == "" {
		writeJSON(w, http.StatusOK, []GitDiffFile{})
		return
	}
	writeJSON(w, http.StatusOK, parseUnifiedDiff(out))
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
