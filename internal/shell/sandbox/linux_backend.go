package sandbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OCODE_SANDBOX_* are the re-exec confiner protocol env vars. They carry the
// command's runtime state from the parent ocode process to the re-exec'd
// confiner child and are ALWAYS stripped before the confined bash executes, so
// a sandboxed command can never see or forge them.
const (
	envConfineRoots = "OCODE_SANDBOX_ROOTS" // JSON-encoded []string of canonical writable roots
	envConfineDir   = "OCODE_SANDBOX_DIR"   // session workdir for the confined command
)

// confinerSubcommand is the hidden argv[1] the re-exec'd Landlock confiner
// dispatches on (main.go routes it to ConfineEntrypoint). It is deliberately
// not a documented CLI surface.
const confinerSubcommand = "sandbox-confine"

// linuxBackendProbes are the injectable seams the Linux wrapper uses to decide
// between Landlock and bubblewrap, plus the self-executable path for the
// Landlock re-exec confiner. Tests inject fakes; production uses real probes.
type linuxBackendProbes struct {
	landlockUsable func() bool
	bwrapUsable    func() bool
	executable     func() (string, error)
}

// linuxWrapper is the Linux confinement backend: Landlock (via a re-exec'd
// confiner subcommand) preferred, bubblewrap (trusted /usr/bin/bwrap) as the
// fallback. Fail-closed: with neither usable, Wrap errors.
type linuxWrapper struct {
	probes linuxBackendProbes
}

func newLinuxWrapper(probes linuxBackendProbes) Wrapper {
	if probes.landlockUsable == nil {
		probes.landlockUsable = func() bool { return false }
	}
	if probes.bwrapUsable == nil {
		probes.bwrapUsable = func() bool { return false }
	}
	if probes.executable == nil {
		probes.executable = os.Executable
	}
	return linuxWrapper{probes: probes}
}

// Available reports whether Landlock or bubblewrap can confine right now.
func (w linuxWrapper) Available() bool {
	return w.probes.landlockUsable() || w.probes.bwrapUsable()
}

// Wrap rewrites cmd to a confined invocation:
//   - Landlock usable → re-exec this binary as `sandbox-confine <command>`
//     with the writable roots + workdir carried in the OCODE_SANDBOX_* env;
//     the child applies the Landlock ruleset itself, then execve's bash.
//   - else bubblewrap usable → build the bwrap argv (ro-bind /, rw binds).
//   - else → error (fail-closed; never run unconfined).
//
// Dir/Env/Std*/ExtraFiles/SysProcAttr are preserved so process-group,
// cancellation, pipes, and the session workdir survive the rewrite.
func (w linuxWrapper) Wrap(cmd *exec.Cmd, roots RootSet) (*exec.Cmd, error) {
	writables := canonicalExistingWritables(roots.WritableRoots)
	if w.probes.landlockUsable() {
		return w.reexecConfiner(cmd, writables)
	}
	if w.probes.bwrapUsable() {
		return w.wrapBwrap(cmd, writables), nil
	}
	return nil, errBackendUnavailable
}

// errBackendUnavailable is the fail-closed error for a Supported-OS wrapper
// that can confine right now.
var errBackendUnavailable = &backendUnavailableError{}

type backendUnavailableError struct{}

func (*backendUnavailableError) Error() string {
	return "sandbox mode active but neither Landlock nor bubblewrap is available: refusing to run unconfined"
}

// reexecConfiner builds the re-exec'd confiner command (Landlock path).
func (w linuxWrapper) reexecConfiner(cmd *exec.Cmd, writableRoots []string) (*exec.Cmd, error) {
	exe, err := w.probes.executable()
	if err != nil {
		return nil, err
	}
	// The original cmd is bash -c <command>; the confiner receives the
	// command string verbatim as argv[2] and re-execs /bin/bash -c with it
	// (argv survives sizes that would overflow a single env var).
	command := ""
	if len(cmd.Args) > 2 {
		command = cmd.Args[2]
	}
	rootsJSON, _ := json.Marshal(writableRoots)
	env := sandboxEnv(cmd.Env, rootsJSON, cmd.Dir)
	return &exec.Cmd{
		Path:        exe,
		Args:        append([]string{exe, confinerSubcommand, command}, cmd.Args[3:]...),
		Env:         env,
		Dir:         cmd.Dir,
		Stdin:       cmd.Stdin,
		Stdout:      cmd.Stdout,
		Stderr:      cmd.Stderr,
		ExtraFiles:  cmd.ExtraFiles,
		SysProcAttr: cmd.SysProcAttr,
	}, nil
}

// wrapBwrap builds the bubblewrap invocation.
func (w linuxWrapper) wrapBwrap(cmd *exec.Cmd, writableRoots []string) *exec.Cmd {
	argv := buildBwrapArgv(writableRoots, cmd.Args)
	return &exec.Cmd{
		Path:        bwrapReadOnlyAbs,
		Args:        argv,
		Env:         cmd.Env,
		Dir:         cmd.Dir,
		Stdin:       cmd.Stdin,
		Stdout:      cmd.Stdout,
		Stderr:      cmd.Stderr,
		ExtraFiles:  cmd.ExtraFiles,
		SysProcAttr: cmd.SysProcAttr,
	}
}

// sandboxEnv materializes the child environment: the wrapped cmd's Env (or the
// inherited one), minus any stale OCODE_SANDBOX_* vars, plus the protocol vars.
func sandboxEnv(baseEnv []string, rootsJSON []byte, dir string) []string {
	env := baseEnv
	if env == nil {
		env = os.Environ()
	}
	env = stripSandboxEnv(env)
	env = append(env, envConfineRoots+"="+string(rootsJSON))
	env = append(env, envConfineDir+"="+dir)
	return env
}

// stripSandboxEnv removes the OCODE_SANDBOX_* protocol vars from env.
func stripSandboxEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, envConfineRoots+"=") || strings.HasPrefix(kv, envConfineDir+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// canonicalExistingWritables resolves each writable root symlinks and keeps
// only paths that exist and are not the filesystem root. Missing roots are
// dropped — Landlock path_beneath and bwrap binds both require an existing
// path, and skipping never widens the boundary (nothing exists there to
// write to).
func canonicalExistingWritables(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" || root == "/" {
			continue
		}
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if canonical == "/" {
			continue
		}
		out = append(out, canonical)
	}
	return out
}
