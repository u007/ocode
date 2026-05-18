package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type gitSection int

const (
	gitSectionChanges gitSection = iota
	gitSectionLog
	gitSectionStash
	gitSectionBranches
)

type gitPanel int

const (
	gitPanelSections gitPanel = iota
	gitPanelFiles
	gitPanelDiff
)

type gitFile struct {
	status string
	path   string
	staged bool
}

type gitCommit struct {
	hash    string
	subject string
	author  string
	age     string
}

type gitModel struct {
	workDir        string
	width          int
	height         int
	section        gitSection
	panel          gitPanel
	stagedFiles    []gitFile
	unstagedFiles  []gitFile
	untrackedFiles []gitFile
	filesCursor    int
	commits        []gitCommit
	commitCursor   int
	stashes        []string
	stashCursor    int
	branches       []string
	branchCursor   int
	diff           viewport.Model
	committing     bool
	commitInput    textarea.Model
	statusMsg      string
	pendingAction  string // "discard" | "drop-stash" | ""
}

func newGitModel(workDir string) gitModel {
	m := gitModel{workDir: workDir}
	m.diff = viewport.New()
	ci := textarea.New()
	ci.Placeholder = "Commit message..."
	ci.SetHeight(3)
	m.commitInput = ci
	m.refresh()
	return m
}

func (m *gitModel) Resize(w, h int) {
	m.width = w
	m.height = h
	sectW := w * 20 / 100
	filesW := w * 30 / 100
	diffW := w - sectW - filesW - 4
	diffH := h - 5
	if diffH < 1 {
		diffH = 1
	}
	m.diff.SetWidth(diffW - 7)
	m.diff.SetHeight(diffH)
	m.commitInput.SetWidth(sectW + filesW)
}

func (m *gitModel) refresh() {
	m.loadChanges()
	m.loadLog()
	m.loadStash()
	m.loadBranches()
}

func (m *gitModel) gitRun(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = m.workDir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (m *gitModel) loadChanges() {
	out, err := m.gitRun("status", "--porcelain")
	if err != nil {
		return
	}
	m.stagedFiles = nil
	m.unstagedFiles = nil
	m.untrackedFiles = nil
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		x, y, path := string(line[0]), string(line[1]), strings.TrimSpace(line[2:])
		switch {
		case x == "?" && y == "?":
			m.untrackedFiles = append(m.untrackedFiles, gitFile{status: "?", path: path})
		default:
			if x != " " && x != "?" {
				m.stagedFiles = append(m.stagedFiles, gitFile{status: x, path: path, staged: true})
			}
			if y != " " && y != "?" {
				m.unstagedFiles = append(m.unstagedFiles, gitFile{status: y, path: path})
			}
		}
	}
}

func (m *gitModel) loadLog() {
	out, err := m.gitRun("log", "--oneline", "--format=%h\t%s\t%an\t%cr", "-50")
	if err != nil {
		return
	}
	m.commits = nil
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) == 4 {
			m.commits = append(m.commits, gitCommit{
				hash:    parts[0],
				subject: parts[1],
				author:  parts[2],
				age:     parts[3],
			})
		}
	}
}

func (m *gitModel) loadStash() {
	out, err := m.gitRun("stash", "list")
	if err != nil {
		m.statusMsg = "stash list: " + err.Error()
		m.stashes = nil
		return
	}
	if out == "" {
		m.stashes = nil
		return
	}
	m.stashes = strings.Split(out, "\n")
}

func (m *gitModel) loadBranches() {
	out, err := m.gitRun("branch", "-a")
	if err != nil {
		m.statusMsg = "branch list: " + err.Error()
		m.branches = nil
		return
	}
	m.branches = nil
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		m.branches = append(m.branches, strings.TrimSpace(strings.TrimPrefix(line, "*")))
	}
}

func (m gitModel) Update(msg tea.Msg, w, h int) (gitModel, tea.Cmd) {
	if m.committing {
		return m.updateCommitInput(msg)
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg, w, h)
	}
	return m, nil
}

