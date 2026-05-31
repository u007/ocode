package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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

func LoadContext(enabled map[string]bool) string {
	var context string
	files := []string{"AGENTS.md", "CLAUDE.md", "OCODE.md", ".cursorrules"}

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

	if pluginInstr := plugins.LoadPluginInstructions(enabled); pluginInstr != "" {
		context += pluginInstr
	}
	if skillCatalog := skill.BuildCatalog(); skillCatalog != "" {
		context += skillCatalog
	}

	return context
}

// globalOcodeDir returns the path to the global ocode configuration directory
// (~/.config/opencode/ on Unix, %APPDATA%/opencode/ on Windows).
func globalOcodeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "opencode")
	}
	return filepath.Join(home, ".config", "opencode")
}

// LoadModelContext loads model-specific OCODE.md files from the project root,
// .opencode/ directory, and global config directory (~/.config/opencode/).
// Files must match the pattern {MODEL_NAME}.OCODE.md (case-insensitive).
// When the active model matches the stem, the file content is included.
// Uses the same git-aware HEAD-vs-working-tree logic as readContextFile.
//
// Precedence: project root > .opencode/ > global config for the same model.
func LoadModelContext(modelName string) string {
	if modelName == "" {
		return ""
	}
	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	if modelLower == "" {
		return ""
	}

	// Collect matching files — priority: project root > .opencode/ > global config.
	// Map key = stem (lowercased) so earlier directories win for the same model.
	found := make(map[string]string) // stem (lowercase) → filepath

	searchDirs := []string{".", filepath.Join(".", ".opencode")}
	if gd := globalOcodeDir(); gd != "" {
		searchDirs = append(searchDirs, gd)
	}
	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			upper := strings.ToUpper(name)
			if !strings.HasSuffix(upper, ".OCODE.MD") {
				continue
			}
			// The suffix is case-insensitive but we need the actual length.
			// Last 9 chars are ".OCODE.md" (or equivalent).
			stem := name[:len(name)-len(".OCODE.md")]
			stemLower := strings.ToLower(stem)
			if stemLower != modelLower {
				continue
			}
			fullPath := filepath.Join(dir, name)
			// Root wins — don't overwrite if already found.
			if _, exists := found[stemLower]; !exists {
				found[stemLower] = fullPath
			}
		}
	}

	if len(found) == 0 {
		return ""
	}

	var context string
	for _, filePath := range found {
		// Use readContextFile for git-aware HEAD-vs-working-tree logic,
		// consistent with how AGENTS.md, CLAUDE.md, and OCODE.md are loaded.
		if content, ok := readContextFile(filePath); ok {
			context += "\n--- " + filepath.Base(filePath) + " ---\n" + content + "\n"
		}
	}
	return context
}
