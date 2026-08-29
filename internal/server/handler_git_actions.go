package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// gitActionRequest is the shared body for git mutation endpoints. Paths may be
// project-relative (as returned by the file tree) or absolute paths inside the
// project root. An empty Paths list lets whole-repo operations (stash/commit)
// proceed. Path is a single-path convenience alias merged into Paths. The
// target project is taken from the ?project= query param (the same convention
// as the existing GET git endpoints) so POST routes stay consistent.
type gitActionRequest struct {
	Paths   []string `json:"paths"`
	Path    string   `json:"path"`
	Message string   `json:"message"`
}

// resolveRepoPath validates a single requested path against dir. It accepts
// paths that do not yet exist on disk (e.g. a deleted file being staged) by
// resolving the nearest existing ancestor, then confirming that ancestor — and
// the full lexical path — stay inside the repo root and outside .git after
// symlink resolution. It returns the absolute path (for diagnostics) and the
// repo-relative pathspec git should operate on.
func resolveRepoPath(dir, p string) (abs string, spec string, err error) {
	if p == "" {
		return "", "", fmt.Errorf("empty path")
	}
	// Resolve the repo root through symlinks first, then build the candidate
	// from the resolved root — comparing an EvalSymlinks(root) against a path
	// joined onto the unresolved root (e.g. /var vs /private/var on macOS)
	// would wrongly report an in-repo path as escaped.
	realDir := dir
	if rd, dErr := filepath.EvalSymlinks(dir); dErr == nil {
		realDir = rd
	}
	candidate := p
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(realDir, p)
	}
	candidate = filepath.Clean(candidate)

	// Walk up to the nearest existing ancestor so missing files (deletions,
	// not-yet-created paths) still validate against a real, resolvable parent.
	cur := candidate
	for {
		if _, statErr := os.Stat(cur); statErr == nil {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", "", fmt.Errorf("path %q escapes the project root", p)
		}
		cur = parent
	}

	realCur, cErr := filepath.EvalSymlinks(cur)
	if cErr != nil {
		return "", "", fmt.Errorf("cannot resolve path %q", p)
	}

	// Containment of the existing ancestor (symlink-resolved) — this is what
	// blocks a symlink inside the project from pointing at /etc, since the
	// ancestor's resolved location must itself sit inside the repo root.
	relCur, rErr := filepath.Rel(realDir, realCur)
	if rErr != nil || relCur == ".." || strings.HasPrefix(relCur, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path %q is outside the project root", p)
	}
	if relCur == ".git" || strings.HasPrefix(relCur, ".git"+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path %q is inside .git and cannot be modified", p)
	}

	// The full (possibly missing) path must also remain lexically inside dir.
	realCand := candidate
	if rc, cErr := filepath.EvalSymlinks(candidate); cErr == nil {
		realCand = rc
	}
	relFull, fErr := filepath.Rel(realDir, realCand)
	if fErr != nil || relFull == ".." || strings.HasPrefix(relFull, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path %q is outside the project root", p)
	}
	if relFull == ".git" || strings.HasPrefix(relFull, ".git"+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path %q is inside .git and cannot be modified", p)
	}

	// git operates with the repo-root-relative pathspec (cmd.Dir is set to dir,
	// which is the same logical location as realDir).
	spec, sErr := filepath.Rel(realDir, candidate)
	if sErr != nil {
		spec = candidate
	}
	return candidate, spec, nil
}

// resolveRepoPaths validates every requested path; empty input yields an empty
// slice (callers decide whether that is an error).
func resolveRepoPaths(dir string, paths []string) ([]string, error) {
	specs := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		_, spec, err := resolveRepoPath(dir, p)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// prepareGitAction validates the request, resolves the target repo directory
// via mutationProjectDir (registered + extra allowed roots, no global
// broadening), enforces that it is a git repository, and resolves+validates
// the requested paths to repo-relative pathspecs. It writes the error
// response and returns ok=false on any failure.
func (h *Handler) prepareGitAction(w http.ResponseWriter, r *http.Request) (dir string, specs []string, message string, ok bool) {
	var req gitActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", nil, "", false
	}
	dir, valid := h.mutationProjectDir(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "unknown project")
		return "", nil, "", false
	}
	// Detect a repository via git itself so worktrees and bare setups are
	// recognised, not just a literal .git directory entry.
	if out, err := gitRunInDir(dir, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		writeError(w, http.StatusBadRequest, "not a git repository")
		return "", nil, "", false
	}
	in := append([]string{}, req.Paths...)
	if req.Path != "" {
		in = append(in, req.Path)
	}
	specs, err := resolveRepoPaths(dir, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", nil, "", false
	}
	return dir, specs, req.Message, true
}

func (h *Handler) HandleGitStage(w http.ResponseWriter, r *http.Request) {
	dir, specs, _, ok := h.prepareGitAction(w, r)
	if !ok {
		return
	}
	if len(specs) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}
	args := append([]string{"add", "--"}, specs...)
	if _, err := gitRunInDir(dir, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gitStatusForDir(dir))
}

func (h *Handler) HandleGitUnstage(w http.ResponseWriter, r *http.Request) {
	dir, specs, _, ok := h.prepareGitAction(w, r)
	if !ok {
		return
	}
	if len(specs) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}
	args := append([]string{"reset", "--"}, specs...)
	if _, err := gitRunInDir(dir, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "git reset failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gitStatusForDir(dir))
}

// HandleGitDiscard reverts working-tree changes for the given tracked paths
// (the git equivalent of "Restore" in a file explorer). Untracked paths are
// ignored by git here; removing them is a file-system delete, exposed
// separately as the Delete action.
func (h *Handler) HandleGitDiscard(w http.ResponseWriter, r *http.Request) {
	dir, specs, _, ok := h.prepareGitAction(w, r)
	if !ok {
		return
	}
	if len(specs) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}
	args := append([]string{"checkout", "HEAD", "--"}, specs...)
	if _, err := gitRunInDir(dir, args...); err != nil {
		// A "pathspec did not match" error means some paths were untracked and
		// simply have nothing to discard — not a real failure.
		if !strings.Contains(err.Error(), "pathspec") {
			writeError(w, http.StatusInternalServerError, "git checkout failed: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, gitStatusForDir(dir))
}

func (h *Handler) HandleGitStash(w http.ResponseWriter, r *http.Request) {
	dir, specs, message, ok := h.prepareGitAction(w, r)
	if !ok {
		return
	}
	args := []string{"stash", "push"}
	if message != "" {
		args = append(args, "-m", message)
	}
	if len(specs) > 0 {
		args = append(args, "--")
		args = append(args, specs...)
	}
	if _, err := gitRunInDir(dir, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "git stash failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gitStatusForDir(dir))
}

func (h *Handler) HandleGitCommit(w http.ResponseWriter, r *http.Request) {
	dir, specs, message, ok := h.prepareGitAction(w, r)
	if !ok {
		return
	}
	if message == "" {
		writeError(w, http.StatusBadRequest, "commit message is required")
		return
	}
	args := []string{"commit", "-m", message}
	if len(specs) > 0 {
		args = append(args, "--")
		args = append(args, specs...)
	}
	if _, err := gitRunInDir(dir, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "git commit failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gitStatusForDir(dir))
}
