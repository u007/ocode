package tui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/secretfile"
	"github.com/u007/ocode/internal/tool"
)

// secretEditorOpener builds the standard editor opener for editor/mode and
// wraps it so encrypted files (internal/secretfile) are transparently
// decrypted for editing and re-encrypted on save. promptPassphrase is left
// nil so secretExecCommand reads the passphrase from the stdin/stderr
// bubbletea itself wires in (see secretExecCommand.readPassphrase) rather
// than assuming os.Stdin is the right fd.
func (m *model) secretEditorOpener(editor, mode string) func(string) tea.Cmd {
	inner := createEditorOpener(editor, mode, func() int { return m.width }, m.supervisor)
	if m.secretSession == nil {
		return inner
	}
	return wrapEditorOpenerForSecrets(m.secretSession, nil, m.supervisor, editor, mode, func() int { return m.width }, inner)
}

// wrapEditorOpenerForSecrets decorates an external-editor opener (see
// createEditorOpener) so that opening an age-encrypted file
// (internal/secretfile) transparently decrypts it to a private temp file,
// runs the real editor against that temp file, and re-encrypts the result
// back over the original path once the editor exits -- only if the
// plaintext actually changed. Plaintext files are handed to inner
// untouched.
//
// The whole decrypt -> prompt -> edit -> re-encrypt sequence runs inside
// secretExecCommand.Run(), which bubbletea only invokes after releasing the
// terminal for the external process (see tea.Exec / tea.ExecCommand) -- the
// same window createEditorOpener already uses to launch vim/nvim, so a
// masked passphrase read there does not fight bubbletea's own input
// handling. Building the returned tea.Cmd eagerly (as createEditorOpener's
// output would be) must NOT decrypt anything: the runtime defers the real
// work until it processes the Cmd's execMsg, so any prompt/decrypt done
// before that point runs at the wrong time relative to terminal ownership.
//
// tmux split/window editor modes are supported by pointing the tmux pane at
// the decrypted temp file instead of the original (still-encrypted) path:
// secretExecCommand writes the plaintext to its temp dir first, then builds
// the pane command against that temp path (see buildTmuxOpenCmd) and blocks
// on tmux's own `wait-for` handshake exactly as the non-secret tmux opener
// does, so the re-encrypt step below still only runs after the pane's editor
// exits.
//
// promptPassphrase overrides how secretExecCommand asks for a project's
// passphrase; pass nil in production so it reads from the stdin/stderr
// bubbletea assigns via SetStdin/SetStderr before Run() (which may not be
// os.Stdin/os.Stderr -- e.g. tea.WithInput, or a separately opened tty when
// os.Stdin isn't a terminal). Tests that don't exercise a real ExecCommand
// stdio wiring should supply a fake here instead.
func wrapEditorOpenerForSecrets(
	sess *secretfile.Session,
	promptPassphrase func(root string) (string, error),
	sup *tool.ProcessSupervisor,
	editor string,
	mode string,
	getWidth func() int,
	inner func(string) tea.Cmd,
) func(string) tea.Cmd {
	tmuxMode := ""
	if isTmuxMode(mode) {
		tmuxMode = mode
	}
	return func(path string) tea.Cmd {
		encrypted, err := secretfile.PeekIsEncrypted(path)
		if err != nil || !encrypted {
			return inner(path)
		}

		cmdParts := strings.Fields(editor)
		if len(cmdParts) == 0 {
			return errMsgCmd(fmt.Errorf("no editor configured"))
		}
		if _, err := exec.LookPath(cmdParts[0]); err != nil {
			return errMsgCmd(fmt.Errorf("editor %q not found in PATH: %w", cmdParts[0], err))
		}

		sec := &secretExecCommand{
			path:             path,
			editorCmdParts:   cmdParts,
			tmuxMode:         tmuxMode,
			tmuxEditor:       editor,
			getWidth:         getWidth,
			sess:             sess,
			promptPassphrase: promptPassphrase,
			sup:              sup,
		}
		return tea.Exec(sec, func(err error) tea.Msg { return editorFinishedMsg{err: err} })
	}
}

