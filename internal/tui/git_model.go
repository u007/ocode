package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

type gitStatusMsg struct {
	text string
}

type gitRefreshMsg struct {
	staged        []gitFile
	unstaged      []gitFile
	untracked     []gitFile
	commits       []gitCommit
	stashes       []string
	branches      []string
	currentBranch string
	aheadBehind   string
}

type diffHunk struct {
	header string
	body   string
	start  int
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
	currentBranch  string
	aheadBehind    string
	diff           viewport.Model
	committing     bool
	commitInput    textarea.Model
	statusMsg      string
	pendingAction  string // "discard" | "drop-stash" | "delete-branch" | ""
	// branch create input
	branchInputMode bool
	branchInputText string
	// stash push input
	stashInputMode bool
	stashInputText string
	// hunk-level staging
	hunks      []diffHunk
	hunkCursor int
	diffHeader string
	// commit log pagination
	commitViewport viewport.Model
	loadingLog     bool
	logsMore       bool
	// diff text selection
	diffRawLines []string
	diffLines    []string
	// external editor integration
	editor       string
	editorOpener func(string) tea.Cmd
}

func newGitModel(workDir string) gitModel {
	m := gitModel{workDir: workDir}
	m.diff = viewport.New()
	m.commitViewport = viewport.New()
	ci := textarea.New()
	ci.Placeholder = "Commit message..."
	ci.SetHeight(5)
	m.commitInput = ci
	if _, err := m.gitRun("rev-parse", "--git-dir"); err != nil {
		m.statusMsg = "not a git repository"
		return m
	}
	m.refresh()
	m.loadDiff()
	return m
}

func (m *gitModel) SetEditor(e string) { m.editor = e }

func (m *gitModel) SetEditorOpener(fn func(string) tea.Cmd) { m.editorOpener = fn }

func (m *gitModel) Resize(w, h int) {
	m.width = w
	m.height = h
	sectW := w * 20 / 100
	filesW := w * 30 / 100
	diffW := w - sectW - filesW
	diffH := h - 5
	if diffH < 1 {
		diffH = 1
	}
	m.diff.SetWidth(diffW - 7)
	m.diff.SetHeight(diffH)
	m.commitViewport.SetWidth(filesW - 7)
	m.commitViewport.SetHeight(h - 5)
	m.commitInput.SetWidth(sectW + filesW)
}

func (m *gitModel) refresh() {
	m.loadChanges()
	m.loadLog()
	m.loadStash()
	m.loadBranches()
}

func (m *gitModel) gitRun(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.workDir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (m *gitModel) gitRunTimeout(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.workDir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (m *gitModel) aheadBehindString() string {
	ahead, err1 := m.gitRun("rev-list", "--count", "@{u}..HEAD")
	behind, err2 := m.gitRun("rev-list", "--count", "HEAD..@{u}")
	if err1 != nil || err2 != nil {
		return "" // no upstream configured
	}
	ahead = strings.TrimSpace(ahead)
	behind = strings.TrimSpace(behind)
	if ahead == "0" && behind == "0" {
		return "✓"
	}
	parts := []string{}
	if ahead != "0" {
		parts = append(parts, "↑"+ahead)
	}
	if behind != "0" {
		parts = append(parts, "↓"+behind)
	}
	return strings.Join(parts, " ")
}

func (m *gitModel) cmdRefresh() tea.Cmd {
	workDir := m.workDir
	return func() tea.Msg {
		tmp := gitModel{workDir: workDir}
		tmp.loadChanges()
		tmp.loadLog()
		tmp.loadStash()
		tmp.loadBranches()
		ab := tmp.aheadBehindString()
		return gitRefreshMsg{
			staged:        tmp.stagedFiles,
			unstaged:      tmp.unstagedFiles,
			untracked:     tmp.untrackedFiles,
			commits:       tmp.commits,
			stashes:       tmp.stashes,
			branches:      tmp.branches,
			currentBranch: tmp.currentBranch,
			aheadBehind:   ab,
		}
	}
}

type loadMoreLogMsg struct {
	commits []gitCommit
	hasMore bool
	err     error
}

func (m *gitModel) cmdLoadMoreLog() tea.Cmd {
	skip := len(m.commits)
	workDir := m.workDir
	return func() tea.Msg {
		cmd := exec.Command("git", "log", "--format=%h\t%s\t%an\t%cr", fmt.Sprintf("--skip=%d", skip), "-50")
		cmd.Dir = workDir
		out, err := cmd.Output()
		if err != nil {
			return loadMoreLogMsg{err: err}
		}
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			return loadMoreLogMsg{}
		}
		var commits []gitCommit
		for _, line := range strings.Split(trimmed, "\n") {
			parts := strings.SplitN(line, "\t", 4)
			if len(parts) == 4 {
				commits = append(commits, gitCommit{
					hash:    parts[0],
					subject: parts[1],
					author:  parts[2],
					age:     parts[3],
				})
			}
		}
		return loadMoreLogMsg{commits: commits, hasMore: len(commits) >= 50}
	}
}

func (m *gitModel) cmdNetworkOp(statusDone, statusFail string, args ...string) tea.Cmd {
	workDir := m.workDir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = workDir
		if out, err := cmd.Output(); err != nil {
			msg := err.Error()
			if len(out) > 0 {
				msg = strings.TrimSpace(string(out)) + ": " + msg
			}
			return gitStatusMsg{text: statusFail + ": " + msg}
		}
		return gitStatusMsg{text: statusDone}
	}
}

