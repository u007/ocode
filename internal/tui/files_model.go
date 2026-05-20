package tui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/quick"
	"github.com/atotto/clipboard"
	"github.com/jamesmercstudio/ocode/internal/config"
)

type filesPreviewMsg struct {
	path     string
	content  string
	raw      string
	size     int64
	language string
	editable bool
}

type filesAddToContextMsg struct {
	path      string
	content   string
	startLine int
	endLine   int
}

type filesMode int

const (
	filesModeNormal filesMode = iota
	filesModePrompt
	filesModeDeleteConfirm
	filesModeEdit
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

type fileNode struct {
	path     string
	name     string
	isDir    bool
	depth    int
	expanded bool
	loaded   bool
}

type filesModel struct {
	workDir         string
	nodes           []fileNode
	cursor          int
	preview         viewport.Model
	fuzzy           bool
	query           string
	allPaths        []string
	width           int
	height          int
	editor          string
	saveEditor      func(string) error
	choosingEditor  bool
	editorCursor    int
	editorTarget    string
	statusMsg       string
	mode            filesMode
	promptInput     textarea.Model
	promptKind      filesPromptKind
	promptTarget    string
	previewPath     string
	previewSize     int64
	previewLang     string
	previewEditable bool
	gitStatus       map[string]string
	editorOpener    func(string) tea.Cmd
	editorMode      string
	panel           filesPanel
	previewRaw      string
	previewRawLines []string
	previewLines    []string
	inlineEditor    inlineFileEditor
	inlineEditPath  string
	inlineEditMtime int64
	inlineEditSize  int64
}

func newFilesModel(workDir string) filesModel {
	m := filesModel{workDir: workDir}
	m.preview = viewport.New()
	m.promptInput = textarea.New()
	m.nodes = loadDirChildren(workDir, 0)
	m.refreshGitStatus()
	return m
}

func loadDirChildren(dir string, depth int) []fileNode {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	nodes := make([]fileNode, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		nodes = append(nodes, fileNode{
			path:  filepath.Join(dir, name),
			name:  name,
			isDir: e.IsDir(),
			depth: depth,
		})
	}
	return nodes
}

func (m *filesModel) SetEditor(e string) { m.editor = e }

func (m *filesModel) SetSaveEditor(fn func(string) error) { m.saveEditor = fn }

func (m *filesModel) SetEditorOpener(fn func(string) tea.Cmd) { m.editorOpener = fn }

func (m *filesModel) SetEditorMode(mode string) { m.editorMode = mode }

func (m *filesModel) Resize(w, h int) {
	m.width = w
	m.height = h
	treeW := w * 35 / 100
	previewW := w - treeW - 3
	previewH := h - 3
	if previewH < 1 {
		previewH = 1
	}
	m.preview.SetWidth(previewW - 7)
	m.preview.SetHeight(previewH)
	m.promptInput.SetWidth(previewW - 7)
	m.promptInput.SetHeight(1)
}

func (m filesModel) Update(msg tea.Msg, w, h int) (filesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case filesPreviewMsg:
		m.applyPreview(msg)
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
		if m.fuzzy {
			return m.updateFuzzy(msg)
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
	case "j", "down":
		if m.cursor < len(m.nodes)-1 {
			m.cursor++
			if m.cursor < len(m.nodes) {
				return m, loadPreviewCmd(m.nodes[m.cursor])
			}
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < len(m.nodes) {
				return m, loadPreviewCmd(m.nodes[m.cursor])
			}
		}
	case "enter", "ctrl+j", "ctrl+m", "space":
		if m.cursor < len(m.nodes) {
			n := &m.nodes[m.cursor]
			if n.isDir {
				m.toggleDir(m.cursor)
			} else {
				return m, m.openInEditor(n.path)
			}
		}
	case "e":
		if m.cursor < len(m.nodes) && !m.nodes[m.cursor].isDir {
			return m, m.openInEditor(m.nodes[m.cursor].path)
		}
	case "E", "shift+e":
		if m.cursor < len(m.nodes) && !m.nodes[m.cursor].isDir {
			m.openEditorPicker(m.nodes[m.cursor].path)
		}
	case "n":
		m.startCreateFile()
	case "N", "shift+n":
		m.startCreateDir()
	case "r":
		m.startRename()
	case "D", "shift+d":
		m.startDelete()
	case "i":
		return m.startInlineEdit()
	case "y":
		m.copySelectedPath()
	case "R", "shift+r":
		return m, m.refreshPreviewCmd()
	case "/":
		m.fuzzy = true
		m.query = ""
		m.buildAllPaths()
	case "tab":
		m.panel = (m.panel + 1) % 2
	}
	return m, nil
}

func (m filesModel) updatePreview(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.preview.ScrollDown(1)
	case "k", "up":
		m.preview.ScrollUp(1)
	case "tab":
		m.panel = (m.panel + 1) % 2
	case "e":
		if m.cursor < len(m.nodes) && !m.nodes[m.cursor].isDir {
			return m, m.openInEditor(m.nodes[m.cursor].path)
		}
	case "i":
		return m.startInlineEdit()
	}
	return m, nil
}

