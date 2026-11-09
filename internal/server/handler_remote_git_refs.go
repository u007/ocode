package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/u007/ocode/internal/gitexec"
	"github.com/u007/ocode/internal/remote"
)

// errNotGitRepository is the validation failure a remote `rev-parse
// --show-toplevel` probe reports, matching the local "not a git repository".
var errNotGitRepository = errors.New("not a git repository")

// remoteGitRefAction is the remote-host counterpart of HandleGitRefAction: it
// runs the same closed action table against the remote repository through the
// ssh/WSL transport, mirroring the local validation, git environment and
// lock-retry policy.
//
// The remote host is the execution authority: every target and name is
// validated with a probe run ON the host, and every value that reaches the
// remote shell is single-quoted. `remoteGitCommand` joins argv with spaces, so
// a request string interpolated raw would be interpreted by the remote shell.
//
// The turn guard cannot be answered locally: projectTurnActive scans THIS
// server's sessions, and a remote project's chats are served by the host's own
// `ocode serve --remote`. A worktree-writing action therefore asks the host's
// server (`GET /api/git/turn-active`) and runs only on a definitive idle
// answer. A host that cannot answer refuses the action rather than mutating a
// checkout this process cannot see.
func (h *Handler) remoteGitRefAction(w http.ResponseWriter, r *http.Request, host string) {
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

	project := r.URL.Query().Get("project")
	rw, err := h.remoteMutationWorkFor(r.Context(), host, project)
	if err != nil {
		if isRemoteTransportError(err) {
			slog.Error("remote git ref action: resolve workspace", "host", host, "project", project, "err", err)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rw, err = remoteGitToplevelFor(r.Context(), rw)
	if err != nil {
		if isRemoteTransportError(err) {
			writeRemoteGitError(w, rw, err)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	reason, verr := validateGitRefActionRemote(r.Context(), rw, req.Action, spec, req)
	if verr != nil {
		if isRemoteTransportError(verr) {
			writeRemoteGitError(w, rw, verr)
			return
		}
		// The probe script only fails when the workspace itself cannot be
		// entered (the toplevel check ran moments earlier), so its stderr is
		// the answer, exactly like the local validator's git errors.
		writeError(w, http.StatusBadRequest, verr.Error())
		return
	}
	if reason != "" {
		writeError(w, http.StatusBadRequest, reason)
		return
	}

	// Same rule as the local path: a worktree-writing action waits for any
	// running chat turn in the project. Here the answer comes from the host's
	// own server. Unverifiable state refuses the action — never assume idle.
	if spec.writesWorktree {
		active, gerr := h.remoteProjectTurnActive(r.Context(), host, project)
		if gerr != nil {
			slog.Error("remote git ref action: cannot verify turn state",
				"host", host, "project", project, "err", gerr)
			var se *pulseRemoteStatusError
			switch {
			case errors.As(gerr, &se) && se.Status == http.StatusNotFound:
				writeError(w, http.StatusConflict,
					"the remote server on "+host+" cannot report turn state; restart that host's server to upgrade it")
			case errors.Is(gerr, errRemoteNotConnected):
				writeError(w, http.StatusConflict,
					"cannot verify whether an agent turn is running on "+host+"; connect the host and retry")
			default:
				writeError(w, http.StatusConflict,
					"cannot verify whether an agent turn is running on "+host+": "+gerr.Error())
			}
			return
		}
		if active {
			writeError(w, http.StatusConflict, req.Action+" refused: a chat turn is running in this project; try again when it finishes")
			return
		}
	}

	tracking := spec.trackingCheckout && remoteGitRefIsRemoteTrackingOnly(r.Context(), rw, req.Target)
	args := spec.argv(req.Target, req.Name, tracking)
	// remoteGitCommand does not quote argv, so quote every word here. Quoting a
	// literal subcommand or flag is harmless in POSIX sh; quoting is the only
	// thing that stops a request value from being interpreted by the shell.
	for i := range args {
		args[i] = remote.ShellQuote(args[i])
	}

	out, err := remoteGitRefRun(r.Context(), rw, remoteGitRefCommand(rw, args))
	if err != nil {
		slog.Error("remote git ref action failed",
			"host", rw.Target.String(), "project", rw.Path, "action", req.Action, "args", args, "err", err)
		if isRemoteTransportError(err) {
			writeRemoteGitError(w, rw, err)
			return
		}
		// 409, not 500: git refusing (unmerged branch, conflict, existing tag)
		// is a legitimate answer to a legitimate request, exactly as locally.
		ws, wsErr := remoteGitWorkspace(r.Context(), rw)
		if wsErr != nil {
			writeRemoteGitError(w, rw, wsErr)
			return
		}
		writeJSON(w, http.StatusConflict, GitRefActionConflict{Error: out, Workspace: ws})
		return
	}
	ws, err := remoteGitWorkspace(r.Context(), rw)
	if err != nil {
		writeRemoteGitError(w, rw, err)
		return
	}
	writeJSON(w, http.StatusOK, GitRefActionResult{Workspace: ws, Output: out})
}

// remoteGitToplevelFor confirms rw.Path is inside a git work tree and
// normalises it to the repository toplevel, matching remoteGitDirForMutation.
// The ref table is repo-wide, so a nested project path must not run git from
// the subdirectory.
func remoteGitToplevelFor(ctx context.Context, rw remoteWork) (remoteWork, error) {
	out, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path, "rev-parse", "--show-toplevel"))
	if err != nil {
		if isRemoteTransportError(err) {
			return rw, err
		}
		return rw, errNotGitRepository
	}
	toplevel := strings.TrimSpace(out)
	if toplevel == "" {
		return rw, errNotGitRepository
	}
	if toplevel != rw.Path {
		if _, cerr := remoteRelCheck(toplevel, rw.Path); cerr == nil {
			rw.Path = toplevel
		}
	}
	return rw, nil
}

// remoteTurnCheckTimeout bounds the pre-mutation turn query. Short on purpose:
// the check must not park the request on a slow tunnel, and no answer refuses
// the action.
const remoteTurnCheckTimeout = 3 * time.Second

// remoteProjectTurnActive asks the host's own server whether a chat turn is
// running in project. It never dials: a host with no live workspace connection
// is reported as errRemoteNotConnected, which the caller turns into a refusal.
func (h *Handler) remoteProjectTurnActive(ctx context.Context, host, project string) (bool, error) {
	if h.remoteHosts == nil {
		return false, errRemoteNotConnected
	}
	ws, ok := h.remoteHosts.connectedWorkspace(host, h.remoteWorkspacePort(host, project))
	if !ok {
		return false, errRemoteNotConnected
	}
	var resp struct {
		TurnActive bool `json:"turn_active"`
	}
	if err := remoteCallCtx(ctx, ws, host, http.MethodGet, "/api/git/turn-active",
		url.Values{"project": {project}}, remoteTurnCheckTimeout, 64<<10, &resp); err != nil {
		return false, err
	}
	return resp.TurnActive, nil
}

// remoteWorkspacePort is the saved SSH port for the project, falling back to
// the registered root a sub-path sits under. Zero means the default port.
func (h *Handler) remoteWorkspacePort(host, project string) int {
	if e, ok := h.remoteProjectEntry(host, project); ok {
		return e.RemotePort
	}
	if root, ok := h.remoteTreeRoot(host, project); ok {
		if e, ok := h.remoteProjectEntry(host, root); ok {
			return e.RemotePort
		}
	}
	return 0
}

// Fixed order of the batched validation script's output lines. The rule table
// asks for at most these five probes; reading them positionally is what lets
// one ssh round trip replace up to four.
const (
	refProbeBranchAtTarget = iota // refs/heads/<target> resolves
	refProbeTargetCommit          // <target>^{commit} resolves
	refProbeTargetFormat          // check-ref-format --branch <target> echoes target
	refProbeNameFormat            // check-ref-format --branch <name> echoes name
	refProbeTagFormat             // check-ref-format refs/tags/<name> succeeds
)

// remoteRefProbes answers every probe the shared rule table can ask for this
// request in ONE ssh round trip, mirroring remoteGitStatus's batched script.
// Without it the worst case (branch/tag with a name) costs four separate ssh
// spawns before the action itself.
//
// A probe never fails the script: each prints its own 0/1 answer and the last
// command exits 0, so a refusal stays a validation answer instead of a 5xx.
// Every interpolated value is single-quoted — the whole line is interpreted by
// the remote shell, so an unquoted request target would be command injection.
// A missing or unparseable line reads as "no", which refuses the action.
func remoteRefProbes(ctx context.Context, rw remoteWork, target, name string) (refProbes, error) {
	q := remote.ShellQuote
	var b strings.Builder
	b.WriteString("cd " + shellQuotePathPOSIX(rw.Path) + " && export GIT_OPTIONAL_LOCKS=0\n")
	b.WriteString("git rev-parse --verify --quiet " + q("refs/heads/"+target) + " >/dev/null 2>&1; printf '%s\\n' \"$?\"\n")
	b.WriteString("git rev-parse --verify --quiet " + q(target+"^{commit}") + " >/dev/null 2>&1; printf '%s\\n' \"$?\"\n")
	b.WriteString("v=$(git check-ref-format --branch " + q(target) + " 2>/dev/null); [ \"$v\" = " + q(target) + " ]; printf '%s\\n' \"$?\"\n")
	b.WriteString("v=$(git check-ref-format --branch " + q(name) + " 2>/dev/null); [ \"$v\" = " + q(name) + " ]; printf '%s\\n' \"$?\"\n")
	b.WriteString("git check-ref-format " + q("refs/tags/"+name) + " >/dev/null 2>&1; printf '%s\\n' \"$?\"\n")
	out, err := remoteRun(ctx, rw, b.String())
	if err != nil {
		return refProbes{}, err
	}
	return parseRemoteRefProbes(out, target, name), nil
}

// parseRemoteRefProbes maps the batched script's fixed-order answers onto the
// rule table's probes. A short or garbled line reads as "no" — the refusing
// direction — so a truncated reply can never turn a rejected action into an
// accepted one. The rule table only asks about this request's target and name;
// anything else is refused rather than guessed.
func parseRemoteRefProbes(out, target, name string) refProbes {
	lines := strings.Split(out, "\n")
	ok := func(i int) bool { return i < len(lines) && strings.TrimSpace(lines[i]) == "0" }
	return refProbes{
		refResolves: func(rev string) bool {
			switch rev {
			case "refs/heads/" + target:
				return ok(refProbeBranchAtTarget)
			case target + "^{commit}":
				return ok(refProbeTargetCommit)
			default:
				return false
			}
		},
		branchNameValid: func(n string) bool {
			switch {
			case n == target:
				return ok(refProbeTargetFormat)
			case n == name:
				return ok(refProbeNameFormat)
			default:
				return false
			}
		},
		refFormatValid: func(ref string) bool {
			return ref == "refs/tags/"+name && ok(refProbeTagFormat)
		},
	}
}

// validateGitRefActionRemote validates a request against the host's repository.
// It supplies batched remote probes to the shared rule table, so the 400
// reasons and their order are byte-identical to the local path. A transport
// failure is returned as an error, never as a validation refusal.
func validateGitRefActionRemote(ctx context.Context, rw remoteWork, action string, spec refActionSpec, req GitRefActionRequest) (string, error) {
	probes, err := remoteRefProbes(ctx, rw, req.Target, req.Name)
	if err != nil {
		return "", err
	}
	return validateGitRefActionUsing(probes, action, spec, req), nil
}

// remoteGitRefCommand builds the mutation command with the environment the
// local path gets from gitOperationEnv, expressed inline because there is no
// remote process to configure. remoteGitCommand supplies the quoted `cd` and
// GIT_OPTIONAL_LOCKS=0; the rest mirror gitOperationEnv so a merge or reset
// cannot open an editor or prompt for credentials over a TTY-less ssh session.
// Callers must quote the argv themselves (args are already quoted here).
func remoteGitRefCommand(rw remoteWork, args []string) string {
	return remoteGitCommandEnv(rw.Path, []string{
		"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "GIT_MERGE_AUTOEDIT=no",
		"GIT_TERMINAL_PROMPT=0", "GIT_LITERAL_PATHSPECS=1",
	}, args...)
}

// remoteGitRefRun executes a ref mutation with the local git-operation bound
// (gitOperationTimeout) and the local lock-retry policy, mirroring
// runGitOperation.
func remoteGitRefRun(ctx context.Context, rw remoteWork, command string) (string, error) {
	var out string
	err := gitexec.WithLockRetry(func() error {
		o, rerr := remoteRunGitOperation(ctx, rw, command)
		out = o
		// Fold git's words into the error exactly as runGitOperation does.
		// The remote exit error is only "exit status 128"; LockHeld matches on
		// err.Error(), so without this the retry never fires on a held
		// index.lock. WithOutput keeps the transport error type (errors.As),
		// so isRemoteTransportError still classifies it.
		return gitexec.WithOutput(rerr, o, "")
	})
	return out, err
}

// remoteGitRunCmd runs one read-only git probe on the host. Every argument is
// single-quoted: remoteGitCommand does not quote argv, and the remote shell
// interprets the whole command line, so a request-controlled target or name
// interpolated raw would execute arbitrary commands. Quoting fixed literals
// too is harmless in POSIX sh.
func remoteGitRunCmd(ctx context.Context, rw remoteWork, args ...string) (string, error) {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = remote.ShellQuote(a)
	}
	return remoteRun(ctx, rw, remoteGitCommand(rw.Path, quoted...))
}

func remoteGitRefResolves(ctx context.Context, rw remoteWork, rev string) bool {
	_, err := remoteGitRunCmd(ctx, rw, "rev-parse", "--verify", "--quiet", rev)
	// intentionally not logged: rev-parse --quiet exits non-zero for every
	// unknown revision, and that is the answer being asked for.
	return err == nil
}

func remoteGitRefIsRemoteTrackingOnly(ctx context.Context, rw remoteWork, target string) bool {
	return remoteGitRefResolves(ctx, rw, "refs/remotes/"+target) && !remoteGitRefResolves(ctx, rw, "refs/heads/"+target)
}
