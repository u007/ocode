package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/quick"
	"github.com/atotto/clipboard"
	"github.com/mattn/go-runewidth"
	"github.com/u007/ocode/internal/config"
)

type filesPreviewMsg struct {
	path        string
	content     string
	raw         string
	size        int64
	language    string
	editable    bool
	session     uint64
	append      bool
	startOffset int64
	nextOffset  int64
	done        bool
	err         error
}

type filesAddToContextMsg struct {
	path      string
	content   string
	startLine int
	endLine   int
}

// filesGitStatusUpdateMsg carries a lightweight git status refresh for the
// files tab. Unlike rebuildTreeKeeping, it only updates badge decorations
// without touching the tree structure or cursor position.
type filesGitStatusUpdateMsg struct {
	gitStatus map[string]string
}

type filesMode int

const (
	filesModeNormal filesMode = iota
	filesModePrompt
	filesModeDeleteConfirm
	filesModeEdit
	filesModeContentSearch
	filesModeFuzzy
	filesModeInFileSearch
)

type filesPromptKind int

const (
	filesPromptCreateFile filesPromptKind = iota
	filesPromptCreateDir
	filesPromptRename
)

type filesPanel int

const (
	filesPanelPicker filesPanel = iota
	filesPanelPreview
)

// filesContentSearchResult holds a single content search match.
type filesContentSearchResult struct {
	path    string // absolute path
	relPath string // relative to workDir
	line    int    // 1-based line number
	text    string // matching line content
}

// filesContentSearchPanel indicates which input field is focused.
type filesContentSearchPanel int

const (
	filesContentSearchQuery filesContentSearchPanel = iota
	filesContentSearchExtFilter
)

type fileNode struct {
	path     string
	name     string
	isDir    bool
	size     int64
	modTime  time.Time
	depth    int
	expanded bool
	loaded   bool
}

type filesModel struct {
	workDir              string
	nodes                []fileNode
	cursor               int
	preview              viewport.Model
	allPaths             []string
	width                int
	height               int
	editor               string
	saveEditor           func(string) error
	choosingEditor       bool
	editorCursor         int
	editorTarget         string
	statusMsg            string
	mode                 filesMode
	promptInput          textarea.Model
	promptKind           filesPromptKind
	promptTarget         string
	previewPath          string
	previewSize          int64
	previewLang          string
	previewEditable      bool
	previewSession       uint64
	previewLoading       bool
	previewHasMore       bool
	previewNextOff       int64
	previewPendingScroll int
	gitStatus            map[string]string
	editorOpener         func(string) tea.Cmd
	editorMode           string
	panel                filesPanel
	previewRaw           string
	previewRawLines      []string
	selectedFiles        map[int]bool
	previewLines         []string
	inlineEditor         inlineFileEditor
	inlineEditPath       string
	inlineEditMtime      int64
	inlineEditSize       int64

	// Delete confirmation fields
	deleteTargets []string // paths to delete (multi-select support)

	// Rename confirmation fields
	promptConfirm bool // true when user is confirming overwrite

	// Fuzzy popup fields
	fuzzyQuery   string   // current search query
	fuzzyResults []string // filtered relative paths
	fuzzyCursor  int      // highlighted result index

	// Hidden files toggle
	showHidden bool // true = show hidden files/folders (starting with .)

	// Content search fields
	contentSearchQuery          string
	contentSearchExts           string // comma-separated extension patterns, e.g. "*.go,*.ts"
	contentSearchResults        []filesContentSearchResult
	contentSearchCursor         int
	contentSearchPanel          filesContentSearchPanel // which input is focused
	contentSearchLoading        bool
	contentSearchDone           bool          // true once search completed
	contentSearchCancel         chan struct{} // non-nil while a streaming search is running
	contentSearchIncludeIgnored bool          // true = search everything, false = skip .gitignore + hidden

	// In-file search fields
	inFileSearchQuery   string  // current search query
	inFileSearchMatches [][]int // highlight ranges: [[line, colstart, line, colend], ...]
	inFileSearchCursor  int     // current match index
	inFileSearchActive  bool    // true when search is active

	// Tree horizontal scroll offset
	treeScrollX int // horizontal scroll offset in the tree panel (columns)
	treeScrollY int // vertical scroll offset in the tree panel (lines)
}

func newFilesModel(workDir string) filesModel {
	m := filesModel{workDir: workDir}
	m.preview = viewport.New()
	m.preview.SoftWrap = true
	m.preview.LeftGutterFunc = diffLineNumbers
	m.promptInput = textarea.New()
	m.nodes = loadDirChildren(workDir, 0, false)
	m.refreshGitStatus()
	return m
}

func loadDirChildren(dir string, depth int, showHidden bool) []fileNode {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	nodes := make([]fileNode, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		info, infoErr := e.Info()
		nodes = append(nodes, fileNode{
			path:    filepath.Join(dir, name),
			name:    name,
			isDir:   e.IsDir(),
			size:    fileSize(info, infoErr),
			modTime: fileModTime(info, infoErr),
			depth:   depth,
		})
	}
	return nodes
}

const (
	previewChunkBytes        = 256 * 1024
	previewChunkLines        = 4096
	previewPrefetchThreshold = 6
	previewEditLimitBytes    = 1 * 1024 * 1024
)

func fileModTime(info os.FileInfo, err error) time.Time {
	if err != nil || info == nil {
		return time.Time{}
	}
	return info.ModTime()
}

func formatModTime(t time.Time) string {
	return formatModTimeAt(t, time.Now())
}

func formatModTimeAt(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 02")
	}
}

func formatFileNodeMeta(n fileNode) string {
	if n.isDir {
		return ""
	}
	return formatBytes(n.size)
}

func readPreviewChunk(path string, startOffset int64) (raw string, nextOffset int64, done bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", startOffset, false, err
	}
	defer f.Close()

	if startOffset > 0 {
		if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
			return "", startOffset, false, err
		}
	}

	reader := bufio.NewReader(f)
	var consumed = startOffset
	var lines []string
	var lineBuf bytes.Buffer
	for len(lines) < previewChunkLines {
		part, readErr := reader.ReadBytes('\n')
		if len(part) > 0 {
			lineBuf.Write(part)
			consumed += int64(len(part))
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if lineBuf.Len() > 0 {
			line := lineBuf.String()
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			lines = append(lines, line)
			lineBuf.Reset()
		}
		if readErr == io.EOF {
			done = true
			break
		}
		if readErr != nil {
			return "", startOffset, false, readErr
		}
		if consumed-startOffset >= previewChunkBytes {
			break
		}
	}
	if len(lines) == 0 {
		return "", consumed, done, nil
	}
	return strings.Join(lines, "\n"), consumed, done, nil
}

func previewLoadMessage(path string, isDir bool, startOffset int64, session uint64, appendChunk bool) tea.Msg {
	if isDir {
		content := "[directory]"
		return filesPreviewMsg{
			path:        path,
			content:     content,
			raw:         content,
			size:        0,
			language:    "directory",
			editable:    false,
			session:     session,
			append:      appendChunk,
			startOffset: startOffset,
			nextOffset:  startOffset,
			done:        true,
		}
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		content := "[cannot read file]"
		return filesPreviewMsg{
			path:        path,
			content:     content,
			raw:         content,
			size:        0,
			language:    languageForPath(path),
			editable:    false,
			session:     session,
			append:      appendChunk,
			startOffset: startOffset,
			nextOffset:  startOffset,
			done:        true,
			err:         statErr,
		}
	}

	if startOffset == 0 {
		f, err := os.Open(path)
		if err != nil {
			content := "[cannot read file]"
			return filesPreviewMsg{
				path:        path,
				content:     content,
				raw:         content,
				size:        fileSize(info, statErr),
				language:    languageForPath(path),
				editable:    false,
				session:     session,
				append:      appendChunk,
				startOffset: startOffset,
				nextOffset:  startOffset,
				done:        true,
				err:         err,
			}
		}
		defer f.Close()

		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		if bytes.IndexByte(buf[:n], 0) >= 0 {
			content := "[binary file]"
			return filesPreviewMsg{
				path:        path,
				content:     content,
				raw:         content,
				size:        fileSize(info, statErr),
				language:    languageForPath(path),
				editable:    false,
				session:     session,
				append:      appendChunk,
				startOffset: startOffset,
				nextOffset:  startOffset,
				done:        true,
			}
		}
	}

	raw, nextOffset, done, err := readPreviewChunk(path, startOffset)
	if err != nil {
		content := "[cannot read file]"
		return filesPreviewMsg{
			path:        path,
			content:     content,
			raw:         content,
			size:        fileSize(info, statErr),
			language:    languageForPath(path),
			editable:    false,
			session:     session,
			append:      appendChunk,
			startOffset: startOffset,
			nextOffset:  startOffset,
			done:        true,
			err:         err,
		}
	}

	language := languageForPath(path)
	editable := info.Size() <= previewEditLimitBytes
	content := highlightContent(raw, language)
	return filesPreviewMsg{
		path:        path,
		content:     content,
		raw:         raw,
		size:        fileSize(info, statErr),
		language:    language,
		editable:    editable,
		session:     session,
		append:      appendChunk,
		startOffset: startOffset,
		nextOffset:  nextOffset,
		done:        done,
	}
}

