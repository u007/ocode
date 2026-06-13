package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildLearnPromptIncludesWorkflowInstructionsAndContext(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	mustWriteLearnPromptFile(t, filepath.Join(root, "internal", "tui", "model.go"), "package tui\n")
	mustWriteLearnPromptFile(t, filepath.Join(root, "internal", "tui", "view.go"), "package tui\n")
	mustWriteLearnPromptFile(t, filepath.Join(root, "skills", "ocode-tui", "SKILL.md"), `---
name: ocode-tui
description: TUI architecture guide.
when_to_use: Use when working on internal/tui.
---

# ocode-tui
`)

	prompt, err := buildLearnPrompt(root, []string{"tui", "coverage"})
	if err != nil {
		t.Fatalf("buildLearnPrompt: %v", err)
	}
	for _, want := range []string{
		"You are the /learn command for this repository.",
		"Load the skill-creator skill",
		"explore subagent",
		"Ready-to-use context-manager prompt",
		"Repository learn context",
		"Project-root skills",
		"did not inspect repo modules or sections",
		"ocode-tui",
		"skills/ocode-tui/SKILL.md",
		"User focus: tui coverage",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in prompt, got:\n%s", want, prompt)
		}
	}
}

func mustWriteLearnPromptFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
