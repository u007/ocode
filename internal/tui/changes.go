package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/lsp"
)

// specFileNames are markdown file names that typically contain specs, designs,
// or planning docs. Only the basename is compared (case-insensitive).
var specFileNames = map[string]bool{
	"spec.md":            true,
	"design.md":          true,
	"architecture.md":    true,
	"plan.md":            true,
	"todo.md":            true,
	"roadmap.md":         true,
	"enhancement.md":     true,
	"enhancement_plan.md": true,
	"implementation.md":  true,
	"rfc.md":             true,
	"proposal.md":        true,
	"adr.md":             true,
	"changelog.md":       true,
	"notes.md":           true,
	"wip.md":             true,
}

// getChangesContext gathers all context for the /changes command: git status,
// staged diff, unstaged diff, LSP diagnostics, and in-progress spec/design
// markdown files.
func getChangesContext(workDir string, lspMgr *lsp.Manager) (string, error) {
	var b strings.Builder

	// --- Git status ---
	statusOut, err := gitRun(workDir, "status", "--short")
	if err == nil && statusOut != "" {
		b.WriteString("## Git Status\n")
		b.WriteString(statusOut)
		b.WriteString("\n\n")
	}

	// --- Staged diff ---
	stagedDiff, err := gitRun(workDir, "diff", "--cached")
	if err == nil && stagedDiff != "" {
		b.WriteString("## Staged Changes\n")
		b.WriteString(stagedDiff)
		b.WriteString("\n\n")
	}

	// --- Unstaged diff ---
	unstagedDiff, err := gitRun(workDir, "diff")
	if err == nil && unstagedDiff != "" {
		b.WriteString("## Unstaged Changes\n")
		b.WriteString(unstagedDiff)
		b.WriteString("\n\n")
	}

	// --- LSP diagnostics ---
	if lspMgr != nil {
		if store := lspMgr.Diagnostics(); store != nil && !store.IsEmpty() {
			snap := store.Snapshot(50)
			if snap.Total > 0 {
				b.WriteString("## LSP Diagnostics\n")
				b.WriteString(fmt.Sprintf("%d total across %d file(s)\n", snap.Total, snap.Files))
				for i, d := range snap.FirstN {
					if i > 0 {
						b.WriteByte('\n')
					}
					b.WriteString(fmt.Sprintf("  %s:%d:%d  [%s]  %s",
						formatSidebarFilePath(d.Path, workDir, 80),
						d.Range.Start.Line+1, d.Range.Start.Character+1,
						d.Severity.String(), d.Message))
				}
				if snap.Total > len(snap.FirstN) {
					b.WriteString(fmt.Sprintf("\n  (showing first %d of %d)", len(snap.FirstN), snap.Total))
				}
				b.WriteString("\n\n")
			}
		}
	}

	// --- Spec / design markdown files ---
	specFiles := findSpecFiles(workDir)
	if len(specFiles) > 0 {
		b.WriteString("## Spec / Design Files (in-progress)\n")
		for _, sf := range specFiles {
			b.WriteString(fmt.Sprintf("### %s\n", sf.relPath))
			// Read first 80 lines to give the LLM a taste without bloating.
			content := readFileLines(sf.absPath, 80)
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		}
	}

	if b.Len() == 0 {
		return "", fmt.Errorf("no changes, diagnostics, or spec files found")
	}

	return b.String(), nil
}

// specFileInfo holds paths for a discovered spec/design markdown file.
type specFileInfo struct {
	relPath string // relative to workDir
	absPath string // absolute
}