func (m *filesModel) SetEditor(e string) { m.editor = e }

func (m *filesModel) SetSaveEditor(fn func(string) error) { m.saveEditor = fn }

func (m *filesModel) SetEditorOpener(fn func(string) tea.Cmd) { m.editorOpener = fn }

func (m *filesModel) SetEditorMode(mode string) { m.editorMode = mode }

// performInFileSearch searches for the query in the current preview content
// and returns highlight ranges. Each range is [line, colstart, line, colend].
func (m *filesModel) performInFileSearch(query string) [][]int {
	if query == "" {
		return nil
	}
	var matches [][]int
	queryLower := strings.ToLower(query)
	for lineIdx, line := range m.previewRawLines {
		lineLower := strings.ToLower(line)
		start := 0
		for {
			idx := strings.Index(lineLower[start:], queryLower)
			if idx == -1 {
				break
			}
			colStart := start + idx
			colEnd := colStart + len(query)
			matches = append(matches, []int{lineIdx, colStart, lineIdx, colEnd})
			start = colStart + 1
		}
	}
	return matches
}

// applyInFileSearchHighlights highlights matches in the preview viewport.
func (m *filesModel) applyInFileSearchHighlights() {
	m.preview.SetHighlights(m.inFileSearchMatches)
	if len(m.inFileSearchMatches) > 0 {
		m.preview.HighlightNext()
	}
}

// setInFileSearchStyles configures the viewport highlight styles for in-file search.
func (m *filesModel) setInFileSearchStyles(styles Styles) {
	m.preview.HighlightStyle = lipgloss.NewStyle().Background(styles.Selected.GetBackground()).Foreground(styles.Selected.GetForeground())
	m.preview.SelectedHighlightStyle = lipgloss.NewStyle().Background(lipgloss.Color("220")).Foreground(lipgloss.Color("0")).Bold(true)
}

func (m *filesModel) Resize(w, h int) {
	m.width = w
	m.height = h
	treeW := w * 35 / 100
	previewW := w - treeW - 3
	previewH := h - 4 // reserve 1 row for bottom status bar
	if previewH < 1 {
		previewH = 1
	}
	m.preview.SetWidth(previewW - 14)
	m.preview.SetHeight(previewH)
	m.promptInput.SetWidth(previewW - 7)
	m.promptInput.SetHeight(1)
}

func (m filesModel) Update(msg tea.Msg, w, h int) (filesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case filesPreviewMsg:
		return m, m.applyPreview(msg)
	case filesGitStatusUpdateMsg:
		m.gitStatus = msg.gitStatus
		return m, nil
	case filesContentSearchBatchMsg:
		// Discard stale messages from a previous (cancelled) search.
		if msg.cancel != m.contentSearchCancel {
			return m, nil
		}
		m.contentSearchResults = append(m.contentSearchResults, msg.batch...)
		m.contentSearchCursor = 0
		m.statusMsg = fmt.Sprintf("Searching... %d results", msg.totalSoFar)
		return m, waitSearchEvent(msg.ch, msg.cancel)
	case filesContentSearchDoneMsg:
		if msg.cancel != m.contentSearchCancel {
			return m, nil
		}
		m.contentSearchLoading = false
		m.contentSearchDone = true
		m.contentSearchCancel = nil
		if msg.err != nil {
			m.statusMsg = "search error: " + msg.err.Error()
		} else if len(m.contentSearchResults) == 0 {
			m.statusMsg = "no results found"
		} else {
			m.statusMsg = fmt.Sprintf("%d results found — ctrl+n/ctrl+p navigate  enter open", len(m.contentSearchResults))
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.choosingEditor {
			return m.updateEditorPicker(msg)
		}
		if m.mode == filesModePrompt {
			return m.updatePrompt(msg)
		}
		if m.mode == filesModeDeleteConfirm {
			return m.updateDeleteConfirm(msg)
		}
		if m.mode == filesModeEdit {
			return m.updateInlineEdit(msg)
		}
		if m.mode == filesModeContentSearch {
			return m.updateContentSearch(msg)
		}
		if m.mode == filesModeFuzzy {
			return m.updateFuzzy(msg)
		}
		if m.mode == filesModeInFileSearch {
			return m.updateInFileSearch(msg)
		}
		if m.panel == filesPanelPreview {
			return m.updatePreview(msg)
		}
		return m.updateTree(msg, w, h)
	}
	return m, nil
}

func (m filesModel) updateTree(msg tea.KeyPressMsg, w, h int) (filesModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "down":
		if m.cursor < len(m.nodes)-1 {
			m.cursor++
			// Reset horizontal scroll when cursor moves
			m.treeScrollX = 0
			if m.cursor < len(m.nodes) {
				return m, m.loadPreviewCmd(m.nodes[m.cursor])
			}
		}
	case "up":
		if m.cursor > 0 {
			m.cursor--
			// Reset horizontal scroll when cursor moves
			m.treeScrollX = 0
			if m.cursor < len(m.nodes) {
				return m, m.loadPreviewCmd(m.nodes[m.cursor])
			}
		}
	case "enter", "ctrl+j", "ctrl+m":
		if m.cursor >= 0 && m.cursor < len(m.nodes) {
			n := &m.nodes[m.cursor]
			if n.isDir {
				m.toggleDir(m.cursor)
			} else {
				return m, m.openInEditor(n.path)
			}
		}
	case "space":
		if m.cursor >= 0 && m.cursor < len(m.nodes) {
			if m.selectedFiles == nil {
				m.selectedFiles = make(map[int]bool)
			}
			if m.selectedFiles[m.cursor] {
				delete(m.selectedFiles, m.cursor)
				if len(m.selectedFiles) == 0 {
					m.selectedFiles = nil
				}
			} else {
				m.selectedFiles[m.cursor] = true
			}
		}
	case "ctrl+e":
		if m.cursor >= 0 && m.cursor < len(m.nodes) && !m.nodes[m.cursor].isDir {
			return m, m.openInEditor(m.nodes[m.cursor].path)
		}
	case "shift+down":
		if m.cursor >= 0 && m.cursor < len(m.nodes)-1 {
			if m.selectedFiles == nil {
				m.selectedFiles = make(map[int]bool)
			}
			m.selectedFiles[m.cursor] = true
			m.cursor++
			m.selectedFiles[m.cursor] = true
			if m.cursor < len(m.nodes) {
				return m, m.loadPreviewCmd(m.nodes[m.cursor])
			}
		}
	case "shift+up":
		if m.cursor > 0 {
			if m.selectedFiles == nil {
				m.selectedFiles = make(map[int]bool)
			}
			m.selectedFiles[m.cursor] = true
			m.cursor--
			m.selectedFiles[m.cursor] = true
			if m.cursor < len(m.nodes) {
				return m, m.loadPreviewCmd(m.nodes[m.cursor])
			}
		}
	case "left":
		m.treeScrollX -= 5
		if m.treeScrollX < 0 {
			m.treeScrollX = 0
		}
	case "right":
		m.treeScrollX += 5

	case "ctrl+v":
		if m.cursor >= 0 && m.cursor < len(m.nodes) && !m.nodes[m.cursor].isDir {
			m.openEditorPicker(m.nodes[m.cursor].path)
		}
	case "ctrl+n":
		m.startCreateFile()
	case "ctrl+b":
		m.startCreateDir()
	case "ctrl+r":
		m.startRename()
	case "ctrl+d":
		m.startDelete()
	case "ctrl+l":
		return m.startInlineEdit()
	case "ctrl+y":
		m.copySelectedPath()
	case "ctrl+o":
		if m.cursor >= 0 && m.cursor < len(m.nodes) {
			return m, openInFileExplorer(m.nodes[m.cursor].path)
		}
	case "ctrl+t":
		return m, m.refreshPreviewCmd()
	case "ctrl+g":
		m.mode = filesModeFuzzy
		m.fuzzyQuery = ""
		m.fuzzyResults = nil
		m.fuzzyCursor = 0
		m.buildAllPaths()
		m.statusMsg = ""
	case "ctrl+f":
		m.mode = filesModeContentSearch
		m.contentSearchQuery = ""
		m.contentSearchExts = ""
		m.contentSearchResults = nil
		m.contentSearchCursor = 0
		m.contentSearchPanel = filesContentSearchQuery
		m.contentSearchLoading = false
		m.contentSearchDone = false
		m.statusMsg = "content search: type query, Tab to switch filter, Enter to search"
	case "ctrl+h":
		m.showHidden = !m.showHidden
		m.nodes = loadDirChildren(m.workDir, 0, m.showHidden)
		m.cursor = 0
		m.previewLoading = false
		m.previewHasMore = false
		m.previewNextOff = 0
		m.previewPendingScroll = 0
		if m.showHidden {
			m.statusMsg = "showing hidden files"
		} else {
			m.statusMsg = "hiding hidden files"
		}
		return m, m.refreshPreviewCmd()
	case "tab":
		m.panel = (m.panel + 1) % 2
	}
	return m, nil
}

