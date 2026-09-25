package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/gitexec"
	"github.com/u007/ocode/internal/projects"
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

// gitRunInDir runs a git command in dir and returns its trimmed stdout. On
// failure the error carries git's own explanation: stderr first, then stdout as
// a fallback (see the fold below) — git does not always put the reason on
// stderr, so a stderr-only fold can surface a bare "exit status 1".
//
// Two ocode-specific behaviours ride along, both about .git/index.lock: the
// child runs with GIT_OPTIONAL_LOCKS=0 (gitexec.Env) so ocode's frequent probes
// never take the optional index lock, and a failed lock acquisition is retried
// briefly (gitexec.WithLockRetry) because the holder is nearly always a
// short-lived git process rather than a stale lock. A user clicking "Stage"
// must not see a red error because something else ran git a moment earlier.
func gitRunInDir(dir string, args ...string) (string, error) {
	var out string
	err := gitexec.WithLockRetry(func() error {
		cmd := exec.Command(gitBinary, args...)
		if dir != "" {
			cmd.Dir = dir
		}
		cmd.Env = gitexec.Env()
		var stderr strings.Builder
		cmd.Stderr = &stderr
		b, cmdErr := cmd.Output()
		out = strings.TrimSpace(string(b))
		return gitexec.WithOutput(cmdErr, stderr.String(), out)
	})
	return out, err
}

type GitStatus struct {
	Branch       string   `json:"branch"`
	StagedFiles  []string `json:"staged_files"`
	ChangedFiles []string `json:"changed_files"`
	HasChanges   bool     `json:"has_changes"`
	// Conflicts lists the repository's unmerged paths. A conflicted path is
	// deliberately NOT also listed in StagedFiles or ChangedFiles: git's diff
	// listings report it in both, and `git diff --name-only` reports it twice,
	// so it used to be counted three times across the two lists and inflate
	// every badge. Always an empty slice, never null.
	Conflicts []GitConflict `json:"conflicts"`
	// Operation reports a halted git operation (merge, rebase, cherry-pick,
	// revert, am, bisect), or nil when none is in progress. It is a pointer
	// with omitempty so an idle repository marshals identically on every poll
	// and the emitter's change-dedup does not publish spuriously.
	Operation *GitOperation `json:"operation,omitempty"`
	// IsRepo distinguishes "clean repository" from "not a repository": both
	// yield empty StagedFiles/ChangedFiles and no changes, but the web editor's
	// unstaged-change decorations must NOT fall back to session diffs in a
	// clean repo the way it does outside a repo.
	IsRepo bool `json:"is_repo"`
	// Divergence from upstream: ahead = local commits not yet pushed,
	// behind = remote commits not yet pulled. -1 means no upstream branch.
	Ahead       int  `json:"ahead"`
	Behind      int  `json:"behind"`
	HasUpstream bool `json:"has_upstream"`
}

// HandleGitStatus returns the working-tree status. By default it reports the
// server's workdir; ?project=<path> selects a registered project root instead
// (unknown paths are rejected so the endpoint can't probe arbitrary dirs).
// ?host=<target> + ?project=<path> selects a registered REMOTE project — the
// same (host, path) pair the terminal endpoint accepts — and the git
// pipeline runs on that host through its transport.
func (h *Handler) HandleGitStatus(w http.ResponseWriter, r *http.Request) {
	if host := hostParam(r); host != "" {
		rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, remoteGitStatus(r.Context(), rw))
		return
	}
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
		resolved, ok := h.resolveRegisteredProjectRoot(p)
		if !ok {
			return "", false
		}
		dir = resolved
	}
	return dir, true
}

