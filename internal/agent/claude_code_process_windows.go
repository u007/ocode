//go:build windows

package agent

import "os/exec"

// Windows has no POSIX process group. Killing the direct child is the best
// effort available without a Job Object; Claude Code's own subprocesses may
// outlive a cancelled turn.
func configureClaudeCodeCommand(cmd *exec.Cmd) {}

func killClaudeCodeProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