func (m filesModel) updatePreview(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	switch msg.String() {
	case "down":
		if cmd := m.scrollPreviewDown(1); cmd != nil {
			return m, cmd
		}
	case "up":
		m.scrollPreviewUp(1)
	case "tab":
		m.panel = (m.panel + 1) % 2
	case "ctrl+e":
		if m.cursor >= 0 && m.cursor < len(m.nodes) && !m.nodes[m.cursor].isDir {
			return m, m.openInEditor(m.nodes[m.cursor].path)
		}
	case "ctrl+l":
		return m.startInlineEdit()
	case "ctrl+f":
		m.mode = filesModeInFileSearch
		m.inFileSearchQuery = ""
		m.inFileSearchMatches = nil
		m.inFileSearchCursor = 0
		m.inFileSearchActive = true
		m.preview.ClearHighlights()
		m.statusMsg = "ctrl+f (type to search, ctrl+n/ctrl+p navigate, esc cancel)"
	}
	return m, nil
}

func (m filesModel) updateInFileSearch(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = filesModeNormal
		m.inFileSearchActive = false
		m.preview.ClearHighlights()
		m.statusMsg = ""
		return m, nil
	case "enter", "ctrl+j", "ctrl+m", "\r", "\n":
		m.mode = filesModeNormal
		m.inFileSearchActive = false
		m.statusMsg = ""
		return m, nil
	case "ctrl+n":
		// Only navigate if there are matches; otherwise do nothing.
		if len(m.inFileSearchMatches) > 0 {
			m.preview.HighlightNext()
		}
		return m, nil
	case "ctrl+p":
		// Only navigate if there are matches; otherwise do nothing.
		if len(m.inFileSearchMatches) > 0 {
			m.preview.HighlightPrevious()
		}
		return m, nil
	case "backspace", "\x7f":
		if len(m.inFileSearchQuery) > 0 {
			m.inFileSearchQuery = m.inFileSearchQuery[:len(m.inFileSearchQuery)-1]
			m.inFileSearchMatches = m.performInFileSearch(m.inFileSearchQuery)
			m.applyInFileSearchHighlights()
			if len(m.inFileSearchMatches) > 0 {
				m.statusMsg = fmt.Sprintf("/%s (%d matches, ctrl+n/ctrl+p navigate, esc cancel)", m.inFileSearchQuery, len(m.inFileSearchMatches))
			} else {
				m.statusMsg = fmt.Sprintf("/%s (no matches, esc cancel)", m.inFileSearchQuery)
			}
		}
		return m, nil
	default:
		// Handle printable characters (exclude control characters)
		if len(msg.String()) == 1 && msg.String()[0] >= 32 && msg.String()[0] != 127 {
			m.inFileSearchQuery += msg.String()
			m.inFileSearchMatches = m.performInFileSearch(m.inFileSearchQuery)
			m.applyInFileSearchHighlights()
			if len(m.inFileSearchMatches) > 0 {
				m.statusMsg = fmt.Sprintf("/%s (%d matches, ctrl+n/ctrl+p navigate, esc cancel)", m.inFileSearchQuery, len(m.inFileSearchMatches))
			} else {
				m.statusMsg = fmt.Sprintf("/%s (no matches, esc cancel)", m.inFileSearchQuery)
			}
		}
		return m, nil
	}
}

func (m filesModel) updatePrompt(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	prevValue := m.promptInput.Value()
	switch msg.String() {
	case "esc":
		m.mode = filesModeNormal
		m.promptConfirm = false
		m.statusMsg = "action cancelled"
		return m, nil
	case "enter", "ctrl+j", "ctrl+m":
		return m.submitPrompt()
	}
	var cmd tea.Cmd
	m.promptInput, cmd = m.promptInput.Update(msg)
	if m.promptInput.Value() != prevValue {
		m.promptConfirm = false
	}
	return m, cmd
}

func (m filesModel) updateDeleteConfirm(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "shift+y":
		targets := m.deleteTargets
		if len(targets) == 0 {
			targets = []string{m.promptTarget}
		}
		var errs []string
		for _, path := range targets {
			info, statErr := os.Stat(path)
			if statErr != nil {
				errs = append(errs, filepath.Base(path)+": "+statErr.Error())
				continue
			}
			var rmErr error
			if info.IsDir() {
				rmErr = os.RemoveAll(path)
			} else {
				rmErr = os.Remove(path)
			}
			if rmErr != nil {
				errs = append(errs, filepath.Base(path)+": "+rmErr.Error())
			}
		}
		m.mode = filesModeNormal
		m.deleteTargets = nil
		m.selectedFiles = nil
		if len(errs) > 0 {
			m.statusMsg = "delete errors: " + strings.Join(errs, "; ")
		} else if len(targets) == 1 {
			m.statusMsg = "deleted: " + filepath.Base(targets[0])
		} else {
			m.statusMsg = fmt.Sprintf("deleted %d items", len(targets))
		}
		// Rebuild from the deepest parent
		if len(targets) > 0 {
			m.rebuildTreeKeeping(filepath.Dir(targets[0]))
		}
		return m, m.refreshPreviewCmd()
	case "n", "N", "esc":
		m.mode = filesModeNormal
		m.deleteTargets = nil
		m.statusMsg = "delete cancelled"
	}
	return m, nil
}

func (m filesModel) startInlineEdit() (filesModel, tea.Cmd) {
	n, ok := m.selectedNode()
	if !ok {
		m.statusMsg = "no file selected"
		return m, nil
	}
	if n.isDir {
		m.statusMsg = "cannot edit directory"
		return m, nil
	}
	if !m.previewEditable || m.previewPath != n.path {
		m.statusMsg = "file is not editable"
		return m, nil
	}
	info, err := os.Stat(n.path)
	if err != nil {
		m.statusMsg = "edit stat failed: " + err.Error()
		return m, nil
	}
	data, err := os.ReadFile(n.path)
	if err != nil {
		m.statusMsg = "edit read failed: " + err.Error()
		return m, nil
	}
	m.mode = filesModeEdit
	m.inlineEditor = newInlineFileEditor(string(data))
	m.inlineEditPath = n.path
	m.inlineEditMtime = info.ModTime().UnixNano()
	m.inlineEditSize = info.Size()
	m.statusMsg = "vim edit: i/a insert  :w save  :q quit  :q! discard  :wq save+quit"
	return m, nil
}

func (m filesModel) updateInlineEdit(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	m.inlineEditor = m.inlineEditor.update(msg)
	cmd := m.inlineEditor.lastCommand
	if cmd == "" {
		return m, nil
	}
	switch cmd {
	case "w":
		return m.saveInlineEdit(false)
	case "wq":
		return m.saveInlineEdit(true)
	case "q":
		if m.inlineEditor.dirty {
			m.statusMsg = "unsaved changes: use :w to save or :q! to discard"
			return m, nil
		}
		m.mode = filesModeNormal
		m.statusMsg = "edit closed"
		return m, nil
	case "q!":
		m.mode = filesModeNormal
		m.statusMsg = "edit discarded"
		return m, m.refreshPreviewCmd()
	default:
		m.statusMsg = "unknown command: " + cmd
		return m, nil
	}
}

