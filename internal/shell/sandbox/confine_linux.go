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
// bubblewrap as fallback), then execve's /bin/bash -c <command> with the
// OCODE_SANDBOX_* vars stripped. Returns a process exit code on failure; on
// success the exec replaces this process and this function never returns.
func confineEntrypoint(args []string) int {
	if len(args) < 3 || args[1] != confinerSubcommand {
		return 0 // not a confiner invocation
	}
	command := args[2]

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
		if err := applyConfineToSelf(roots, command, env); err != nil {
			fmt.Fprintf(os.Stderr, "sandbox-confine: %v\n", err)
			return 1
		}
		return 0 // unreachable on success
	}
	if bwrapUsable() {
		argv := buildBwrapArgv(roots, []string{"/bin/bash", "-c", command})
		if err := syscall.Exec(argv[0], argv, env); err != nil {
			fmt.Fprintf(os.Stderr, "sandbox-confine: bwrap exec: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "sandbox-confine: no confinement backend available (fail-closed)")
	return 1
}
