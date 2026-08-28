package tui

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/secretfile"
)

// setupProject creates a temp project with a generated (unlocked) key and
// returns its root and the key.
func setupProject(t *testing.T) (string, *secretfile.ProjectKey) {
	t.Helper()
	root := t.TempDir()
	key, err := secretfile.GenerateProjectKey(secretfile.ProjectKeyPath(root), "pw")
	if err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}
	return root, key
}

// writeFakeEditor writes an executable shell script standing in for an
// external editor: it overwrites its last argument (the file path) with
// content, simulating a save.
func writeFakeEditor(t *testing.T, dir, content string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake editor script is a POSIX shell script")
	}
	script := filepath.Join(dir, "fake-editor.sh")
	body := "#!/bin/sh\ncat > \"$1\" <<'EOF'\n" + content + "EOF\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	return script
}

// writeNoopEditor writes an executable shell script that exits without
// touching its argument, simulating an editor session with no save.
func writeNoopEditor(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake editor script is a POSIX shell script")
	}
	script := filepath.Join(dir, "noop-editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrue\n"), 0o700); err != nil {
		t.Fatalf("write noop editor: %v", err)
	}
	return script
}

func TestSecretExecCommand_Run_ReencryptsOnChange(t *testing.T) {
	root, key := setupProject(t)
	path := filepath.Join(root, "secret.txt")
	ciphertext, err := key.Encrypt([]byte("before\n"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if err := os.WriteFile(path, ciphertext, 0o640); err != nil {
		t.Fatalf("seed: %v", err)
	}

	editor := writeFakeEditor(t, t.TempDir(), "after\n")

	sess := secretfile.NewSession()
	promptCalls := 0
	prompt := func(string) (string, error) { promptCalls++; return "pw", nil }

	cmd := &secretExecCommand{
		path:             path,
		editorCmdParts:   []string{editor},
		sess:             sess,
		promptPassphrase: prompt,
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if promptCalls != 1 {
		t.Fatalf("expected exactly one passphrase prompt, got %d", promptCalls)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !secretfile.IsEncrypted(got) {
		t.Fatal("expected file on disk to still be encrypted")
	}
	plain, err := key.Decrypt(got)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "after\n" {
		t.Fatalf("expected re-encrypted content %q, got %q", "after\n", plain)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode changed: got %o, want %o", info.Mode().Perm(), 0o640)
	}

	// A second command against the same session should reuse the cached key.
	cmd2 := &secretExecCommand{
		path:             path,
		editorCmdParts:   []string{writeNoopEditor(t, t.TempDir())},
		sess:             sess,
		promptPassphrase: prompt,
	}
	if err := cmd2.Run(); err != nil {
		t.Fatalf("Run (second): %v", err)
	}
	if promptCalls != 1 {
		t.Fatalf("expected cached key to skip a second prompt, got %d prompts", promptCalls)
	}
}

func TestSecretExecCommand_Run_NoChangeSkipsRewrite(t *testing.T) {
	root, key := setupProject(t)
	path := filepath.Join(root, "secret.txt")
	ciphertext, err := key.Encrypt([]byte("unchanged\n"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if err := os.WriteFile(path, ciphertext, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sess := secretfile.NewSession()
	sess.Set(root, key)

	cmd := &secretExecCommand{
		path:             path,
		editorCmdParts:   []string{writeNoopEditor(t, t.TempDir())},
		sess:             sess,
		promptPassphrase: func(string) (string, error) { return "pw", nil },
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != string(ciphertext) {
		t.Fatal("expected ciphertext to be byte-identical when plaintext did not change")
	}
}

func TestSecretExecCommand_Run_RemovesTempDir(t *testing.T) {
	root, key := setupProject(t)
	path := filepath.Join(root, "secret.txt")
	ciphertext, err := key.Encrypt([]byte("data\n"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if err := os.WriteFile(path, ciphertext, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	scriptDir := t.TempDir()
	marker := filepath.Join(scriptDir, "marker")
	editor := writeInspectingEditor(t, scriptDir, marker)

	sess := secretfile.NewSession()
	sess.Set(root, key)
	cmd := &secretExecCommand{
		path:             path,
		editorCmdParts:   []string{editor},
		sess:             sess,
		promptPassphrase: func(string) (string, error) { return "pw", nil },
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	argPath, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker (editor never ran?): %v", err)
	}
	tmpDirSeen := filepath.Dir(string(argPath))
	if _, err := os.Stat(tmpDirSeen); !os.IsNotExist(err) {
		t.Fatalf("expected temp dir to be removed, stat err = %v", err)
	}
}

// writeInspectingEditor writes an executable shell script that records the
// path it was invoked with into marker, so the test can inspect it after
// Run returns (the temp dir the editor saw will already be gone by then).
func writeInspectingEditor(t *testing.T, dir, marker string) string {
	t.Helper()
	script := filepath.Join(dir, "inspect-editor.sh")
	body := "#!/bin/sh\nprintf '%s' \"$1\" > " + marker + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("write inspecting editor: %v", err)
	}
	return script
}

// TestSecretExecCommand_Run_WithRealNvim runs an actual nvim headless
// session (not a fake shell script) against secretExecCommand, so the real
// swap-file/backup dance an editor does is exercised and contained by the
// private temp dir, and the re-encrypted content really comes from nvim's
// save. Skipped when nvim is not on PATH.
func TestSecretExecCommand_Run_WithRealNvim(t *testing.T) {
	nvimPath, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim not found in PATH")
	}
	if runtime.GOOS == "windows" {
		t.Skip("nvim headless args below assume a POSIX shell environment")
	}

	root, key := setupProject(t)
	path := filepath.Join(root, "secret.txt")
	ciphertext, err := key.Encrypt([]byte("original content\n"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if err := os.WriteFile(path, ciphertext, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sess := secretfile.NewSession()
	promptCalls := 0
	prompt := func(string) (string, error) { promptCalls++; return "pw", nil }

	// -u NONE skips the user's own vimrc (independent of what's installed on
	// this machine); the normal! commands replace the buffer and save,
	// exercising nvim's real write path (including any swap file it creates
	// alongside the temp file, which must stay contained in the temp dir).
	cmd := &secretExecCommand{
		path: path,
		editorCmdParts: []string{
			nvimPath, "--headless", "-u", "NONE",
			"-c", "normal! ggVGd",
			"-c", "normal! iheadless edit content",
			"-c", "wq",
		},
		sess:             sess,
		promptPassphrase: prompt,
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if promptCalls != 1 {
		t.Fatalf("expected exactly one passphrase prompt, got %d", promptCalls)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !secretfile.IsEncrypted(got) {
		t.Fatal("expected file on disk to still be encrypted")
	}
	plain, err := key.Decrypt(got)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "headless edit content\n" {
		t.Fatalf("expected content saved by nvim, got %q", plain)
	}
}

func TestSecretExecCommand_Run_MissingKeyFailsBeforePrompting(t *testing.T) {
	root := t.TempDir()
	// A project key that was never `secretfile.GenerateProjectKey`'d, but a
	// file that happens to look encrypted (e.g. copied in from elsewhere).
	other, err := secretfile.GenerateProjectKey(secretfile.ProjectKeyPath(t.TempDir()), "pw")
	if err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}
	path := filepath.Join(root, "secret.txt")
	ciphertext, err := other.Encrypt([]byte("data"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if err := os.WriteFile(path, ciphertext, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	promptCalls := 0
	cmd := &secretExecCommand{
		path:             path,
		editorCmdParts:   []string{writeNoopEditor(t, t.TempDir())},
		sess:             secretfile.NewSession(),
		promptPassphrase: func(string) (string, error) { promptCalls++; return "pw", nil },
	}

	err = cmd.Run()
	if err == nil {
		t.Fatal("expected an error when no project key exists")
	}
	if !strings.Contains(err.Error(), "secret init") {
		t.Fatalf("expected error to mention 'secret init', got: %v", err)
	}
	if promptCalls != 0 {
		t.Fatalf("expected no passphrase prompt when the key file is missing, got %d", promptCalls)
	}
}

func TestSecretExecCommand_ReadPassphrase_RejectsNonFileStdin(t *testing.T) {
	c := &secretExecCommand{stdin: strings.NewReader(""), stderr: &bytes.Buffer{}}
	if _, err := c.readPassphrase("/some/root"); err == nil {
		t.Fatal("expected an error when stdin is not a real terminal file")
	}
}

func TestWrapEditorOpenerForSecrets_PlainFilePassesThrough(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plain.txt")
	if err := os.WriteFile(path, []byte("plain content"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sess := secretfile.NewSession()
	promptCalls := 0
	prompt := func(string) (string, error) { promptCalls++; return "", nil }

	var innerCalledWith string
	inner := func(p string) tea.Cmd {
		innerCalledWith = p
		return func() tea.Msg { return editorFinishedMsg{} }
	}

	opener := wrapEditorOpenerForSecrets(sess, prompt, nil, "vi", "external", func() int { return 80 }, inner)
	opener(path)()

	if innerCalledWith != path {
		t.Fatalf("expected inner called with original path %q, got %q", path, innerCalledWith)
	}
	if promptCalls != 0 {
		t.Fatalf("expected no passphrase prompt for plain file, got %d calls", promptCalls)
	}
}

func TestWrapEditorOpenerForSecrets_TmuxModeWrapsEncryptedFile(t *testing.T) {
	root, key := setupProject(t)
	path := filepath.Join(root, "secret.txt")
	ciphertext, err := key.Encrypt([]byte("data"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if err := os.WriteFile(path, ciphertext, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sess := secretfile.NewSession()
	innerCalled := false
	inner := func(p string) tea.Cmd {
		innerCalled = true
		return func() tea.Msg { return editorFinishedMsg{} }
	}

	opener := wrapEditorOpenerForSecrets(sess, func(string) (string, error) {
		return "pw", nil
	}, nil, "vi", config.EditorModeTmuxSplit, func() int { return 80 }, inner)
	cmd := opener(path)

	if cmd == nil {
		t.Fatal("expected a non-nil Cmd for an encrypted file in tmux mode")
	}
	if innerCalled {
		t.Fatal("expected inner not to be called for an encrypted file in tmux mode; the pane must be pointed at the decrypted temp file, not the ciphertext path")
	}
}

func TestWrapEditorOpenerForSecrets_TmuxModePlainFilePassesThrough(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plain.txt")
	if err := os.WriteFile(path, []byte("plain content"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sess := secretfile.NewSession()
	var innerCalledWith string
	inner := func(p string) tea.Cmd {
		innerCalledWith = p
		return func() tea.Msg { return editorFinishedMsg{} }
	}

	opener := wrapEditorOpenerForSecrets(sess, func(string) (string, error) {
		t.Fatal("should not prompt for a plain file")
		return "", nil
	}, nil, "vi", config.EditorModeTmuxWindow, func() int { return 80 }, inner)
	opener(path)()

	if innerCalledWith != path {
		t.Fatalf("expected a plain file in tmux mode to pass through to inner unmodified, got %q", innerCalledWith)
	}
}

func TestSecretExecCommand_BuildEditorCmd_TmuxModeTargetsTempFile(t *testing.T) {
	c := &secretExecCommand{
		tmuxMode:   config.EditorModeTmuxSplit,
		tmuxEditor: "nvim",
		getWidth:   func() int { return 200 },
	}
	cmd := c.buildEditorCmd("/tmp/ocode-secretedit-123/secret.txt")

	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "split-window") {
		t.Fatalf("expected a tmux split-window invocation, got args %v", cmd.Args)
	}
	if !strings.Contains(joined, "/tmp/ocode-secretedit-123/secret.txt") {
		t.Fatalf("expected the pane command to target the decrypted temp file, got args %v", cmd.Args)
	}
	if strings.Contains(joined, "nvim") == false {
		t.Fatalf("expected the pane command to invoke the configured editor, got args %v", cmd.Args)
	}
}

func TestWrapEditorOpenerForSecrets_EncryptedFileDoesNotCallInner(t *testing.T) {
	root, key := setupProject(t)
	path := filepath.Join(root, "secret.txt")
	ciphertext, err := key.Encrypt([]byte("data"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if err := os.WriteFile(path, ciphertext, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sess := secretfile.NewSession()
	innerCalled := false
	inner := func(p string) tea.Cmd {
		innerCalled = true
		return func() tea.Msg { return editorFinishedMsg{} }
	}

	opener := wrapEditorOpenerForSecrets(sess, func(string) (string, error) { return "pw", nil }, nil, "vi", "external", func() int { return 80 }, inner)
	cmd := opener(path)
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd for an encrypted file")
	}
	if innerCalled {
		t.Fatal("expected inner not to be called directly for an encrypted file; the real editor invocation is built inside secretExecCommand")
	}
}
