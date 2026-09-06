package remote

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/u007/ocode/internal/tool"
)

// WSLTransport implements Transport by shelling out to wsl.exe — only
// meaningful when ocode itself is running on Windows (enforced by
// validateTargetOS before a WSLTransport is ever constructed, not by this
// type itself). See docs/superpowers/specs/2026-08-29-remote-ssh/04-phase3-wsl.md.
type WSLTransport struct {
	// Distro names the target distro; "" means wsl.exe's default distro.
	Distro     string
	Supervisor *tool.ProcessSupervisor

	seq atomic.Int64
}

var _ Transport = (*WSLTransport)(nil)

func NewWSLTransport(distro string, sup *tool.ProcessSupervisor) *WSLTransport {
	return &WSLTransport{Distro: distro, Supervisor: sup}
}

func (w *WSLTransport) Describe() string {
	if w.Distro == "" {
		return "wsl (default distro)"
	}
	return "wsl " + w.Distro
}

func (w *WSLTransport) nextID(prefix string) string {
	return fmt.Sprintf("remote-wsl-%s-%d", prefix, w.seq.Add(1))
}

// run mirrors SSHTransport.run exactly (same supervisor bookkeeping
// contract) — duplicated rather than shared because the two types differ
// in exec.Command construction, and the run/wait/MarkExited plumbing is
// only five lines; factor out only if a third transport needs it too.
func (w *WSLTransport) run(cmd *exec.Cmd, kindLabel string) error {
	if w.Supervisor == nil {
		return cmd.Run()
	}
	id := w.nextID(kindLabel)
	if _, err := tool.StartSupervised(w.Supervisor, cmd, tool.ProcessRegistration{
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
		if asExitError(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		w.Supervisor.MarkExited(id, code)
		return waitErr
	}
	w.Supervisor.MarkExited(id, 0)
	return nil
}

// wslExecArgs builds the wsl.exe argv for a non-interactive command,
// wrapping it in `sh -c` for the same reason ssh's "ssh host command" form
// does: the command string may itself contain shell operators (&&, quoting
// from shellQuotePath, etc.) that must be interpreted by a real shell
// inside the distro, not split by wsl.exe's own argv handling. Factored out
// (pure function, no exec.Cmd) so argv shape is unit-testable without
// wsl.exe present.
func wslExecArgs(distro, command string) []string {
	args := []string{}
	if distro != "" {
		args = append(args, "-d", distro)
	}
	return append(args, "--", "sh", "-c", command)
}

// wslInteractiveArgs builds the wsl.exe argv for ExecInteractive. Per the
// spec, this form deliberately does NOT wrap in `sh -c`: wsl.exe allocates
// the console/pty natively for a foreground command, and TUI launch
// commands built by WrapLaunch are already a single properly-shell-quoted
// string. It is split into words here — rather than passed as one argv
// element — because wsl.exe itself rejoins the args after "--" with spaces
// before handing them to the distro's default shell, so splitting on
// whitespace and letting wsl.exe rejoin is a no-op on the resulting command
// line while matching how wsl.exe's own argv parsing expects to receive it.
func wslInteractiveArgs(distro, command string) []string {
	args := []string{}
	if distro != "" {
		args = append(args, "-d", distro)
	}
	return append(args, append([]string{"--"}, strings.Fields(command)...)...)
}

func wslCopyArgs(distro, destPath string) []string {
	return wslExecArgs(distro, "cat > "+shellQuotePath(destPath))
}

func (w *WSLTransport) Exec(command string) (ExecResult, error) {
	cmd := exec.Command("wsl.exe", wslExecArgs(w.Distro, command)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := w.run(cmd, "exec")
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr == nil {
		res.ExitCode = 0
		return res, nil
	}
	var exitErr *exec.ExitError
	if asExitError(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("wsl %s %q: exit %d: %s", w.Describe(), command, res.ExitCode, stderr.String())
	}
	return res, fmt.Errorf("wsl %s %q: %w", w.Describe(), command, runErr)
}

func (w *WSLTransport) ExecStdin(command string, stdin io.Reader) (ExecResult, error) {
	cmd := exec.Command("wsl.exe", wslExecArgs(w.Distro, command)...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := w.run(cmd, "exec-stdin")
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr == nil {
		res.ExitCode = 0
		return res, nil
	}
	var exitErr *exec.ExitError
	if asExitError(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("wsl %s %q: exit %d: %s", w.Describe(), command, res.ExitCode, stderr.String())
	}
	return res, fmt.Errorf("wsl %s %q: %w", w.Describe(), command, runErr)
}

func (w *WSLTransport) ExecInteractive(command string) error {
	cmd := exec.Command("wsl.exe", wslInteractiveArgs(w.Distro, command)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return w.run(cmd, "tui")
}

// Copy streams src into destPath inside the distro via `cat > destPath`
// over wsl.exe's stdin — chosen over the \\wsl$\<distro>\... UNC path per
// the spec's explicit "pick one in implementation" latitude, since the
// stdin-stream approach reuses this file's existing Exec-style plumbing
// exactly (no path-translation code, no UNC-availability detection) and
// the spec calls UNC-with-stdin-fallback, not UNC-only.
func (w *WSLTransport) Copy(src io.Reader, size int64, destPath string) error {
	cmd := exec.Command("wsl.exe", wslCopyArgs(w.Distro, destPath)...)
	cmd.Stdin = src
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := w.run(cmd, "copy"); err != nil {
		return fmt.Errorf("wsl copy to %s (%s): %w: %s", destPath, humanBytes(size), err, stderr.String())
	}
	return nil
}
