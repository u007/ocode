//go:build darwin

package sandbox

import (
	"os"
	"os/exec"
)

const sandboxExecPath = "/usr/bin/sandbox-exec"

// seatbeltWrapper is the macOS backend: commands run under sandbox-exec with a
// generated Seatbelt (SBPL) profile that confines writes to the writable roots
// while leaving reads, exec, and egress open.
type seatbeltWrapper struct{}

// newWrapper selects the real Seatbelt backend on macOS.
func newWrapper() Wrapper { return seatbeltWrapper{} }

// Available is true when the trusted absolute sandbox-exec exists and is
// executable.
func (seatbeltWrapper) Available() bool {
	fi, err := os.Stat(sandboxExecPath)
	return err == nil && fi.Mode()&0o111 != 0
}

// Wrap rewrites cmd to run under sandbox-exec with a profile confining writes
// to roots.WritableRoots. The profile is validated (safe variant); any
// escaping/root error is returned so the caller fails closed. Std* pipe
// plumbing and Dir/Env are carried to the wrapped cmd.
func (seatbeltWrapper) Wrap(cmd *exec.Cmd, roots RootSet) (*exec.Cmd, error) {
	prof, err := seatbeltProfileSafe(roots)
	if err != nil {
		return nil, err
	}
	return &exec.Cmd{
		Path:        sandboxExecPath,
		Args:        append([]string{sandboxExecPath, "-p", prof}, cmd.Args...),
		Env:         cmd.Env,
		Dir:         cmd.Dir,
		Stdin:       cmd.Stdin,
		Stdout:      cmd.Stdout,
		Stderr:      cmd.Stderr,
		ExtraFiles:  cmd.ExtraFiles,
		SysProcAttr: cmd.SysProcAttr,
	}, nil
}
