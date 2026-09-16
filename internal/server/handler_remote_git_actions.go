package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/remote"
)

// Remote variants of the git mutation handlers (handler_git_actions.go /
// handler_git_hunks.go). When ?host= is present the request is admitted
// only for a registered (host, path) remote project — the same admission
// rule as the terminal endpoint — and the git operation runs on the remote
// host through remoteWork.

// remotePrepareGitAction is prepareGitAction for remote projects: decodes
// the body, validates the host/path pair, confirms it is a git repository,
// and converts caller paths into safe repo-relative specs. specs are
// validated by remoteSafeSpec (no shell metacharacters) and joined onto the
// root when relative.
func (h *Handler) remotePrepareGitAction(w http.ResponseWriter, r *http.Request, host string) (rw remoteWork, specs []string, message string, ok bool) {
	var req gitActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return rw, nil, "", false
	}
	work, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return rw, nil, "", false
	}
	ctx := r.Context()
	if out, rerr := remoteRun(ctx, work, remoteGitCommand(work.Path, "rev-parse", "--is-inside-work-tree")); rerr != nil || strings.TrimSpace(out) != "true" {
		writeError(w, http.StatusBadRequest, "not a git repository")
		return rw, nil, "", false
	}
	in := append([]string{}, req.Paths...)
	if req.Path != "" {
		in = append(in, req.Path)
	}
	for _, p := range in {
		if p == "" {
			continue
		}
		abs := remoteAbsJoin(work.Path, p)
		rel, relErr := remoteRelCheck(work.Path, abs)
		if relErr != nil {
			writeError(w, http.StatusBadRequest, relErr.Error())
			return rw, nil, "", false
		}
		if rel == "." {
			continue
		}
		spec, specErr := remoteSafeSpec(rel)
		if specErr != nil {
			writeError(w, http.StatusBadRequest, specErr.Error())
			return rw, nil, "", false
		}
		specs = append(specs, spec)
	}
	return work, specs, req.Message, true
}

// remoteGitDirForMutation is gitDirForMutation for remote projects: it
// validates the (host, path) pair and confirms the remote side is a work
// tree, returning the repository toplevel as reported by the remote git
// (relative specs must resolve against it, not the requested subdir).
func (h *Handler) remoteGitDirForMutation(w http.ResponseWriter, r *http.Request, host string) (remoteWork, bool) {
	work, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return work, false
	}
	root, rerr := remoteRun(r.Context(), work, remoteGitCommand(work.Path, "rev-parse", "--show-toplevel"))
	if rerr != nil || strings.TrimSpace(root) == "" {
		writeError(w, http.StatusBadRequest, "not a git repository")
		return work, false
	}
	// Normalize to the repo toplevel (mirrors gitDirForMutation): a nested
	// remote project path must not silently broaden the operation's scope
	// beyond what the caller asked for.
	if toplevel := strings.TrimSpace(root); toplevel != "" && toplevel != work.Path {
		if _, cerr := remoteRelCheck(toplevel, work.Path); cerr == nil {
			work.Path = toplevel
		}
	}
	return work, true
}

// Local fallbacks: the original bodies extracted so the remote branch keeps
// the handlers' single-entry flow readable.
// remoteGitHunk applies one hunk operation over the transport. The same
// server-side re-derivation rules as the local path apply: the diff is
// re-fetched remotely, the selected hunk extracted, and the minimal patch
// piped into `git apply` via stdin (never argv).
func (h *Handler) remoteGitHunk(w http.ResponseWriter, r *http.Request, host string) {
	var req gitHunkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if out, rerr := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "rev-parse", "--is-inside-work-tree")); rerr != nil || strings.TrimSpace(out) != "true" {
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
	abs := remoteAbsJoin(rw.Path, req.Path)
	spec, rerr := remoteRelCheck(rw.Path, abs)
	if rerr != nil {
		writeError(w, http.StatusBadRequest, rerr.Error())
		return
	}
	if spec == "." {
		writeError(w, http.StatusBadRequest, "empty path")
		return
	}
	if _, serr := remoteSafeSpec(spec); serr != nil {
		writeError(w, http.StatusBadRequest, serr.Error())
		return
	}
	if !req.Staged && remoteIsUntrackedPath(ctx, rw, spec) {
		if req.Hunk != 0 {
			writeError(w, http.StatusBadRequest, "untracked files have a single hunk (index 0)")
			return
		}
		switch req.Action {
		case "stage":
			if _, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "add", "--", spec)); err != nil {
				writeError(w, http.StatusBadRequest, "git add failed: "+err.Error())
				return
			}
		case "discard":
			// Removing an untracked file remotely: rm -- the spec.
			if _, err := remoteRun(ctx, rw, "rm -f -- "+remote.ShellQuote(rw.Path+"/"+spec)); err != nil {
				writeError(w, http.StatusBadRequest, "failed to remove "+spec+": "+err.Error())
				return
			}
		default:
			writeError(w, http.StatusBadRequest, "cannot "+req.Action+" an untracked file")
			return
		}
		writeJSON(w, http.StatusOK, remoteGitWorkspace(ctx, rw))
		return
	}

	// Re-derive the diff remotely and rebuild the selected hunk.
	diffArgs := []string{"diff", "--no-color", "-u"}
	if req.Staged {
		diffArgs = append(diffArgs, "--cached")
	}
	diffArgs = append(diffArgs, "--", spec)
	out, derr := remoteRun(ctx, rw, remoteGitCommand(rw.Path, diffArgs...))
	out = strings.TrimRight(out, "\n")
	if derr != nil || out == "" {
		writeError(w, http.StatusBadRequest, "no diff found for "+spec)
		return
	}
	header, hunks := splitDiffHunks(out)
	if req.Hunk >= len(hunks) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("hunk index %d out of range (file has %d hunks)", req.Hunk, len(hunks)))
		return
	}
	var applyArgs []string
	switch req.Action {
	case "stage":
		if req.Staged {
			writeError(w, http.StatusBadRequest, "cannot stage a hunk from the staged diff")
			return
		}
		applyArgs = []string{"--cached"}
	case "unstage":
		if !req.Staged {
			writeError(w, http.StatusBadRequest, "cannot unstage a hunk from the unstaged diff")
			return
		}
		applyArgs = []string{"--cached", "--reverse"}
	case "discard":
		if req.Staged {
			writeError(w, http.StatusBadRequest, "discarding a staged hunk is not supported — unstage it first")
			return
		}
		applyArgs = []string{"--reverse"}
	}
	patch := strings.TrimRight(header, "\n") + "\n" + hunks[req.Hunk] + "\n"
	if err := remoteGitApply(ctx, rw, applyArgs, patch); err != nil {
		writeError(w, http.StatusBadRequest, "git apply failed (the file may have changed): "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, remoteGitWorkspace(ctx, rw))
}

// remoteIsUntrackedPath is isUntrackedPath over the transport.
func remoteIsUntrackedPath(ctx context.Context, rw remoteWork, spec string) bool {
	out, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "status", "--porcelain", "-u", "--", spec))
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

// remoteGitApply pipes patch into `git apply` on the remote host via stdin.
func remoteGitApply(ctx context.Context, rw remoteWork, opts []string, patch string) error {
	args := append([]string{"apply", "--whitespace=nowarn"}, opts...)
	args = append(args, "-")
	cmd, err := remote.ExecCommand(rw.Target, remoteGitCommand(rw.Path, args...))
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(patch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := runWithContext(ctx, cmd); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
