package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GitWorktree is one checkout of the project's repository, as reported by
// `git worktree list --porcelain`. Bare entries are skipped: they have no
// working tree to open.
type GitWorktree struct {
	Path      string `json:"path"`
	Head      string `json:"head"`
	Branch    string `json:"branch"`
	Detached  bool   `json:"detached"`
	Locked    bool   `json:"locked"`
	Prunable  bool   `json:"prunable"`
	IsMain    bool   `json:"is_main"`
	IsCurrent bool   `json:"is_current"`
}

// worktreesDirName is where the UI creates worktrees, relative to the main
// worktree root. It is the convention CLAUDE.md documents.
const worktreesDirName = ".worktrees"

// parseGitWorktreeList parses `git worktree list --porcelain`. Records are
// separated by blank lines. Git always lists the main worktree first, so the
// first record is IsMain even when a later record is skipped.
func parseGitWorktreeList(out string) []GitWorktree {
	var entries []GitWorktree
	var cur GitWorktree
	var open, bare bool
	seen := 0
	flush := func() {
		if open && !bare {
			entries = append(entries, cur)
		}
		cur, open, bare = GitWorktree{}, false, false
	}
	for _, line := range strings.Split(out, "\n") {
		key, val, _ := strings.Cut(line, " ")
		switch {
		case line == "":
			flush()
		case key == "worktree":
			flush()
			seen++
			cur.Path, cur.IsMain, open = val, seen == 1, true
		case key == "HEAD":
			cur.Head = val
		case key == "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			bare = true
		case key == "locked":
			cur.Locked = true
		case key == "prunable":
			cur.Prunable = true
		}
	}
	flush()
	return entries
}

