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

func NewIgnoreMatcher() *IgnoreMatcher {
	var patterns []gitignore.Pattern

	// Load .gitignore
	if data, err := os.ReadFile(".gitignore"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, gitignore.ParsePattern(line, nil))
			}
		}
	}

	// Load .ignore (standard for opencode/ripgrep)
	if data, err := os.ReadFile(".ignore"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, gitignore.ParsePattern(line, nil))
			}
		}
	}

	return &IgnoreMatcher{
		matcher: gitignore.NewMatcher(patterns),
	}
}

func (m *IgnoreMatcher) IsIgnored(path string, isDir bool) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	return m.matcher.Match(parts, isDir)
}