// resolveRegisteredProjectRoot maps a ?project= value to a saved project root.
//
// A remote project is registered with its path verbatim ("~/www/aimsai2" — the
// separator and "~" belong to the remote shell, see projects.AddRemote), but the
// host-side `ocode serve --remote` expands "~" when it saves that project
// (projects.Add). A request proxied through /api/remote/{host}/ therefore
// arrives carrying the tilde form while the host's registry holds the expanded
// path, so the exact-match gate rejects it. Fall back to the home-expanded
// form, resolved against THIS server's home (the host, for a proxied request).
//
// The trust decision is unchanged: only a saved project root is ever returned,
// and the expansion narrows rather than widens the accepted set — "~user" and
// every non-tilde path pass through unchanged.
func (h *Handler) resolveRegisteredProjectRoot(p string) (string, bool) {
	if h.isRegisteredProjectRoot(p) {
		return p, true
	}
	expanded, err := projects.ExpandHome(p)
	if err != nil || expanded == p {
		return "", false
	}
	if h.isRegisteredProjectRoot(expanded) {
		return expanded, true
	}
	return "", false
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

// gitBinary is the git executable every server git helper invokes (status
// probes and mutations alike). It is a var (not a literal) so tests can
// substitute a stub — e.g. one that never returns, to exercise the
// gitStatusTimeout bound, or one that reports its own environment, to prove
// GIT_OPTIONAL_LOCKS=0 reaches the child.
var gitBinary = "git"

// gitStatusTimeout bounds the total time gitStatusForDir spends running git
// probes for ONE project. Local git is otherwise unbounded, unlike the remote
// path (remoteExecTimeout): a repo whose git wedges — a stalled network mount,
// an index.lock held by a dead process, a pathological tree — would otherwise
// hang its own HTTP request and, worse, stall the shared git-status emitter
// for every other viewed project. When the budget expires the remaining
// probes fail fast and the project reports an empty, non-repo status; the next
// poll retries.
var gitStatusTimeout = 10 * time.Second

// gitStatusForDir computes the working-tree status of the repo at dir. It is
// shared by the legacy GET endpoint (with the server's workdir) and the
// subscriber-aware server-push git watcher (per project root). A non-repo or
// erroring dir yields an empty, no-changes status. All git probes share one
// deadline (gitStatusTimeout) so a wedged repo can never pin the caller.
func gitStatusForDir(dir string) GitStatus {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	// runRaw returns stdout verbatim; run trims it for the line-oriented
	// probes. The `-z` probe must not be trimmed, because its records are
	// NUL-separated and a path is allowed to end in whitespace.
	runRaw := func(args ...string) string {
		cmd := exec.CommandContext(ctx, gitBinary, args...)
		if dir != "" {
			cmd.Dir = dir
		}
		// GIT_OPTIONAL_LOCKS=0: `git status`/`git diff` otherwise refresh the
		// index and take .git/index.lock as a side effect. This probe runs for
		// every viewed project every 10s (emitters.go), so without it ocode is
		// a permanent lock contender against the user's own git commands — and
		// a probe SIGKILLed by gitStatusTimeout mid-write could strand the lock.
		cmd.Env = gitexec.Env()
		out, _ := cmd.Output()
		return string(out)
	}
	run := func(args ...string) string {
		return strings.TrimSpace(runRaw(args...))
	}

	// Initialize slices so JSON serializes [] (not null) — the web UI reads
	// staged_files.length / changed_files.length unconditionally.
	status := GitStatus{
		Branch:       run("rev-parse", "--abbrev-ref", "HEAD"),
		StagedFiles:  []string{},
		ChangedFiles: []string{},
		Conflicts:    []GitConflict{},
	}

	// Unmerged paths, with their status code and which index stages exist. One
	// `-z` probe supplies all of it: the record carries an object id per stage
	// and an all-zero id marks a deleted side. The non-`-z` porcelain v2 probe
	// further down cannot be reused, because changing its record separator
	// would break the branch-header parse.
	status.Conflicts = parseUnmergedPorcelain(runRaw("status", "--porcelain=v2", "-z"))

	// A conflicted path is reported by both diff listings (and twice within
	// one of them), so divert it: it belongs to the conflicts list alone.
	conflicted := make(map[string]bool, len(status.Conflicts))
	for _, c := range status.Conflicts {
		conflicted[c.Path] = true
	}
	for _, f := range dedupePaths(strings.Split(run("diff", "--name-only", "--cached"), "\n")) {
		if !conflicted[f] {
			status.StagedFiles = append(status.StagedFiles, f)
		}
	}
	for _, f := range dedupePaths(strings.Split(run("diff", "--name-only"), "\n")) {
		if !conflicted[f] {
			status.ChangedFiles = append(status.ChangedFiles, f)
		}
	}
	// Untracked files are part of the working-tree changes (the Git workspace
	// endpoint lists them under unstaged, and the web Git tab badge counts
	// staged + unstaged). `git diff --name-only` omits them, so append the
	// `??` entries from `git status --porcelain` to keep the lightweight
	// status counts in sync with the workspace snapshot.
	seen := make(map[string]bool, len(status.StagedFiles)+len(status.ChangedFiles))
	for _, f := range status.StagedFiles {
		seen[f] = true
	}
	for _, f := range status.ChangedFiles {
		seen[f] = true
	}
	for _, line := range strings.Split(run("status", "--porcelain", "-u"), "\n") {
		if len(line) < 4 {
			continue
		}
		if !strings.Contains(line[:2], "?") {
			continue
		}
		f := strings.Trim(line[3:], `"`)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		status.ChangedFiles = append(status.ChangedFiles, f)
	}
	status.HasChanges = len(status.StagedFiles) > 0 || len(status.ChangedFiles) > 0 || len(status.Conflicts) > 0

	// Divergence from upstream using --branch (works for detached HEAD too).
	branchLine := run("status", "--porcelain=v2", "--branch")
	status.HasUpstream = false
	status.Ahead = 0
	status.Behind = 0
	for _, line := range strings.Split(branchLine, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "# branch.ab ") {
			continue
		}
		// Format: # branch.ab +3 -2
		parts := strings.Split(line, " ")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "+0" || p == "-0" || p == "+" || p == "-" || p == "#" || p == "branch.ab" {
				continue
			}
			if strings.HasPrefix(p, "+") {
				status.Ahead, _ = strconv.Atoi(p[1:])
				status.HasUpstream = true
			} else if strings.HasPrefix(p, "-") {
				status.Behind, _ = strconv.Atoi(p[1:])
				status.HasUpstream = true
			}
		}
		break
	}
	// Detect upstream even when divergence is 0.
	if branchLine == "" || !strings.Contains(branchLine, "# branch.ab") {
		upstream := run("rev-parse", "--abbrev-ref", "@{upstream}")
		if upstream != "" && upstream != "HEAD" && !strings.Contains(upstream, "fatal:") && !strings.Contains(upstream, "unknown") {
			status.HasUpstream = true
		}
	}

	// Same dir handling as the run closure above (empty dir = server workdir).
	repoCmd := exec.CommandContext(ctx, gitBinary, "rev-parse", "--git-dir")
	if dir != "" {
		repoCmd.Dir = dir
	}
	repoCmd.Env = gitexec.Env()
	_, err := repoCmd.Output()
	status.IsRepo = err == nil

	// A halted operation is a state of the repository, so it is reported
	// alongside IsRepo. Detection spends one more git process, still inside the
	// shared deadline above.
	if op, opErr := gitOperationStateForDir(ctx, dir); opErr != nil {
		// Three outcomes are handled here. Two are expected and deliberately
		// not logged: a directory that is not a repository (the normal answer
		// for the many saved non-repo directories), and the shared probe
		// budget expiring, which is the documented degradation mode — the
		// remaining probes fail fast and the next poll retries. Logging the
		// latter would spam a slow repository on every poll.
		//
		// Everything else is a real failure and IS logged, so a broken
		// repository can never masquerade as a clean, idle one.
		expected := errors.Is(opErr, errGitNotARepository) ||
			errors.Is(opErr, context.DeadlineExceeded) ||
			ctx.Err() != nil
		if !expected {
			log.Printf("git status: detect operation state for %s: %v", dir, opErr)
		}
	} else {
		status.Operation = op
	}
	return status
}