func (m filesModel) saveInlineEdit(exit bool) (filesModel, tea.Cmd) {
	info, err := os.Stat(m.inlineEditPath)
	if err != nil {
		m.statusMsg = "edit stat failed: " + err.Error()
		return m, nil
	}
	if info.ModTime().UnixNano() != m.inlineEditMtime || info.Size() != m.inlineEditSize {
		m.statusMsg = "file changed on disk; reload before saving"
		return m, nil
	}
	if err := os.WriteFile(m.inlineEditPath, []byte(m.inlineEditor.content()), info.Mode()); err != nil {
		m.statusMsg = "edit save failed: " + err.Error()
		return m, nil
	}
	info, err = os.Stat(m.inlineEditPath)
	if err != nil {
		m.statusMsg = "edit stat failed: " + err.Error()
		return m, nil
	}
	m.inlineEditor.markClean()
	m.inlineEditMtime = info.ModTime().UnixNano()
	m.inlineEditSize = info.Size()
	m.refreshGitStatus()
	m.statusMsg = "saved: " + filepath.Base(m.inlineEditPath)
	if exit {
		m.mode = filesModeNormal
	}
	return m, m.refreshPreviewCmd()
}

func (m filesModel) updateEditorPicker(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	choices := m.editorChoices()
	switch msg.String() {
	case "esc":
		m.choosingEditor = false
	case "j", "down":
		if m.editorCursor < len(choices)-1 {
			m.editorCursor++
		}
	case "k", "up":
		if m.editorCursor > 0 {
			m.editorCursor--
		}
	case "enter", "ctrl+j", "ctrl+m":
		if len(choices) == 0 {
			m.statusMsg = "no editor choices available"
			return m, nil
		}
		choice := choices[m.editorCursor]
		changed := choice != m.editor
		m.editor = choice
		m.choosingEditor = false
		if changed && m.saveEditor != nil {
			if err := m.saveEditor(choice); err != nil {
				m.statusMsg = "editor save failed: " + err.Error()
				return m, nil
			}
		}
		m.statusMsg = "editor: " + choice
		// Route through the parent model so it can rebuild the (stale) editorOpener
		// with the freshly chosen editor before opening. Opening directly here would
		// delegate to the opener captured at startup and ignore the new selection.
		target := m.editorTarget
		return m, func() tea.Msg { return editorPickedMsg{editor: choice, target: target} }
	}
	return m, nil
}

func (m *filesModel) openEditorPicker(path string) {
	m.choosingEditor = true
	m.editorTarget = path
	m.editorCursor = 0
	choices := m.editorChoices()
	for i, choice := range choices {
		if choice == m.editor {
			m.editorCursor = i
			break
		}
	}
}

func (m filesModel) editorChoices() []string {
	seen := map[string]bool{}
	choices := []string{}
	add := func(editor string) {
		editor = strings.TrimSpace(editor)
		if editor == "" || seen[editor] {
			return
		}
		seen[editor] = true
		choices = append(choices, editor)
	}
	add(m.editor)
	add(os.Getenv("VISUAL"))
	add(os.Getenv("EDITOR"))
	for _, candidate := range []string{"vim", "nvim", "vi", "nano", "code --wait", "cursor --wait"} {
		name := strings.Fields(candidate)[0]
		if _, err := exec.LookPath(name); err == nil {
			add(candidate)
		}
	}
	return choices
}

func (m filesModel) updateFuzzy(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.mode = filesModeNormal
		m.fuzzyQuery = ""
		m.fuzzyResults = nil
		m.statusMsg = ""
	case "enter", "ctrl+j", "ctrl+m":
		if len(m.fuzzyResults) > 0 && m.fuzzyCursor >= 0 && m.fuzzyCursor < len(m.fuzzyResults) {
			m.navigateTo(m.fuzzyResults[m.fuzzyCursor])
		}
		m.mode = filesModeNormal
		m.fuzzyQuery = ""
		m.fuzzyResults = nil
		m.statusMsg = ""
	case "down":
		if m.fuzzyCursor < len(m.fuzzyResults)-1 {
			m.fuzzyCursor++
		}
	case "up":
		if m.fuzzyCursor > 0 {
			m.fuzzyCursor--
		}
	case "backspace":
		if len(m.fuzzyQuery) > 0 {
			m.fuzzyQuery = m.fuzzyQuery[:len(m.fuzzyQuery)-1]
			m.fuzzyResults = fuzzyFilter(m.allPaths, m.fuzzyQuery)
			m.fuzzyCursor = 0
		}
	default:
		if len(msg.Text) > 0 {
			m.fuzzyQuery += msg.Text
			m.fuzzyResults = fuzzyFilter(m.allPaths, m.fuzzyQuery)
			m.fuzzyCursor = 0
		}
	}
	// Load preview for highlighted result
	if m.mode == filesModeFuzzy && len(m.fuzzyResults) > 0 && m.fuzzyCursor >= 0 && m.fuzzyCursor < len(m.fuzzyResults) {
		absPath := filepath.Join(m.workDir, m.fuzzyResults[m.fuzzyCursor])
		n := fileNode{path: absPath, name: filepath.Base(absPath), isDir: false}
		return m, m.loadPreviewCmd(n)
	}
	return m, nil
}

func (m *filesModel) toggleDir(idx int) {
	n := &m.nodes[idx]
	if n.expanded {
		depth := n.depth
		end := idx + 1
		for end < len(m.nodes) && m.nodes[end].depth > depth {
			end++
		}
		m.nodes = append(m.nodes[:idx+1], m.nodes[end:]...)
		n.expanded = false
	} else {
		children := loadDirChildren(n.path, n.depth+1, m.showHidden)
		newNodes := make([]fileNode, 0, len(m.nodes)+len(children))
		newNodes = append(newNodes, m.nodes[:idx+1]...)
		newNodes = append(newNodes, children...)
		newNodes = append(newNodes, m.nodes[idx+1:]...)
		m.nodes = newNodes
		n = &m.nodes[idx]
		n.expanded = true
		n.loaded = true
	}
}

func loadPreviewCmd(n fileNode) tea.Cmd {
	return func() tea.Msg {
		return previewLoadMessage(n.path, n.isDir, 0, 0, false)
	}
}

func (m *filesModel) loadPreviewCmd(n fileNode) tea.Cmd {
	m.previewSession++
	m.previewLoading = true
	m.previewPendingScroll = 0
	session := m.previewSession
	return func() tea.Msg {
		return previewLoadMessage(n.path, n.isDir, 0, session, false)
	}
}

func (m *filesModel) loadMorePreviewCmd() tea.Cmd {
	if m.previewPath == "" || !m.previewHasMore || m.previewLoading {
		return nil
	}
	m.previewLoading = true
	path := m.previewPath
	offset := m.previewNextOff
	session := m.previewSession
	return func() tea.Msg {
		return previewLoadMessage(path, false, offset, session, true)
	}
}

func fileSize(info os.FileInfo, err error) int64 {
	if err != nil || info == nil {
		return 0
	}
	return info.Size()
}

func (m *filesModel) applyPreview(msg filesPreviewMsg) tea.Cmd {
	if msg.session != 0 && msg.session != m.previewSession {
		return nil
	}
	m.previewLoading = false
	if msg.err != nil {
		m.statusMsg = "preview error: " + msg.err.Error()
		return nil
	}
	if !msg.append {
		m.previewPath = msg.path
		m.previewSize = msg.size
		m.previewLang = msg.language
		m.previewEditable = msg.editable
		m.previewRaw = msg.raw
		m.previewRawLines = nil
		m.previewLines = nil
		m.previewHasMore = !msg.done
		m.previewNextOff = msg.nextOffset
		if msg.raw != "" {
			m.previewRawLines = strings.Split(msg.raw, "\n")
		}
		if msg.content != "" {
			m.previewLines = strings.Split(msg.content, "\n")
		}
		m.preview.SetContent(msg.content)
		m.preview.GotoTop()
	} else {
		if m.previewPath != "" && m.previewPath != msg.path {
			return nil
		}
		if msg.raw != "" {
			if m.previewRaw != "" {
				m.previewRaw += "\n"
			}
			m.previewRaw += msg.raw
			if len(m.previewRawLines) > 0 {
				m.previewRawLines = append(m.previewRawLines, strings.Split(msg.raw, "\n")...)
			} else {
				m.previewRawLines = strings.Split(msg.raw, "\n")
			}
		}
		if msg.content != "" {
			if len(m.previewLines) > 0 {
				m.previewLines = append(m.previewLines, strings.Split(msg.content, "\n")...)
			} else {
				m.previewLines = strings.Split(msg.content, "\n")
			}
		}
		m.previewHasMore = !msg.done
		m.previewNextOff = msg.nextOffset
		m.preview.SetContent(strings.Join(m.previewLines, "\n"))
	}

	if m.previewPendingScroll > 0 {
		m.preview.ScrollDown(m.previewPendingScroll)
		m.previewPendingScroll = 0
	}

	if m.previewHasMore && !m.previewLoading && m.previewShouldLoadMore() {
		return m.loadMorePreviewCmd()
	}
	return nil
}

