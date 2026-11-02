package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// GitRefActionRequest is the body of POST /api/git/ref-action.
type GitRefActionRequest struct {
	// Action names one entry of gitRefActions. The vocabulary is closed, so a
	// request cannot smuggle an arbitrary git subcommand.
	Action string `json:"action"`
	// Target is a commit hash or a branch name. Which one each action takes is
	// set in gitRefActions.
	Target string `json:"target"`
	// Name is the new branch or tag name, for the actions that create one.
	Name string `json:"name,omitempty"`
	// Confirm must be true for reset-hard, which discards uncommitted work.
	Confirm bool `json:"confirm,omitempty"`
}

// GitRefActionResult is the response of POST /api/git/ref-action.
type GitRefActionResult struct {
	Workspace GitWorkspace `json:"workspace"`
	Output    string       `json:"output"`
}

// GitRefActionConflict is the 409 body of POST /api/git/ref-action when git
// refuses. Error is git's own text. Workspace is the status after git stopped,
// so a conflicted cherry-pick or revert shows its unmerged paths and halted
// operation without a second request.
type GitRefActionConflict struct {
	Error     string       `json:"error"`
	Workspace GitWorkspace `json:"workspace"`
}

// refTargetRule says which target shapes an action accepts. A target that
// looks like a hash is a hash; any other target must be a branch name.
type refTargetRule int

const (
	refTargetAny    refTargetRule = iota // hash or branch
	refTargetHash                        // commit hash only
	refTargetBranch                      // branch name only
)

// refActionSpec is one row of gitRefActions.
type refActionSpec struct {
	target    refTargetRule
	needsName bool
	// writesWorktree marks an action that can overwrite or move files in the
	// checkout, so it is refused while a chat turn is running in the project.
	writesWorktree bool
	// argv builds the git arguments once the request has been validated.
	argv func(dir, target, name string) []string
}

// gitRefActions is the closed action table for POST /api/git/ref-action.
var gitRefActions = map[string]refActionSpec{
	"checkout": {target: refTargetAny, argv: func(dir, target, _ string) []string {
		// A remote-tracking name with no local branch of the same name needs
		// --track, or git would detach HEAD at that commit.
		if gitRefIsRemoteTrackingOnly(dir, target) {
			return []string{"checkout", "--track", target}
		}
		return []string{"checkout", target}
	}},
	"branch": {target: refTargetAny, needsName: true, argv: func(_, target, name string) []string {
		return []string{"branch", name, target}
	}},
	"branch-checkout": {target: refTargetAny, needsName: true, argv: func(_, target, name string) []string {
		return []string{"checkout", "-b", name, target}
	}},
	"branch-delete": {target: refTargetBranch, argv: func(_, target, _ string) []string {
		return []string{"branch", "-d", target}
	}},
	"merge": {target: refTargetAny, writesWorktree: true, argv: func(_, target, _ string) []string {
		return []string{"merge", "--no-edit", target}
	}},
	"cherry-pick": {target: refTargetHash, writesWorktree: true, argv: func(_, target, _ string) []string {
		return []string{"cherry-pick", target}
	}},
	"revert": {target: refTargetHash, writesWorktree: true, argv: func(_, target, _ string) []string {
		return []string{"revert", "--no-edit", target}
	}},
	"reset-soft": {target: refTargetAny, argv: func(_, target, _ string) []string {
		return []string{"reset", "--soft", target}
	}},
	"reset-mixed": {target: refTargetAny, argv: func(_, target, _ string) []string {
		return []string{"reset", "--mixed", target}
	}},
	"reset-hard": {target: refTargetAny, writesWorktree: true, argv: func(_, target, _ string) []string {
		return []string{"reset", "--hard", target}
	}},
	"tag": {target: refTargetAny, needsName: true, argv: func(_, target, name string) []string {
		return []string{"tag", name, target}
	}},
}

// HandleGitRefAction runs one ref action (checkout, branch, merge, reset, ...)
// on the project's repository. The action comes from gitRefActions, and every
// target and name is validated before git runs.
func (h *Handler) HandleGitRefAction(w http.ResponseWriter, r *http.Request) {
	if hostParam(r) != "" {
		writeError(w, http.StatusNotImplemented, "ref actions are not supported for remote projects yet")
		return
	}
	var req GitRefActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	spec, ok := gitRefActions[req.Action]
	if !ok {
		writeError(w, http.StatusBadRequest, req.Action+" is not a supported ref action")
		return
	}
	dir, ok := h.gitDirForMutation(w, r)
	if !ok {
		return
	}
	if reason := validateGitRefAction(dir, req.Action, spec, req); reason != "" {
		writeError(w, http.StatusBadRequest, reason)
		return
	}
	if spec.writesWorktree && h.projectTurnActive(dir) {
		writeError(w, http.StatusConflict, req.Action+" refused: a chat turn is running in this project; try again when it finishes")
		return
	}
	args := spec.argv(dir, req.Target, req.Name)
	out, err := runGitOperation(r.Context(), dir, args...)
	if err != nil {
		slog.Error("git ref action failed",
			"project", dir, "action", req.Action, "args", args, "err", err)
		// 409, not 500: git refusing (unmerged branch, conflict, existing tag)
		// is a legitimate answer to a legitimate request. git's own words are
		// the error, verbatim. The refreshed workspace rides along so a conflict
		// left by cherry-pick or revert is visible without another request.
		ws, wsErr := gitWorkspaceForDir(dir)
		if wsErr != nil {
			writeLocalGitError(w, dir, wsErr)
			return
		}
		writeJSON(w, http.StatusConflict, GitRefActionConflict{Error: out, Workspace: ws})
		return
	}
	ws, err := gitWorkspaceForDir(dir)
	if err != nil {
		writeLocalGitError(w, dir, err)
		return
	}
	writeJSON(w, http.StatusOK, GitRefActionResult{Workspace: ws, Output: out})
}

