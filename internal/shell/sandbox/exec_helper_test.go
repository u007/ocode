package sandbox

import "os/exec"

// bashCmd builds a plain *exec.Cmd for wrapper tests (Args[0] == path).
func bashCmd(path string, args ...string) *exec.Cmd {
	return &exec.Cmd{Path: path, Args: append([]string{path}, args...)}
}