// dedupePaths drops empty entries and duplicates while preserving order. Git
// can emit the same path more than once (an unmerged path appears twice in
// `git diff --name-only`), and the web renders these lists in order.
func dedupePaths(lines []string) []string {
	out := make([]string, 0, len(lines))
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

// HandleGitDiff returns the unified diff for the working tree.
// Supports ?path= filter for a single file and ?staged=true for the index.
func (h *Handler) HandleGitDiff(w http.ResponseWriter, r *http.Request) {
	pathFilter := r.URL.Query().Get("path")
	staged := r.URL.Query().Get("staged") == "true" || r.URL.Query().Get("staged") == "1"
	if host := hostParam(r); host != "" {
		rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		files, derr := remoteGitDiff(r.Context(), rw, staged, pathFilter)
		if derr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": derr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, files)
		return
	}
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
	if host := hostParam(r); host != "" {
		rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, remoteGitWorkspace(r.Context(), rw))
		return
	}
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
	if host := hostParam(r); host != "" {
		rw, lerr := h.remoteWorkFor(host, r.URL.Query().Get("project"))
		if lerr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": lerr.Error()})
			return
		}
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit = n
			}
		}
		limit = clampGitLogLimit(limit)
		commits, gerr := remoteGitLog(r.Context(), rw, limit)
		if gerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": gerr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, commits)
		return
	}
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
	if host := hostParam(r); host != "" {
		rw, rerr := h.remoteWorkFor(host, r.URL.Query().Get("project"))
		if rerr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": rerr.Error()})
			return
		}
		if rev == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit is required"})
			return
		}
		files, serr := remoteGitShow(r.Context(), rw, rev)
		if serr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": serr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, files)
		return
	}
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

