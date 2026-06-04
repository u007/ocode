package tui

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// filesContentSearchBatchMsg delivers a batch of incremental search results.
// The ch/cancel fields allow the handler to chain the next waitSearchEvent cmd.
type filesContentSearchBatchMsg struct {
	batch      []filesContentSearchResult
	totalSoFar int
	ch         chan filesContentSearchBatchMsg
	cancel     chan struct{}
}

// filesContentSearchDoneMsg signals that the search walk has finished.
type filesContentSearchDoneMsg struct {
	total  int
	err    error
	cancel chan struct{}
}

// startContentSearchCmd launches a background goroutine that walks the
// project tree and streams search results in batches. Returns a cmd that
// starts the chain of waitSearchEvent reads.
//
// Documented limitations:
//   - Only root .gitignore and .ignore files are consulted; nested ignore
//     files are not loaded.
//   - Result rows show plain line snippets; the matching substring is not
//     highlighted in the list view.
//
// When includeIgnored is false, hidden files/dirs, common ignore dirs, and
// paths matched by .gitignore / .ignore are skipped.
func startContentSearchCmd(workDir, query, exts string, includeIgnored bool) (tea.Cmd, chan struct{}) {
	if query == "" {
		return nil, nil
	}

	cancel := make(chan struct{})
	ch := make(chan filesContentSearchBatchMsg, 4)

	go func() {
		defer close(ch)

		// Build the regex from the query (case-insensitive).
		re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(query))
		if err != nil {
			re = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(query))
		}

		// Parse extension filter: "*.go,*.ts" → ["go", "ts"]
		extFilters := parseExtFilters(exts)

		// Load .gitignore / .ignore patterns when excluding ignored files.
		var ignoreMatcher gitignore.Matcher
		if !includeIgnored {
			var patterns []gitignore.Pattern
			if data, err := os.ReadFile(filepath.Join(workDir, ".gitignore")); err == nil {
				scanner := bufio.NewScanner(strings.NewReader(string(data)))
				for scanner.Scan() {
					line := scanner.Text()
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						patterns = append(patterns, gitignore.ParsePattern(line, nil))
					}
				}
			}
			if data, err := os.ReadFile(filepath.Join(workDir, ".ignore")); err == nil {
				scanner := bufio.NewScanner(strings.NewReader(string(data)))
				for scanner.Scan() {
					line := scanner.Text()
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						patterns = append(patterns, gitignore.ParsePattern(line, nil))
					}
				}
			}
			ignoreMatcher = gitignore.NewMatcher(patterns)
		}

		const (
			maxResults    = 500
			batchSize     = 10
			flushInterval = 100 * time.Millisecond
		)

		var (
			buf   []filesContentSearchResult
			total int
			timer *time.Timer
		)

		flush := func() {
			if len(buf) == 0 {
				return
			}
			total += len(buf)
			sent := make([]filesContentSearchResult, len(buf))
			copy(sent, buf)
			select {
			case ch <- filesContentSearchBatchMsg{batch: sent, totalSoFar: total, ch: ch, cancel: cancel}:
			case <-cancel:
			}
			buf = buf[:0]
			if timer != nil {
				timer.Stop()
				timer = nil
			}
		}

		_ = filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			// Check cancellation at each entry.
			select {
			case <-cancel:
				return filepath.SkipAll
			default:
			}

			name := d.Name()
			if d.IsDir() {
				if !includeIgnored {
					if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "target" || name == ".history" {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !includeIgnored {
				if strings.HasPrefix(name, ".") {
					return nil
				}
				rel, _ := filepath.Rel(workDir, path)
				if rel != "" && ignoreMatcher.Match(strings.Split(rel, string(filepath.Separator)), false) {
					return nil
				}
			}
			// Apply extension filter if specified.
			if len(extFilters) > 0 {
				if !matchesExtFilter(path, extFilters) {
					return nil
				}
			}
			// Read and search.
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			// Skip binary files (quick null-byte check).
			probe := data
			if len(probe) > 512 {
				probe = probe[:512]
			}
			for _, b := range probe {
				if b == 0 {
					return nil
				}
			}
			lines := strings.Split(string(data), "\n")
			rel, _ := filepath.Rel(workDir, path)
			for i, line := range lines {
				if re.MatchString(line) {
					buf = append(buf, filesContentSearchResult{
						path:    path,
						relPath: rel,
						line:    i + 1,
						text:    line,
					})
					if len(buf) >= batchSize {
						flush()
					}
					if total+len(buf) >= maxResults {
						flush()
						return filepath.SkipAll
					}
				}
			}
			// Start flush timer on first buffered result.
			if len(buf) > 0 && timer == nil {
				timer = time.NewTimer(flushInterval)
			}
			return nil
		})

		// Flush remaining buffered results.
		flush()
	}()

	return waitSearchEvent(ch, cancel), cancel
}

