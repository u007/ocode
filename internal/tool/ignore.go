package tool

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

type IgnoreMatcher struct {
	matcher gitignore.Matcher
}

// NewIgnoreMatcher loads .gitignore/.ignore from root — the resolved search
// root, NOT the process cwd. The server hosts sessions for many projects in
// one process, and the desktop shell launched from Finder has cwd "/", so a
// cwd-relative read silently finds no .gitignore there and every search
// walks node_modules/dist/build output unfiltered.
func NewIgnoreMatcher(root string, extraPatterns []string) *IgnoreMatcher {
	var patterns []gitignore.Pattern

	// Load .gitignore
	if data, err := os.ReadFile(filepath.Join(root, ".gitignore")); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, gitignore.ParsePattern(line, nil))
			}
		}
	}

	// Load .ignore (standard for opencode/ripgrep)
	if data, err := os.ReadFile(filepath.Join(root, ".ignore")); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, gitignore.ParsePattern(line, nil))
			}
		}
	}

	for _, p := range extraPatterns {
		patterns = append(patterns, gitignore.ParsePattern(p, nil))
	}

	return &IgnoreMatcher{
		matcher: gitignore.NewMatcher(patterns),
	}
}

func (m *IgnoreMatcher) IsIgnored(path string, isDir bool) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	return m.matcher.Match(parts, isDir)
}
