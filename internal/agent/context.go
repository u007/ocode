package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jamesmercstudio/ocode/internal/plugins"
	"github.com/jamesmercstudio/ocode/internal/skill"
)

// gitShowHead reads a file from the git HEAD revision. Returns empty string if
// the file is not tracked by git or if the repo is unavailable.
func gitShowHead(path string) string {
	if _, err := os.Stat(filepath.Join(".git")); os.IsNotExist(err) {
		return "" // not a git repo
	}
	cmd := exec.Command("git", "show", "HEAD:"+path)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// hasUnstagedChanges returns true if the file has unstaged changes compared to
// the git index (i.e. modifications not yet staged).
func hasUnstagedChanges(path string) bool {
	cmd := exec.Command("git", "diff", "--exit-code", "--", path)
	// Exit code 0 = no diff, 1 = diff found, other = error
	return cmd.Run() != nil
}

// readContextFile reads the best available version of a context file:
//  1. If the file has no unstaged changes OR is untracked, read from the working tree.
//  2. If the file has unstaged changes, read from git HEAD (stable committed version).
//
// This ensures that local edits to AGENTS.md, CLAUDE.md, etc. do not silently
// alter the base context sent to the LLM. The user can commit the changes to
// make them effective.
func readContextFile(path string) (string, bool) {
	// First check if the file is tracked by git and has unstaged changes.
	// If gitShowHead succeeds the file IS tracked; if hasUnstagedChanges
	// returns true there are working-tree modifications.
	if head := gitShowHead(path); head != "" {
		if hasUnstagedChanges(path) {
			// File tracked by git AND has unstaged changes — use HEAD version.
			emitDebug("CONTEXT", fmt.Sprintf("using HEAD version of %s due to unstaged changes", path))
			return head, true
		}
		// File tracked, no unstaged changes — use working tree.
	}

	// Fallback: read from working tree.
	content, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(content), true
}

func LoadContext() string {
	var context string
	files := []string{"AGENTS.md", "CLAUDE.md", ".cursorrules"}

	for _, f := range files {
		if content, ok := readContextFile(f); ok {
			context += "\n--- " + f + " ---\n" + content + "\n"
		}
	}

	rulesDir := filepath.Join(".opencode", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
				path := filepath.Join(rulesDir, entry.Name())
				if content, err := os.ReadFile(path); err == nil {
					context += "\n--- " + entry.Name() + " ---\n" + string(content) + "\n"
				}
			}
		}
	}

	if pluginInstr := plugins.LoadPluginInstructions(); pluginInstr != "" {
		context += pluginInstr
	}
	if skillCatalog := skill.BuildCatalog(); skillCatalog != "" {
		context += skillCatalog
	}

	return context
}