func (m *gitModel) parseStatus(out string) {
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

func (m *gitModel) loadChanges() {
	out, err := m.gitRun("status", "--porcelain")
	if err != nil {
		m.statusMsg = "status: " + err.Error()
		return
	}
	m.parseStatus(out)
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
	m.logsMore = len(m.commits) >= 50
	m.loadingLog = false
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

func (m *gitModel) parseBranches(out string) {
	m.branches = nil
	m.currentBranch = ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		isCurrent := strings.HasPrefix(line, "*")
		name := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if isCurrent {
			m.currentBranch = name
		}
		m.branches = append(m.branches, name)
	}
}

func (m *gitModel) loadBranches() {
	out, err := m.gitRun("branch", "-a")
	if err != nil {
		m.statusMsg = "branch list: " + err.Error()
		m.branches = nil
		return
	}
	m.parseBranches(out)
}

func (m gitModel) Update(msg tea.Msg, w, h int) (gitModel, tea.Cmd) {
	switch msg := msg.(type) {
	case gitStatusMsg:
		m.statusMsg = msg.text
		return m, m.cmdRefresh()
	case gitRefreshMsg:
		m.stagedFiles = msg.staged
		m.unstagedFiles = msg.unstaged
		m.untrackedFiles = msg.untracked
		m.commits = msg.commits
		m.stashes = msg.stashes
		m.branches = msg.branches
		m.currentBranch = msg.currentBranch
		m.aheadBehind = msg.aheadBehind
		m.loadingLog = false
		m.logsMore = len(m.commits) >= 50
		files := m.currentFileList()
		if len(files) == 0 {
			m.filesCursor = 0
		} else if m.filesCursor >= len(files) {
			m.filesCursor = len(files) - 1
		}
		m.loadDiff()
		return m, nil
	case loadMoreLogMsg:
		m.loadingLog = false
		if msg.err != nil {
			m.statusMsg = "git log failed: " + msg.err.Error()
			return m, nil
		}
		if len(msg.commits) > 0 {
			m.commits = append(m.commits, msg.commits...)
		}
		m.logsMore = msg.hasMore
		return m, nil
	}
	if m.committing {
		return m.updateCommitInput(msg)
	}
	if m.branchInputMode {
		return m.updateBranchInput(msg)
	}
	if m.stashInputMode {
		return m.updateStashInput(msg)
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
		case "ctrl+enter":
			text := strings.TrimSpace(m.commitInput.Value())
			if text != "" {
				if _, err := m.gitRun("commit", "-m", text); err != nil {
					m.statusMsg = "commit failed: " + err.Error()
				} else {
					m.statusMsg = "committed"
					m.committing = false
					m.commitInput.Reset()
					return m, m.cmdRefresh()
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	return m, cmd
}

func (m gitModel) updateBranchInput(msg tea.Msg) (gitModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.branchInputMode = false
			m.branchInputText = ""
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.branchInputText)
			if name != "" {
				if _, err := m.gitRun("checkout", "-b", name); err != nil {
					m.statusMsg = "create branch failed: " + err.Error()
				} else {
					m.statusMsg = "created and switched to " + name
				}
			}
			m.branchInputMode = false
			m.branchInputText = ""
			return m, m.cmdRefresh()
		case "backspace":
			runes := []rune(m.branchInputText)
			if len(runes) > 0 {
				m.branchInputText = string(runes[:len(runes)-1])
			}
			return m, nil
		default:
			if len(msg.String()) == 1 {
				m.branchInputText += msg.String()
			}
			return m, nil
		}
	}
	return m, nil
}

func (m gitModel) updateStashInput(msg tea.Msg) (gitModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.stashInputMode = false
			m.stashInputText = ""
			return m, nil
		case "enter":
			args := []string{"stash", "push"}
			if note := strings.TrimSpace(m.stashInputText); note != "" {
				args = append(args, "-m", note)
			}
			if _, err := m.gitRun(args...); err != nil {
				m.statusMsg = "stash failed: " + err.Error()
			} else {
				m.statusMsg = "stashed"
			}
			m.stashInputMode = false
			m.stashInputText = ""
			return m, m.cmdRefresh()
		case "backspace":
			runes := []rune(m.stashInputText)
			if len(runes) > 0 {
				m.stashInputText = string(runes[:len(runes)-1])
			}
			return m, nil
		default:
			if len(msg.String()) == 1 {
				m.stashInputText += msg.String()
			}
			return m, nil
		}
	}
	return m, nil
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

