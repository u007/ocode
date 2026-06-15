package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/memory"
)

func TestBuildMemUpdatePromptTargetsSelectedScope(t *testing.T) {
	wd := t.TempDir()
	home := filepath.Join(wd, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll home: %v", err)
	}
	t.Setenv("HOME", home)

	snap, err := memory.Status(wd)
	if err != nil {
		t.Fatalf("memory.Status: %v", err)
	}
	for path, body := range map[string]string{
		snap.User.Path:    "remember user prefs\n",
		snap.Project.Path: "remember project decisions\n",
		snap.Global.Path:  "remember global lessons\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}

	prompt, err := buildMemUpdatePrompt(wd, []string{"user", "stable", "preference"})
	if err != nil {
		t.Fatalf("buildMemUpdatePrompt: %v", err)
	}
	for _, want := range []string{
		"You are the /mem update command for ocode.",
		"Scope: user",
		"Target scope: user preferences",
		snap.User.Path,
		"User memory",
		"remember user prefs",
		"User focus: stable preference",
		"Update only the selected scope file",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in prompt:\n%s", want, prompt)
		}
	}

	projectPrompt, err := buildMemUpdatePrompt(wd, []string{"remember", "project", "history"})
	if err != nil {
		t.Fatalf("buildMemUpdatePrompt project default: %v", err)
	}
	if !strings.Contains(projectPrompt, "Scope: project") {
		t.Fatalf("expected project default scope, got:\n%s", projectPrompt)
	}
	if !strings.Contains(projectPrompt, "User focus: remember project history") {
		t.Fatalf("expected default focus to include args, got:\n%s", projectPrompt)
	}
}
