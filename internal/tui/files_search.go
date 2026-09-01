package tui

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// filesContentSearchBatchMsg delivers a batch of incremental search results.
// The ch/cancel fields allow the handler to chain the next waitSearchEvent cmd.
type filesContentSearchBatchMsg struct {
	batch      []filesContentSearchResult
	totalSoFar int
	ch         chan filesContentSearchBatchMsg
	cancel     chan struct{}
	generation uint64
	err        error
}

// filesContentSearchDoneMsg signals that the search walk has finished.
type filesContentSearchDoneMsg struct {
	total      int
	err        error
	cancel     chan struct{}
	generation uint64
}

type filesContentSearchOptions struct {
	matchMode     filesContentSearchMode
	caseSensitive bool
	wholeWord     bool
	generation    uint64
}

// contentSearchResultsStartLine is the first line occupied by a result in
// contentView. Keep this shared with the mouse hit-testing in model.go.
const contentSearchResultsStartLine = 9

// compileContentSearchPattern preserves the historical default (a
// case-insensitive literal substring search) while allowing the search UI to
// opt into regular expressions, case sensitivity, and whole-word matching.
func compileContentSearchPattern(query string, options filesContentSearchOptions) (*regexp.Regexp, error) {
	pattern := query
	if options.matchMode != filesContentSearchRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if options.wholeWord {
		// Treat Unicode letters/numbers and underscore as word characters. The
		// standard regexp \b is ASCII-oriented and makes non-ASCII identifiers
		// behave unexpectedly.
		pattern = `(?:^|[^\pL\pN_])(?:` + pattern + `)(?:$|[^\pL\pN_])`
	}
	if !options.caseSensitive {
		pattern = `(?i:` + pattern + `)`
	}
	return regexp.Compile(pattern)
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
	return startContentSearchCmdWithOptions(workDir, query, exts, includeIgnored, filesContentSearchOptions{})
}

func startContentSearchCmdWithOptions(workDir, query, exts string, includeIgnored bool, options filesContentSearchOptions) (tea.Cmd, chan struct{}) {
	if query == "" {
		return nil, nil
	}

	cancel := make(chan struct{})
	ch := make(chan filesContentSearchBatchMsg, 4)

	go func() {
		defer close(ch)

		re, err := compileContentSearchPattern(query, options)
		if err != nil {
			select {
			case ch <- filesContentSearchBatchMsg{ch: ch, cancel: cancel, generation: options.generation, err: err}:
			case <-cancel:
			}
			return
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
			maxResults = 500
			batchSize  = 10
		)

		var (
			buf   []filesContentSearchResult
			total int
		)

		flush := func() {
			if len(buf) == 0 {
				return
			}
			total += len(buf)
			sent := make([]filesContentSearchResult, len(buf))
			copy(sent, buf)
			select {
			case ch <- filesContentSearchBatchMsg{batch: sent, totalSoFar: total, ch: ch, cancel: cancel, generation: options.generation}:
			case <-cancel:
			}
			buf = buf[:0]
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
				select {
				case <-cancel:
					return filepath.SkipAll
				default:
				}
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
			return nil
		})

		// Flush remaining buffered results.
		flush()
	}()

	return waitSearchEventWithGeneration(ch, cancel, options.generation), cancel
}

// waitSearchEvent reads the next batch from the search channel.
func waitSearchEvent(ch chan filesContentSearchBatchMsg, cancel chan struct{}) tea.Cmd {
	return waitSearchEventWithGeneration(ch, cancel, 0)
}

