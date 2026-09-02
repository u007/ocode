package remote

import "io"

// ExecResult is the outcome of a non-interactive remote command.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Transport is the seam between connect targets: system ssh today, wsl.exe
// in Phase 3. Every stage of the connect flow (reachability, platform
// detect, provisioning, credential sync, multiplex detect, launch) goes
// through this interface so a second transport is nothing more than a
// second implementation.
type Transport interface {
	// Exec runs a non-interactive remote command (a single string,
	// interpreted by the remote's login shell — so "~" and shell
	// operators work as expected) and captures its output. It never
	// allocates a pty and never touches the local terminal.
	Exec(command string) (ExecResult, error)

	// ExecStdin is Exec, but pipes stdin's content to the remote command's
	// stdin rather than inheriting nothing. Use this — never string
	// interpolation into the command itself — for anything that must not
	// appear in argv/process listings/logs (the credential sync payload is
	// the reason this method exists).
	ExecStdin(command string, stdin io.Reader) (ExecResult, error)

	// ExecInteractive runs a remote command attached to the local
	// terminal (stdin/stdout/stderr inherited, a remote pty allocated)
	// and blocks until it exits. Used for the final TUI launch stage.
	ExecInteractive(command string) error

	// Copy uploads local file content to a remote destination path,
	// writing through io.Reader rather than requiring a local file on
	// disk (the cross-compiled binary is a natural fit: build to a temp
	// file or pipe it directly).
	Copy(src io.Reader, size int64, destPath string) error

	// Describe returns a short human-readable label for progress output
	// (e.g. "ssh user@host").
	Describe() string
}
