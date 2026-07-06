package knowledge

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultExcludedPatterns lists file patterns that are always excluded from
// the knowledge bundle. These are matched against the filename (basename only)
// during Bundle.Docs scanning. Users can add additional patterns via .okfignore.
var DefaultExcludedPatterns = []string{
	"PLAN-*.md",
	"*.OCODE.md",
}

// Bundle represents an OKF knowledge bundle rooted at a docs/ directory.
type Bundle struct {
	// Root is the absolute path of the docs/ directory.
	Root string

	// OKFVersion is the OKF version declared in docs/index.md frontmatter.
	OKFVersion string
}

// DetectBundle checks whether workDir contains an OKF knowledge bundle. It
// returns (bundle, true) when <workDir>/docs/index.md exists and its
// frontmatter has a non-empty "okf_version" key.
//
// A mkdocs-style index.md without the okf_version marker returns (nil, false).
func DetectBundle(workDir string) (*Bundle, bool) {
	docsDir := filepath.Join(workDir, "docs")

	// Check if docs/ exists and is a directory.
	info, err := os.Stat(docsDir)
	if err != nil || !info.IsDir() {
		return nil, false
	}

	indexPath := filepath.Join(docsDir, "index.md")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, false
	}

	doc, err := ParseDoc("index.md", raw)
	if err != nil || !doc.Conforming {
		return nil, false
	}

	// For DetectBundle, we need to find okf_version directly from the
	// frontmatter, not from the parsed Doc fields (since okf_version is an
	// unknown key to our Doc type).
	version := extractOKFVersion(raw)
	if version == "" {
		return nil, false
	}

	return &Bundle{
		Root:       docsDir,
		OKFVersion: version,
	}, true
}

// extractOKFVersion reads the okf_version value from raw markdown frontmatter
// bytes. This is a lightweight scan that avoids full YAML parsing for the
// common fast-path case (no bundle detected).
func extractOKFVersion(raw []byte) string {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return ""
	}
	// Skip past the opening --- delimiter, handling both \n and \r\n.
	rest := s[strings.Index(s, "\n")+1:]
	if rest == "" {
		return ""
	}

	// Scan line by line for okf_version.
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || trimmed == "..." {
			break
		}
		if strings.HasPrefix(trimmed, "okf_version:") {
			val := strings.TrimSpace(trimmed[len("okf_version:"):])
			val = strings.Trim(val, `"' `)
			return val
		}
	}
	return ""
}

// hasOKFVersion returns true when raw markdown content has okf_version in its
// frontmatter. Used by InitBundle to guard against overwriting pre-existing
// non-OKF files (C3).
func hasOKFVersion(raw []byte) bool {
	return extractOKFVersion(raw) != ""
}