// waitSearchEvent reads the next batch from the search channel.
func waitSearchEvent(ch chan filesContentSearchBatchMsg, cancel chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-cancel:
			return filesContentSearchDoneMsg{err: nil, cancel: cancel}
		case batch, ok := <-ch:
			if !ok {
				return filesContentSearchDoneMsg{cancel: cancel}
			}
			return batch
		}
	}
}

// parseExtFilters parses a comma-separated list of extension patterns.
// Supports: "*.go", ".go", "go", "*.go,*.ts"
func parseExtFilters(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	var exts []string
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Remove leading * or .
		ext := strings.TrimLeft(part, "*.")
		if ext != "" {
			exts = append(exts, strings.ToLower(ext))
		}
	}
	return exts
}

// matchesExtFilter checks if a file path matches any of the extension filters.
func matchesExtFilter(path string, exts []string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		return false
	}
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// updateContentSearch handles key presses in content search mode.
func (m filesModel) updateContentSearch(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		// Cancel any running search.
		if m.contentSearchCancel != nil {
			close(m.contentSearchCancel)
			m.contentSearchCancel = nil
		}
		m.mode = filesModeNormal
		m.contentSearchLoading = false
		m.statusMsg = ""
		return m, nil
	case "tab":
		// Toggle between query and ext filter inputs.
		if m.contentSearchPanel == filesContentSearchQuery {
			m.contentSearchPanel = filesContentSearchExtFilter
		} else {
			m.contentSearchPanel = filesContentSearchQuery
		}
		return m, nil
	case "i", "I":
		// Toggle include-ignored and re-run search if there's a query.
		m.contentSearchIncludeIgnored = !m.contentSearchIncludeIgnored
		if m.contentSearchQuery != "" {
			// Cancel any existing search.
			if m.contentSearchCancel != nil {
				close(m.contentSearchCancel)
			}
			m.contentSearchLoading = true
			m.contentSearchDone = false
			m.contentSearchResults = nil
			m.contentSearchCursor = 0
			m.statusMsg = "searching..."
			cmd, cancel := startContentSearchCmd(m.workDir, m.contentSearchQuery, m.contentSearchExts, m.contentSearchIncludeIgnored)
			m.contentSearchCancel = cancel
			return m, cmd
		}
		return m, nil
	case "enter", "ctrl+j", "ctrl+m":
		if m.contentSearchLoading {
			return m, nil
		}
		if m.contentSearchDone && len(m.contentSearchResults) > 0 {
			// Navigate to the selected result.
			m.navigateToSearchResult(m.contentSearchResults[m.contentSearchCursor])
			return m, nil
		}
		if strings.TrimSpace(m.contentSearchQuery) == "" {
			m.statusMsg = "type a query first"
			return m, nil
		}
		// Start a new search.
		// Cancel any existing search.
		if m.contentSearchCancel != nil {
			close(m.contentSearchCancel)
		}
		m.contentSearchLoading = true
		m.contentSearchDone = false
		m.contentSearchResults = nil
		m.contentSearchCursor = 0
		m.statusMsg = "searching..."
		cmd, cancel := startContentSearchCmd(m.workDir, m.contentSearchQuery, m.contentSearchExts, m.contentSearchIncludeIgnored)
		m.contentSearchCancel = cancel
		return m, cmd
	case "j", "down":
		if len(m.contentSearchResults) > 0 && m.contentSearchCursor < len(m.contentSearchResults)-1 {
			m.contentSearchCursor++
		}
	case "k", "up":
		if m.contentSearchCursor > 0 {
			m.contentSearchCursor--
		}
	case "backspace":
		if m.contentSearchPanel == filesContentSearchQuery {
			if len(m.contentSearchQuery) > 0 {
				m.contentSearchQuery = m.contentSearchQuery[:len(m.contentSearchQuery)-1]
			}
		} else {
			if len(m.contentSearchExts) > 0 {
				m.contentSearchExts = m.contentSearchExts[:len(m.contentSearchExts)-1]
			}
		}
	default:
		if len(msg.Text) > 0 {
			if m.contentSearchPanel == filesContentSearchQuery {
				m.contentSearchQuery += msg.Text
			} else {
				m.contentSearchExts += msg.Text
			}
		}
	}
	return m, nil
}

