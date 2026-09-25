package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// Phase 05, mutation half: the conflict-resolve and operation endpoints over
// the SSH/WSL transport.
//
// The local handlers are the contract; these are ports, not reimplementations.
// Every decision the local path makes from the index (is this path actually
// conflicted? is the chosen side a deletion?) is re-derived from the REMOTE
// status, never taken from the request body, and the pure helpers
// (parseUnmergedPorcelain, gitOperationStateFor, gitOperationCommand,
// hasConflictMarkers) are shared verbatim.

// remoteGitConflictSpec validates a caller-supplied conflict path for a remote
// project and returns its repo-relative spec.
//
// This is the same three-step gate the other remote mutations use, in the same
// order: join onto the remote root, confirm containment with remoteRelCheck,
// then reject shell metacharacters with remoteSafeSpec. remoteSafeSpec is the
// strict one — it refuses `:` along with quotes, semicolons, backticks,
// redirection, glob and control characters — and it is deliberately NOT
// relaxed here: it is the guard that stops a path from injecting into the
// remote shell. A filename it refuses (a colon, `[]`, `()`) is reported to
// the user as an error rather than silently skipped; see TODO.md.
func (h *Handler) remoteGitConflictSpec(w http.ResponseWriter, rw remoteWork, p string) (string, bool) {
	if p == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return "", false
	}
	abs := remoteAbsJoin(rw.Path, p)
	rel, err := remoteRelCheck(rw.Path, abs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	if rel == "." || rel == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return "", false
	}
	spec, err := remoteSafeSpec(rel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return spec, true
}

// remoteGitMutationCommand runs a git mutation on the remote host with the
// settings the conflict/operation endpoints require.
//
// The local path sets these through the process environment; here there is no
// process to configure, so they are expressed INSIDE the shell string, placed
// immediately before `git` (an assignment before `cd` would apply only to cd).
//
//   - GIT_LITERAL_PATHSPECS=1, so a caller path is always a filename and never
//     pathspec magic. `git --literal-pathspecs` would be equivalent, but the
//     env var keeps the command identical to the local one.
//   - GIT_EDITOR / GIT_SEQUENCE_EDITOR, so `merge --continue` cannot block on
//     an editor: the ssh session has no TTY, so a real editor would hang the
//     request until it times out. These are set as env, not via
//     `-c core.editor`, because an editor already exported in the remote shell
//     would otherwise win.
//
// GIT_OPTIONAL_LOCKS=0 is already applied by remoteGitCommand.
func remoteGitMutationCommand(rw remoteWork, args ...string) string {
	env := "GIT_LITERAL_PATHSPECS=1 GIT_EDITOR=true GIT_SEQUENCE_EDITOR=true"
	return strings.Replace(remoteGitCommand(rw.Path, args...), "GIT_OPTIONAL_LOCKS=0 git", env+" GIT_OPTIONAL_LOCKS=0 git", 1)
}

