package agent

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/commands"
	"github.com/u007/ocode/internal/memory"
	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/skill"
)

// bundledModelConfigFS, when set via SetBundledModelConfigFS, is an embedded
// filesystem containing model-specific OCODE.md files (e.g.
// deepseek-v4-flash.OCODE.md). LoadModelContext falls back to this when no
// disk-based file matches the active model, ensuring every build ships with
// its own model instructions baked in.
var bundledModelConfigFS fs.FS

// SetBundledModelConfigFS registers an embedded filesystem of model-specific
// OCODE.md files. Called from main() during startup; the FS is used as a
// fallback by LoadModelContext when no matching file is found on disk.
func SetBundledModelConfigFS(fsys fs.FS) {
	bundledModelConfigFS = fsys
}

// loadBundledModelContext reads the embedded OCODE.md file for the given
// model name from bundledModelConfigFS. Returns the file content formatted
// with the standard framing header, or empty string if no embedded file
// matches.
func loadBundledModelContext(modelName string) string {
	if bundledModelConfigFS == nil {
		return ""
	}
	name := modelName + ".OCODE.md"
	content, err := fs.ReadFile(bundledModelConfigFS, name)
	if err != nil {
		return ""
	}
	return "\n--- " + name + " ---\n" + string(content) + "\n"
}

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

func LoadContext(enabled map[string]bool, memoryEnabled bool) string {
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
	if refCatalog := BuildReferenceCatalog(enabled); refCatalog != "" {
		context += refCatalog
	}
	if memoryEnabled {
		if mem := memory.PromptFragment(""); mem != "" {
			context += mem
		}
	}

	return context
}

// BuildReferenceCatalog returns a compact prompt fragment describing the
// currently available slash commands and agents, plus the rule that mentions
// should trigger loading the corresponding prompt/skill before answering.
//
// Skills are already covered by skill.BuildCatalog(); this fragment focuses on
// the remaining reference types so the system prompt stays concise while still
// giving the model a clear loading rule.
func BuildReferenceCatalog(enabled map[string]bool) string {
	var b strings.Builder
	b.WriteString("\n--- Reference Guidance ---\n")
	b.WriteString("When a slash command, skill, or agent is mentioned by name, load the matching command prompt, SKILL.md, or agent system prompt before responding.\n")

	cmds := commands.LoadCommands(enabled)
	if len(cmds) > 0 {
		sort.Slice(cmds, func(i, j int) bool {
			return strings.ToLower(cmds[i].Name) < strings.ToLower(cmds[j].Name)
		})
		b.WriteString("\nSlash Command Catalog\n")
		b.WriteString(strings.Repeat("-", 23))
		b.WriteString("\n")
		for _, cmd := range cmds {
			b.WriteString("- /")
			b.WriteString(cmd.Name)
			if cmd.Description != "" {
				b.WriteString(": ")
				b.WriteString(cmd.Description)
			}
			b.WriteString("\n")
		}
	}

	agents := PrimaryAgentSpecs()
	if len(agents) > 0 {
		sort.Slice(agents, func(i, j int) bool {
			return strings.ToLower(agents[i].Name) < strings.ToLower(agents[j].Name)
		})
		b.WriteString("\nAgent Catalog\n")
		b.WriteString(strings.Repeat("-", 13))
		b.WriteString("\n")
		for _, spec := range agents {
			b.WriteString("- ")
			b.WriteString(spec.Name)
			if spec.Description != "" {
				b.WriteString(": ")
				b.WriteString(spec.Description)
			}
			b.WriteString("\n")
		}
	}

	return b.String()
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
// Stem matching rules:
//   - Exact: stem (lowercased) equals the model id (lowercased). This is the
//     original behavior.
//   - Wildcard: a stem that ends in a single trailing '*' matches any model
//     whose id starts with the stem's prefix. A bare '*' stem is rejected to
//     prevent an accidental "matches everything" file. Only a trailing '*' is
//     treated as a wildcard; '*' anywhere else in the stem is literal.
//
// Precedence: project root > .opencode/ > global config for the same model.
// Within a single directory, an exact match beats a wildcard match for the
// same model — so a specific {model}.OCODE.md overrides a wildcard catch-all
// in the same dir.
func LoadModelContext(modelName string) string {
	if modelName == "" {
		return ""
	}
	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	if modelLower == "" {
		return ""
	}

	// stemMatches reports whether the given (lowercased) file stem matches the
	// (lowercased) active model. Returns ok=true with isWild=false for an
	// exact match, ok=true with isWild=true for a prefix-wildcard match, and
	// ok=false otherwise.
	stemMatches := func(stemLower string) (ok, isWild bool) {
		if stemLower == modelLower {
			return true, false
		}
		if strings.HasSuffix(stemLower, "*") {
			prefix := strings.TrimSuffix(stemLower, "*")
			// Require at least one literal char before the '*' so a bare
			// '*.OCODE.md' cannot accidentally match every model and shadow
			// all real files (project-root-wins would otherwise make it
			// silently override everything).
			if prefix != "" && strings.HasPrefix(modelLower, prefix) {
				return true, true
			}
		}
		return false, false
	}

	searchDirs := []string{".", filepath.Join(".", ".opencode")}
	if gd := globalOcodeDir(); gd != "" {
		searchDirs = append(searchDirs, gd)
	}

	// Per-directory: keep at most one path. If both an exact and a wildcard
	// match the same model in the same directory, the exact match wins.
	// Across directories, the first directory to claim a match (the
	// highest-priority one) wins — preserves the original first-match-wins
	// precedence rule.
	var matched []string
	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var exactPath, wildcardPath string
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
			ok, isWild := stemMatches(stemLower)
			if !ok {
				continue
			}
			fullPath := filepath.Join(dir, name)
			if isWild {
				if wildcardPath == "" {
					wildcardPath = fullPath
				}
			} else if exactPath == "" {
				exactPath = fullPath
			}
		}
		if exactPath != "" {
			matched = append(matched, exactPath)
			break // higher-priority dir already claimed; stop.
		}
		if wildcardPath != "" {
			matched = append(matched, wildcardPath)
			break
		}
	}

	if len(matched) == 0 {
		// No disk-based file found — fall back to the embedded model config
		// (e.g. deepseek-v4-flash.OCODE.md bundled via //go:embed in main.go).
		return loadBundledModelContext(modelName)
	}

	var context string
	for _, filePath := range matched {
		// Use readContextFile for git-aware HEAD-vs-working-tree logic,
		// consistent with how AGENTS.md, CLAUDE.md, and OCODE.md are loaded.
		if content, ok := readContextFile(filePath); ok {
			context += "\n--- " + filepath.Base(filePath) + " ---\n" + content + "\n"
		}
	}
	return context
}
