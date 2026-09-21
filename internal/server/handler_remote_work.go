package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// remoteWork is a resolved remote work target: the parsed host target and
// the verbatim remote project path. Resolved by remoteWorkFor from the
// ?host= query param plus the saved projects store; every remote-aware
// git/files endpoint goes through it.
type remoteWork struct {
	Target remote.Target
	Path   string
}

// remoteProjectEntry looks up a saved project entry by (host, path) pair,
// mirroring remoteProjectRegistered's matching (verbatim path, canonical
// host). Returns ok=false when not registered.
func (h *Handler) remoteProjectEntry(host, path string) (projects.Project, bool) {
	if h.projects == nil || host == "" || path == "" {
		return projects.Project{}, false
	}
	for _, p := range h.projects.List() {
		if p.Host == host && p.Path == path {
			return p, true
		}
	}
	return projects.Project{}, false
}

// remoteWorkFor resolves the ?host= + ?project= pair (or the JSON body's
// host/project fields, handled by callers) into a validated remoteWork.
// The pair must be a registered remote project — the same admission rule
// the terminal endpoint applies (remoteProjectRegistered) — so the endpoint
// can't be aimed at unregistered hosts. ParseTarget accepts the same
// canonical strings the projects store keys on ("[user@]host" with an
// optional :port suffix, or "wsl:<distro>").
func (h *Handler) remoteWorkFor(host, path string) (remoteWork, error) {
	if host == "" {
		return remoteWork{}, fmt.Errorf("host is required")
	}
	if path == "" {
		return remoteWork{}, fmt.Errorf("path is required")
	}
	target, err := remote.ParseTarget(host)
	if err != nil {
		return remoteWork{}, fmt.Errorf("invalid remote host: %w", err)
	}
	// A saved entry may carry an explicit port; honor it when the query's
	// host string did not include one.
	if entry, ok := h.remoteProjectEntry(target.String(), path); ok {
		if target.Port == 0 && entry.RemotePort > 0 {
			target.Port = entry.RemotePort
		}
	} else {
		return remoteWork{}, fmt.Errorf("host/project_path is not a remote project registered with this server")
	}
	if err := remote.ValidateTargetOS(target); err != nil {
		return remoteWork{}, err
	}
	return remoteWork{Target: target, Path: path}, nil
}

// hostParam extracts the remote ?host= routing parameter. Empty means the
// local-filesystem path (the historical behavior).
func hostParam(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("host"))
}

// remoteRun executes one command on the remote target and returns trimmed
// stdout. stderr is surfaced in the error, mirroring gitRunInDir. A
// non-zero remote exit yields an error carrying the remote stderr.
// Transport-level failures (ssh won't start, connection dead, command
// timed out) are wrapped in *remoteTransportError so HTTP handlers can
// map them to 5xx instead of 400 — see isRemoteTransportError.
func remoteRun(ctx context.Context, rw remoteWork, command string) (string, error) {
	return remoteRunRaw(ctx, rw, command)
}

// remoteTransportError marks transport-level remote failures: the ssh
// process itself failed (not the remote command's exit status), or the
// run was killed by the timeout/cancellation bound. Callers use
// isRemoteTransportError to pick 502/500 over 400 for these.
type remoteTransportError struct {
	msg string
}

func (e *remoteTransportError) Error() string { return e.msg }

// isRemoteTransportError reports whether err is (or wraps) a transport
// failure rather than a remote-command error carrying stderr text.
func isRemoteTransportError(err error) bool {
	if err == nil {
		return false
	}
	var te *remoteTransportError
	if errors.As(err, &te) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "remote command timed out after") ||
		strings.Contains(msg, "remote command cancelled")
}