func (m filesModel) previewAtBottom() bool {
	if m.preview.TotalLineCount() == 0 {
		return false
	}
	return m.preview.YOffset()+m.preview.VisibleLineCount() >= m.preview.TotalLineCount()
}

func (m filesModel) previewShouldLoadMore() bool {
	if !m.previewHasMore || m.previewLoading || m.previewPath == "" {
		return false
	}
	loaded := m.preview.TotalLineCount()
	visible := m.preview.VisibleLineCount()
	if loaded <= visible+previewPrefetchThreshold {
		return true
	}
	return m.preview.YOffset()+visible >= loaded-previewPrefetchThreshold
}

func (m *filesModel) scrollPreviewDown(n int) tea.Cmd {
	if n <= 0 {
		return nil
	}
	if m.previewLoading && m.previewAtBottom() {
		m.previewPendingScroll += n
		return nil
	}
	if m.previewHasMore && !m.previewLoading && m.previewAtBottom() {
		m.previewPendingScroll += n
		return m.loadMorePreviewCmd()
	}
	m.preview.ScrollDown(n)
	if m.previewShouldLoadMore() {
		return m.loadMorePreviewCmd()
	}
	return nil
}

func (m *filesModel) scrollPreviewUp(n int) {
	if n <= 0 {
		return
	}
	m.preview.ScrollUp(n)
}

func (m *filesModel) clearActiveFile() {
	m.previewSession++
	m.previewLoading = false
	m.previewHasMore = false
	m.previewNextOff = 0
	m.previewPendingScroll = 0
	m.cursor = -1
	m.previewPath = ""
	m.previewSize = 0
	m.previewLang = ""
	m.previewEditable = false
	m.preview.SetContent("")
	m.previewRaw = ""
	m.previewRawLines = nil
	m.previewLines = nil
	m.preview.GotoTop()
}

func highlightContent(content string, language string) string {
	if language == "" || language == "text" || language == "directory" {
		return content
	}
	var highlighted bytes.Buffer
	if err := quick.Highlight(&highlighted, content, language, "terminal16m", "monokai"); err != nil {
		// intentionally not logged: unknown lexer/style should not block plain-text preview
		return content
	}
	return highlighted.String()
}

func (m *filesModel) buildAllPaths() {
	m.allPaths = nil
	_ = filepath.Walk(m.workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := filepath.Base(path)
		if strings.HasPrefix(name, ".") && path != m.workDir {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(m.workDir, path)
			m.allPaths = append(m.allPaths, rel)
		}
		return nil
	})
}

func (m *filesModel) navigateTo(relPath string) {
	m.nodes = loadDirChildren(m.workDir, 0, m.showHidden)
	parts := strings.Split(relPath, string(filepath.Separator))
	current := m.workDir
	for i, part := range parts {
		target := filepath.Join(current, part)
		found := false
		for idx := 0; idx < len(m.nodes); idx++ {
			n := m.nodes[idx]
			if n.name == part && n.path == target {
				if i < len(parts)-1 && n.isDir {
					m.toggleDir(idx)
					// m.nodes was mutated by toggleDir; the index loop sees the updated slice
				} else {
					m.cursor = idx
					// one-shot navigation at startup; sync read is acceptable here
					if idx < len(m.nodes) {
						if result, ok := m.loadPreviewCmd(m.nodes[idx])().(filesPreviewMsg); ok {
							m.applyPreview(result)
						}
					}
				}
				found = true
				break
			}
		}
		if !found {
			break
		}
		current = target
	}
}

func (m filesModel) treeNodeForClick(mouse tea.Mouse, headerHeight int, styles Styles) (int, bool) {
	treeW := m.width * 35 / 100
	if mouse.X >= treeW {
		return 0, false
	}
	// Tree content starts after header + 1 (border top line), plus the hint rows
	// prepended in View(). treeHeaderRows is the single source of truth for those
	// rows — View renders exactly these and the click offset is their count, so
	// the two can never drift (which previously broke hit-boxes on narrow screens).
	treeContentTop := headerHeight + 1 + len(m.treeHeaderRows(treeW, styles))
	if mouse.Y < treeContentTop {
		return 0, false
	}
	nodeIndex := mouse.Y - treeContentTop
	if nodeIndex < 0 || nodeIndex >= len(m.nodes) {
		return 0, false
	}
	return nodeIndex, true
}

func (m *filesModel) rebuildTreeKeeping(path string) {
	rel, err := filepath.Rel(m.workDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = ""
	}
	m.nodes = loadDirChildren(m.workDir, 0, m.showHidden)
	m.refreshGitStatus()
	if rel != "" && rel != "." {
		m.navigateTo(rel)
		return
	}
	if m.cursor >= len(m.nodes) {
		m.cursor = len(m.nodes) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *filesModel) refreshPreviewCmd() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.nodes) {
		return nil
	}
	return m.loadPreviewCmd(m.nodes[m.cursor])
}

func (m filesModel) selectedNode() (fileNode, bool) {
	if m.cursor < 0 || m.cursor >= len(m.nodes) {
		return fileNode{}, false
	}
	return m.nodes[m.cursor], true
}

func (m filesModel) selectedFilePaths() []string {
	if len(m.selectedFiles) == 0 {
		return nil
	}
	indices := make([]int, 0, len(m.selectedFiles))
	for idx := range m.selectedFiles {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	paths := make([]string, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(m.nodes) && !m.nodes[idx].isDir {
			paths = append(paths, m.nodes[idx].path)
		}
	}
	return paths
}

func (m filesModel) selectionHint() string {
	if len(m.selectedFiles) == 0 {
		return ""
	}
	return fmt.Sprintf("%d selected — space toggle  shift+↑↓ extend  D delete  esc clear", len(m.selectedFiles))
}

func (m filesModel) selectedActionDir() string {
	n, ok := m.selectedNode()
	if !ok {
		return m.workDir
	}
	if n.isDir {
		return n.path
	}
	return filepath.Dir(n.path)
}

func (m *filesModel) startCreateFile() {
	m.mode = filesModePrompt
	m.promptKind = filesPromptCreateFile
	m.promptTarget = m.selectedActionDir()
	m.promptInput.SetValue("")
	m.promptInput.Focus()
	m.statusMsg = "new file name"
}

func (m *filesModel) startCreateDir() {
	m.mode = filesModePrompt
	m.promptKind = filesPromptCreateDir
	m.promptTarget = m.selectedActionDir()
	m.promptInput.SetValue("")
	m.promptInput.Focus()
	m.statusMsg = "new directory name"
}

func (m *filesModel) startRename() {
	n, ok := m.selectedNode()
	if !ok {
		return
	}
	m.mode = filesModePrompt
	m.promptKind = filesPromptRename
	m.promptTarget = n.path
	m.promptInput.SetValue(n.name)
	m.promptInput.Focus()
	m.statusMsg = "rename"
}

func (m *filesModel) startDelete() {
	// Collect targets: multi-selected items or just cursor node
	m.deleteTargets = nil
	if len(m.selectedFiles) > 0 {
		// Collect all selected paths, sorted by depth descending (children first)
		type pathDepth struct {
			path  string
			depth int
		}
		var items []pathDepth
		for idx := range m.selectedFiles {
			if idx >= 0 && idx < len(m.nodes) {
				items = append(items, pathDepth{path: m.nodes[idx].path, depth: m.nodes[idx].depth})
			}
		}
		// Sort by depth descending so children are deleted before parents
		sort.Slice(items, func(i, j int) bool { return items[i].depth > items[j].depth })
		for _, item := range items {
			m.deleteTargets = append(m.deleteTargets, item.path)
		}
		m.mode = filesModeDeleteConfirm
		m.statusMsg = fmt.Sprintf("delete %d items?", len(m.deleteTargets))
		return
	}
	n, ok := m.selectedNode()
	if !ok {
		return
	}
	m.mode = filesModeDeleteConfirm
	m.promptTarget = n.path
	m.deleteTargets = nil
	m.statusMsg = "delete " + n.name + "? y/N"
}

func (m filesModel) submitPrompt() (filesModel, tea.Cmd) {
	name := strings.TrimSpace(m.promptInput.Value())
	if name == "" || strings.Contains(name, string(filepath.Separator)) {
		m.statusMsg = "invalid name"
		return m, nil
	}
	var target string
	switch m.promptKind {
	case filesPromptCreateFile:
		target = filepath.Join(m.promptTarget, name)
		f, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			m.statusMsg = "create file failed: " + err.Error()
			return m, nil
		}
		if err := f.Close(); err != nil {
			m.statusMsg = "create file close failed: " + err.Error()
			return m, nil
		}
	case filesPromptCreateDir:
		target = filepath.Join(m.promptTarget, name)
		if err := os.Mkdir(target, 0755); err != nil {
			m.statusMsg = "create directory failed: " + err.Error()
			return m, nil
		}
	case filesPromptRename:
		target = filepath.Join(filepath.Dir(m.promptTarget), name)
		if _, err := os.Stat(target); err == nil && !m.promptConfirm {
			// Target exists and user hasn't confirmed yet
			m.promptConfirm = true
			m.statusMsg = name + " already exists"
			return m, nil
		}
		if err := os.Rename(m.promptTarget, target); err != nil {
			m.statusMsg = "rename failed: " + err.Error()
			return m, nil
		}
	}
	m.mode = filesModeNormal
	m.statusMsg = "saved: " + name
	m.rebuildTreeKeeping(target)
	return m, m.refreshPreviewCmd()
}