// secretExecCommand implements tea.ExecCommand. Its Run method performs the
// full decrypt -> edit -> re-encrypt cycle for one encrypted file, entirely
// inside the window bubbletea has released the terminal for.
type secretExecCommand struct {
	path           string
	editorCmdParts []string
	// tmuxMode is "" for a direct child-process editor, or the tmux editor
	// mode (config.EditorModeTmuxSplit/EditorModeTmuxWindow) when the real
	// editor must run detached in a tmux pane instead.
	tmuxMode   string
	tmuxEditor string
	getWidth   func() int

	sess             *secretfile.Session
	promptPassphrase func(root string) (string, error)
	sup              *tool.ProcessSupervisor

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func (c *secretExecCommand) SetStdin(r io.Reader) {
	if c.stdin == nil {
		c.stdin = r
	}
}

func (c *secretExecCommand) SetStdout(w io.Writer) {
	if c.stdout == nil {
		c.stdout = w
	}
}

func (c *secretExecCommand) SetStderr(w io.Writer) {
	if c.stderr == nil {
		c.stderr = w
	}
}

// readPassphrase asks for root's passphrase on c.stderr and reads it from
// c.stdin without echoing it. c.stdin/c.stderr are whatever bubbletea wired
// via SetStdin/SetStderr before calling Run() -- not necessarily
// os.Stdin/os.Stderr -- so this must not read those directly: bubbletea's
// own input reader is guaranteed stopped by the time Run() executes (see
// tea.Program.releaseTerminal's cancelReader.Cancel + waitForReadLoop), but
// only for the fd it actually handed to SetStdin.
func (c *secretExecCommand) readPassphrase(root string) (string, error) {
	if c.promptPassphrase != nil {
		return c.promptPassphrase(root)
	}

	stderr := c.stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	f, ok := c.stdin.(*os.File)
	if !ok {
		return "", fmt.Errorf("no terminal available to read a passphrase from")
	}

	fmt.Fprintf(stderr, "\nEncrypted file in project %s\nPassphrase: ", root)
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	return string(b), nil
}

// buildEditorCmd builds the command that edits tmpFile: a direct child
// process for the plain editor modes, or a tmux pane command (see
// buildTmuxOpenCmd) pointed at tmpFile -- never at c.path -- when tmuxMode is
// set, so the pane only ever sees the decrypted temp copy.
func (c *secretExecCommand) buildEditorCmd(tmpFile string) *exec.Cmd {
	if c.tmuxMode != "" {
		width := 80
		if c.getWidth != nil {
			width = c.getWidth()
		}
		return buildTmuxOpenCmd(c.tmuxMode, c.tmuxEditor, tmpFile, width)()
	}
	args := append(append([]string{}, c.editorCmdParts[1:]...), tmpFile)
	return exec.Command(c.editorCmdParts[0], args...)
}

func (c *secretExecCommand) Run() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", c.path, err)
	}

	root := paths.ProjectRoot(filepath.Dir(c.path))
	key := c.sess.Get(root)
	if key == nil {
		keyPath := secretfile.ProjectKeyPath(root)
		if _, err := os.Stat(keyPath); err != nil {
			return fmt.Errorf("no project key at %s; run `ocode secret init` first", keyPath)
		}

		passphrase, perr := c.readPassphrase(root)
		if perr != nil {
			return fmt.Errorf("passphrase for %s: %w", root, perr)
		}
		unlocked, uerr := secretfile.UnlockProjectKey(keyPath, passphrase)
		if uerr != nil {
			return uerr
		}
		key = unlocked
		c.sess.Set(root, key)
	}

	plaintext, err := key.Decrypt(data)
	if err != nil {
		return fmt.Errorf("decrypt %s: %w", c.path, err)
	}

	tmpDir, err := os.MkdirTemp("", "ocode-secretedit-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		return fmt.Errorf("chmod temp dir: %w", err)
	}
	if c.sup != nil {
		_ = c.sup.RegisterShutdownCallback(func() { os.RemoveAll(tmpDir) })
	}

	tmpFile := filepath.Join(tmpDir, filepath.Base(c.path))
	if err := os.WriteFile(tmpFile, plaintext, 0o600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	editorCmd := c.buildEditorCmd(tmpFile)
	editorCmd.Stdin = c.stdin
	editorCmd.Stdout = c.stdout
	editorCmd.Stderr = c.stderr

	runErr := editorCmd.Run()

	edited, rerr := os.ReadFile(tmpFile)
	if rerr != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("read edited %s: %w", c.path, rerr)
	}
	if bytes.Equal(edited, plaintext) {
		return runErr
	}

	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(c.path); statErr == nil {
		mode = info.Mode().Perm()
	}
	ciphertext, eerr := key.Encrypt(edited)
	if eerr != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("re-encrypt %s: %w", c.path, eerr)
	}
	if werr := secretfile.WriteFileAtomic(c.path, ciphertext, mode); werr != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("write %s: %w", c.path, werr)
	}
	return runErr
}

func errMsgCmd(err error) tea.Cmd {
	return func() tea.Msg { return editorFinishedMsg{err: err} }
}
