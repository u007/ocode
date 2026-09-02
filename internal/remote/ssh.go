package remote

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"

	"github.com/u007/ocode/internal/tool"
)

// SSHTransport implements Transport by shelling out to the system ssh/scp
// binaries — never x/crypto/ssh — so it inherits ssh_config, agent auth,
// hardware keys, ProxyJump, ControlMaster, and known_hosts verification for
// free, exactly as VS Code Remote-SSH does.
type SSHTransport struct {
	Target Target
	// Supervisor, if non-nil, receives every spawned ssh/scp child so it is
	// tracked and torn down like any other ocode-managed process. May be
	// nil for one-shot CLI invocations that predate any agent/TUI session
	// (there is no global default supervisor); callers should still
	// construct one for the lifetime of a `remote` command when possible.
	Supervisor *tool.ProcessSupervisor

	seq atomic.Int64
}

var _ Transport = (*SSHTransport)(nil)

func NewSSHTransport(t Target, sup *tool.ProcessSupervisor) *SSHTransport {
	return &SSHTransport{Target: t, Supervisor: sup}
}

func (s *SSHTransport) Describe() string {
	return "ssh " + s.Target.String()
}

func (s *SSHTransport) nextID(prefix string) string {
	return fmt.Sprintf("remote-%s-%d", prefix, s.seq.Add(1))
}

// run starts cmd under the supervisor (when set) and waits for it,
// returning the same shape regardless of whether a supervisor is present.
func (s *SSHTransport) run(cmd *exec.Cmd, kindLabel string) error {
	if s.Supervisor == nil {
		return cmd.Run()
	}
	id := s.nextID(kindLabel)
	if _, err := tool.StartSupervised(s.Supervisor, cmd, tool.ProcessRegistration{
		ID:      id,
		Name:    kindLabel,
		Command: cmd.String(),
		Kind:    tool.ProcessKindRemote,
	}); err != nil {
		return err
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		code := -1
		var exitErr *exec.ExitError
		if ok := asExitError(waitErr, &exitErr); ok {
			code = exitErr.ExitCode()
		}
		s.Supervisor.MarkExited(id, code)
		return waitErr
	}
	s.Supervisor.MarkExited(id, 0)
	return nil
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// Exec runs a single non-interactive command over ssh, capturing stdout and
// stderr separately (never inheriting the terminal — house rule, see
// AGENTS.md "capture subprocess output").
func (s *SSHTransport) Exec(command string) (ExecResult, error) {
	cmd := exec.Command("ssh", s.Target.String(), command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := s.run(cmd, "exec")
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr == nil {
		res.ExitCode = 0
		return res, nil
	}
	var exitErr *exec.ExitError
	if asExitError(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("ssh %s %q: exit %d: %s", s.Target.String(), command, res.ExitCode, stderr.String())
	}
	return res, fmt.Errorf("ssh %s %q: %w", s.Target.String(), command, runErr)
}

// ExecStdin runs command over ssh with stdin's content piped to the
// remote process's stdin — never interpolated into the command string, so
// its content never appears in this (or the remote's) process argv.
func (s *SSHTransport) ExecStdin(command string, stdin io.Reader) (ExecResult, error) {
	cmd := exec.Command("ssh", s.Target.String(), command)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := s.run(cmd, "exec-stdin")
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr == nil {
		res.ExitCode = 0
		return res, nil
	}
	var exitErr *exec.ExitError
	if asExitError(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("ssh %s %q: exit %d: %s", s.Target.String(), command, res.ExitCode, stderr.String())
	}
	return res, fmt.Errorf("ssh %s %q: %w", s.Target.String(), command, runErr)
}

// ExecInteractive runs a command over `ssh -t`, attached to the local
// terminal, and blocks until it exits.
func (s *SSHTransport) ExecInteractive(command string) error {
	cmd := exec.Command("ssh", "-t", s.Target.String(), command)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return s.run(cmd, "tui")
}

// Copy uploads src to destPath on the remote via scp, streaming from src so
// callers (a cross-compiled binary) never need to materialize it under a
// fixed local path first.
func (s *SSHTransport) Copy(src io.Reader, size int64, destPath string) error {
	tmp, err := os.CreateTemp("", "ocode-remote-upload-*")
	if err != nil {
		return fmt.Errorf("create upload temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return fmt.Errorf("stage upload temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close upload temp file: %w", err)
	}

	dest := s.Target.String() + ":" + destPath
	cmd := exec.Command("scp", tmpPath, dest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := s.run(cmd, "scp"); err != nil {
		return fmt.Errorf("scp to %s (%s): %w: %s", dest, humanBytes(size), err, stderr.String())
	}
	return nil
}

func humanBytes(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	units := []string{"KB", "MB", "GB"}
	f := float64(n)
	for _, u := range units {
		f /= 1024
		if f < 1024 {
			return fmt.Sprintf("%.1f %s", f, u)
		}
	}
	return fmt.Sprintf("%.1f TB", f/1024)
}