func (m *filesModel) copySelectedPath() {
	n, ok := m.selectedNode()
	if !ok {
		return
	}
	rel, err := filepath.Rel(m.workDir, n.path)
	if err != nil {
		m.statusMsg = "copy path failed: " + err.Error()
		return
	}
	if err := clipboard.WriteAll(rel); err != nil {
		m.statusMsg = "copy path failed: " + err.Error()
		return
	}
	m.statusMsg = "copied path: " + rel
}

func (m filesModel) openInEditor(path string) tea.Cmd {
	if isBinaryFile(path) {
		log.Printf("[editor] files openInEditor: using system opener for binary file=%q", path)
		return openFileWithOSDefault(path)
	}
	if m.editorOpener != nil {
		log.Printf("[editor] files openInEditor: delegating to editorOpener for file=%q", path)
		return m.editorOpener(path)
	}
	editor := m.editor
	if editor == "" {
		editor = "vi"
	}
	cmdParts := strings.Fields(editor)
	if len(cmdParts) == 0 {
		log.Printf("[editor] files openInEditor: no valid editor command configured")
		return func() tea.Msg { return editorFinishedMsg{err: os.ErrInvalid} }
	}
	// Validate the editor binary exists before attempting to run it.
	if _, err := exec.LookPath(cmdParts[0]); err != nil {
		log.Printf("[editor] files openInEditor: editor %q not found in PATH: %v", cmdParts[0], err)
		return func() tea.Msg {
			return editorFinishedMsg{err: fmt.Errorf("editor %q not found in PATH: %w", cmdParts[0], err)}
		}
	}
	cmdParts = append(cmdParts, path)
	c := exec.Command(cmdParts[0], cmdParts[1:]...)
	log.Printf("[editor] files openInEditor fallback: editor=%q file=%q full_cmd=%v", editor, path, cmdParts)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		log.Printf("[editor] files openInEditor fallback finished: editor=%q file=%q err=%v", editor, path, err)
		return editorFinishedMsg{err: err}
	})
}

// openInFileExplorer opens the given path (file or directory) in the system's
// native file explorer (Finder on macOS, Explorer on Windows, file manager on Linux).
func openInFileExplorer(path string) tea.Cmd {
	return func() tea.Msg {
		// If it's a file, open the parent directory so the file is revealed
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			path = filepath.Dir(path)
		}
		log.Printf("[explorer] opening folder: %q", path)
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", path)
		case "windows":
			cmd = exec.Command("explorer", path)
		default: // linux and others
			cmd = exec.Command("xdg-open", path)
		}
		if err := cmd.Start(); err != nil {
			log.Printf("[explorer] failed to open folder %q: %v", path, err)
		}
		return nil
	}
}

func openFileWithOSDefault(path string) tea.Cmd {
	return func() tea.Msg {
		log.Printf("[opener] opening file with system default app: %q", path)
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", path)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", path)
		default:
			cmd = exec.Command("xdg-open", path)
		}
		if err := cmd.Start(); err != nil {
			log.Printf("[opener] failed to open file %q: %v", path, err)
			return editorFinishedMsg{err: err}
		}
		return nil
	}
}

func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n <= 0 {
		return false
	}
	return bytes.IndexByte(buf[:n], 0) >= 0
}

func (m *filesModel) refreshGitStatus() {
	m.gitStatus = map[string]string{}
	if _, err := os.Stat(filepath.Join(m.workDir, ".git")); err != nil {
		return
	}
	out, err := exec.Command("git", "-C", m.workDir, "status", "--short").Output()
	if err != nil {
		m.statusMsg = "git status failed: " + err.Error()
		return
	}
	m.gitStatus = parseGitStatusShort(string(out))
}

func parseGitStatusShort(out string) map[string]string {
	status := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		code := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if idx := strings.LastIndex(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		badge := "M"
		if strings.Contains(code, "?") {
			badge = "?"
		} else if strings.Contains(code, "A") {
			badge = "A"
		} else if strings.Contains(code, "D") {
			badge = "D"
		} else if strings.Contains(code, "R") {
			badge = "R"
		}
		status[path] = badge
	}
	return status
}

// autoRefreshFilesGitStatusCmd returns a tea.Cmd that runs `git status --short`
// in a goroutine and delivers a filesGitStatusUpdateMsg with the parsed badges.
// This is the non-intrusive background refresh — it only touches the badge
// decorations, never the tree structure or cursor position.
func autoRefreshFilesGitStatusCmd(workDir string) tea.Cmd {
	return func() tea.Msg {
		if _, err := os.Stat(filepath.Join(workDir, ".git")); err != nil {
			return filesGitStatusUpdateMsg{gitStatus: map[string]string{}}
		}
		out, err := exec.Command("git", "-C", workDir, "status", "--short").Output()
		if err != nil {
			return filesGitStatusUpdateMsg{gitStatus: map[string]string{}}
		}
		return filesGitStatusUpdateMsg{gitStatus: parseGitStatusShort(string(out))}
	}
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".json":
		return "json"
	case ".md", ".markdown":
		return "markdown"
	case ".css":
		return "css"
	case ".html":
		return "html"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".py":
		return "python"
	case ".txt":
		return "text"
	default:
		return "text"
	}
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	if n < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
}

func (m *filesModel) applySelectionHighlight(startLine, startCol, endLine, endCol int) {
	if len(m.previewLines) == 0 {
		return
	}
	highlighted := applySelectionHighlight(m.previewLines, m.previewRawLines, startLine, startCol, endLine, endCol)
	m.preview.SetContent(strings.Join(highlighted, "\n"))
}

func (m *filesModel) clearSelectionHighlight() {
	if len(m.previewLines) == 0 {
		return
	}
	m.preview.SetContent(strings.Join(m.previewLines, "\n"))
}

func (m filesModel) extractSelectionText(startLine, startCol, endLine, endCol int) string {
	return extractSelectionText(m.previewRawLines, startLine, startCol, endLine, endCol)
}

func (m filesModel) previewContentVisible() bool {
	return m.panel == filesPanelPreview && len(m.previewRawLines) > 0
}

func (m filesModel) previewHeaderLines() int {
	n := 0
	if m.previewHeader() != "" {
		n++ // path | lang | size
	}
	if m.mode == filesModeNormal && m.previewEditable {
		n++ // editor hint
	}
	if m.mode == filesModePrompt || m.mode == filesModeDeleteConfirm {
		n = 1 // status line only, preview is replaced
	} else if m.choosingEditor {
		return 0 // editor picker replaces everything
	} else if m.mode == filesModeContentSearch {
		return 0 // content search replaces everything
	}
	if m.statusMsg != "" {
		n += 2 // status + blank
	} else if m.editor != "" {
		n += 2 // "editor: ..." + blank line
	}
	return n
}

