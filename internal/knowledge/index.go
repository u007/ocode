package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// GenerateIndex regenerates docs/index.md from all conforming docs' frontmatter.
//
// The root index.md frontmatter (containing only okf_version) is preserved.
// Conforming docs are grouped by top-level directory — root-level (no directory)
// docs go under "# Concepts", and each subdirectory gets its own section.
// Within each section, entries are sorted by title, formatted as:
//
//   - [title](path) - description
//
// Non-conforming files (no parseable frontmatter) are listed under
// "# Unclassified" as filename links only (no description).
// Deprecated docs are annotated with "(deprecated)".
//
// On failure, the previous index.md is left intact (write to temp file + rename).
func GenerateIndex(b *Bundle) error {
	docs, err := b.Docs()
	if err != nil {
		return fmt.Errorf("knowledge: generate index: %w", err)
	}

	// Classify docs into conforming and non-conforming.
	type sectionEntry struct {
		path         string
		title        string
		description  string
		isDeprecated bool
	}

	// Map: top-level directory name -> entries
	sections := make(map[string][]sectionEntry)
	var nonConforming []string

	for _, doc := range docs {
		if !doc.Conforming {
			nonConforming = append(nonConforming, doc.Path)
			continue
		}

		dir := topLevelDir(doc.Path)
		entry := sectionEntry{
			path:         doc.Path,
			title:        doc.Title,
			description:  doc.Description,
			isDeprecated: doc.Status == "deprecated",
		}
		sections[dir] = append(sections[dir], entry)
	}

	// Build the index content.
	var buf strings.Builder

	// Preserve okf_version frontmatter from the existing index.md.
	buf.WriteString("---\n")
	buf.WriteString("okf_version: " + b.OKFVersion + "\n")
	buf.WriteString("---\n\n")

	// Sort section names for stable output.
	sectionNames := make([]string, 0, len(sections))
	for name := range sections {
		sectionNames = append(sectionNames, name)
	}
	sort.Strings(sectionNames)

	// Render each section.
	for _, sectionName := range sectionNames {
		entries := sections[sectionName]
		sort.Slice(entries, func(i, j int) bool {
			return strings.ToLower(entries[i].title) < strings.ToLower(entries[j].title)
		})

		heading := sectionName
		if heading == "" {
			heading = "Concepts"
		}
		buf.WriteString("# " + heading + "\n\n")

		for _, entry := range entries {
			path := entry.path
			title := entry.title
			if title == "" {
				title = path
			}
			buf.WriteString("- [" + title + "](" + path + ")")
			if entry.description != "" {
				buf.WriteString(" - " + entry.description)
			}
			if entry.isDeprecated {
				buf.WriteString(" (deprecated)")
			}
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}

	// Render non-conforming files under "# Unclassified".
	if len(nonConforming) > 0 {
		sort.Strings(nonConforming)
		buf.WriteString("# Unclassified\n\n")
		for _, path := range nonConforming {
			buf.WriteString("- [" + filepath.Base(path) + "](" + path + ")\n")
		}
		buf.WriteString("\n")
	}

	content := []byte(buf.String())

	// Write to temp file first, then rename atomically.
	indexPath := filepath.Join(b.Root, "index.md")
	tmpFile, err := os.CreateTemp(b.Root, "index.md.tmp.*")
	if err != nil {
		return fmt.Errorf("knowledge: create temp file for index: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on failure.
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("knowledge: write temp index: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("knowledge: close temp index: %w", err)
	}

	// Rename atomically (replaces existing index.md if any).
	if err := os.Rename(tmpPath, indexPath); err != nil {
		return fmt.Errorf("knowledge: rename temp index to index.md: %w", err)
	}
	cleanup = false // Success, no cleanup needed.

	return nil
}

// AppendLog appends an entry to docs/log.md.
//
// Action must be one of "Creation", "Update", "Deprecation", or "Deletion".
// Creates docs/log.md with "# Directory Update Log" heading if absent.
// Inserts under today's "## YYYY-MM-DD" heading, creating it at the TOP of
// existing entries (newest first) if absent.
func AppendLog(b *Bundle, action, docPath, summary string) error {
	logPath := filepath.Join(b.Root, "log.md")
	today := time.Now().Format("2006-01-02")

	// Read existing log content.
	existing, err := os.ReadFile(logPath)
	var lines []string
	var fileExists bool
	if err == nil {
		fileExists = true
		lines = strings.Split(string(existing), "\n")
	} else if os.IsNotExist(err) {
		// File doesn't exist yet, start fresh.
		fileExists = false
	} else {
		return fmt.Errorf("knowledge: read log.md: %w", err)
	}

	// The entry line to insert.
	entry := fmt.Sprintf("* **%s**: %s ([%s](/%s))", action, summary, docPath, docPath)

	// If file doesn't exist, create it with heading, date, and entry.
	if !fileExists {
		content := fmt.Sprintf("# Directory Update Log\n\n## %s\n\n%s\n", today, entry)
		return os.WriteFile(logPath, []byte(content), 0644)
	}

	// File exists. Parse its structure.
	// We need to find where the "## YYYY-MM-DD" heading for today should go.
	// Newest-first ordering: new dates at the top of the date section list.

	// Find the first date heading in the file.
	type dateSection struct {
		lineIndex int // index in lines of the "## YYYY-MM-DD" heading
		date      string
	}
	var dateSections []dateSection

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			dateStr := strings.TrimSpace(line[3:])
			// Validate it looks like a date.
			if _, err := time.Parse("2006-01-02", dateStr); err == nil {
				dateSections = append(dateSections, dateSection{lineIndex: i, date: dateStr})
			}
		}
	}

	// Find today's section.
	todaySectionIdx := -1
	for i, ds := range dateSections {
		if ds.date == today {
			todaySectionIdx = i
			break
		}
	}

	if todaySectionIdx >= 0 {
		// Today's section exists. Insert the entry after the heading.
		// The heading is at lines[todayLineIdx].
		// We need to find the first blank line or next section heading after the heading.
		sectionStart := dateSections[todaySectionIdx].lineIndex
		insertAt := sectionStart + 1

		// Find where the section content ends (next heading or end of file).
		var sectionEnd int
		if todaySectionIdx+1 < len(dateSections) {
			sectionEnd = dateSections[todaySectionIdx+1].lineIndex
		} else {
			sectionEnd = len(lines)
		}

		// Check if there's a blank line after the heading.
		if insertAt < sectionEnd && strings.TrimSpace(lines[insertAt]) == "" {
			insertAt++
		}

		// Insert the entry at the right position (after the heading, before existing entries).
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:insertAt]...)
		newLines = append(newLines, entry)
		newLines = append(newLines, lines[insertAt:]...)

		return os.WriteFile(logPath, []byte(strings.Join(newLines, "\n")), 0644)
	}

	// Today's section doesn't exist. Insert it at the top (after the main heading).
	// Find where to insert: after "# Directory Update Log" heading and its trailing blank line.
	insertAt := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# Directory Update Log") {
			insertAt = i + 1
			// Skip blank lines after the heading.
			for insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) == "" {
				insertAt++
			}
			break
		}
	}

	// Create the new section with the entry.
	sectionBlock := fmt.Sprintf("\n## %s\n\n%s", today, entry)

	newLines := make([]string, 0, len(lines)+4)
	newLines = append(newLines, lines[:insertAt]...)
	newLines = append(newLines, sectionBlock)
	if insertAt < len(lines) {
		newLines = append(newLines, lines[insertAt:]...)
	}

	return os.WriteFile(logPath, []byte(strings.Join(newLines, "\n")), 0644)
}

// topLevelDir returns the top-level directory name for a bundle-relative path.
// For root-level docs (no directory), returns "".
func topLevelDir(relPath string) string {
	idx := strings.Index(relPath, string(filepath.Separator))
	if idx == -1 {
		// Also check forward slash (bundle-relative paths use /).
		idx = strings.Index(relPath, "/")
	}
	if idx == -1 {
		return ""
	}
	return relPath[:idx]
}