func (m gitModel) updateCommitInput(msg tea.Msg) (gitModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.committing = false
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.commitInput.Value())
			if text != "" {
				if _, err := m.gitRun("commit", "-m", text); err != nil {
					m.statusMsg = "commit failed: " + err.Error()
				} else {
					m.statusMsg = "committed"
					m.committing = false
					m.commitInput.Reset()
					m.refresh()
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	return m, cmd
}

func (m gitModel) handleKey(msg tea.KeyPressMsg, w, h int) (gitModel, tea.Cmd) {
	key := msg.String()
	switch key {
	case "tab":
		m.panel = (m.panel + 1) % 3
		return m, nil
	}

	switch m.panel {
	case gitPanelSections:
		return m.handleSectionKey(key)
	case gitPanelFiles:
		return m.handleFilesKey(key)
	case gitPanelDiff:
		return m.handleDiffKey(key)
	}
	return m, nil
}

func (m gitModel) handleSectionKey(key string) (gitModel, tea.Cmd) {
	sections := []gitSection{gitSectionChanges, gitSectionLog, gitSectionStash, gitSectionBranches}
	cur := int(m.section)
	switch key {
	case "j", "down":
		if cur < len(sections)-1 {
			m.section = sections[cur+1]
			m.filesCursor = 0
			m.loadDiff()
		}
	case "k", "up":
		if cur > 0 {
			m.section = sections[cur-1]
			m.filesCursor = 0
			m.loadDiff()
		}
	case "enter":
		m.panel = gitPanelFiles
	}
	return m, nil
}

func (m gitModel) handleFilesKey(key string) (gitModel, tea.Cmd) {
	// Clear pending confirmation when user presses any key other than the confirm key
	if key != "d" {
		m.pendingAction = ""
	}
	switch key {
	case "j", "down":
		switch m.section {
		case gitSectionChanges:
			files := m.currentFileList()
			if m.filesCursor < len(files)-1 {
				m.filesCursor++
				m.loadDiff()
			}
		case gitSectionLog:
			if m.commitCursor < len(m.commits)-1 {
				m.commitCursor++
				m.loadDiff()
			}
		case gitSectionStash:
			if m.stashCursor < len(m.stashes)-1 {
				m.stashCursor++
				m.loadDiff()
			}
		case gitSectionBranches:
			if m.branchCursor < len(m.branches)-1 {
				m.branchCursor++
				m.loadDiff()
			}
		}
	case "k", "up":
		switch m.section {
		case gitSectionChanges:
			if m.filesCursor > 0 {
				m.filesCursor--
				m.loadDiff()
			}
		case gitSectionLog:
			if m.commitCursor > 0 {
				m.commitCursor--
				m.loadDiff()
			}
		case gitSectionStash:
			if m.stashCursor > 0 {
				m.stashCursor--
				m.loadDiff()
			}
		case gitSectionBranches:
			if m.branchCursor > 0 {
				m.branchCursor--
				m.loadDiff()
			}
		}
	case "s":
		if m.section == gitSectionChanges && m.filesCursor >= len(m.stagedFiles) {
			idx := m.filesCursor - len(m.stagedFiles)
			unstaged := m.allUnstagedAndUntracked()
			if idx < len(unstaged) {
				f := unstaged[idx]
				if _, err := m.gitRun("add", f.path); err != nil {
					m.statusMsg = "stage failed: " + err.Error()
				} else {
					m.statusMsg = "staged " + f.path
					m.refresh()
					m.loadDiff()
				}
			}
		}
	case "u":
		if m.section == gitSectionChanges && m.filesCursor < len(m.stagedFiles) {
			f := m.stagedFiles[m.filesCursor]
			if _, err := m.gitRun("restore", "--staged", f.path); err != nil {
				m.statusMsg = "unstage failed: " + err.Error()
			} else {
				m.statusMsg = "unstaged " + f.path
				m.refresh()
				m.loadDiff()
			}
		}
	case "d":
		if m.section == gitSectionChanges {
			if m.filesCursor >= len(m.stagedFiles) {
				idx := m.filesCursor - len(m.stagedFiles)
				unstaged := m.allUnstagedAndUntracked()
				if idx < len(unstaged) {
					if m.pendingAction == "discard" {
						m.pendingAction = ""
						f := unstaged[idx]
						if f.status == "?" {
							m.statusMsg = "cannot discard untracked file"
							return m, nil
						}
						if _, err := m.gitRun("restore", f.path); err != nil {
							m.statusMsg = "discard failed: " + err.Error()
						} else {
							m.statusMsg = "discarded " + f.path
							m.refresh()
							m.loadDiff()
						}
					} else {
						m.pendingAction = "discard"
						m.statusMsg = "press d again to discard"
					}
				}
			} else {
				m.statusMsg = "cannot discard staged file — unstage first"
			}
		} else if m.section == gitSectionStash && m.stashCursor < len(m.stashes) {
			if m.pendingAction == "drop-stash" {
				m.pendingAction = ""
				ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
				if _, err := m.gitRun("stash", "drop", ref); err != nil {
					m.statusMsg = "drop failed: " + err.Error()
				} else {
					m.statusMsg = "stash dropped"
					m.refresh()
				}
			} else {
				m.pendingAction = "drop-stash"
				m.statusMsg = "press d again to drop stash"
			}
		}
	case "c":
		if m.section == gitSectionChanges {
			m.committing = true
			m.commitInput.Reset()
			m.commitInput.Focus()
		}
	case "a":
		if m.section == gitSectionStash && m.stashCursor < len(m.stashes) {
			ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
			if _, err := m.gitRun("stash", "apply", ref); err != nil {
				m.statusMsg = "stash apply failed: " + err.Error()
			} else {
				m.statusMsg = "stash applied"
				m.refresh()
			}
		}
	case "enter":
		if m.section == gitSectionBranches && m.branchCursor < len(m.branches) {
			branch := m.branches[m.branchCursor]
			var err error
			if strings.HasPrefix(branch, "remotes/origin/") {
				remote := strings.TrimPrefix(branch, "remotes/origin/")
				_, err = m.gitRun("checkout", "--track", "origin/"+remote)
			} else {
				_, err = m.gitRun("checkout", branch)
			}
			if err != nil {
				m.statusMsg = "checkout failed: " + err.Error()
			} else {
				m.statusMsg = "switched to " + branch
				m.refresh()
			}
		}
	}
	return m, nil
}

func (m gitModel) handleDiffKey(key string) (gitModel, tea.Cmd) {
	switch key {
	case "j", "down":
		m.diff.ScrollDown(1)
	case "k", "up":
		m.diff.ScrollUp(1)
	}
	return m, nil
}

func (m *gitModel) currentFileList() []gitFile {
	switch m.section {
	case gitSectionChanges:
		var all []gitFile
		all = append(all, m.stagedFiles...)
		all = append(all, m.allUnstagedAndUntracked()...)
		return all
	}
	return nil
}

func (m *gitModel) allUnstagedAndUntracked() []gitFile {
	var out []gitFile
	out = append(out, m.unstagedFiles...)
	out = append(out, m.untrackedFiles...)
	return out
}

func (m *gitModel) loadDiff() {
	switch m.section {
	case gitSectionChanges:
		files := m.currentFileList()
		if m.filesCursor >= len(files) {
			m.diff.SetContent("")
			return
		}
		f := files[m.filesCursor]
		var out string
		var err error
		if f.staged {
			out, err = m.gitRun("diff", "--cached", f.path)
		} else {
			out, err = m.gitRun("diff", f.path)
		}
		if err != nil {
			out = "error: " + err.Error()
		}
		m.diff.SetContent(out)
		m.diff.GotoTop()
	case gitSectionLog:
		if m.commitCursor >= len(m.commits) {
			m.diff.SetContent("")
			return
		}
		c := m.commits[m.commitCursor]
		out, err := m.gitRun("show", "--stat", c.hash)
		if err != nil {
			out = "error: " + err.Error()
		}
		m.diff.SetContent(out)
		m.diff.GotoTop()
	case gitSectionStash:
		if m.stashCursor >= len(m.stashes) {
			m.diff.SetContent("")
			return
		}
		ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
		out, err := m.gitRun("stash", "show", "-p", ref)
		if err != nil {
			out = "error: " + err.Error()
		}
		m.diff.SetContent(out)
		m.diff.GotoTop()
	case gitSectionBranches:
		if m.branchCursor >= len(m.branches) {
			m.diff.SetContent("")
			return
		}
		out, err := m.gitRun("log", "--oneline", "-20", m.branches[m.branchCursor])
		if err != nil {
			out = "error: " + err.Error()
		}
		m.diff.SetContent(out)
		m.diff.GotoTop()
	}
}

func (m gitModel) View(w, h int, styles Styles, chatUnread bool) string {
	sectW := w * 20 / 100
	filesW := w * 30 / 100
	diffW := w - sectW - filesW - 4

	focusBorder := func(focused bool) lipgloss.Style {
		if focused {
			return borderStyle.BorderForeground(lipgloss.Color("#7AA2F7"))
		}
		return borderStyle
	}

	sectionNames := []string{"Changes", "Log", "Stash", "Branches"}
	sectionLines := make([]string, len(sectionNames))
	for i, name := range sectionNames {
		if gitSection(i) == m.section {
			sectionLines[i] = lipgloss.NewStyle().Reverse(true).Width(sectW - 4).Render(name)
		} else {
			sectionLines[i] = name
		}
	}
	sectPane := focusBorder(m.panel == gitPanelSections).Width(sectW - 2).Height(h - 4).Render(
		strings.Join(sectionLines, "\n"),
	)

	fileLines := m.renderFileList(filesW - 4)
	filesPane := focusBorder(m.panel == gitPanelFiles).Width(filesW - 2).Height(h - 4).Render(
		strings.Join(fileLines, "\n"),
	)

	diffSB := renderScrollbar(m.diff.Height(), m.diff.TotalLineCount(), m.diff.VisibleLineCount(), m.diff.YOffset())
	diffContent := lipgloss.JoinHorizontal(lipgloss.Top, m.diff.View(), diffSB)
	diffPane := focusBorder(m.panel == gitPanelDiff).Width(diffW - 2).Height(h - 4).Render(diffContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, sectPane, filesPane, diffPane)

	tabBar := renderTabBar(tabGit, chatUnread)
	headerLeft := styles.Header.Render("\u25c6 ocode  Git")
	headerPad := w - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar)
	if headerPad < 0 {
		headerPad = 0
	}
	header := headerLeft + strings.Repeat(" ", headerPad) + tabBar

	status := hintStyle.Render(m.statusMsg)

	parts := []string{header, row}
	if m.committing {
		parts = append(parts, borderStyle.Width(sectW+filesW-2).Render(m.commitInput.View()))
	}
	parts = append(parts, status)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m gitModel) renderFileList(width int) []string {
	var lines []string
	switch m.section {
	case gitSectionChanges:
		idx := 0
		if len(m.stagedFiles) > 0 {
			lines = append(lines, hintStyle.Render("\u25cf staged"))
			for _, f := range m.stagedFiles {
				line := "  " + f.status + " " + f.path
				if idx == m.filesCursor && m.panel == gitPanelFiles {
					line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
				}
				lines = append(lines, line)
				idx++
			}
		}
		if len(m.unstagedFiles)+len(m.untrackedFiles) > 0 {
			lines = append(lines, hintStyle.Render("\u25cb unstaged/untracked"))
			for _, f := range m.allUnstagedAndUntracked() {
				line := "  " + f.status + " " + f.path
				if idx == m.filesCursor && m.panel == gitPanelFiles {
					line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
				}
				lines = append(lines, line)
				idx++
			}
		}
	case gitSectionLog:
		for i, c := range m.commits {
			line := fmt.Sprintf("%s  %s  %s", c.hash, c.subject, c.age)
			if i == m.commitCursor {
				line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	case gitSectionStash:
		for i, s := range m.stashes {
			line := s
			if i == m.stashCursor {
				line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	case gitSectionBranches:
		for i, b := range m.branches {
			line := b
			if i == m.branchCursor {
				line = lipgloss.NewStyle().Reverse(true).Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	}
	return lines
}