// remoteGitResolveConflict resolves a conflict on a remote (SSH/WSL) project.
//
// Port of gitResolveConflictLocal. It replaces the phase-04 501 stub, which
// existed so the request could never silently fall through to the LOCAL
// implementation and check out a same-named path on the wrong machine.
func (h *Handler) remoteGitResolveConflict(w http.ResponseWriter, r *http.Request, host string) {
	var req GitConflictResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	switch req.Resolution {
	case "ours", "theirs", "mark":
	default:
		writeError(w, http.StatusBadRequest, `resolution must be "ours", "theirs" or "mark"`)
		return
	}

	rw, ok := h.remoteGitDirForMutation(w, r, host)
	if !ok {
		return
	}
	spec, ok := h.remoteGitConflictSpec(w, rw, req.Path)
	if !ok {
		return
	}

	// Re-detect from the REMOTE index, never from the request: the per-side
	// flags decide whether the chosen side can be checked out at all.
	status := remoteGitStatus(r.Context(), rw)
	var conflict *GitConflict
	for i := range status.Conflicts {
		if status.Conflicts[i].Path == spec {
			conflict = &status.Conflicts[i]
			break
		}
	}
	if conflict == nil {
		writeError(w, http.StatusConflict, "path is not a conflicted file: "+spec)
		return
	}

	if req.Resolution == "mark" {
		if err := h.remoteGitMarkResolved(w, r.Context(), rw, spec); err != nil {
			return
		}
		writeJSON(w, http.StatusOK, remoteGitWorkspace(r.Context(), rw))
		return
	}

	var sideExists bool
	var sideFlag string
	if req.Resolution == "ours" {
		sideExists, sideFlag = conflict.Ours, "--ours"
	} else {
		sideExists, sideFlag = conflict.Theirs, "--theirs"
	}

	var args []string
	if sideExists {
		args = []string{"checkout", sideFlag, "--", spec}
	} else {
		// The chosen side is a deletion: accepting it means removing the path,
		// which `git checkout --ours/--theirs` cannot do.
		args = []string{"rm", "-f", "--", spec}
	}
	if err := remoteGitMutation(r.Context(), rw, remoteGitMutationCommand(rw, args...)); err != nil {
		slog.Error("remote git conflict resolve: side checkout failed",
			"host", host, "project", rw.Path, "path", spec, "resolution", req.Resolution, "args", args, "err", err)
		writeError(w, http.StatusInternalServerError, "git "+args[0]+" failed: "+err.Error())
		return
	}
	// `git rm` already stages the removal; checkout needs an explicit add.
	if sideExists {
		if err := remoteGitMutation(r.Context(), rw, remoteGitMutationCommand(rw, "add", "--", spec)); err != nil {
			slog.Error("remote git conflict resolve: stage resolved side failed",
				"host", host, "project", rw.Path, "path", spec, "resolution", req.Resolution, "err", err)
			writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, remoteGitWorkspace(r.Context(), rw))
}

// remoteGitMarkResolved is gitMarkResolved over the transport: it verifies the
// file has no conflict markers left, then stages it.
func (h *Handler) remoteGitMarkResolved(w http.ResponseWriter, ctx context.Context, rw remoteWork, spec string) error {
	abs := remoteAbsJoin(rw.Path, spec)
	// remoteReadFileCapped already distinguishes the three cases the local
	// path handles separately: a missing file (a deleted side, which the user
	// resolved by accepting the removal — nothing to scan, just stage it), a
	// directory, and a file over the scan limit.
	content, found, err := remoteReadFileCapped(ctx, rw, abs, conflictMarkerScanLimit)
	switch {
	case err == nil && !found:
		// Deleted side: stage the accepted removal.
		if err := remoteGitMutation(ctx, rw, remoteGitMutationCommand(rw, "add", "--", spec)); err != nil {
			slog.Error("remote git conflict mark: stage accepted deletion failed",
				"project", rw.Path, "path", spec, "err", err)
			writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
			return err
		}
		return nil
	case errors.Is(err, errTooLarge):
		writeError(w, http.StatusBadRequest,
			spec+" is too large to verify it has no conflict markers; resolve it with ours or theirs instead")
		return err
	case err != nil:
		// A read failure is not a license to stage the file: staging a file
		// whose markers were never checked would silently mark real
		// conflicts resolved.
		slog.Error("remote git conflict mark: read failed", "project", rw.Path, "path", spec, "err", err)
		writeError(w, http.StatusInternalServerError, "cannot read "+spec)
		return err
	}
	// The marker rule itself is the shared pure helper, so a remote "mark"
	// accepts and rejects exactly what a local one does — including a bare
	// `=======` line, which is a Markdown setext underline, not a marker.
	if hasConflictMarkers(string(content)) {
		writeError(w, http.StatusBadRequest, spec+" still contains conflict markers")
		return errors.New("conflict markers present")
	}
	if err := remoteGitMutation(ctx, rw, remoteGitMutationCommand(rw, "add", "--", spec)); err != nil {
		slog.Error("remote git conflict mark: stage failed", "project", rw.Path, "path", spec, "err", err)
		writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
		return err
	}
	return nil
}

// remoteGitOperation continues/aborts/skips an operation on a remote project.
//
// Port of the local operation handler, replacing the phase-04 501 stub. The
// action is mapped by the SHARED gitOperationCommand, and the kind is
// re-detected from the remote state, so a stale panel still cannot abort a
// different operation.
func (h *Handler) remoteGitOperation(w http.ResponseWriter, r *http.Request, host string) {
	var req GitOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Action == "" {
		writeError(w, http.StatusBadRequest, "action is required")
		return
	}
	if req.Kind == "" {
		writeError(w, http.StatusBadRequest, "kind is required")
		return
	}

	rw, ok := h.remoteGitDirForMutation(w, r, host)
	if !ok {
		return
	}
	// ONE status round trip, reused for both checks below. remoteGitStatus is
	// a whole batched ssh script, so calling it twice here would cost a second
	// round trip on every operation — the exact cost the batched probe exists
	// to avoid.
	status := remoteGitStatus(r.Context(), rw)
	detected := status.Operation
	if detected == nil {
		writeError(w, http.StatusConflict, "no git operation is in progress")
		return
	}
	if detected.Kind != req.Kind {
		writeError(w, http.StatusConflict,
			"git operation changed: it is now "+detected.Kind+", not "+req.Kind)
		return
	}
	args, ok := gitOperationCommand(detected.Kind, req.Action)
	if !ok {
		writeError(w, http.StatusBadRequest, req.Action+" is not supported for a "+detected.Kind)
		return
	}

	// Continuing with conflicts still present fails inside git with prose the
	// user has to decode, so it is refused up front with an actionable reason.
	// Skip stays allowed: skipping a conflicted step is its purpose.
	if req.Action == "continue" && len(status.Conflicts) > 0 {
		writeError(w, http.StatusConflict, "resolve the remaining conflicts before continuing")
		return
	}

	if err := remoteGitMutation(r.Context(), rw, remoteGitMutationCommand(rw, args...)); err != nil {
		slog.Error("remote git operation action failed",
			"host", host, "project", rw.Path, "kind", detected.Kind, "action", req.Action, "args", args, "err", err)
		// 409, not 500: git refusing is a legitimate answer to a legitimate
		// request (a hook vetoed the commit, the state moved under us).
		writeError(w, http.StatusConflict, "git "+args[0]+" failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, GitOperationResult{
		Workspace: remoteGitWorkspace(r.Context(), rw),
	})
}
