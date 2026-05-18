package tui

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type filesPreviewMsg struct{ content string }

type fileNode struct {
	path     string
	name     string
	isDir    bool
	depth    int
	expanded bool
	loaded   bool
}

type filesModel struct {
	workDir        string
	nodes          []fileNode
	cursor         int
	preview        viewport.Model
	fuzzy          bool
	query          string
	allPaths       []string
	width          int
	height         int
	editor         string
	saveEditor     func(string) error
	choosingEditor bool
	editorCursor   int
	editorTarget   string
	statusMsg      string
}

func newFilesModel(workDir string) filesModel {
	m := filesModel{workDir: workDir}
	m.preview = viewport.New()
	m.nodes = loadDirChildren(workDir, 0)
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
}

func (m filesModel) Update(msg tea.Msg, w, h int) (filesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case filesPreviewMsg:
		m.preview.SetContent(msg.content)
		m.preview.GotoTop()
		return m, nil
	case tea.KeyPressMsg:
		if m.choosingEditor {
			return m.updateEditorPicker(msg)
		}
		if m.fuzzy {
			return m.updateFuzzy(msg)
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
	case "/":
		m.fuzzy = true
		m.query = ""
		m.buildAllPaths()
	}
	return m, nil
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
			return filesPreviewMsg{content: "[directory]"}
		}
		f, err := os.Open(n.path)
		if err != nil {
			return filesPreviewMsg{content: "[cannot read file]"}
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
			return filesPreviewMsg{content: "[binary file]"}
		}

		content := string(data)
		if nr > 1024*1024 {
			content = string(data[:1024*1024]) + "\n[truncated — 1MB limit]"
		}
		return filesPreviewMsg{content: content}
	}
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

func (m filesModel) openInEditor(path string) tea.Cmd {
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

func (m filesModel) View(w, h int, styles Styles, chatUnread bool) string {
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
		if i == m.cursor {
			line = lipgloss.NewStyle().Reverse(true).Width(treeW - 2).Render(line)
		}
		treeLines = append(treeLines, line)
	}
	treeContent := strings.Join(treeLines, "\n")
	treePane := borderStyle.Width(treeW - 2).Height(h - 3).Render(treeContent)

	previewSB := renderScrollbar(m.preview.Height(), m.preview.TotalLineCount(), m.preview.VisibleLineCount(), m.preview.YOffset())
	previewContent := lipgloss.JoinHorizontal(lipgloss.Top, m.preview.View(), previewSB)
	if m.choosingEditor {
		previewContent = m.editorPickerView(previewW-4, styles)
	} else if m.statusMsg != "" {
		previewContent = hintStyle.Render(m.statusMsg) + "\n\n" + previewContent
	} else if m.editor != "" {
		previewContent = hintStyle.Render("editor: "+m.editor+"  (E to change)") + "\n\n" + previewContent
	}
	previewPane := borderStyle.Width(previewW - 2).Render(previewContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, treePane, previewPane)

	tabBar := renderTabBar(tabFiles, chatUnread)
	headerLeft := styles.Header.Render("\u25c6 ocode  Files")
	headerPad := w - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar)
	if headerPad < 0 {
		headerPad = 0
	}
	header := headerLeft + strings.Repeat(" ", headerPad) + tabBar

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
			line = lipgloss.NewStyle().Reverse(true).Width(width).Render("> " + choice)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