// --- Stash ---

// GitStash describes one entry from `git stash list`. Index is the position in
// the stash reflog (stash@{index}); the web UI addresses entries by it, never
// by a caller-supplied ref string.
type GitStash struct {
	Index   int    `json:"index"`
	Ref     string `json:"ref"`
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// gitStashListFormat emits one NUL-delimited record per stash: the reflog
// selector (%gd → "stash@{0}"), full and short hash, the reflog subject
// (%gs → "WIP on main: …" or "On main: <message>"), author date and name.
const gitStashListFormat = "%gd%x00%H%x00%h%x00%gs%x00%aI%x00%an"

// stashRev builds the reflog ref for a stash index. The server formats the
// integer itself, so no caller-controlled text ever reaches the git argv.
func stashRev(index int) (string, error) {
	if index < 0 {
		return "", fmt.Errorf("invalid stash index")
	}
	return fmt.Sprintf("stash@{%d}", index), nil
}

// parseStashIndex extracts N from a "stash@{N}" reflog selector.
func parseStashIndex(ref string) (int, bool) {
	const prefix = "stash@{"
	if !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, "}") {
		return 0, false
	}
	n, err := strconv.Atoi(ref[len(prefix) : len(ref)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// parseGitStashList parses `git stash list --format=<gitStashListFormat>`,
// newest first (the order git emits). An unexpected selector falls back to
// emission order so the entry stays addressable.
func parseGitStashList(out string) []GitStash {
	stashes := make([]GitStash, 0)
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) < 6 {
			continue
		}
		idx, ok := parseStashIndex(fields[0])
		if !ok {
			idx = len(stashes)
		}
		stashes = append(stashes, GitStash{
			Index:   idx,
			Ref:     fields[0],
			Hash:    fields[1],
			Short:   fields[2],
			Message: fields[3],
			Date:    fields[4],
			Author:  fields[5],
		})
	}
	return stashes
}

// gitStashListForDir lists the stash entries of the repo at dir. A non-repo
// dir (or a repo with no stashes) yields an empty slice, never null.
func gitStashListForDir(dir string) []GitStash {
	out, err := gitRunInDir(dir, "stash", "list", "--format="+gitStashListFormat)
	if err != nil {
		return []GitStash{}
	}
	return parseGitStashList(out)
}

// gitStashShowForDir returns the parsed per-file diff of one stash entry.
// --include-untracked surfaces files that were untracked when the stash was
// created: `git stash push -u` stores them in the stash's third parent and
// without the flag they would be missing from the listing.
func gitStashShowForDir(dir string, index int) ([]GitDiffFile, error) {
	rev, err := stashRev(index)
	if err != nil {
		return nil, err
	}
	resolved, err := gitRunInDir(dir, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil || resolved == "" {
		return nil, fmt.Errorf("unknown stash")
	}
	out, err := gitRunInDir(dir, "stash", "show", "-p", "--no-color", "--include-untracked", rev)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return []GitDiffFile{}, nil
	}
	return parseUnifiedDiff(out), nil
}

// stashIndexParam parses the required ?index= stash selector. It writes the
// error response and returns ok=false when missing or malformed.
func stashIndexParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("index"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "stash index is required")
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "invalid stash index")
		return 0, false
	}
	return n, true
}

// HandleGitStashList lists the stash entries of the target repo. ?project= and
// ?host= follow the same convention as the other git endpoints.
func (h *Handler) HandleGitStashList(w http.ResponseWriter, r *http.Request) {
	if host := hostParam(r); host != "" {
		rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		stashes, lerr := remoteGitStashList(r.Context(), rw)
		if lerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": lerr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, stashes)
		return
	}
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}
	writeJSON(w, http.StatusOK, gitStashListForDir(dir))
}

// HandleGitStashShow returns the files changed by one stash entry (?index=N).
func (h *Handler) HandleGitStashShow(w http.ResponseWriter, r *http.Request) {
	index, ok := stashIndexParam(w, r)
	if !ok {
		return
	}
	if host := hostParam(r); host != "" {
		rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		files, serr := remoteGitStashShow(r.Context(), rw, index)
		if serr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": serr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, files)
		return
	}
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown project"})
		return
	}
	files, err := gitStashShowForDir(dir, index)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, files)
}
