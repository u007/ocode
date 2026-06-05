package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestValidateTmuxEditorMode(t *testing.T) {
	origGetenv := tmuxGetenv
	origLookPath := tmuxLookPath
	origRunTmux := runTmuxCmd
	t.Cleanup(func() {
		tmuxGetenv = origGetenv
		tmuxLookPath = origLookPath
		runTmuxCmd = origRunTmux
	})

	t.Run("external mode no error", func(t *testing.T) {
		if err := validateTmuxEditorMode(config.EditorModeExternal); err != nil {
			t.Fatalf("external mode should not error, got: %v", err)
		}
	})

	t.Run("empty mode no error", func(t *testing.T) {
		if err := validateTmuxEditorMode(""); err != nil {
			t.Fatalf("empty mode should not error, got: %v", err)
		}
	})

	t.Run("missing TMUX env", func(t *testing.T) {
		tmuxGetenv = func(key string) string {
			if key == "TMUX" {
				return ""
			}
			return os.Getenv(key)
		}

		err := validateTmuxEditorMode(config.EditorModeTmuxSplit)
		if err == nil {
			t.Fatal("expected error for missing TMUX")
		}
		if !strings.Contains(err.Error(), "requires running inside tmux") {
			t.Fatalf("error should mention tmux, got: %v", err)
		}
		if !strings.Contains(err.Error(), config.EditorModeExternal) {
			t.Fatalf("error should mention fix to external, got: %v", err)
		}
	})

	t.Run("missing tmux binary", func(t *testing.T) {
		tmuxGetenv = func(key string) string {
			if key == "TMUX" {
				return "/dev/pts/1"
			}
			return os.Getenv(key)
		}
		tmuxLookPath = func(file string) (string, error) {
			return "", fmt.Errorf("not found")
		}

		err := validateTmuxEditorMode(config.EditorModeTmuxWindow)
		if err == nil {
			t.Fatal("expected error for missing tmux binary")
		}
		if !strings.Contains(err.Error(), "requires the tmux binary") {
			t.Fatalf("error should mention tmux binary, got: %v", err)
		}
	})

	t.Run("tmux command fails", func(t *testing.T) {
		tmuxGetenv = func(key string) string {
			if key == "TMUX" {
				return "/dev/pts/1"
			}
			return os.Getenv(key)
		}
		tmuxLookPath = func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		}
		runTmuxCmd = func(args ...string) (string, error) {
			return "", errors.New("connection refused")
		}

		err := validateTmuxEditorMode(config.EditorModeTmuxSplit)
		if err == nil {
			t.Fatal("expected error for failed tmux command")
		}
		if !strings.Contains(err.Error(), "failed to communicate with tmux") {
			t.Fatalf("error should mention tmux server, got: %v", err)
		}
	})

	t.Run("valid tmux session", func(t *testing.T) {
		tmuxGetenv = func(key string) string {
			if key == "TMUX" {
				return "/dev/pts/1"
			}
			return os.Getenv(key)
		}
		tmuxLookPath = func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		}
		runTmuxCmd = func(args ...string) (string, error) {
			return "my-session", nil
		}

		if err := validateTmuxEditorMode(config.EditorModeTmuxSplit); err != nil {
			t.Fatalf("valid tmux should not error, got: %v", err)
		}
	})
}

func TestValidateStartupEditorConfigRequiresEditorForTmuxMode(t *testing.T) {
	origGetenv := tmuxGetenv
	origLookPath := tmuxLookPath
	origRunTmux := runTmuxCmd
	t.Cleanup(func() {
		tmuxGetenv = origGetenv
		tmuxLookPath = origLookPath
		runTmuxCmd = origRunTmux
	})

	tmuxGetenv = func(key string) string {
		if key == "TMUX" {
			return "/tmp/tmux"
		}
		return os.Getenv(key)
	}
	tmuxLookPath = func(file string) (string, error) {
		if file == "tmux" {
			return "/usr/bin/tmux", nil
		}
		return "", fmt.Errorf("not found")
	}
	runTmuxCmd = func(args ...string) (string, error) { return "session", nil }

	err := validateStartupEditorConfig(&config.OcodeConfig{Editor: "missing-editor", EditorMode: config.EditorModeTmuxSplit})
	if err == nil {
		t.Fatal("expected missing editor error")
	}
	if !strings.Contains(err.Error(), "missing-editor") {
		t.Fatalf("expected error to mention missing editor, got: %v", err)
	}
}

