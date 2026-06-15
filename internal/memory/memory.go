package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/skill"
)

const (
	userMemoryFileName    = "user.md"
	globalMemoryFileName  = "global.md"
	projectMemoryFileName = "memory.md"
	memoryScopeHeader     = "--- ocode-mem ---"
	memoryIntro           = "Memory context is enabled. Use the layered memory sources below before answering."
	previewLineLimit      = 3
	previewRuneLimit      = 140
)

type Scope struct {
	Name    string
	Path    string
	Present bool
	Preview string
}

type Snapshot struct {
	User    Scope
	Project Scope
	Global  Scope
}

// Paths holds the resolved on-disk locations for the three persistent memory scopes.
type Paths struct {
	User    string
	Project string
	Global  string
}

func Status(workDir string) (Snapshot, error) {
	paths, err := ResolvePaths(workDir)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		User:    scopeStatus("User memory", paths.User),
		Project: scopeStatus("Project memory", paths.Project),
		Global:  scopeStatus("Global history", paths.Global),
	}, nil
}

func RenderStatus(s Snapshot) string {
	var b strings.Builder
	b.WriteString("Memory context injection\n")
	b.WriteString("Status: enabled\n\n")
	for _, scope := range []struct {
		title string
		s     Scope
	}{
		{title: "Project memory", s: s.Project},
		{title: "User memory", s: s.User},
		{title: "Global history", s: s.Global},
	} {
		b.WriteString(scope.title)
		b.WriteString("\n")
		b.WriteString("  Path: ")
		b.WriteString(scope.s.Path)
		b.WriteString("\n")
		preview := scope.s.Preview
		if preview == "" {
			if scope.s.Present {
				preview = "(empty)"
			} else {
				preview = "(not set)"
			}
		}
		b.WriteString("  Preview: ")
		b.WriteString(preview)
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func RenderPrompt(s Snapshot) string {
	var b strings.Builder
	b.WriteString("\n[ocode:memory]\n")
	b.WriteString("Layered memory context is enabled. Read the most specific scope first and promote only durable facts upward.\n")
	for _, scope := range []struct {
		title string
		s     Scope
	}{
		{title: "Project memory", s: s.Project},
		{title: "User memory", s: s.User},
		{title: "Global history", s: s.Global},
	} {
		b.WriteString("\n## ")
		b.WriteString(scope.title)
		b.WriteString("\n")
		b.WriteString("- path: ")
		b.WriteString(scope.s.Path)
		b.WriteString("\n")
		preview := scope.s.Preview
		if preview == "" {
			if scope.s.Present {
				preview = "(empty)"
			} else {
				preview = "(not set)"
			}
		}
		b.WriteString("- preview: ")
		b.WriteString(preview)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func PromptFragment(workDir string) string {
	paths, err := ResolvePaths(workDir)
	if err != nil {
		return ""
	}

	var b strings.Builder
	if snap, err := Status(workDir); err == nil {
		b.WriteString(RenderPrompt(snap))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(memoryScopeHeader)
	b.WriteString("\n")
	b.WriteString(memoryIntro)
	b.WriteString("\n")

	if skillText := loadMemorySkill(); skillText != "" {
		b.WriteString("\n--- ocode-mem/SKILL.md ---\n")
		b.WriteString(strings.TrimRight(skillText, "\n"))
		b.WriteString("\n")
	}

	for _, scope := range []struct {
		label string
		path  string
	}{
		{label: "Project memory", path: paths.Project},
		{label: "User memory", path: paths.User},
		{label: "Global history", path: paths.Global},
	} {
		body, err := os.ReadFile(scope.path)
		if err != nil || strings.TrimSpace(string(body)) == "" {
			continue
		}
		b.WriteString("\n--- ")
		b.WriteString(scope.label)
		b.WriteString(" (")
		b.WriteString(scope.path)
		b.WriteString(") ---\n")
		b.WriteString(strings.TrimRight(string(body), "\n"))
		b.WriteString("\n")
	}

	return b.String()
}

func ResolvePaths(workDir string) (Paths, error) {
	base, err := paths.GlobalDataDir()
	if err != nil {
		return Paths{}, err
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	slug := projectSlug(workDir)
	if slug == "" {
		return Paths{}, fmt.Errorf("resolve project slug")
	}
	return Paths{
		User:    filepath.Join(base, "memory", userMemoryFileName),
		Project: filepath.Join(base, "project", slug, projectMemoryFileName),
		Global:  filepath.Join(base, "memory", globalMemoryFileName),
	}, nil
}

func scopeStatus(name, path string) Scope {
	s := Scope{Name: name, Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s
		}
		s.Present = true
		s.Preview = fmt.Sprintf("(error reading: %v)", err)
		return s
	}
	s.Present = !info.IsDir()
	if !s.Present {
		return s
	}
	body, err := os.ReadFile(path)
	if err != nil {
		s.Preview = fmt.Sprintf("(error reading: %v)", err)
		return s
	}
	s.Preview = previewSnippet(string(body))
	return s
}

func previewSnippet(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	parts := make([]string, 0, previewLineLimit)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
		if len(parts) == previewLineLimit {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	snippet := strings.Join(parts, " ⏎ ")
	runes := []rune(snippet)
	if len(runes) > previewRuneLimit {
		snippet = string(runes[:previewRuneLimit-1]) + "…"
	}
	return snippet
}

func loadMemorySkill() string {
	if s, err := skill.LoadSkill("ocode-mem"); err == nil && s != nil {
		return s.Content
	}
	return ""
}

func projectSlug(workDir string) string {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	if workDir == "" {
		return ""
	}
	if gitRoot := gitToplevel(workDir); gitRoot != "" {
		workDir = gitRoot
	}
	workDir = filepath.Clean(workDir)
	if runtime.GOOS == "windows" {
		workDir = strings.ToLower(workDir)
	}
	hash := sha256.Sum256([]byte(workDir))
	return hex.EncodeToString(hash[:])[:12]
}

func gitToplevel(wd string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = wd
	if output, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(output))
	}
	return ""
}