func (m *gitModel) resetCursors() {
	m.filesCursor = 0
	m.commitCursor = 0
	m.stashCursor = 0
	m.branchCursor = 0
}

func (m gitModel) handleSectionKey(key string) (gitModel, tea.Cmd) {
	sections := []gitSection{gitSectionChanges, gitSectionLog, gitSectionStash, gitSectionBranches}
	cur := int(m.section)
	switch key {
	case "j", "down":
		if cur < len(sections)-1 {
			m.section = sections[cur+1]
			m.resetCursors()
			m.loadDiff()
		}
	case "k", "up":
		if cur > 0 {
			m.section = sections[cur-1]
			m.resetCursors()
			m.loadDiff()
		}
	case "enter":
		m.panel = gitPanelFiles
	}
	return m, nil
}

func (m gitModel) handleFilesKey(key string) (gitModel, tea.Cmd) {
	// Clear pending confirmation when user presses any key other than the confirm key
	if key != "d" && key != "x" {
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
				if m.commitCursor >= m.commitViewport.YOffset()+m.commitViewport.VisibleLineCount() {
					m.commitViewport.ScrollDown(1)
				}
				m.loadDiff()
				if !m.loadingLog && m.logsMore && m.commitCursor >= len(m.commits)-5 {
					m.loadingLog = true
					return m, m.cmdLoadMoreLog()
				}
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
				if m.commitCursor < m.commitViewport.YOffset() {
					m.commitViewport.ScrollUp(1)
				}
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
					return m, m.cmdRefresh()
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
				return m, m.cmdRefresh()
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
							return m, m.cmdRefresh()
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
					return m, m.cmdRefresh()
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
				return m, m.cmdRefresh()
			}
		}
	case "f":
		if m.section == gitSectionChanges || m.section == gitSectionBranches {
			m.statusMsg = "fetching..."
			return m, m.cmdNetworkOp("fetched", "fetch failed", "fetch", "--all")
		}
	case "p":
		if m.section == gitSectionChanges || m.section == gitSectionBranches {
			m.statusMsg = "pulling..."
			return m, m.cmdNetworkOp("pulled", "pull failed", "pull")
		}
	case "P":
		if m.section == gitSectionChanges || m.section == gitSectionBranches {
			m.statusMsg = "pushing..."
			return m, m.cmdNetworkOp("pushed", "push failed", "push")
		}
	case "n":
		if m.section == gitSectionBranches {
			m.branchInputMode = true
			m.branchInputText = ""
			m.statusMsg = "new branch name:"
		}
	case "x":
		if m.section == gitSectionBranches && m.branchCursor < len(m.branches) {
			branch := m.branches[m.branchCursor]
			if branch == m.currentBranch {
				m.statusMsg = "cannot delete current branch"
				return m, nil
			}
			if m.pendingAction == "delete-branch" {
				m.pendingAction = ""
				if _, err := m.gitRun("branch", "-d", branch); err != nil {
					m.statusMsg = "delete failed: " + err.Error()
				} else {
					m.statusMsg = "deleted " + branch
				}
				return m, m.cmdRefresh()
			}
			m.pendingAction = "delete-branch"
			m.statusMsg = "press x again to delete " + branch
		}
	case "S":
		if m.section == gitSectionChanges {
			m.stashInputMode = true
			m.stashInputText = ""
			m.statusMsg = "stash message (optional, enter to confirm):"
		}
	case "E":
		if m.section == gitSectionChanges {
			files := m.currentFileList()
			if m.filesCursor < len(files) {
				path := filepath.Join(m.workDir, files[m.filesCursor].path)
				m.statusMsg = "opening editor..."
				return m, m.openInEditor(path)
			}
		}
	case "enter":
		switch m.section {
		case gitSectionBranches:
			if m.branchCursor < len(m.branches) {
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
					return m, m.cmdRefresh()
				}
			}
		case gitSectionStash:
			if m.stashCursor < len(m.stashes) {
				ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
				if _, err := m.gitRun("stash", "pop", ref); err != nil {
					m.statusMsg = "pop failed: " + err.Error()
				} else {
					m.statusMsg = "stash popped"
					return m, m.cmdRefresh()
				}
			}
		}
	}
	return m, nil
}

