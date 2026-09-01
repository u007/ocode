package tool

import (
	"context"
	"os/exec"
	"runtime"
)

// bashInvocation returns the platform shell argv for a bash-tool command:
// `bash -c <command>` on Unix, `cmd /C <command>` on Windows.
func bashInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "bash", []string{"-c", command}
}

// buildBashCmd is the single bash-command construction site for both the
// foreground (exec.go) and background (process.go) paths. It encapsulates the
// GOOS branch and the session-workdir wiring.
//
// A non-nil ctx is honored via exec.CommandContext so a foreground timeout can
// kill the child (and, via setProcGroup, its whole process group); a nil ctx —
// the background launch — uses plain exec.Command semantics so the child is
// NOT tied to a caller context that may be cancelled mid-flight.
//
// dir sets cmd.Dir when non-empty (the session project root), so relative
// commands resolve against the session, not the process working directory.
// Part 02 of the shell-sandbox plan extends this with the sandbox wrap.
func buildBashCmd(ctx context.Context, command, dir string) *exec.Cmd {
	shell, args := bashInvocation(command)
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, shell, args...)
	} else {
		cmd = exec.Command(shell, args...)
	}
	if runtime.GOOS != "windows" {
		setProcGroup(cmd)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd
}