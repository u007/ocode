//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

// configureClaudeCodeCommand starts the CLI in its own process group so a
// cancelled turn can kill the whole tree (node plus any child it spawned).
func configureClaudeCodeCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killClaudeCodeProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