func (m filesModel) previewHeader() string {
	if m.previewPath == "" {
		return ""
	}
	rel, err := filepath.Rel(m.workDir, m.previewPath)
	if err != nil {
		rel = m.previewPath
	}
	parts := []string{rel}
	if m.previewLang != "" {
		parts = append(parts, m.previewLang)
	}
	if m.previewSize > 0 {
		parts = append(parts, formatBytes(m.previewSize))
	}
	return strings.Join(parts, "  |  ")
}

func (m filesModel) View(w, h int, styles Styles, chatUnread, exitPending bool) string {
	treeW := w * 35 / 100
	previewW := w - treeW - 3

	// Build raw lines first to determine max width for horizontal scrolling
	rawLines := make([]string, 0, len(m.nodes))
	styledLines := make([]string, 0, len(m.nodes))
	// The tree pane is rendered with Width(treeW-2); in lipgloss v2 that width is
	// the full frame (border 2 + padding 2 are counted inside it), so the usable
	// content area is treeW-2-4 = treeW-6. A 1-char scrollbar is joined horizontally
	// with the content via JoinHorizontal, so subtract 1 more for the scrollbar.
	// Truncating rows to anything wider makes them wrap onto a second line.
	treeContentWidth := treeW - 7
	if treeContentWidth < 1 {
		treeContentWidth = 1
	}
	for i, n := range m.nodes {
		indent := strings.Repeat("  ", n.depth)
		icon := "  "
		if n.isDir {
			if n.expanded {
				icon = "\u25be "
			} else {
				icon = "\u25b8 "
			}
		}
		// Selection marker
		marker := "  "
		if m.selectedFiles != nil && m.selectedFiles[i] {
			marker = "✓ "
		}
		rawLine := marker + indent + icon + n.name
		if rel, err := filepath.Rel(m.workDir, n.path); err == nil {
			if badge := m.gitStatus[rel]; badge != "" {
				rawLine = badge + " " + rawLine
			}
		}
		if meta := formatFileNodeMeta(n); meta != "" {
			gap := treeContentWidth - visualLineWidth(rawLine) - visualLineWidth(meta)
			if gap < 1 {
				gap = 1
			}
			rawLine += strings.Repeat(" ", gap) + meta
		}
		rawLine = truncateToWidth(rawLine, treeContentWidth)
		styledLine := rawLine
		// Dim hidden files when showHidden is enabled
		if m.showHidden && strings.HasPrefix(n.name, ".") {
			styledLine = styles.Hint.Render(styledLine)
		}
		rawLines = append(rawLines, rawLine)
		styledLines = append(styledLines, styledLine)
	}

	// Calculate max content width for horizontal scrolling bounds
	maxWidth := 0
	for _, line := range rawLines {
		if w := visualLineWidth(line); w > maxWidth {
			maxWidth = w
		}
	}

	// Clamp treeScrollX to valid range
	availW := treeContentWidth // account for border + padding + marker
	if maxWidth <= availW {
		m.treeScrollX = 0
	} else if m.treeScrollX > maxWidth-availW {
		m.treeScrollX = maxWidth - availW
	}
	if m.treeScrollX < 0 {
		m.treeScrollX = 0
	}

	// Apply horizontal scroll offset and build final tree lines
	treeLines := make([]string, 0, len(m.nodes))
	for i, line := range styledLines {
		if m.treeScrollX > 0 {
			line = skipVisibleChars(line, m.treeScrollX)
		}
		if i == m.cursor {
			line = styles.Selected.Render(truncateToWidth(line, treeContentWidth))
		} else {
			line = truncateToWidth(line, treeContentWidth)
		}
		treeLines = append(treeLines, line)
	}

	// Get header rows (search hint, multi-select status, etc.)
	headerRows := m.treeHeaderRows(treeW, styles)
	headerRowCount := len(headerRows)

	// Calculate available height for tree lines (excluding header rows and pane frame)
	// h - 4 = pane height (h minus top header area)
	// - 2 = pane frame (top + bottom border)
	// - headerRowCount = header rows that will be prepended above the file list
	treeContentHeight := h - 4 - 2 - headerRowCount
	if treeContentHeight < 1 {
		treeContentHeight = 1
	}

	// Clamp treeScrollY to valid range
	maxScrollY := len(treeLines) - treeContentHeight
	if maxScrollY < 0 {
		maxScrollY = 0
	}
	if m.treeScrollY > maxScrollY {
		m.treeScrollY = maxScrollY
	}
	if m.treeScrollY < 0 {
		m.treeScrollY = 0
	}

	// Keep cursor visible in viewport
	if m.cursor < m.treeScrollY {
		m.treeScrollY = m.cursor
	}
	if m.cursor >= m.treeScrollY+treeContentHeight {
		m.treeScrollY = m.cursor - treeContentHeight + 1
	}
	if m.treeScrollY < 0 {
		m.treeScrollY = 0
	}
	if len(treeLines) > 0 && m.treeScrollY > len(treeLines)-1 {
		m.treeScrollY = len(treeLines) - 1
	}

	// Slice visible lines
	visibleStart := m.treeScrollY
	visibleEnd := m.treeScrollY + treeContentHeight
	if visibleEnd > len(treeLines) {
		visibleEnd = len(treeLines)
	}
	visibleLines := treeLines[visibleStart:visibleEnd]

	// Pad with empty lines if needed to fill viewport
	for len(visibleLines) < treeContentHeight {
		visibleLines = append(visibleLines, "")
	}

	treeContent := strings.Join(visibleLines, "\n")
	// Prepend hint rows. headerRows was calculated earlier (before height calculation)
	// to ensure rendered row count and click hit-box offset stay in lockstep.
	if headerRowCount > 0 {
		treeContent = strings.Join(headerRows, "\n") + "\n" + treeContent
	}

	focusBorder := func(focused bool) lipgloss.Style {
		if focused {
			return borderStyle.BorderForeground(selectedStyle.GetBackground())
		}
		return borderStyle
	}

	if m.mode == filesModeFuzzy {
		treeContent = m.fuzzyPopupView(treeW-2, h-4, styles)
	}
	// Render scrollbar for tree pane
	// Scrollbar container height = headers + file list (full pane height)
	actualContentHeight := headerRowCount + treeContentHeight
	treeSB := renderScrollbar(actualContentHeight, len(treeLines), treeContentHeight, m.treeScrollY)
	// Join tree content with scrollbar
	treeContentFull := lipgloss.JoinHorizontal(lipgloss.Top, treeContent, treeSB)
	treePane := focusBorder(m.panel == filesPanelPicker).Width(treeW - 2).Height(h - 4).Render(treeContentFull)

	previewSB := renderScrollbar(m.preview.Height(), m.preview.TotalLineCount(), m.preview.VisibleLineCount(), m.preview.YOffset())
	if m.inFileSearchActive {
		m.preview.HighlightStyle = lipgloss.NewStyle().Background(styles.Selected.GetBackground()).Foreground(styles.Selected.GetForeground())
		m.preview.SelectedHighlightStyle = lipgloss.NewStyle().Background(lipgloss.Color("220")).Foreground(lipgloss.Color("0")).Bold(true)
	}
	previewBody := m.preview.View()
	if m.mode == filesModeEdit {
		previewBody = m.inlineEditor.view(previewW-7, h-5)
	}
	previewContent := lipgloss.JoinHorizontal(lipgloss.Top, previewBody, previewSB)
	contentWidth := previewW - 4
	if contentWidth < 1 {
		contentWidth = 1
	}
	if header := m.previewHeader(); header != "" {
		previewContent = lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Hint.Render(header)) + "\n" + previewContent
	}
	if m.mode == filesModeNormal && m.previewEditable {
		hint := "tab jump  i vim edit  e external  E choose editor  a add to context  / search  /editor set default"
		if isTmuxMode(m.editorMode) {
			hint = "tab jump  i vim edit  e " + m.tmuxOpenHint() + "  E choose editor  a add to context  / search  /editor set default"
		}
		previewContent = lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Hint.Render(hint)) + "\n" + previewContent
	}
	if m.mode == filesModeEdit {
		previewContent = lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Hint.Render("vim edit: i/a insert  esc normal  :w save  :q quit  :q! discard  :wq save+quit")) + "\n" + previewContent
	}
	if m.choosingEditor {
		previewContent = m.editorPickerView(previewW-4, styles)
	} else if m.mode == filesModePrompt {
		promptContent := lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Hint.Render(m.statusMsg))
		if m.promptConfirm {
			promptContent = lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Error.Render("⚠ "+m.statusMsg)) + "\n" + lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Hint.Render("press enter again to confirm, esc to cancel"))
		}
		previewContent = promptContent + "\n" + m.promptInput.View()
	} else if m.mode == filesModeDeleteConfirm {
		deleteLines := []string{styles.Error.Render(m.statusMsg), ""}
		if len(m.deleteTargets) > 0 {
			for _, t := range m.deleteTargets {
				name := filepath.Base(t)
				if info, err := os.Stat(t); err == nil && info.IsDir() {
					name += "/"
				}
				deleteLines = append(deleteLines, styles.Error.Render("  "+name))
			}
		} else {
			name := filepath.Base(m.promptTarget)
			if info, err := os.Stat(m.promptTarget); err == nil && info.IsDir() {
				name += "/"
			}
			deleteLines = append(deleteLines, styles.Error.Render("  "+name))
		}
		deleteLines = append(deleteLines, "", lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Hint.Render("press y to confirm, esc/n to cancel")))
		previewContent = strings.Join(deleteLines, "\n")
	} else if m.mode == filesModeContentSearch {
		previewContent = m.contentView(previewW-4, h, styles)
	} else if m.statusMsg != "" {
		previewContent = lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Hint.Render(m.statusMsg)) + "\n\n" + previewContent
	} else if m.editor != "" {
		previewContent = lipgloss.NewStyle().Width(contentWidth).MaxHeight(1).Render(styles.Hint.Render("editor: "+m.editor+"  (E to change)")) + "\n\n" + previewContent
	}
	previewPane := focusBorder(m.panel == filesPanelPreview).Width(previewW - 2).Render(previewContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, treePane, previewPane)

	tabBar := renderTabBar(tabFiles, chatUnread)
	var exitBtn string
	if exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("\u2715 exit?")
	} else {
		exitBtn = styles.Hint.Padding(0, 1).Render("\u2715 exit")
	}
	headerLeft := appHeaderLeftPad + styles.Header.Render("\u25c6 ocode  Files") + appHeaderHintGap + hintStyle.Render("opencode clone")
	headerPad := w - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar) - lipgloss.Width(exitBtn)
	if headerPad < 0 {
		headerPad = 0
	}
	// Top pad + left gap + thin title/hint gap, matching the chat tab header.
	renderedHeader := appHeaderTopPad + headerLeft + strings.Repeat(" ", headerPad) + tabBar + exitBtn

	// Bottom status bar with keybindings (matching renderStatus in model.go)
	statusStr := hintStyle.Width(w - 2).MaxHeight(1).Render(
		"ctrl+f search  ctrl+g fuzzy find  space select  ctrl+h hidden  tab jump  ctrl+l edit  ctrl+o open  ctrl+n new file  ctrl+b new folder  ctrl+r rename  ctrl+d delete  ctrl+y path  ctrl+e editor",
	)
	parts := []string{renderedHeader, row, statusStr}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m filesModel) editorPickerView(width int, styles Styles) string {
	choices := m.editorChoices()
	lines := []string{
		styles.Header.Render("Choose editor"),
		styles.Hint.Render("j/k move  enter select+open  esc cancel"),
		"",
	}
	for i, choice := range choices {
		line := "  " + choice
		if i == m.editorCursor {
			line = styles.Selected.Width(width).Render("> " + choice)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// fuzzyPopupView renders the fuzzy search popup that replaces the tree pane.
func (m filesModel) fuzzyPopupView(width, height int, styles Styles) string {
	lines := []string{}

	// Input line with cursor
	inputLine := styles.Selected.Render("\U0001f50d " + m.fuzzyQuery + "\u2588")
	lines = append(lines, inputLine)
	lines = append(lines, strings.Repeat("\u2500", width))

	// Results list
	maxResults := height - 4 // room for input, separator, hint
	if maxResults < 1 {
		maxResults = 1
	}

	// Ensure results are up to date
	results := m.fuzzyResults
	if results == nil {
		results = fuzzyFilter(m.allPaths, m.fuzzyQuery)
	}

	if len(results) == 0 {
		if m.fuzzyQuery != "" {
			lines = append(lines, styles.Hint.Render("  no matches"))
		} else {
			lines = append(lines, styles.Hint.Render("  type to search..."))
		}
	} else {
		// Show results, scrolling if needed
		start := 0
		if m.fuzzyCursor >= maxResults {
			start = m.fuzzyCursor - maxResults + 1
		}
		end := start + maxResults
		if end > len(results) {
			end = len(results)
		}

		for i := start; i < end; i++ {
			path := results[i]
			line := "  " + path
			if i == m.fuzzyCursor {
				line = styles.Selected.Width(width).Render("> " + path)
			}
			lines = append(lines, line)
		}
	}

	// Hint line
	totalResults := len(results)
	hint := fmt.Sprintf("%d results  \u2191\u2193 navigate  enter select  esc cancel", totalResults)
	lines = append(lines, strings.Repeat("\u2500", width))
	lines = append(lines, styles.Hint.Render(hint))

	return strings.Join(lines, "\n")
}

func (m filesModel) addToContextCmd() tea.Cmd {
	return func() tea.Msg {
		rel, err := filepath.Rel(m.workDir, m.previewPath)
		if err != nil {
			rel = m.previewPath
		}
		return filesAddToContextMsg{
			path:      rel,
			content:   m.previewRaw,
			startLine: 0,
			endLine:   len(m.previewRawLines),
		}
	}
}

func isTmuxMode(mode string) bool {
	return mode == config.EditorModeTmuxSplit || mode == config.EditorModeTmuxWindow
}

func (m filesModel) tmuxOpenHint() string {
	switch m.editorMode {
	case config.EditorModeTmuxSplit:
		return "tmux split: " + m.editor
	case config.EditorModeTmuxWindow:
		return "tmux window: " + m.editor
	default:
		return "editor: " + m.editor
	}
}

// treeHint returns the keybinding hints shown at the top of the file tree
// panel when in normal mode (no multi-select active). It always renders as
// exactly 2 lines so that click hit-box math is stable across screen widths.
func (m filesModel) treeHint() string {
	if len(m.selectedFiles) > 0 {
		return ""
	}
	line1 := "j/k navigate  enter open  space select  shift+↑↓ extend  n new  N folder"
	line2 := "r rename  D del  / search  ctrl+f grep  o reveal  ←→ scroll"
	return line1 + "\n" + line2
}

// treeHeaderRows returns the hint rows rendered directly above the file list.
// Each row is clamped with MaxHeight(1) so it occupies exactly one visual line
// regardless of screen width — this is what makes the layout deterministic and
// keeps View() (which prepends these rows) and treeNodeForClick() (whose offset
// is len(rows)) from ever disagreeing. It is the single source of truth for the
// tree's top chrome.
func (m filesModel) treeHeaderRows(treeW int, styles Styles) []string {
	cw := treeW - 7 // pane content width: frame(treeW-2) minus border(2)+padding(2)+scrollbar(1)
	if cw < 1 {
		cw = 1
	}
	clamp := func(s string) string { return styles.Hint.Width(cw).MaxHeight(1).Render(s) }
	if hint := m.selectionHint(); hint != "" {
		return []string{clamp(hint)}
	}
	if m.panel == filesPanelPicker && m.mode == filesModeNormal && len(m.selectedFiles) == 0 {
		rows := strings.Split(m.treeHint(), "\n")
		for i := range rows {
			rows[i] = clamp(rows[i])
		}
		return rows
	}
	return nil
}

// skipVisibleChars skips the first n visible characters in a string that may
// contain ANSI escape sequences. It preserves all escape sequences intact.
func skipVisibleChars(s string, n int) string {
	if n <= 0 {
		return s
	}
	visible := 0
	i := 0
	for i < len(s) && visible < n {
		if loc := ansiEscapeIdx(s[i:]); loc[0] == 0 {
			i += loc[1]
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		if r == '\t' {
			w = tabWidth - (visible % tabWidth)
		}
		if w <= 0 {
			i += size
			continue
		}
		if visible+w > n {
			break
		}
		visible += w
		i += size
	}
	return s[i:]
}
