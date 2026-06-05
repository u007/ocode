package tui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

var (
	tmuxGetenv     = os.Getenv
	tmuxLookPath   = exec.LookPath
	tmuxExecutable = "tmux"
	tmuxWaitSeq    uint64
)

type tmuxRunner func(args ...string) (string, error)

var runTmuxCmd tmuxRunner = func(args ...string) (string, error) {
	cmd := exec.Command(tmuxExecutable, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func validateTmuxEditorMode(mode string) error {
	if mode != config.EditorModeTmuxSplit && mode != config.EditorModeTmuxWindow {
		return nil
	}

	if tmuxGetenv("TMUX") == "" {
		return fmt.Errorf("tmux editor mode %q requires running inside tmux.\nStart ocode inside a tmux session, or set editor_mode to %q with:\n  /editor-mode %s", mode, config.EditorModeExternal, config.EditorModeExternal)
	}

	if _, err := tmuxLookPath(tmuxExecutable); err != nil {
		return fmt.Errorf("tmux editor mode %q requires the tmux binary in PATH", mode)
	}

	if _, err := runTmuxCmd("display-message", "-p", "#S"); err != nil {
		return fmt.Errorf("failed to communicate with tmux server: %w", err)
	}

	return nil
}

func validateStartupEditorConfig(cfg *config.OcodeConfig) error {
	if cfg == nil || !isTmuxMode(cfg.EditorMode) {
		return nil
	}
	if err := validateTmuxEditorMode(cfg.EditorMode); err != nil {
		return err
	}
	if err := validateEditorCmd(config.ResolveEditor(cfg)); err != nil {
		return err
	}
	return nil
}

func validateEditorCmd(editor string) error {
	if editor == "" {
		return fmt.Errorf("no editor configured")
	}
	cmdParts := strings.Fields(editor)
	if len(cmdParts) == 0 {
		return fmt.Errorf("editor command is empty")
	}
	if _, err := tmuxLookPath(cmdParts[0]); err != nil {
		return fmt.Errorf("editor %q not found in PATH (%w)", cmdParts[0], err)
	}
	return nil
}

func buildTmuxOpenCmd(mode string, editor string, path string, width int) teaCmdBuilder {
	return func() *exec.Cmd {
		cmdParts := strings.Fields(editor)
		cmdParts = append(cmdParts, shellQuote(path))
		editorCmd := strings.Join(cmdParts, " ")
		waitToken := fmt.Sprintf("ocode-editor-%d-%d", os.Getpid(), atomic.AddUint64(&tmuxWaitSeq, 1))
		paneCmd := editorCmd + "; tmux wait-for -S " + shellQuote(waitToken)

		switch mode {
		case config.EditorModeTmuxWindow:
			return exec.Command(tmuxExecutable, "new-window", paneCmd, ";", "wait-for", waitToken)
		default:
			if width >= 120 {
				return exec.Command(tmuxExecutable, "split-window", "-h", paneCmd, ";", "wait-for", waitToken)
			}
			return exec.Command(tmuxExecutable, "split-window", "-v", paneCmd, ";", "wait-for", waitToken)
		}
	}
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type teaCmdBuilder func() *exec.Cmd

func createEditorOpener(editor, mode string, getWidth func() int, sup *tool.ProcessSupervisor) func(string) tea.Cmd {
	if mode != config.EditorModeTmuxSplit && mode != config.EditorModeTmuxWindow {
		return func(path string) tea.Cmd {
			cmdParts := strings.Fields(editor)
			// Validate the editor binary exists before attempting to run it.
			if _, err := exec.LookPath(cmdParts[0]); err != nil {
				log.Printf("[editor] editor binary not found in PATH: %q (full editor string: %q, file: %q)", cmdParts[0], editor, path)
				return func() tea.Msg {
					return editorFinishedMsg{err: fmt.Errorf("editor %q not found in PATH: %w", cmdParts[0], err)}
				}
			}
			cmdParts = append(cmdParts, path)
			c := exec.Command(cmdParts[0], cmdParts[1:]...)
			if runtime.GOOS != "windows" {
				c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			}
			log.Printf("[editor] launching external editor: %q  file=%q  full_cmd=%v", editor, path, cmdParts)
			id := fmt.Sprintf("editor-%d-%d", os.Getpid(), time.Now().UnixNano())
			if sup != nil {
				_, _ = sup.Register(tool.ProcessRegistration{
					ID:               id,
					Command:          editor + " " + path,
					Kind:             tool.ProcessKindEditor,
					Cmd:              c,
					OwnsProcessGroup: runtime.GOOS != "windows",
					StartedAt:        time.Now(),
				})
			}
			return tea.ExecProcess(c, func(err error) tea.Msg {
				if sup != nil {
					if err == nil {
						sup.MarkExited(id, 0)
					} else {
						code := 1
						if exitErr, ok := err.(*exec.ExitError); ok {
							code = exitErr.ExitCode()
						}
						sup.MarkKilled(id, code)
					}
				}
				log.Printf("[editor] finished: %q  file=%q  err=%v", editor, path, err)
				return editorFinishedMsg{err: err}
			})
		}
	}

	return func(path string) tea.Cmd {
		width := 80
		if getWidth != nil {
			width = getWidth()
		}
		builder := buildTmuxOpenCmd(mode, editor, path, width)
		c := builder()
		log.Printf("[editor] launching tmux editor: mode=%q editor=%q file=%q cmd=%v", mode, editor, path, c.Args)
		return tea.ExecProcess(c, func(err error) tea.Msg {
			log.Printf("[editor] tmux editor finished: mode=%q editor=%q file=%q err=%v", mode, editor, path, err)
			return editorFinishedMsg{err: err}
		})
	}
}