// remoteRunRaw is remoteRun without output trimming (the raw stdout), for
// commands whose payload shape matters byte-for-byte (diffs).
func remoteRunRaw(ctx context.Context, rw remoteWork, command string) (string, error) {
	cmd, err := remote.ExecCommand(rw.Target, command)
	if err != nil {
		return "", &remoteTransportError{msg: err.Error()}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := runWithContext(ctx, cmd); err != nil {
		if isRemoteTransportError(err) {
			return "", err
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		if isRemoteTransportError(fmt.Errorf("%s", msg)) {
			return "", &remoteTransportError{msg: msg}
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// runWithContext runs cmd, aborting it when ctx is done. Context support is
// attached at start time (CommandContext) so an abandoned web request can't
// leave an ssh child running; without a deadline (plain requests) the exec
// is bounded by remoteExecTimeout anyway.
func runWithContext(ctx context.Context, cmd *exec.Cmd) error {
	return runBounded(ctx, cmd, remoteExecTimeout)
}

// remoteShellResult is the outcome of running one shell command on a remote
// target, shaped like internal/shell.Result so the /api/shell handler can
// answer local and remote requests identically.
type remoteShellResult struct {
	Output   string
	ExitCode int
	Err      error
}

// remoteShellRun executes command on rw through the remote host's OWN login
// shell (remote.LoginShellScript), with rw.Path as the working directory.
//
// The remote shell is resolved remotely, per the "remote server is the
// execution authority" rule in docs/superpowers/specs/2026-09-11-desktop-remote-ssh-workspace-design.md:
// the local process's $SHELL and the local /etc/shells describe this machine,
// not the target, so the command runs under a shell the target actually has. A
// non-zero remote exit is reported via ExitCode, never Err, mirroring
// internal/shell.Run; Err is reserved for transport failures (ssh could not
// start, the connection died, the command exceeded the bound).
//
// timeout is the caller's budget: the `!` prefix passes shell.DefaultTimeout so
// a remote command gets the same allowance as a local one (the shorter
// remoteExecTimeout is a git/files bound, too tight for interactive commands).
func remoteShellRun(ctx context.Context, rw remoteWork, command string, timeout time.Duration) remoteShellResult {
	cmd, err := remote.ExecCommand(rw.Target, remote.CdedLoginShellScript(rw.Path, command))
	if err != nil {
		return remoteShellResult{ExitCode: 1, Err: err}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := runBounded(ctx, cmd, timeout); err != nil {
		out := joinedShellOutput(stdout.String(), stderr.String())
		// A remote command that ran and exited non-zero is reported via
		// ExitCode with its output, never Err — the same contract
		// internal/shell.Run documents. Everything else (the ssh process
		// failed to start, the connection died, the bound fired) is a genuine
		// error with no usable output of its own.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return remoteShellResult{Output: out, ExitCode: exitErr.ExitCode()}
		}
		return remoteShellResult{Output: out, ExitCode: 1, Err: err}
	}
	return remoteShellResult{Output: joinedShellOutput(stdout.String(), stderr.String())}
}

// joinedShellOutput mirrors internal/shell's combined-stream behavior: callers
// render one output string, so stdout and stderr are concatenated (stdout
// first, since a shell typically emits its useful payload there).
func joinedShellOutput(stdout, stderr string) string {
	if stderr == "" {
		return stdout
	}
	if stdout == "" {
		return stderr
	}
	return stdout + stderr
}

// remoteShellCacheEntry is one memoized remote shell probe. A probe is an ssh
// round-trip, so it is cached per host for the process lifetime: a host's
// installed shells change far less often than the Settings UI polls this.
// There is no TTL by design — a deliberately changed remote shell is a
// reconnect, and a stale answer is still validated remote-side at exec time
// (the launch/login loops test -x themselves), so caching can never cause a
// failed command, only a slightly stale picker.
type remoteShellCacheEntry struct {
	info remote.RemoteShellInfo
}

// remoteShellProbeTimeout bounds one shell probe. The probe is a handful of
// `[ -x ]` tests plus an /etc/shells read, so anything near this is a dead or
// hung ssh, not a slow remote.
const remoteShellProbeTimeout = 15 * time.Second

// remoteShellProbeFn runs remote.ShellProbeCommand on t and returns its
// stdout. A package var so tests can substitute canned probe output instead
// of shelling out to a real ssh. The default is kind-aware (remote.ExecCommand
// routes a WSL target through wsl.exe) and bounded: a hung ssh must not pin
// the HTTP request goroutine, and the output buffers are capped so a runaway
// remote cannot grow the process without limit.
var remoteShellProbeFn = func(ctx context.Context, t remote.Target) (string, error) {
	cmd, err := remote.ExecCommand(t, remote.ShellProbeCommand())
	if err != nil {
		return "", err
	}
	stdout := &remote.LimitedBuffer{Max: remote.MaxExecOutput}
	cmd.Stdout = stdout
	cmd.Stderr = &remote.LimitedBuffer{Max: remote.MaxExecOutput}
	if err := runBounded(ctx, cmd, remoteShellProbeTimeout); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

// remoteShellCacheKey identifies a probe target. Target.String() drops the SSH
// port, and two registered targets on one host that differ only by port are
// different machines (or containers) with different shells, so the port is
// part of the key.
func remoteShellCacheKey(t remote.Target) string {
	if t.Port > 0 {
		return t.String() + ":" + strconv.Itoa(t.Port)
	}
	return t.String()
}

// remoteShellInfo probes (or returns the cached probe for) the shells a remote
// target can actually run. The probe runs outside h.mu because it blocks on
// ssh; the cache has its own mutex for that reason.
//
// A probe failure (including the bound firing) is not an error: the result
// degrades to /bin/sh, is cached like any other answer, and callers render it
// as "the remote reported no shells".
func (h *Handler) remoteShellInfo(ctx context.Context, rw remoteWork) remote.RemoteShellInfo {
	key := remoteShellCacheKey(rw.Target)

	h.remoteShellCacheMu.Lock()
	if h.remoteShellCache != nil {
		if entry, ok := h.remoteShellCache[key]; ok {
			h.remoteShellCacheMu.Unlock()
			return entry.info
		}
	}
	h.remoteShellCacheMu.Unlock()

	// Probe without holding the lock: two concurrent first-requests for the
	// same host may both ssh, which is harmless (idempotent, read-only) and
	// strictly better than serializing every other host behind one slow probe.
	info := remote.RemoteShellInfo{Default: "/bin/sh"}
	if out, err := remoteShellProbeFn(ctx, rw.Target); err != nil {
		log.Printf("[remote] shell probe failed for %s; using /bin/sh: %v", key, err)
	} else {
		info = remote.ParseShellProbe(out)
	}

	h.remoteShellCacheMu.Lock()
	if h.remoteShellCache == nil {
		h.remoteShellCache = map[string]remoteShellCacheEntry{}
	}
	h.remoteShellCache[key] = remoteShellCacheEntry{info: info}
	h.remoteShellCacheMu.Unlock()
	return info
}

// runBounded runs cmd with a bounded lifetime: a hung network filesystem or
// dead ssh must not pin the request goroutine forever. The command is
// rebuilt through CommandContext when possible — but callers hand us an
// already-built *exec.Cmd, so the bound is enforced with a watcher goroutine
// that kills the process when the deadline fires (no cmd.Cancel mutation,
// which os/exec only honors on CommandContext-created commands).
func runBounded(ctx context.Context, cmd *exec.Cmd, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := cmd.Start(); err != nil {
		return &remoteTransportError{msg: err.Error()}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			<-done // reap
		}
		if ctx.Err() == context.Canceled && ctx != context.Background() {
			return &remoteTransportError{msg: "remote command cancelled"}
		}
		return &remoteTransportError{msg: fmt.Sprintf("remote command timed out after %s", timeout)}
	}
}

// remoteExecTimeout bounds every remote exec. Git status/diff on a modest
// repo is well under this; fetch/pull/push get their own longer bound via
// remoteRunNetwork.
const remoteExecTimeout = 30 * time.Second

// remoteRunNetwork executes a git network command (fetch/pull/push/reset)
// on the remote target with a longer bound than plain execs. The remote
// shell runs the command with the same non-interactive defenses as the
// local runGitNetwork path (GIT_TERMINAL_PROMPT=0, GIT_EDITOR=true, ...):
// a divergent git pull that needs a merge message and a missing
// credential must fail fast instead of hanging on an invisible prompt on
// the remote side with no tty (bounded by remoteNetworkTimeout only as a
// backstop). BatchMode ssh still guards the ssh layer itself.
func remoteRunNetwork(ctx context.Context, rw remoteWork, command string) error {
	command = "GIT_TERMINAL_PROMPT=0 GIT_EDITOR=true GIT_SEQUENCE_EDITOR=true GIT_MERGE_AUTOEDIT=no " + command
	cmd, err := remote.ExecCommand(rw.Target, command)
	if err != nil {
		return &remoteTransportError{msg: err.Error()}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := runBounded(ctx, cmd, remoteNetworkTimeout); err != nil {
		if isRemoteTransportError(err) {
			return err
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// remoteNetworkTimeout bounds fetch/pull/push/reset (which may transfer).
const remoteNetworkTimeout = 120 * time.Second

// remoteGitCommand builds "cd <dir> && GIT_OPTIONAL_LOCKS=0 git <args...>" with
// dir shell-quoted (the remote shell interprets it). Args are NOT quoted:
// callers pass repo-relative pathspecs already validated to contain no shell
// metachars (see remotePathSpec), or fixed literals.
//
// The env prefix is the same opt-out the local helpers apply (gitexec.Env):
// without it ocode's remote status/diff probes refresh the index on the host
// and take its .git/index.lock as a side effect, contending with the user's own
// git on that machine. A leading VAR=value is POSIX sh syntax and both remote
// shells (ssh → POSIX sh, WSL → sh) accept it; mandatory locks are unaffected.
func remoteGitCommand(dir string, args ...string) string {
	parts := make([]string, 0, len(args)+4)
	parts = append(parts, "cd", shellQuotePathPOSIX(dir), "&&", "GIT_OPTIONAL_LOCKS=0", "git")
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

// shellQuotePathPOSIX quotes a path for use in a remote shell command. The
// remote for an SSH project is a POSIX shell; for WSL it is sh inside the
// distro — same quoting rules.
func shellQuotePathPOSIX(p string) string {
	return remote.ShellQuotePath(p)
}

// remoteSafeSpec validates a caller-supplied path spec before it is embedded
// in a remote shell command. Remote pathspecs must be repo-relative and free
// of shell metacharacters: the resolver quotes nothing here (unlike the
// local path, a remote pathspec with a quote could inject). Valid specs are
// returned unchanged; anything containing quotes, semicolons, redirection,
// variable expansion, glob-newline, or control characters is rejected.
func remoteSafeSpec(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	// Reject absolute paths: specs must be repo-relative (the frontend sends
	// tree-anchored relative paths). Windows-style "C:..." forms are covered
	// by the ":" rejection below.
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("remote pathspecs must be repo-relative")
	}
	for _, r := range p {
		switch {
		case r < 0x20 || r == 0x7f:
			return "", fmt.Errorf("path contains control characters")
		case strings.ContainsRune("'\";`$&|<>\\!*?[](){}#:", r):
			return "", fmt.Errorf("path contains unsupported characters: %q", p)
		}
	}
	return p, nil
}

// remoteAbsJoin resolves a possibly-remote-project-relative path against the
// remote project root. Relative paths from the frontend (tree Path values)
// become "<root>/<rel>"; absolute remote paths pass through unchanged
// (callers still bound them to the root with remoteRelCheck).
func remoteAbsJoin(root, p string) string {
	if p == "" {
		return root
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return root + "/" + p
}

// remoteRelCheck validates that p stays inside root and returns the
// repo-relative spec (lexical — the remote side has no EvalSymlinks here;
// symlink containment on remote trees is accepted as a documented residual,
// mirroring the file tree's lexical handling of linked folders).
func remoteRelCheck(root, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if !strings.HasPrefix(p, root+"/") {
		if p == root {
			return ".", nil
		}
		return "", fmt.Errorf("path is outside the project root")
	}
	rel := p[len(root)+1:]
	if rel == "" {
		return ".", nil
	}
	if rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || strings.HasSuffix(rel, "/..") {
		return "", fmt.Errorf("path is outside the project root")
	}
	if rel == ".git" || strings.HasPrefix(rel, ".git/") || strings.HasSuffix(rel, "/.git") || strings.Contains(rel, "/.git/") {
		return "", fmt.Errorf("path is inside .git and cannot be modified")
	}
	return rel, nil
}

// remoteReadFile returns a file's bytes from the remote host. Content
// travels base64-encoded so binary files and any encoding survive the exec
// pipe; the command itself carries only the path. found=false means the
// remote path does not exist (the caller maps that to its own response
// shape, e.g. 404).
func remoteReadFile(ctx context.Context, rw remoteWork, path string) ([]byte, bool, error) {
	const marker = "OCODE_EOF_9f7c_END"
	q := remote.ShellQuotePath(path)
	// Portable form: no GNU-only flags. `base64` wraps at 76 columns on
	// GNU and 76 on busybox; the client strips all newlines before decode.
	// An empty file yields empty output followed by the marker on its own
	// line — handled by the LastIndex split below.
	cmd := "if [ -d " + q + " ]; then echo DIR; exit 0; fi; " +
		"if [ -f " + q + " ]; then base64 < " + q + "; echo " + marker + "; else echo MISSING; exit 0; fi"
	out, err := remoteRun(ctx, rw, cmd)
	if err != nil {
		return nil, false, err
	}
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "DIR" {
		return nil, false, fmt.Errorf("path is a directory")
	}
	idx := strings.LastIndex(trimmed, "\n"+marker)
	if idx < 0 {
		if trimmed == marker {
			return []byte{}, true, nil
		}
		return nil, false, fmt.Errorf("remote read failed: unexpected output")
	}
	b64 := strings.ReplaceAll(trimmed[:idx], "\n", "")
	b64 = strings.ReplaceAll(b64, "\r", "")
	data, decErr := base64Decode(b64)
	if decErr != nil {
		return nil, false, fmt.Errorf("remote read failed: %w", decErr)
	}
	return data, true, nil
}

// base64Decode decodes standard base64 (with padding) from s.
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(s))
}

// remoteReadFileCapped is remoteReadFile with a server-side size bound: the
// remote stat is checked before the base64 read, so a file over maxBytes
// returns errTooLarge (carrying the actual size) without streaming the
// payload — the raw-preview endpoint maps that to its 400, mirroring the
// local os.Stat gate. Non-regular files (dirs, missing) behave exactly
// like remoteReadFile (DIR error / found=false).
var errTooLarge = fmt.Errorf("file exceeds the size limit")

func remoteReadFileCapped(ctx context.Context, rw remoteWork, path string, maxBytes int64) ([]byte, bool, error) {
	q := remote.ShellQuotePath(path)
	// Portable stat: `wc -c` prints "<bytes> <path>" on GNU and busybox;
	// dirs/missing are filtered first so the count parse only sees files.
	// Quoting: path travels only as a quoted shell word, never interpolated.
	sizeOut, serr := remoteRun(ctx, rw, "if [ -d "+q+" ]; then echo DIR; exit 0; fi; "+
		"if [ ! -f "+q+" ]; then echo MISSING; exit 0; fi; "+
		"wc -c < "+q)
	if serr != nil {
		return nil, false, serr
	}
	trimmed := strings.TrimSpace(sizeOut)
	if trimmed == "DIR" {
		return nil, false, fmt.Errorf("path is a directory")
	}
	if trimmed == "MISSING" {
		return nil, false, nil
	}
	var size int64
	if _, perr := fmt.Sscanf(trimmed, "%d", &size); perr != nil || size < 0 {
		return nil, false, fmt.Errorf("remote read failed: unexpected size output")
	}
	if size > maxBytes {
		return nil, false, fmt.Errorf("%w: %d bytes exceeds %d-byte limit", errTooLarge, size, maxBytes)
	}
	return remoteReadFile(ctx, rw, path)
}

// remoteWriteFile writes content to a remote path through stdin (never
// interpolated into argv). The command is a fixed shell script; the only
// variable input is the path (quoted) — the payload travels on stdin.
func remoteWriteFile(ctx context.Context, rw remoteWork, path string, data []byte) error {
	script := "cat > " + remote.ShellQuotePath(path)
	cmd, err := remote.ExecCommand(rw.Target, script)
	if err != nil {
		return err
	}
	cmd.Stdin = bytes.NewReader(data)
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

// remoteMkdirAll creates dir (and parents) on the remote host.
func remoteMkdirAll(ctx context.Context, rw remoteWork, dir string) error {
	_, err := remoteRun(ctx, rw, "mkdir -p -- "+remote.ShellQuotePath(dir))
	return err
}

// remoteExists reports whether path exists (file or dir) on the remote host.
func remoteExists(ctx context.Context, rw remoteWork, path string) bool {
	out, err := remoteRun(ctx, rw, "[ -e "+remote.ShellQuotePath(path)+" ] && echo yes || echo no")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "yes"
}

// remoteStatIsDir reports whether path is a directory on the remote host.
func remoteStatIsDir(ctx context.Context, rw remoteWork, path string) bool {
	out, err := remoteRun(ctx, rw, "[ -d "+remote.ShellQuotePath(path)+" ] && echo yes || echo no")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "yes"
}
