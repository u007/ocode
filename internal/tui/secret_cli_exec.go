package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/secretcli"
)

// secretCmdFinishedMsg reports the outcome of a `/secret ...` slash command
// once bubbletea has restored control after the suspended terminal run.
type secretCmdFinishedMsg struct{ err error }

// secretCliExecCommand runs secretcli.Run(args) in the window bubbletea
// releases the real terminal for, so passphrase prompts (term.ReadPassword),
// y/N confirmations, and per-file progress lines render exactly as they do
// for `ocode secret ...` on the command line -- including Ctrl-C cancelling
// a directory-wide encrypt/decrypt between files (secretjob.TransformFile
// writes atomically, so a cancelled run never corrupts a file, and
// re-running simply skips whatever already finished).
type secretCliExecCommand struct {
	args []string

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func (c *secretCliExecCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *secretCliExecCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *secretCliExecCommand) SetStderr(w io.Writer) { c.stderr = w }

func (c *secretCliExecCommand) Run() error {
	stdin, ok := c.stdin.(*os.File)
	if !ok {
		return fmt.Errorf("no terminal available to run `ocode secret`")
	}
	stderr := c.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	restore := secretcli.SetIO(stdin, stderr)
	defer restore()

	return secretcli.Run(c.args)
}

// secretPathArgIndex are the /secret subcommands whose second argument is a
// (possibly relative) path that must be resolved against the TUI's workDir
// rather than the process's actual working directory, since the two can
// differ.
var secretPathSubcommands = map[string]bool{
	"init":    true,
	"encrypt": true,
	"decrypt": true,
	"rekey":   true,
}

func resolveSecretCmdArgs(workDir string, args []string) []string {
	if len(args) < 2 || !secretPathSubcommands[args[0]] || filepath.IsAbs(args[1]) {
		return args
	}
	resolved := append([]string{}, args...)
	resolved[1] = filepath.Join(workDir, args[1])
	return resolved
}

// runSecretCmd implements `/secret <init|encrypt|decrypt|rekey> [path]`,
// suspending the TUI to run the exact same secretcli.Run used by
// `ocode secret ...` on the command line.
func runSecretCmd(m *model, args []string) tea.Cmd {
	if len(args) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /secret <init|encrypt|decrypt|rekey> [path]"})
		return nil
	}
	cmd := &secretCliExecCommand{args: resolveSecretCmdArgs(m.workDir, args)}
	return tea.Exec(cmd, func(err error) tea.Msg { return secretCmdFinishedMsg{err: err} })
}