func (m gitModel) openInEditor(path string) tea.Cmd {
	if m.editorOpener != nil {
		return m.editorOpener(path)
	}
	editor := m.editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
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

func (m gitModel) handleDiffKey(key string) (gitModel, tea.Cmd) {
	switch key {
	case "j", "down":
		m.diff.ScrollDown(1)
	case "k", "up":
		m.diff.ScrollUp(1)
	case "]":
		if m.hunkCursor < len(m.hunks)-1 {
			m.hunkCursor++
			m.diff.SetYOffset(m.hunks[m.hunkCursor].start)
		}
	case "[":
		if m.hunkCursor > 0 {
			m.hunkCursor--
			m.diff.SetYOffset(m.hunks[m.hunkCursor].start)
		}
	case "s":
		if m.section == gitSectionChanges && m.hunkCursor < len(m.hunks) {
			return m.applyHunk(false)
		}
	case "u":
		if m.section == gitSectionChanges && m.hunkCursor < len(m.hunks) {
			return m.applyHunk(true)
		}
	}
	return m, nil
}

func (m gitModel) applyHunk(reverse bool) (gitModel, tea.Cmd) {
	if m.hunkCursor >= len(m.hunks) {
		return m, nil
	}
	hunk := m.hunks[m.hunkCursor]
	patch := m.diffHeader + hunk.body
	tmp, err := os.CreateTemp("", "ocode-hunk-*.patch")
	if err != nil {
		m.statusMsg = "hunk apply: " + err.Error()
		return m, nil
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(patch); err != nil {
		m.statusMsg = "hunk apply: " + err.Error()
		return m, nil
	}
	tmp.Close()
	args := []string{"apply", "--cached", tmp.Name()}
	if reverse {
		args = []string{"apply", "--cached", "--reverse", tmp.Name()}
	}
	if _, err := m.gitRun(args...); err != nil {
		m.statusMsg = "hunk apply failed: " + err.Error()
		return m, nil
	}
	action := "staged hunk"
	if reverse {
		action = "unstaged hunk"
	}
	m.statusMsg = action
	return m, m.cmdRefresh()
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

func (m *gitModel) setDiffContent(content string) {
	m.diff.SetContent(content)
	raw := stripANSI(content)
	m.diffRawLines = strings.Split(raw, "\n")
	m.diffLines = strings.Split(content, "\n")
}

func (m *gitModel) applyDiffSelectionHighlight(startLine, startCol, endLine, endCol int) {
	if len(m.diffLines) == 0 {
		return
	}
	highlighted := applySelectionHighlight(m.diffLines, m.diffRawLines, startLine, startCol, endLine, endCol)
	m.diff.SetContent(strings.Join(highlighted, "\n"))
}

func (m *gitModel) clearDiffSelectionHighlight() {
	if len(m.diffLines) == 0 {
		return
	}
	m.diff.SetContent(strings.Join(m.diffLines, "\n"))
}

func parseHunks(diff string) []diffHunk {
	var hunks []diffHunk
	var current strings.Builder
	var header string
	lineIdx := 0
	startLine := 0
	for _, line := range strings.Split(diff, "\n") {
		plain := stripANSI(line)
		if strings.HasPrefix(plain, "@@") {
			if current.Len() > 0 && header != "" {
				hunks = append(hunks, diffHunk{header: header, body: current.String(), start: startLine})
				current.Reset()
			} else {
				current.Reset()
			}
			header = plain
			startLine = lineIdx
		}
		current.WriteString(plain + "\n")
		lineIdx++
	}
	if current.Len() > 0 && header != "" {
		hunks = append(hunks, diffHunk{header: header, body: current.String(), start: startLine})
	}
	return hunks
}

func extractDiffHeader(diff string) string {
	var lines []string
	for _, line := range strings.Split(diff, "\n") {
		plain := stripANSI(line)
		if strings.HasPrefix(plain, "@@") {
			break
		}
		lines = append(lines, plain)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func (m *gitModel) loadDiff() {
	switch m.section {
	case gitSectionChanges:
		files := m.currentFileList()
		if m.filesCursor >= len(files) {
			m.setDiffContent("")
			m.hunks = nil
			m.hunkCursor = 0
			m.diffHeader = ""
			return
		}
		f := files[m.filesCursor]
		var out string
		var err error
		if f.staged {
			out, err = m.gitRun("diff", "--no-color", "--cached", f.path)
		} else {
			out, err = m.gitRun("diff", "--no-color", f.path)
		}
		if err != nil {
			out = "error: " + err.Error()
		}
		m.setDiffContent(renderUnifiedDiff(out, currentStyles()))
		m.diff.GotoTop()
		m.hunks = parseHunks(out)
		m.hunkCursor = 0
		m.diffHeader = extractDiffHeader(out)
	case gitSectionLog:
		if m.commitCursor >= len(m.commits) {
			m.setDiffContent("")
			m.hunks = nil
			m.hunkCursor = 0
			m.diffHeader = ""
			return
		}
		c := m.commits[m.commitCursor]
		out, err := m.gitRun("show", "--no-color", "--stat", c.hash)
		if err != nil {
			out = "error: " + err.Error()
		}
		m.setDiffContent(renderUnifiedDiff(out, currentStyles()))
		m.diff.GotoTop()
		m.hunks = nil
		m.hunkCursor = 0
		m.diffHeader = ""
	case gitSectionStash:
		if m.stashCursor >= len(m.stashes) {
			m.setDiffContent("")
			m.hunks = nil
			m.hunkCursor = 0
			m.diffHeader = ""
			return
		}
		ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
		out, err := m.gitRun("stash", "show", "--no-color", "-p", ref)
		if err != nil {
			out = "error: " + err.Error()
		}
		m.setDiffContent(renderUnifiedDiff(out, currentStyles()))
		m.diff.GotoTop()
		m.hunks = nil
		m.hunkCursor = 0
		m.diffHeader = ""
	case gitSectionBranches:
		if m.branchCursor >= len(m.branches) {
			m.setDiffContent("")
			m.hunks = nil
			m.hunkCursor = 0
			m.diffHeader = ""
			return
		}
		out, err := m.gitRun("log", "--oneline", "-20", m.branches[m.branchCursor])
		if err != nil {
			out = "error: " + err.Error()
		}
		m.setDiffContent(out)
		m.diff.GotoTop()
		m.hunks = nil
		m.hunkCursor = 0
		m.diffHeader = ""
	}
}

func (m *gitModel) loadFilePreview(f gitFile) {
	path := filepath.Join(m.workDir, f.path)
	data, err := os.ReadFile(path)
	if err != nil {
		m.setDiffContent("error: " + err.Error())
		m.diff.GotoTop()
		m.hunks = nil
		m.hunkCursor = 0
		m.diffHeader = ""
		return
	}
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	if strings.ContainsRune(string(probe), '\x00') {
		m.setDiffContent("[binary file]")
		m.diff.GotoTop()
		m.hunks = nil
		m.hunkCursor = 0
		m.diffHeader = ""
		return
	}
	content := string(data)
	if len(data) > 1024*1024 {
		content = string(data[:1024*1024]) + "\n[truncated — 1MB limit]"
	}
	header := hintStyle.Render("Preview: "+f.path+"  (E edit)") + "\n\n"
	m.setDiffContent(header + highlightContent(content, languageForPath(f.path)))
	m.diff.GotoTop()
	m.hunks = nil
	m.hunkCursor = 0
	m.diffHeader = ""
}

func (m gitModel) renderHints() string {
	if m.committing {
		return "ctrl+enter commit  esc cancel"
	}
	if m.branchInputMode || m.stashInputMode {
		return "enter confirm  esc cancel"
	}
	base := "tab next panel  "
	switch m.panel {
	case gitPanelSections:
		return base + "j/k navigate  enter focus files"
	case gitPanelFiles:
		switch m.section {
		case gitSectionChanges:
			return base + "s stage  u unstage  d discard  E edit  S stash  c commit  f fetch  p pull  P push"
		case gitSectionLog:
			return base + "j/k navigate"
		case gitSectionStash:
			return base + "enter pop  a apply  d drop"
		case gitSectionBranches:
			return base + "enter checkout  n new  x delete  f fetch  p pull  P push"
		}
	case gitPanelDiff:
		if m.section == gitSectionChanges {
			return base + "j/k scroll  [/] prev/next hunk  s stage hunk  u unstage hunk"
		}
		return base + "j/k scroll"
	}
	return base
}

func (m *gitModel) buildCommitViewport(width int) {
	var lines []string
	for i, c := range m.commits {
		line := fmt.Sprintf("%s  %s  %s", c.hash, c.subject, c.age)
		if i == m.commitCursor {
			line = selectedStyle.Width(width).Render(line)
		}
		lines = append(lines, line)
	}
	m.commitViewport.SetContent(strings.Join(lines, "\n"))
}

func (m gitModel) View(w, h int, styles Styles, chatUnread, exitPending bool) string {
	sectW := w * 20 / 100
	filesW := w * 30 / 100
	diffW := w - sectW - filesW

	focusBorder := func(focused bool) lipgloss.Style {
		if focused {
			return borderStyle.BorderForeground(selectedStyle.GetBackground())
		}
		return borderStyle
	}

	sectionNames := []string{"Changes", "Log", "Stash", "Branches"}
	sectionLines := make([]string, len(sectionNames))
	for i, name := range sectionNames {
		if gitSection(i) == m.section {
			sectionLines[i] = selectedStyle.Width(sectW - 4).Render(name)
		} else {
			sectionLines[i] = name
		}
	}
	sectPane := focusBorder(m.panel == gitPanelSections).Width(sectW - 2).Height(h - 4).Render(
		strings.Join(sectionLines, "\n"),
	)

	var filesContent string
	if m.section == gitSectionLog {
		savedOffset := m.commitViewport.YOffset()
		m.buildCommitViewport(filesW - 7)
		m.commitViewport.SetYOffset(savedOffset)
		logSB := renderScrollbar(m.commitViewport.Height(), m.commitViewport.TotalLineCount(), m.commitViewport.VisibleLineCount(), m.commitViewport.YOffset())
		filesContent = lipgloss.JoinHorizontal(lipgloss.Top, m.commitViewport.View(), logSB)
	} else {
		fileLines := m.renderFileList(filesW - 4)
		filesContent = strings.Join(fileLines, "\n")
	}
	filesPane := focusBorder(m.panel == gitPanelFiles).Width(filesW - 2).Height(h - 4).Render(filesContent)

	diffSB := renderScrollbar(m.diff.Height(), m.diff.TotalLineCount(), m.diff.VisibleLineCount(), m.diff.YOffset())
	diffContent := lipgloss.JoinHorizontal(lipgloss.Top, m.diff.View(), diffSB)
	diffPane := focusBorder(m.panel == gitPanelDiff).Width(diffW - 2).Height(h - 4).Render(diffContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, sectPane, filesPane, diffPane)

	tabBar := renderTabBar(tabGit, chatUnread)
	var exitBtn string
	if exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("✕ exit?")
	} else {
		exitBtn = hintStyle.Padding(0, 1).Render("✕ exit")
	}
	ab := ""
	if m.aheadBehind != "" {
		ab = "  " + m.aheadBehind
	}
	headerLeft := styles.Header.Render("◆ ocode  Git" + ab)
	headerPad := w - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar) - lipgloss.Width(exitBtn)
	if headerPad < 0 {
		headerPad = 0
	}
	header := headerLeft + strings.Repeat(" ", headerPad) + tabBar + exitBtn

	var parts []string
	parts = append(parts, header, row)
	if m.committing {
		dialogW := w * 60 / 100
		if dialogW < 40 {
			dialogW = w - 4
		}
		if dialogW > w-4 {
			dialogW = w - 4
		}
		title := styles.Header.Render("Commit message")
		hint := hintStyle.Render("ctrl+enter commit  esc cancel")
		body := lipgloss.JoinVertical(lipgloss.Left, title, "", m.commitInput.View(), "", hint)
		dialog := borderStyle.Width(dialogW).Render(body)
		parts = append(parts, lipgloss.PlaceHorizontal(w, lipgloss.Center, dialog))
	}
	if m.branchInputMode {
		prompt := hintStyle.Render("New branch: ") + m.branchInputText + "█"
		parts = append(parts, borderStyle.Width(sectW+filesW-2).Render(prompt))
	}
	if m.stashInputMode {
		prompt := hintStyle.Render("Stash message: ") + m.stashInputText + "█"
		parts = append(parts, borderStyle.Width(sectW+filesW-2).Render(prompt))
	}
	hints := hintStyle.Render(m.renderHints())
	statusBar := hints
	if m.statusMsg != "" {
		statusBar = hints + "   " + errorStyle.Render(m.statusMsg)
	}
	parts = append(parts, statusBar)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m gitModel) renderFileList(width int) []string {
	var lines []string
	switch m.section {
	case gitSectionChanges:
		idx := 0
		if len(m.stagedFiles) > 0 {
			lines = append(lines, hintStyle.Render("● staged"))
			for _, f := range m.stagedFiles {
				line := "  " + f.status + " " + f.path
				if idx == m.filesCursor && m.panel == gitPanelFiles {
					line = selectedStyle.Width(width).Render(line)
				}
				lines = append(lines, line)
				idx++
			}
		}
		if len(m.unstagedFiles)+len(m.untrackedFiles) > 0 {
			lines = append(lines, hintStyle.Render("○ unstaged/untracked"))
			for _, f := range m.allUnstagedAndUntracked() {
				line := "  " + f.status + " " + f.path
				if idx == m.filesCursor && m.panel == gitPanelFiles {
					line = selectedStyle.Width(width).Render(line)
				}
				lines = append(lines, line)
				idx++
			}
		}
	case gitSectionLog:
		for i, c := range m.commits {
			line := fmt.Sprintf("%s  %s  %s", c.hash, c.subject, c.age)
			if i == m.commitCursor {
				line = selectedStyle.Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	case gitSectionStash:
		for i, s := range m.stashes {
			line := s
			if i == m.stashCursor {
				line = selectedStyle.Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	case gitSectionBranches:
		for i, b := range m.branches {
			marker := "  "
			if b == m.currentBranch {
				marker = "* "
			}
			line := marker + b
			if i == m.branchCursor {
				line = selectedStyle.Width(width).Render(line)
			}
			lines = append(lines, line)
		}
	}
	return lines
}