func TestValidateEditorCmd(t *testing.T) {
	origLookPath := tmuxLookPath
	t.Cleanup(func() { tmuxLookPath = origLookPath })

	t.Run("empty editor fails", func(t *testing.T) {
		err := validateEditorCmd("")
		if err == nil {
			t.Fatal("expected error for empty editor")
		}
	})

	t.Run("editor not found in PATH", func(t *testing.T) {
		tmuxLookPath = func(file string) (string, error) {
			return "", fmt.Errorf("not found")
		}
		err := validateEditorCmd("nonexistent-editor")
		if err == nil {
			t.Fatal("expected error for missing editor")
		}
		if !strings.Contains(err.Error(), "not found in PATH") {
			t.Fatalf("error should mention PATH, got: %v", err)
		}
	})

	t.Run("valid editor passes", func(t *testing.T) {
		tmuxLookPath = func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		}
		if err := validateEditorCmd("nvim"); err != nil {
			t.Fatalf("expected no error for valid editor, got: %v", err)
		}
	})
}

func TestBuildTmuxOpenCmd(t *testing.T) {
	t.Run("tmux-window mode", func(t *testing.T) {
		builder := buildTmuxOpenCmd(config.EditorModeTmuxWindow, "nvim", "/path/to/file.go", 80)
		cmd := builder()
		if len(cmd.Args) < 2 {
			t.Fatal("expected at least 2 args")
		}
		if cmd.Args[0] != "tmux" {
			t.Fatalf("expected tmux, got %s", cmd.Args[0])
		}
		if cmd.Args[1] != "new-window" {
			t.Fatalf("expected new-window, got %s", cmd.Args[1])
		}
		if !strings.Contains(cmd.Args[2], "nvim '/path/to/file.go'") {
			t.Fatalf("expected editor command, got %s", cmd.Args[2])
		}
	})

	t.Run("quotes path with spaces", func(t *testing.T) {
		builder := buildTmuxOpenCmd(config.EditorModeTmuxSplit, "nvim", "/path/with space/file.go", 120)
		cmd := builder()
		joined := strings.Join(cmd.Args, " ")
		if !strings.Contains(joined, "'/path/with space/file.go'") {
			t.Fatalf("expected shell-quoted path with spaces, got %#v", cmd.Args)
		}
	})

	t.Run("waits for editor exit before returning", func(t *testing.T) {
		builder := buildTmuxOpenCmd(config.EditorModeTmuxSplit, "nvim", "/path/to/file.go", 120)
		cmd := builder()
		joined := strings.Join(cmd.Args, " ")
		if !strings.Contains(joined, "wait-for") {
			t.Fatalf("expected tmux wait-for command, got %#v", cmd.Args)
		}
	})

	t.Run("tmux-split with wide terminal", func(t *testing.T) {
		builder := buildTmuxOpenCmd(config.EditorModeTmuxSplit, "nvim", "/path/to/file.go", 120)
		cmd := builder()
		if cmd.Args[0] != "tmux" {
			t.Fatalf("expected tmux, got %s", cmd.Args[0])
		}
		if cmd.Args[1] != "split-window" {
			t.Fatalf("expected split-window, got %s", cmd.Args[1])
		}
		if cmd.Args[2] != "-h" {
			t.Fatalf("expected -h for wide terminal, got %s", cmd.Args[2])
		}
	})

	t.Run("tmux-split with narrow terminal", func(t *testing.T) {
		builder := buildTmuxOpenCmd(config.EditorModeTmuxSplit, "nvim", "/path/to/file.go", 80)
		cmd := builder()
		if cmd.Args[1] != "split-window" {
			t.Fatalf("expected split-window, got %s", cmd.Args[1])
		}
		if cmd.Args[2] != "-v" {
			t.Fatalf("expected -v for narrow terminal, got %s", cmd.Args[2])
		}
	})
}