func (m filesModel) updatePrompt(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = filesModeNormal
		m.statusMsg = "action cancelled"
		return m, nil
	case "enter", "ctrl+j", "ctrl+m":
		return m.submitPrompt()
	}
	var cmd tea.Cmd
	m.promptInput, cmd = m.promptInput.Update(msg)
	return m, cmd
}

func (m filesModel) updateDeleteConfirm(msg tea.KeyPressMsg) (filesModel, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "shift+y":
		path := m.promptTarget
		if err := os.Remove(path); err != nil {
			m.statusMsg = "delete failed: " + err.Error()
			m.mode = filesModeNormal
			return m, nil
		}
		m.mode = filesModeNormal
		m.statusMsg = "deleted: " + filepath.Base(path)
		m.rebuildTreeKeeping(filepath.Dir(path))
		return m, m.refreshPreviewCmd()
	case "n", "N", "esc":
		m.mode = filesModeNormal
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
		m.editor = choice
		m.choosingEditor = false
		if m.saveEditor != nil {
			if err := m.saveEditor(choice); err != nil {
				m.statusMsg = "editor save failed: " + err.Error()
				return m, nil
			}
		}
		m.statusMsg = "editor: " + choice
		return m, m.openInEditor(m.editorTarget)
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
		m.fuzzy = false
		m.query = ""
	case "enter":
		results := fuzzyFilter(m.allPaths, m.query)
		if len(results) > 0 {
			m.navigateTo(results[0])
		}
		m.fuzzy = false
		m.query = ""
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
		}
	default:
		if len(msg.Text) > 0 {
			m.query += msg.Text
		}
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
		children := loadDirChildren(n.path, n.depth+1)
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
		if n.isDir {
			return filesPreviewMsg{path: n.path, content: "[directory]", language: "directory"}
		}
		info, statErr := os.Stat(n.path)
		f, err := os.Open(n.path)
		if err != nil {
			return filesPreviewMsg{path: n.path, content: "[cannot read file]", language: languageForPath(n.path)}
		}
		defer f.Close()

		buf := make([]byte, 1024*1024+1)
		nr, _ := f.Read(buf)
		data := buf[:nr]

		probe := data
		if len(probe) > 512 {
			probe = probe[:512]
		}
		if bytes.IndexByte(probe, 0) >= 0 {
			return filesPreviewMsg{path: n.path, content: "[binary file]", size: fileSize(info, statErr), language: languageForPath(n.path)}
		}

		content := string(data)
		editable := nr <= 1024*1024
		if nr > 1024*1024 {
			content = string(data[:1024*1024]) + "\n[truncated — 1MB limit]"
		}
		language := languageForPath(n.path)
		return filesPreviewMsg{path: n.path, content: highlightContent(content, language), raw: content, size: fileSize(info, statErr), language: language, editable: editable}
	}
}

func fileSize(info os.FileInfo, err error) int64 {
	if err != nil || info == nil {
		return 0
	}
	return info.Size()
}