// validateGitRefAction returns the 400 reason for a request that git must not
// see, or "" when the request may run.
func validateGitRefAction(dir, action string, spec refActionSpec, req GitRefActionRequest) string {
	if action == "reset-hard" && !req.Confirm {
		return "reset-hard discards uncommitted work; send confirm:true to run it"
	}
	if req.Target == "" || strings.HasPrefix(req.Target, "-") {
		return "target must be a commit hash or branch name"
	}
	hash := gitRefTargetIsHash(dir, req.Target)
	switch {
	case spec.target == refTargetHash && !hash:
		return action + " needs a commit hash"
	case spec.target == refTargetBranch && hash:
		return action + " needs a branch name"
	case !hash && !gitBranchNameValid(dir, req.Target):
		return fmt.Sprintf("invalid target %q", req.Target)
	}
	// Every target must resolve to a commit. Without this, a file name that is
	// not a revision reaches `checkout`, which would restore that file and
	// discard its local edits.
	if !gitRefResolves(dir, req.Target+"^{commit}") {
		return fmt.Sprintf("target %q does not resolve to a commit", req.Target)
	}
	if !spec.needsName {
		if req.Name != "" {
			return action + " does not take a name"
		}
		return ""
	}
	if req.Name == "" {
		return "name is required"
	}
	if strings.HasPrefix(req.Name, "-") {
		return fmt.Sprintf("invalid name %q", req.Name)
	}
	if action == "tag" {
		if !gitRefFormatValid(dir, "refs/tags/"+req.Name) {
			return fmt.Sprintf("invalid tag name %q", req.Name)
		}
		return ""
	}
	if !gitBranchNameValid(dir, req.Name) {
		return fmt.Sprintf("invalid branch name %q", req.Name)
	}
	return ""
}

// gitBranchNameValid reports whether name is an acceptable branch name.
// check-ref-format echoes a valid name unchanged. It rejects a leading "-"
// (which would otherwise read as an option) and "@{-N}" (which would
// otherwise expand to another branch), so comparing the echo catches both.
func gitBranchNameValid(dir, name string) bool {
	out, err := gitRunInDir(dir, "check-ref-format", "--branch", name)
	// intentionally not logged: check-ref-format exits non-zero for every
	// invalid name, and that refusal is the validation answer.
	return err == nil && out == name
}

// gitRefFormatValid reports whether ref is a valid full ref name such as
// refs/tags/<name>. Without --branch, check-ref-format prints nothing on
// success, so the exit status is the answer.
func gitRefFormatValid(dir, ref string) bool {
	_, err := gitRunInDir(dir, "check-ref-format", ref)
	// intentionally not logged: check-ref-format exits non-zero for every
	// invalid name, and that refusal is the validation answer.
	return err == nil
}

// gitRefResolves reports whether rev names an object or ref. --quiet makes an
// unknown revision exit non-zero without output, which is the answer asked
// for, not a failure.
func gitRefResolves(dir, rev string) bool {
	_, err := gitRunInDir(dir, "rev-parse", "--verify", "--quiet", rev)
	// intentionally not logged: rev-parse --quiet exits non-zero for every
	// unknown revision, and that is the answer being asked for.
	return err == nil
}

// gitRefTargetIsHash reports whether target should be read as a commit hash.
// A hex-looking string is a hash only when no local branch has that name: git
// itself prefers the ref over an abbreviated object id, so a branch named cafe
// or dead is a branch.
func gitRefTargetIsHash(dir, target string) bool {
	return isHexHash(target) && !gitRefResolves(dir, "refs/heads/"+target)
}

// projectTurnActive reports whether a chat session rooted at dir has a turn in
// flight. The check is a snapshot: a turn that starts right after it returns is
// not seen, which the ref action accepts rather than holding every session's
// turn lock across git.
func (h *Handler) projectTurnActive(dir string) bool {
	root := resolvePath(dir)
	for _, e := range h.sessions.Snapshot() {
		if e.turnActive && e.ProjectRoot != "" && resolvePath(e.ProjectRoot) == root {
			return true
		}
	}
	return false
}

// gitRefIsRemoteTrackingOnly reports whether target names a remote-tracking
// branch with no local branch of the same name.
func gitRefIsRemoteTrackingOnly(dir, target string) bool {
	return gitRefResolves(dir, "refs/remotes/"+target) && !gitRefResolves(dir, "refs/heads/"+target)
}
