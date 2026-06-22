package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const docSyncMaxDiffBytes = 32 * 1024

func buildDocSyncPrompt(workDir string, mode string) (string, error) {
	diff, err := collectDocSyncDiff(workDir, mode)
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("no changes found in git diff — nothing to sync")
	}

	docFiles := collectDocSyncTargets(workDir)

	var b strings.Builder
	b.WriteString("You are running /doc-sync for this repository.\n\n")

	b.WriteString("Goal: update project documentation to accurately reflect the current codebase — nothing more.\n\n")

	b.WriteString("Files you MAY update:\n")
	if len(docFiles) == 0 {
		b.WriteString("  (none found — AGENTS.md, .opencode/rules/*.md, .opencode/skills/*/SKILL.md)\n")
	} else {
		for _, f := range docFiles {
			b.WriteString("  - ")
			b.WriteString(f)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	b.WriteString("Rules (enforce strictly):\n")
	b.WriteString("- Read each candidate file before deciding whether it needs a change.\n")
	b.WriteString("- Only fix what is PROVABLY wrong or missing given the diff below — no speculative improvements.\n")
	b.WriteString("- Do not increase file length unless filling a genuine gap the diff introduced.\n")
	b.WriteString("- Noop is the correct answer most of the time.\n")
	b.WriteString("- Never touch CLAUDE.md.\n")
	b.WriteString("- Edit in-place with the Edit tool — do not rewrite whole files unless the file is tiny (<20 lines).\n")
	b.WriteString("- After all edits, summarise what changed and why in one short paragraph.\n\n")

	b.WriteString("Session changes (git diff):\n")
	b.WriteString("```diff\n")
	b.WriteString(diff)
	b.WriteString("\n```\n")

	return b.String(), nil
}

// collectDocSyncDiff runs git diff for the requested scope.
// "session" (default) = uncommitted changes (staged + unstaged vs HEAD).
// "all" = last 10 commits + uncommitted.
func collectDocSyncDiff(workDir, mode string) (string, error) {
	var args []string
	if mode == "all" {
		// diff from 10 commits ago to working tree
		args = []string{"diff", "HEAD~10"}
	} else {
		// uncommitted changes only
		args = []string{"diff", "HEAD"}
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		// git exits non-zero on empty diff in some versions; treat as empty not error
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) == 0 {
			return "", nil
		}
		return "", err
	}
	result := string(out)
	if len(result) > docSyncMaxDiffBytes {
		result = result[:docSyncMaxDiffBytes] + "\n... (diff truncated)\n"
	}
	return result, nil
}

// collectDocSyncTargets returns the doc files that may be updated, relative paths.
func collectDocSyncTargets(workDir string) []string {
	var files []string

	for _, name := range []string{"AGENTS.md", "OCODE.md"} {
		if _, err := os.Stat(filepath.Join(workDir, name)); err == nil {
			files = append(files, name)
		}
	}

	rulesDir := filepath.Join(workDir, ".opencode", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				files = append(files, filepath.Join(".opencode", "rules", e.Name()))
			}
		}
	}

	skillsDir := filepath.Join(workDir, ".opencode", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				p := filepath.Join(".opencode", "skills", e.Name(), "SKILL.md")
				if _, err := os.Stat(filepath.Join(workDir, p)); err == nil {
					files = append(files, p)
				}
			}
		}
	}

	return files
}

func (m *model) handleDocSyncCmd(args []string) tea.Cmd {
	mode := "session"
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "all":
			mode = "all"
		case "session":
			mode = "session"
		default:
			m.messages = append(m.messages, message{
				role: roleAssistant,
				text: fmt.Sprintf("/doc-sync: unknown mode %q — use 'session' or 'all'", args[0]),
			})
			return nil
		}
	}

	prompt, err := buildDocSyncPrompt(m.workDir, mode)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("/doc-sync: %v", err)})
		return nil
	}

	if m.agent != nil {
		m.agent.ResetSubagentDispatch()
	}
	m.rerenderTranscriptAndMaybeScroll()
	return m.sendCustomCommandPrompt(prompt)
}