// loadOKFIgnore reads .okfignore from the bundle root and returns the list
// of patterns defined in it. Returns nil if the file doesn't exist or can't
// be read. Each line is trimmed; blank lines and lines starting with # are
// skipped.
func loadOKFIgnore(docsRoot string) []string {
	raw, err := os.ReadFile(filepath.Join(docsRoot, ".okfignore"))
	if err != nil {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// excludedFile returns true when a file should be excluded from the bundle.
// Default patterns (DefaultExcludedPatterns) are matched against the basename;
// .okfignore patterns are matched against both the basename and the bundle-
// relative path.
func excludedFile(relPath, name string, ignorePatterns []string) bool {
	for _, p := range DefaultExcludedPatterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	for _, p := range ignorePatterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
		if ok, _ := filepath.Match(p, relPath); ok {
			return true
		}
	}
	return false
}

// excludedDir returns true when a directory matches an exclusion pattern,
// in which case the walker should skip the entire directory. Only .okfignore
// patterns are checked (default patterns are file-specific globs that don't
// naturally match directory names).
func excludedDir(name string, ignorePatterns []string) bool {
	for _, p := range ignorePatterns {
		// Allow patterns ending in / to match directory names.
		p = strings.TrimSuffix(p, "/")
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// Docs walks the bundle directory and returns all non-reserved, non-hidden,
// non-excluded markdown documents, parsed and sorted by path.
//
// Reserved files (index.md, log.md) and dot-files (hidden files starting with
// ".") are skipped. Only files with the .md extension are considered.
// Default exclusion patterns (DefaultExcludedPatterns) and optional .okfignore
// patterns are applied. Docs are returned sorted alphabetically by their
// bundle-relative path.
func (b *Bundle) Docs() ([]*Doc, error) {
	if b.Root == "" {
		return nil, fmt.Errorf("knowledge: bundle root is empty")
	}

	var docs []*Doc

	// Load .okfignore exclusion patterns (if any).
	ignorePatterns := loadOKFIgnore(b.Root)

	err := filepath.WalkDir(b.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden files and directories (starting with ".").
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// For directories, check if they match .okfignore patterns
		// before descending into them.
		if d.IsDir() {
			if excludedDir(d.Name(), ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process .md files.
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		// Skip reserved files.
		baseName := d.Name()
		if baseName == "index.md" || baseName == "log.md" {
			return nil
		}

		// Compute bundle-relative path (needed for exclusion check).
		relPath, err := filepath.Rel(b.Root, path)
		if err != nil {
			slog.Debug("knowledge: failed to compute relative path", "path", path, "err", err)
			relPath = baseName
		}

		// Check exclusion patterns (default + .okfignore).
		if excludedFile(relPath, baseName, ignorePatterns) {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			slog.Debug("knowledge: failed to read doc file", "path", path, "err", err)
			return nil // Skip unreadable files.
		}

		doc, err := ParseDoc(relPath, raw)
		if err != nil {
			// ParseDoc shouldn't error in practice, but handle gracefully.
			slog.Debug("knowledge: ParseDoc error", "path", path, "err", err)
			return nil
		}

		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge: walk bundle %s: %w", b.Root, err)
	}

	// Sort by bundle-relative path.
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Path < docs[j].Path
	})

	return docs, nil
}

// InitBundle creates a new OKF knowledge bundle at docs/ in the given
// workDir. It creates docs/ if it does not exist, writes index.md with
// okf_version: "0.1" frontmatter, and creates an empty log.md.
// It errors if the bundle marker (okf_version in index.md) already exists.
//
// The operation acquires the bundle lock to prevent concurrent init races (M1).
func InitBundle(workDir string) error {
	docsDir := filepath.Join(workDir, "docs")

	// Check if docs/ exists and if index.md already has the marker.
	if bundle, ok := DetectBundle(workDir); ok {
		return fmt.Errorf("knowledge: bundle already initialized at %s (okf_version=%s)", bundle.Root, bundle.OKFVersion)
	}

	// Create docs/ directory if needed.
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return fmt.Errorf("knowledge: create docs dir: %w", err)
	}

	// Non-destructive check: refuse to overwrite pre-existing files that do
	// not already carry the OKF marker. A repo might have a mkdocs/docusaurus
	// index.md that we must not destroy (C3 / spec H3).
	indexPath := filepath.Join(docsDir, "index.md")
	if existing, err := os.ReadFile(indexPath); err == nil {
		if !hasOKFVersion(existing) {
			return fmt.Errorf("knowledge: %s already exists as a non-OKF file (mkdocs/docusaurus-style). Move or rename it first, or add 'okf_version' frontmatter to adopt it as the bundle index.", indexPath)
		}
		// If it already has okf_version, DetectBundle above would have caught
		// it. This is a belt-and-suspenders check.
		return fmt.Errorf("knowledge: bundle already initialized at %s", docsDir)
	}

	logPath := filepath.Join(docsDir, "log.md")
	if existing, err := os.ReadFile(logPath); err == nil {
		if !hasOKFVersion(existing) {
			return fmt.Errorf("knowledge: %s already exists as a non-OKF file. Move or rename it first.", logPath)
		}
	}

	// Write the bundle files under the lock to prevent concurrent init races (M1).
	indexContent := `---
okf_version: "0.1"
---

# Concepts

_No documents yet. Use /docs init to annotate existing files or add docs via the context sub-agent._
`
	logContent := `# Directory Update Log

`
	return WithBundleLock(docsDir, func() error {
		if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
			return fmt.Errorf("knowledge: write index.md: %w", err)
		}
		if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
			return fmt.Errorf("knowledge: write log.md: %w", err)
		}
		slog.Debug("knowledge: initialized bundle", "docsDir", docsDir)
		return nil
	})
}