func waitSearchEventWithGeneration(ch chan filesContentSearchBatchMsg, cancel chan struct{}, generation uint64) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-cancel:
			return filesContentSearchDoneMsg{cancel: cancel, generation: generation}
		case batch, ok := <-ch:
			if !ok {
				return filesContentSearchDoneMsg{cancel: cancel, generation: generation}
			}
			if batch.err != nil {
				return filesContentSearchDoneMsg{err: batch.err, cancel: cancel, generation: batch.generation}
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

func (m *filesModel) cancelContentSearch() {
	if m.contentSearchCancel == nil {
		return
	}
	select {
	case <-m.contentSearchCancel:
		// Already closed.
	default:
		close(m.contentSearchCancel)
	}
	m.contentSearchCancel = nil
}

func (m *filesModel) markContentSearchDirty() {
	m.cancelContentSearch()
	m.contentSearchGeneration++
	m.contentSearchLoading = false
	m.contentSearchDone = false
	m.contentSearchError = ""
	m.contentSearchResults = nil
	m.contentSearchCursor = 0
	m.contentSearchDirty = true
}

func (m filesModel) startContentSearch() (filesModel, tea.Cmd) {
	m.cancelContentSearch()
	m.contentSearchGeneration++
	m.contentSearchLoading = true
	m.contentSearchDone = false
	m.contentSearchError = ""
	m.contentSearchDirty = false
	m.contentSearchResults = nil
	m.contentSearchCursor = 0
	m.statusMsg = "searching..."
	cmd, cancel := startContentSearchCmdWithOptions(
		m.workDir,
		m.contentSearchQuery,
		m.contentSearchExts,
		m.contentSearchIncludeIgnored,
		filesContentSearchOptions{
			matchMode:     m.contentSearchMatch,
			caseSensitive: m.contentSearchCaseSensitive,
			wholeWord:     m.contentSearchWholeWord,
			generation:    m.contentSearchGeneration,
		},
	)
	m.contentSearchCancel = cancel
	return m, cmd
}

func (m *filesModel) cycleContentSearchPanel(backward bool) {
	const panelCount = int(filesContentSearchIgnore) + 1
	panel := int(m.contentSearchPanel)
	if backward {
		panel = (panel - 1 + panelCount) % panelCount
	} else {
		panel = (panel + 1) % panelCount
	}
	m.contentSearchPanel = filesContentSearchPanel(panel)
}

// updateContentSearch handles key presses in content search mode.
func (m filesModel) updateContentSearch(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.cancelContentSearch()
		m.mode = filesModeNormal
		m.contentSearchLoading = false
		m.statusMsg = ""
		return m, nil
	case "tab", "shift+tab":
		m.cycleContentSearchPanel(key == "shift+tab")
		return m, nil
	case "ctrl+l":
		// Toggle include-ignored and re-run search if there's a query.
		m.contentSearchIncludeIgnored = !m.contentSearchIncludeIgnored
		if m.contentSearchQuery != "" {
			return m.startContentSearch()
		}
		m.markContentSearchDirty()
		return m, nil
	case "enter", "ctrl+j", "ctrl+m":
		if m.contentSearchLoading {
			return m, nil
		}
		if m.contentSearchDone && !m.contentSearchDirty && len(m.contentSearchResults) > 0 {
			// Navigate to the selected result.
			m.navigateToSearchResult(m.contentSearchResults[m.contentSearchCursor])
			return m, nil
		}
		if strings.TrimSpace(m.contentSearchQuery) == "" {
			m.statusMsg = "type a query first"
			return m, nil
		}
		return m.startContentSearch()
	case "ctrl+n", "down":
		if len(m.contentSearchResults) > 0 && m.contentSearchCursor < len(m.contentSearchResults)-1 {
			m.contentSearchCursor++
		}
	case "ctrl+p", "up":
		if m.contentSearchCursor > 0 {
			m.contentSearchCursor--
		}
	case "backspace":
		if m.contentSearchPanel == filesContentSearchQuery {
			if len(m.contentSearchQuery) > 0 {
				m.markContentSearchDirty()
				m.contentSearchQuery = m.contentSearchQuery[:len(m.contentSearchQuery)-1]
			}
		} else if m.contentSearchPanel == filesContentSearchExtFilter {
			if len(m.contentSearchExts) > 0 {
				m.markContentSearchDirty()
				m.contentSearchExts = m.contentSearchExts[:len(m.contentSearchExts)-1]
			}
		}
	case "space", "left", "right":
		switch m.contentSearchPanel {
		case filesContentSearchMatchField:
			m.markContentSearchDirty()
			if m.contentSearchMatch == filesContentSearchLiteral {
				m.contentSearchMatch = filesContentSearchRegex
			} else {
				m.contentSearchMatch = filesContentSearchLiteral
			}
		case filesContentSearchCase:
			m.markContentSearchDirty()
			m.contentSearchCaseSensitive = !m.contentSearchCaseSensitive
		case filesContentSearchWholeWord:
			m.markContentSearchDirty()
			m.contentSearchWholeWord = !m.contentSearchWholeWord
		case filesContentSearchIgnore:
			m.markContentSearchDirty()
			m.contentSearchIncludeIgnored = !m.contentSearchIncludeIgnored
		}
	default:
		if len(msg.Text) > 0 {
			if m.contentSearchPanel == filesContentSearchQuery {
				m.markContentSearchDirty()
				m.contentSearchQuery += msg.Text
			} else if m.contentSearchPanel == filesContentSearchExtFilter {
				m.markContentSearchDirty()
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
			if msg, ok := m.loadPreviewCmd(n)().(filesPreviewMsg); ok {
				m.applyPreview(msg)
				targetLine := result.line - 1
				for targetLine >= len(m.previewRawLines) && m.previewHasMore {
					if nextMsg, ok := m.loadMorePreviewCmd()().(filesPreviewMsg); ok {
						m.applyPreview(nextMsg)
					} else {
						break
					}
				}
				// For markdown, previewRawLines holds rendered lines whose count
				// differs from source lines. Estimate the rendered line position
				// proportionally so content-search jumps land in the right area.
				if m.previewLang == "markdown" && len(m.previewRawLines) > 0 {
					sourceLineCount := len(strings.Split(m.previewRaw, "\n"))
					if sourceLineCount > 0 {
						targetLine = targetLine * len(m.previewRawLines) / sourceLineCount
					}
				}
				// Scroll to the matching line (0-indexed).
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

// handlePasteMsg handles paste events in text-input modes on the files tab.
// It routes pasted content to the active input field based on the current mode.
func (m filesModel) handlePasteMsg(msg tea.PasteMsg) (filesModel, tea.Cmd) {
	switch m.mode {
	case filesModeContentSearch:
		m.markContentSearchDirty()
		if m.contentSearchPanel == filesContentSearchQuery {
			m.contentSearchQuery += msg.Content
		} else {
			m.contentSearchExts += msg.Content
		}
		return m, nil
	case filesModeFuzzy:
		m.fuzzyQuery += msg.Content
		m.fuzzyResults = fuzzyFilter(m.allPaths, m.fuzzyQuery)
		m.fuzzyCursor = 0
		return m, nil
	case filesModeInFileSearch:
		m.inFileSearchQuery += msg.Content
		m.inFileSearchMatches = m.performInFileSearch(m.inFileSearchQuery)
		m.inFileSearchCursor = 0
		m.applyInFileSearchHighlights()
		if len(m.inFileSearchMatches) > 0 {
			match := m.inFileSearchMatches[0]
			m.preview.EnsureVisible(match[0], 0, 0)
		}
		return m, nil
	case filesModePrompt:
		// textarea.Model handles PasteMsg natively
		var cmd tea.Cmd
		m.promptInput, cmd = m.promptInput.Update(msg)
		return m, cmd
	case filesModeEdit:
		// paste in the inline editor is not supported — it uses
		// per-character KeyPressMsg handling rather than a textarea model
		return m, nil
	}
	return m, nil
}

// contentView renders the content search UI in the preview panel.
func (m filesModel) contentView(width, height int, styles Styles) string {
	var lines []string

	// Search inputs
	queryLabel := "Search: "
	extLabel := "Exts: "
	matchLabel := "Match: "
	caseLabel := "Case: "
	wholeLabel := "Word: "

	queryVal := m.contentSearchQuery
	if m.contentSearchPanel == filesContentSearchQuery {
		queryVal += "█"
	}
	queryLine := styles.Hint.Render(queryLabel) + styles.Selected.Width(width-len(queryLabel)).Render(queryVal)
	queryLine = lipgloss.NewStyle().Width(width).MaxHeight(1).Render(queryLine)

	extVal := m.contentSearchExts
	if extVal == "" {
		extVal = "(all files)"
	}
	if m.contentSearchPanel == filesContentSearchExtFilter {
		extVal += "█"
	}
	extLine := styles.Hint.Render(extLabel) + styles.Selected.Width(width-len(extLabel)).Render(extVal)
	extLine = lipgloss.NewStyle().Width(width).MaxHeight(1).Render(extLine)

	matchVal := "literal"
	if m.contentSearchMatch == filesContentSearchRegex {
		matchVal = "regex"
	}
	caseVal := "insensitive"
	if m.contentSearchCaseSensitive {
		caseVal = "sensitive"
	}
	wholeVal := "off"
	if m.contentSearchWholeWord {
		wholeVal = "on"
	}
	fieldLine := func(label, value string, panel filesContentSearchPanel) string {
		style := styles.Hint
		if m.contentSearchPanel == panel {
			style = styles.Selected
		}
		line := style.Render(label) + styles.Selected.Render(value)
		return lipgloss.NewStyle().Width(width).MaxHeight(1).Render(line)
	}

	matchLine := fieldLine(matchLabel, matchVal, filesContentSearchMatchField)
	caseLine := fieldLine(caseLabel, caseVal, filesContentSearchCase)
	wholeLine := fieldLine(wholeLabel, wholeVal, filesContentSearchWholeWord)

	// Ignore toggle
	ignoreVal := "excluded"
	if m.contentSearchIncludeIgnored {
		ignoreVal = "included"
	}
	ignoreStyle := styles.Hint
	if m.contentSearchPanel == filesContentSearchIgnore {
		ignoreStyle = styles.Selected
	}
	ignoreLine := ignoreStyle.Render("Ignored: " + ignoreVal + "  (Ctrl+L toggle)")
	ignoreLine = lipgloss.NewStyle().Width(width).MaxHeight(1).Render(ignoreLine)

	lines = append(lines, queryLine, extLine, matchLine, caseLine, wholeLine, ignoreLine, "")

	// Hints
	if m.contentSearchLoading {
		lines = append(lines, lipgloss.NewStyle().Width(width).MaxHeight(1).Render(styles.Hint.Render(fmt.Sprintf("Searching... %d results so far", len(m.contentSearchResults)))))
	} else if m.contentSearchDone {
		if m.contentSearchError != "" {
			lines = append(lines, lipgloss.NewStyle().Width(width).MaxHeight(1).Render(styles.Error.Render("Search error: "+m.contentSearchError)))
		} else if len(m.contentSearchResults) == 0 {
			lines = append(lines, lipgloss.NewStyle().Width(width).MaxHeight(1).Render(styles.Hint.Render("No results found")))
		} else {
			lines = append(lines, lipgloss.NewStyle().Width(width).MaxHeight(1).Render(styles.Hint.Render(fmt.Sprintf("%d results — ctrl+n/ctrl+p navigate  enter open  esc back", len(m.contentSearchResults)))))
		}
	} else {
		lines = append(lines, lipgloss.NewStyle().Width(width).MaxHeight(1).Render(styles.Hint.Render("Tab/Shift+Tab focus  Space toggle  Enter run  Esc back")))
	}

	lines = append(lines, "")

	// Results
	start, end := m.contentSearchResultWindow(height)

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

func (m filesModel) contentSearchResultWindow(height int) (start, end int) {
	visibleResults := height - contentSearchResultsStartLine - 2
	if visibleResults < 1 {
		visibleResults = 1
	}
	if visibleResults > len(m.contentSearchResults) {
		visibleResults = len(m.contentSearchResults)
	}
	if len(m.contentSearchResults) > visibleResults {
		start = m.contentSearchCursor - visibleResults/2
		if start < 0 {
			start = 0
		}
		if start > len(m.contentSearchResults)-visibleResults {
			start = len(m.contentSearchResults) - visibleResults
		}
	}
	end = min(start+visibleResults, len(m.contentSearchResults))
	return start, end
}

func contentSearchPaneHeight(terminalHeight int) int {
	return max(1, terminalHeight-6)
}