// gitWorktreesForDir lists the checkouts of dir's repository, main worktree
// first, then by path. IsCurrent marks the checkout dir itself belongs to.
func gitWorktreesForDir(dir string) ([]GitWorktree, error) {
	out, err := gitRunInDir(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	current, err := gitRunInDir(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	entries := parseGitWorktreeList(out)
	for i := range entries {
		entries[i].IsCurrent = samePath(entries[i].Path, current)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsMain != entries[j].IsMain {
			return entries[i].IsMain
		}
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

// samePath compares two filesystem paths after cleaning and resolving
// symlinks, so /var vs /private/var spellings of one directory match. A path
// that cannot be resolved (for example a missing checkout) falls back to its
// cleaned spelling.
func samePath(a, b string) bool {
	return resolvePath(a) == resolvePath(b)
}

func resolvePath(p string) string {
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// rejectRemoteWorktree answers 501 for a remote host and reports whether the
// request may go on. Worktrees are local-only for now: resolving a remote
// request to this machine's repo would list or mutate the wrong checkout.
func rejectRemoteWorktree(w http.ResponseWriter, r *http.Request) bool {
	if hostParam(r) != "" {
		writeError(w, http.StatusNotImplemented, "worktrees are only available for local projects")
		return false
	}
	return true
}

// HandleGitWorktreeList lists the checkouts of the project's repository.
func (h *Handler) HandleGitWorktreeList(w http.ResponseWriter, r *http.Request) {
	if !rejectRemoteWorktree(w, r) {
		return
	}
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	entries, err := gitWorktreesForDir(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list worktrees: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// gitWorktreeCreateRequest is the body of POST /api/git/worktree. The server
// builds the checkout path itself from the branch name; the caller never
// supplies a filesystem path.
type gitWorktreeCreateRequest struct {
	Branch string `json:"branch"`
}

// HandleGitWorktreeCreate checks out Branch into .worktrees/<branch> under the
// main worktree. An existing local branch is checked out; otherwise a new
// branch is created from HEAD. Returns the refreshed worktree list.
func (h *Handler) HandleGitWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	if !rejectRemoteWorktree(w, r) {
		return
	}
	// Mutations use the same gate as every other git write (registered root or
	// extra allowed path), not the read resolver the list endpoint uses.
	dir, valid := h.mutationProjectDir(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	var req gitWorktreeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// check-ref-format echoes a valid name unchanged. It rejects a leading
	// "-" (which would otherwise read as an option) and "@{-N}" (which would
	// otherwise expand to another branch), so comparing the echo catches both.
	name, err := gitRunInDir(dir, "check-ref-format", "--branch", req.Branch)
	if err != nil || name != req.Branch {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid branch name %q", req.Branch))
		return
	}
	entries, err := gitWorktreesForDir(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list worktrees: "+err.Error())
		return
	}
	if len(entries) == 0 || !entries[0].IsMain {
		writeError(w, http.StatusBadRequest, "repository has no main checkout to add a worktree to (bare repository?)")
		return
	}
	mainRoot := entries[0].Path
	target := filepath.Join(mainRoot, worktreesDirName, filepath.FromSlash(req.Branch))
	if info, err := os.Stat(target); err == nil {
		// git accepts an existing EMPTY directory (e.g. a leftover parent of
		// a removed nested branch); anything else is a conflict.
		children, rerr := os.ReadDir(target)
		if !info.IsDir() || rerr != nil || len(children) > 0 {
			writeError(w, http.StatusConflict, "path already exists: "+target)
			return
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, "stat worktree path: "+err.Error())
		return
	}
	if err := ensureWorktreesDirExcluded(mainRoot); err != nil {
		writeError(w, http.StatusInternalServerError, "exclude worktrees dir: "+err.Error())
		return
	}
	exists, err := localBranchExists(mainRoot, req.Branch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "look up branch: "+err.Error())
		return
	}
	// Fork a new branch from the OPEN project's HEAD, not the main checkout's:
	// the command runs in mainRoot, so HEAD there would be the wrong commit.
	head, err := gitRunInDir(dir, "rev-parse", "HEAD")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve HEAD: "+err.Error())
		return
	}
	args := []string{"worktree", "add", "-b", req.Branch, target, head}
	if exists {
		args = []string{"worktree", "add", target, req.Branch}
	}
	if _, err := gitRunInDir(mainRoot, args...); err != nil {
		writeError(w, http.StatusConflict, "git worktree add failed: "+err.Error())
		return
	}
	h.writeWorktreeList(w, dir)
}

// gitWorktreeRemoveRequest is the body of POST /api/git/worktree/remove.
type gitWorktreeRemoveRequest struct {
	Path string `json:"path"`
}

// HandleGitWorktreeRemove removes one linked worktree. Path must exactly match
// a listed checkout; the main worktree and the project currently open are
// refused. Git itself refuses a dirty or locked tree, and that refusal is
// returned as 409. Returns the refreshed worktree list.
func (h *Handler) HandleGitWorktreeRemove(w http.ResponseWriter, r *http.Request) {
	if !rejectRemoteWorktree(w, r) {
		return
	}
	dir, valid := h.mutationProjectDir(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	var req gitWorktreeRemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entries, err := gitWorktreesForDir(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list worktrees: "+err.Error())
		return
	}
	var target *GitWorktree
	for i := range entries {
		if samePath(entries[i].Path, req.Path) {
			target = &entries[i]
			break
		}
	}
	switch {
	case target == nil:
		writeError(w, http.StatusBadRequest, "not a worktree of this repository: "+req.Path)
		return
	case target.IsMain:
		writeError(w, http.StatusBadRequest, "cannot remove the main worktree")
		return
	case target.IsCurrent:
		writeError(w, http.StatusConflict, "this worktree is the open project; switch to another project first")
		return
	}
	if target.Prunable {
		// The directory is gone, which `worktree remove` cannot handle; prune
		// drops the stale administrative entry instead.
		if _, err := gitRunInDir(dir, "worktree", "prune"); err != nil {
			writeError(w, http.StatusConflict, "git worktree prune failed: "+err.Error())
			return
		}
	} else if _, err := gitRunInDir(dir, "worktree", "remove", target.Path); err != nil {
		writeError(w, http.StatusConflict, "git worktree remove failed: "+err.Error())
		return
	}
	removeEmptyWorktreeParents(entries[0].Path, target.Path)
	h.writeWorktreeList(w, dir)
}

// removeEmptyWorktreeParents deletes now-empty parent directories of a removed
// worktree (left behind by nested branch names like feature/x), stopping at
// .worktrees itself. os.Remove only succeeds on an empty directory, so a
// non-empty or unrelated directory is never touched.
func removeEmptyWorktreeParents(mainRoot, removed string) {
	base := filepath.Join(mainRoot, worktreesDirName)
	for p := filepath.Dir(removed); strings.HasPrefix(p, base+string(filepath.Separator)); p = filepath.Dir(p) {
		if err := os.Remove(p); err != nil {
			// intentionally not logged: non-empty or already-gone parent is the normal stop condition
			return
		}
	}
}

// writeWorktreeList answers a mutation with the refreshed list, so the panel
// never shows a stale checkout set.
func (h *Handler) writeWorktreeList(w http.ResponseWriter, dir string) {
	entries, err := gitWorktreesForDir(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list worktrees: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// localBranchExists reports whether refs/heads/<branch> exists. for-each-ref
// exits 0 with no output when nothing matches, so this avoids mistaking a
// missing branch for a failed command. Its prefix match is compared exactly.
func localBranchExists(root, branch string) (bool, error) {
	ref := "refs/heads/" + branch
	out, err := gitRunInDir(root, "for-each-ref", "--format=%(refname)", ref)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if line == ref {
			return true, nil
		}
	}
	return false, nil
}

// ensureWorktreesDirExcluded adds .worktrees/ to the repo's info/exclude when
// it is not already there. The file is local and never committed, so this
// keeps the checkout directory out of `git status` without editing the
// project's .gitignore. The path comes from `git rev-parse --git-path`, which
// resolves correctly for linked worktrees.
func ensureWorktreesDirExcluded(root string) error {
	rel, err := gitRunInDir(root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	path := rel
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, rel)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	entry := worktreesDirName + "/"
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	prefix := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		prefix = "\n"
	}
	if _, err := f.WriteString(prefix + entry + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
