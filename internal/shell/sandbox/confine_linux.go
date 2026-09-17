//go:build linux

package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
)

// confineEntrypoint is the re-exec'd Landlock confiner: parsed the protocol
// env, chdirs to the session workdir, applies no_new_privs + Landlock (or
// bubblewrap as fallback), then execve's `<shell> [-l] -c <command>` with the
// OCODE_SANDBOX_* vars stripped. Returns a process exit code on failure; on
// success the exec replaces this process and this function never returns.
func confineEntrypoint(args []string) int {
	if len(args) < 4 || args[1] != confinerSubcommand {
		return 0 // not a confiner invocation
	}
	// args[2:] is the original shell argv tail: <shell> [-l] -c <command>.
	// The last element is the command; exec the original shell shape so the
	// desktop login-shell invocation (zsh -l -c) survives confinement.
	// A bare shell name ("bash") must be resolved through PATH here: execve
	// does no PATH lookup, so a relative "bash" would resolve against the
	// session CWD and fail — the whole sandbox would be unusable.
	shell := resolveConfineShell(args[2])
	if shell == "" {
		fmt.Fprintf(os.Stderr, "sandbox-confine: cannot resolve shell %q\n", args[2])
		return 1
	}
	command := args[len(args)-1]
	shellArgs := args[3 : len(args)-1] // e.g. [] or ["-l"]

	var roots []string
	if raw := os.Getenv(envConfineRoots); raw != "" {
		if err := json.Unmarshal([]byte(raw), &roots); err != nil {
			fmt.Fprintf(os.Stderr, "sandbox-confine: bad roots env: %v\n", err)
			return 1
		}
	}
	dir := os.Getenv(envConfineDir)
	env := stripSandboxEnv(os.Environ())
	if dir != "" {
		_ = os.Chdir(dir)
	}

	if landlockUsable() {
		if err := applyConfineToSelf(roots, shell, shellArgs, command, env); err != nil {
			fmt.Fprintf(os.Stderr, "sandbox-confine: %v\n", err)
			return 1
		}
		return 0 // unreachable on success
	}
	if bwrapUsable() {
		argv := buildBwrapArgv(roots, append([]string{shell}, append(shellArgs, command)...))
		if err := syscall.Exec(argv[0], argv, env); err != nil {
			fmt.Fprintf(os.Stderr, "sandbox-confine: bwrap exec: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "sandbox-confine: no confinement backend available (fail-closed)")
	return 1
}