// navigateToSearchResult jumps to the file and line of a search result.
func (m *filesModel) navigateToSearchResult(result filesContentSearchResult) {
	m.mode = filesModeNormal
	m.statusMsg = ""
	// Navigate the tree to the file.
	relPath := result.relPath
	m.navigateTo(relPath)
	// Load preview and scroll to the matching line.
	if m.cursor >= 0 && m.cursor < len(m.nodes) {
		n := m.nodes[m.cursor]
		if !n.isDir {
			if msg, ok := loadPreviewCmd(n)().(filesPreviewMsg); ok {
				m.applyPreview(msg)
				// Scroll to the matching line (0-indexed).
				targetLine := result.line - 1
				totalLines := m.preview.TotalLineCount()
				visibleLines := m.preview.Height()
				if totalLines > visibleLines {
					offset := targetLine - visibleLines/2
					if offset < 0 {
						offset = 0
					}
					if offset > totalLines-visibleLines {
						offset = totalLines - visibleLines
					}
					m.preview.GotoTop()
					m.preview.ScrollDown(offset)
				}
			}
		}
	}
}

// contentView renders the content search UI in the preview panel.
func (m filesModel) contentView(width, height int, styles Styles) string {
	var lines []string

	// Search inputs
	queryLabel := "Search: "
	extLabel := "Exts: "

	queryVal := m.contentSearchQuery
	if m.contentSearchPanel == filesContentSearchQuery {
		queryVal += "█"
	}
	queryLine := styles.Hint.Render(queryLabel) + styles.Selected.Width(width-len(queryLabel)).Render(queryVal)

	extVal := m.contentSearchExts
	if extVal == "" {
		extVal = "(all files)"
	}
	if m.contentSearchPanel == filesContentSearchExtFilter {
		extVal += "█"
	}
	extLine := styles.Hint.Render(extLabel) + styles.Selected.Width(width-len(extLabel)).Render(extVal)

	// Ignore toggle
	ignoreIcon := "●"
	ignoreLabel := "Skip .gitignore+hidden"
	if m.contentSearchIncludeIgnored {
		ignoreIcon = "○"
		ignoreLabel = "Skip .gitignore+hidden"
	}
	ignoreLine := styles.Hint.Render("  " + ignoreIcon + " " + ignoreLabel + "  (i toggle)")

	lines = append(lines, queryLine, extLine, ignoreLine, "")

	// Hints
	if m.contentSearchLoading {
		lines = append(lines, styles.Hint.Render(fmt.Sprintf("Searching... %d results so far", len(m.contentSearchResults))))
	} else if m.contentSearchDone {
		if len(m.contentSearchResults) == 0 {
			lines = append(lines, styles.Hint.Render("No results found"))
		} else {
			lines = append(lines, styles.Hint.Render(fmt.Sprintf("%d results — j/k navigate  enter open  esc back", len(m.contentSearchResults))))
		}
	} else {
		lines = append(lines, styles.Hint.Render("Tab switch query/ext  Enter run  esc cancel"))
	}

	lines = append(lines, "")

	// Results
	visibleResults := height - len(lines) - 2
	if visibleResults < 1 {
		visibleResults = 1
	}
	if visibleResults > len(m.contentSearchResults) {
		visibleResults = len(m.contentSearchResults)
	}

	// Show a window of results around the cursor
	start := 0
	if len(m.contentSearchResults) > visibleResults {
		start = m.contentSearchCursor - visibleResults/2
		if start < 0 {
			start = 0
		}
		if start > len(m.contentSearchResults)-visibleResults {
			start = len(m.contentSearchResults) - visibleResults
		}
	}
	end := start + visibleResults
	if end > len(m.contentSearchResults) {
		end = len(m.contentSearchResults)
	}

	for i := start; i < end; i++ {
		r := m.contentSearchResults[i]
		// Format: relPath:lineNum  text
		lineNum := fmt.Sprintf("%d", r.line)
		fileLabel := fmt.Sprintf("%s:%s", r.relPath, lineNum)

		// Truncate text to fit width
		maxTextWidth := width - len(fileLabel) - 4
		if maxTextWidth < 10 {
			maxTextWidth = 10
		}
		text := r.text
		if len(text) > maxTextWidth {
			text = text[:maxTextWidth] + "…"
		}

		line := "  " + styles.Hint.Render(fileLabel) + "  " + text
		if i == m.contentSearchCursor {
			line = styles.Selected.Width(width).Render("> " + fileLabel + "  " + text)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}