func (m *filesModel) applyPreview(msg filesPreviewMsg) {
	m.previewPath = msg.path
	m.previewSize = msg.size
	m.previewLang = msg.language
	m.previewEditable = msg.editable
	m.preview.SetContent(msg.content)
	m.previewRaw = msg.raw
	m.previewRawLines = strings.Split(msg.raw, "\n")
	m.previewLines = strings.Split(msg.content, "\n")
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
	m.nodes = loadDirChildren(m.workDir, 0)
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
						if result, ok := loadPreviewCmd(m.nodes[idx])().(filesPreviewMsg); ok {
							m.preview.SetContent(result.content)
							m.preview.GotoTop()
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

func (m filesModel) treeNodeForClick(mouse tea.Mouse, headerHeight int) (int, bool) {
	treeW := m.width * 35 / 100
	if mouse.X >= treeW {
		return 0, false
	}
	// Tree content starts after header + 1 (border top line)
	treeContentTop := headerHeight + 1
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
	m.nodes = loadDirChildren(m.workDir, 0)
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

func (m filesModel) refreshPreviewCmd() tea.Cmd {
	if m.cursor >= len(m.nodes) {
		return nil
	}
	return loadPreviewCmd(m.nodes[m.cursor])
}

func (m filesModel) selectedNode() (fileNode, bool) {
	if m.cursor < 0 || m.cursor >= len(m.nodes) {
		return fileNode{}, false
	}
	return m.nodes[m.cursor], true
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
	m.statusMsg = "new file name"
}

func (m *filesModel) startCreateDir() {
	m.mode = filesModePrompt
	m.promptKind = filesPromptCreateDir
	m.promptTarget = m.selectedActionDir()
	m.promptInput.SetValue("")
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
	m.statusMsg = "rename"
}

func (m *filesModel) startDelete() {
	n, ok := m.selectedNode()
	if !ok {
		return
	}
	m.mode = filesModeDeleteConfirm
	m.promptTarget = n.path
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
	if m.editorOpener != nil {
		return m.editorOpener(path)
	}
	editor := m.editor
	if editor == "" {
		editor = "vi"
	}
	cmdParts := strings.Fields(editor)
	if len(cmdParts) == 0 {
		return func() tea.Msg { return editorFinishedMsg{err: os.ErrInvalid} }
	}
	cmdParts = append(cmdParts, path)
	c := exec.Command(cmdParts[0], cmdParts[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
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
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
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

	treeLines := make([]string, 0, len(m.nodes))
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
		line := indent + icon + n.name
		if rel, err := filepath.Rel(m.workDir, n.path); err == nil {
			if badge := m.gitStatus[rel]; badge != "" {
				line = badge + " " + line
			}
		}
		if i == m.cursor {
			line = selectedStyle.Width(treeW - 2).Render(line)
		}
		treeLines = append(treeLines, line)
	}
	treeContent := strings.Join(treeLines, "\n")

	focusBorder := func(focused bool) lipgloss.Style {
		if focused {
			return borderStyle.BorderForeground(selectedStyle.GetBackground())
		}
		return borderStyle
	}

	treePane := focusBorder(m.panel == filesPanelPicker).Width(treeW - 2).Height(h - 3).Render(treeContent)

	previewSB := renderScrollbar(m.preview.Height(), m.preview.TotalLineCount(), m.preview.VisibleLineCount(), m.preview.YOffset())
	previewBody := m.preview.View()
	if m.mode == filesModeEdit {
		previewBody = m.inlineEditor.view(previewW-7, h-5)
	}
	previewContent := lipgloss.JoinHorizontal(lipgloss.Top, previewBody, previewSB)
	if header := m.previewHeader(); header != "" {
		previewContent = hintStyle.Render(header) + "\n" + previewContent
	}
	if m.mode == filesModeNormal && m.previewEditable {
		hint := "tab jump  i vim edit  e external  E choose editor  a add to context  /editor set default"
		if isTmuxMode(m.editorMode) {
			hint = "tab jump  i vim edit  e " + m.tmuxOpenHint() + "  E choose editor  a add to context  /editor set default"
		}
		previewContent = hintStyle.Render(hint) + "\n" + previewContent
	}
	if m.mode == filesModeEdit {
		previewContent = hintStyle.Render("vim edit: i/a insert  esc normal  :w save  :q quit  :q! discard  :wq save+quit") + "\n" + previewContent
	}
	if m.choosingEditor {
		previewContent = m.editorPickerView(previewW-4, styles)
	} else if m.mode == filesModePrompt {
		previewContent = hintStyle.Render(m.statusMsg) + "\n" + m.promptInput.View()
	} else if m.mode == filesModeDeleteConfirm {
		previewContent = hintStyle.Render(m.statusMsg) + "\n" + hintStyle.Render("press y to confirm, esc/n to cancel")
	} else if m.statusMsg != "" {
		previewContent = hintStyle.Render(m.statusMsg) + "\n\n" + previewContent
	} else if m.editor != "" {
		previewContent = hintStyle.Render("editor: "+m.editor+"  (E to change)") + "\n\n" + previewContent
	}
	previewPane := focusBorder(m.panel == filesPanelPreview).Width(previewW - 2).Render(previewContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, treePane, previewPane)

	tabBar := renderTabBar(tabFiles, chatUnread)
	var exitBtn string
	if exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("\u2715 exit?")
	} else {
		exitBtn = hintStyle.Padding(0, 1).Render("\u2715 exit")
	}
	headerLeft := styles.Header.Render("\u25c6 ocode  Files")
	headerPad := w - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar) - lipgloss.Width(exitBtn)
	if headerPad < 0 {
		headerPad = 0
	}
	header := headerLeft + strings.Repeat(" ", headerPad) + tabBar + exitBtn

	fuzzyBar := ""
	if m.fuzzy {
		results := fuzzyFilter(m.allPaths, m.query)
		preview := ""
		if len(results) > 3 {
			results = results[:3]
		}
		preview = strings.Join(results, "  ")
		fuzzyBar = hintStyle.Render("/ " + m.query + "  " + preview)
	}

	parts := []string{header, row}
	if fuzzyBar != "" {
		parts = append(parts, fuzzyBar)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m filesModel) editorPickerView(width int, styles Styles) string {
	choices := m.editorChoices()
	lines := []string{
		styles.Header.Render("Choose editor"),
		hintStyle.Render("j/k move  enter select+open  esc cancel"),
		"",
	}
	for i, choice := range choices {
		line := "  " + choice
		if i == m.editorCursor {
			line = selectedStyle.Width(width).Render("> " + choice)
		}
		lines = append(lines, line)
	}
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