// findSpecFiles walks the repo (max depth 2) looking for markdown files whose
// base name matches specFileNames. It also picks up any .md file in the root
// that looks like a spec/design document (name contains "spec", "design",
// "plan", "todo", "rfc", "proposal", "adr", "wip").
func findSpecFiles(workDir string) []specFileInfo {
	var result []specFileInfo

	// First pass: exact matches from the known set.
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && path != workDir {
				// Only go 2 levels deep.
				rel, _ := filepath.Rel(workDir, path)
				if strings.Count(rel, string(os.PathSeparator)) >= 2 {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if d.Name() == ".git" || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		base := strings.ToLower(d.Name())
		if specFileNames[base] {
			rel, _ := filepath.Rel(workDir, path)
			result = append(result, specFileInfo{relPath: rel, absPath: path})
		}
		return nil
	})

	// Second pass: root-level .md files with spec-like keywords in name.
	entries, err := os.ReadDir(workDir)
	if err == nil {
		seen := make(map[string]bool, len(result))
		for _, r := range result {
			seen[strings.ToLower(r.relPath)] = true
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				continue
			}
			base := strings.ToLower(e.Name())
			if seen[base] {
				continue
			}
			if specFileNames[base] {
				continue // already caught above
			}
			// Heuristic: name contains a spec-like keyword.
			if strings.Contains(base, "spec") || strings.Contains(base, "design") ||
				strings.Contains(base, "plan") || strings.Contains(base, "todo") ||
				strings.Contains(base, "rfc") || strings.Contains(base, "proposal") ||
				strings.Contains(base, "adr") || strings.Contains(base, "wip") ||
				strings.Contains(base, "roadmap") || strings.Contains(base, "enhancement") {
				result = append(result, specFileInfo{relPath: e.Name(), absPath: filepath.Join(workDir, e.Name())})
			}
		}
	}

	// Cap at 5 files to avoid context bloat.
	if len(result) > 5 {
		result = result[:5]
	}
	return result
}

// readFileLines reads the first n lines of a file and returns them as a string.
func readFileLines(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.SplitN(string(data), "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
		lines = append(lines, "... (truncated)")
	}
	return strings.Join(lines, "\n")
}

// buildChangesPrompt creates the LLM prompt for the /changes command.
func buildChangesPrompt(context string) string {
	var b strings.Builder
	b.WriteString("You are a change analysis assistant. Analyze the following repository changes and provide a clear, structured overview.\n\n")
	b.WriteString("## Instructions\n\n")
	b.WriteString("Provide your analysis in these sections:\n\n")
	b.WriteString("### 1. Overview\n")
	b.WriteString("A brief 1-2 sentence summary of what's happening in this changeset.\n\n")
	b.WriteString("### 2. Changed Files\n")
	b.WriteString("Group changes by purpose (e.g. \"auth module\", \"UI fixes\"). List each file with a one-line description of what changed.\n\n")
	b.WriteString("### 3. LSP Errors\n")
	b.WriteString("List any LSP diagnostics (errors, warnings) found in the changed files. If none, say \"No LSP issues.\"\n\n")
	b.WriteString("### 4. Spec / Design Status\n")
	b.WriteString("For any spec or design markdown files found, note whether they appear complete or have TODO/WIP markers. Flag anything that looks unfinished.\n\n")
	b.WriteString("### 5. Risks & Suggestions\n")
	b.WriteString("Call out potential issues: missing error handling, incomplete implementations, broken patterns, or anything that looks like it needs follow-up.\n\n")
	b.WriteString("Be concise. Use bullet points. No filler.\n\n")
	b.WriteString("---\n\n")
	b.WriteString("## Repository Changes\n\n")
	b.WriteString(context)
	return b.String()
}

// runChangesCmd is the command wrapper for /changes.
func runChangesCmd(m *model, args []string) tea.Cmd {
	return m.handleChangesCmd(args)
}

// handleChangesCmd gathers all change context and sends it to the agent.
func (m *model) handleChangesCmd(args []string) tea.Cmd {
	ctx, err := getChangesContext(m.workDir, m.lspMgr)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("/changes: %v", err)})
		return nil
	}
	prompt := buildChangesPrompt(ctx)
	return m.sendCustomCommandPrompt(prompt)
}
