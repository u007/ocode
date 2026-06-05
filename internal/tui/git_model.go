package tui

import (
	"context"
	"fmt"
	"log"
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

type gitPendingAction string

const (
	gitPendingNone         gitPendingAction = ""
	gitPendingDiscard      gitPendingAction = "discard"
	gitPendingDropStash    gitPendingAction = "drop-stash"
	gitPendingDeleteBranch gitPendingAction = "delete-branch"
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
	autoRefresh   bool // when true, skip diff reload to preserve scroll/selection
}

// gitBranchRefreshMsg is a lightweight message for updating only the current
// branch and ahead/behind info in the sidebar, without touching other git state.
type gitBranchRefreshMsg struct {
	currentBranch string
	aheadBehind   string
}

type diffHunk struct {
	header string
	body   string
	start  int
}

type gitCommitMsgMsg struct {
	text string
	err  error
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
	selectedFiles  map[int]bool
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
	pendingAction  gitPendingAction
	// branch create input
	branchInputMode bool
	branchInputText string
	// stash push input
	stashInputMode bool
	stashInputText string
	// gitignore path input
	ignorePathInputMode bool
	ignorePathInputText string
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
	// ai commit message generation
	generateCommitMsg func(diff string) tea.Cmd
	generatingMsg     bool
	// file list filter
	filterActive bool
	filterQuery  string
	// logger receives a copy of every terminal-state user action (push, pull,
	// fetch, commit, stage/unstage, stash, branch ops, etc.) so the main
	// model can append it to the log tab. nil means "no sink installed",
	// which keeps unit tests and headless callers quiet by default.
	logger func(kind DebugEntryKind, msg string)
}

func diffLineNumbers(ctx viewport.GutterContext) string {
	if ctx.Soft {
		return "     │ "
	}
	if ctx.Index >= ctx.TotalLines {
		return "   ~ │ "
	}
	return fmt.Sprintf("%4d │ ", ctx.Index+1)
}

func newGitModel(workDir string) gitModel {
	m := gitModel{workDir: workDir}
	m.diff = viewport.New()
	m.diff.SoftWrap = true
	m.diff.LeftGutterFunc = diffLineNumbers
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
	commitInputRows := 0
	if m.committing {
		commitInputRows = m.commitInput.Height() + 2
	}
	diffH := h - 5 - commitInputRows
	if diffH < 1 {
		diffH = 1
	}
	m.diff.SetWidth(diffW - 14)
	m.diff.SetHeight(diffH)
	m.commitViewport.SetWidth(filesW - 7)
	m.commitViewport.SetHeight(h - 5 - commitInputRows)
	// full width minus border
	m.commitInput.SetWidth(w - 4)
}

func (m *gitModel) refresh() {
	m.loadChanges()
	m.loadLog()
	m.loadStash()
	m.loadBranches()
}

// SetLogger installs a sink that receives a copy of every terminal-state
// user action (push, pull, fetch, commit, stage/unstage, stash, branch ops,
// etc.) so the main model can append it to the log tab. Passing nil is
// allowed and disables logging — useful in tests.
func (m *gitModel) SetLogger(logger func(kind DebugEntryKind, msg string)) {
	m.logger = logger
}

// logGit forwards msg to the installed logger if one is set. It is a no-op
// when no logger is installed, so callers don't need to nil-check.
func (m *gitModel) logGit(msg string) {
	if m.logger != nil {
		m.logger(DebugKindGit, msg)
	}
}

// firstLine trims a possibly multi-line git error message to its first line
// and caps the length so a single failed `git push` doesn't flood the log
// tab with git's full stderr. The status bar at the bottom of the Git tab
// still shows the full error verbatim.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 160
	if len(s) > max {
		s = s[:max-1] + "…"
	}
	return strings.TrimSpace(s)
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

// cmdAutoRefresh is like cmdRefresh but sets autoRefresh=true on the returned
// gitRefreshMsg. The Update handler skips loadDiff() for auto-refresh messages
// so that the diff viewport scroll position and any active text selection are
// preserved.
func (m *gitModel) cmdAutoRefresh() tea.Cmd {
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
			autoRefresh:   true,
		}
	}
}

// cmdBranchRefresh returns a lightweight command that only fetches the current
// branch and ahead/behind info. This is used to keep the sidebar branch display
// up-to-date regardless of which tab is active.
func (m *gitModel) cmdBranchRefresh() tea.Cmd {
	workDir := m.workDir
	return func() tea.Msg {
		tmp := gitModel{workDir: workDir}
		// Only load branches to get current branch
		tmp.loadBranches()
		ab := tmp.aheadBehindString()
		return gitBranchRefreshMsg{
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
	case gitCommitMsgMsg:
		m.generatingMsg = false
		if msg.err != nil {
			m.statusMsg = "generate failed: " + msg.err.Error()
		} else {
			m.commitInput.SetValue(msg.text)
			m.statusMsg = "✨ generated"
		}
		return m, nil
	case gitStatusMsg:
		// Choke point for network ops (push/pull/fetch) and any other
		// statusMsg-returning operation. Log the terminal outcome so the
		// log tab has a record of every git action.
		m.statusMsg = msg.text
		m.logGit(firstLine(msg.text))
		return m, m.cmdRefresh()
	case gitBranchRefreshMsg:
		// Lightweight branch-only update for sidebar (doesn't touch other state)
		if msg.currentBranch != "" {
			m.currentBranch = msg.currentBranch
		}
		m.aheadBehind = msg.aheadBehind
		return m, nil
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
		// During auto-refresh, skip diff reload to preserve scroll position
		// and any active text selection.
		if !msg.autoRefresh {
			m.loadDiff()
		}
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
	if m.ignorePathInputMode {
		return m.updateIgnorePathInput(msg)
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
			m.Resize(m.width, m.height)
			return m, nil
		case "ctrl+enter":
			text := strings.TrimSpace(m.commitInput.Value())
			if text != "" {
				if _, err := m.gitRun("commit", "-m", text); err != nil {
					m.statusMsg = "commit failed: " + err.Error()
					m.logGit("commit failed: " + firstLine(err.Error()))
				} else {
					m.statusMsg = "committed"
					subject := text
					if i := strings.IndexByte(subject, '\n'); i >= 0 {
						subject = subject[:i]
					}
					if len(subject) > 60 {
						subject = subject[:59] + "…"
					}
					m.logGit("commit: " + subject)
					m.committing = false
					m.commitInput.Reset()
					m.Resize(m.width, m.height)
					return m, m.cmdRefresh()
				}
			}
			return m, nil
		case "ctrl+g":
			if m.generateCommitMsg != nil && !m.generatingMsg {
				diff := m.commitDiff()
				if diff == "" {
					m.statusMsg = "nothing to commit"
					return m, nil
				}
				m.generatingMsg = true
				m.statusMsg = "✨ generating..."
				return m, m.generateCommitMsg(diff)
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	return m, cmd
}

func (m *gitModel) commitDiff() string {
	if len(m.stagedFiles) > 0 {
		out, err := m.gitRun("diff", "--cached", "--stat", "--no-color")
		if err == nil && out != "" {
			diff, _ := m.gitRun("diff", "--cached", "--no-color")
			return out + "\n" + diff
		}
	}
	out, err := m.gitRun("diff", "HEAD", "--stat", "--no-color")
	if err == nil && out != "" {
		diff, _ := m.gitRun("diff", "HEAD", "--no-color")
		return out + "\n" + diff
	}
	// fallback: untracked file names
	var paths []string
	for _, f := range m.untrackedFiles {
		paths = append(paths, f.path)
	}
	return strings.Join(paths, "\n")
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
					m.logGit("create branch failed: " + firstLine(err.Error()))
				} else {
					m.statusMsg = "created and switched to " + name
					m.logGit("create branch: " + name)
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
			note := strings.TrimSpace(m.stashInputText)
			if note != "" {
				args = append(args, "-m", note)
			}
			if _, err := m.gitRun(args...); err != nil {
				m.statusMsg = "stash failed: " + err.Error()
				m.logGit("stash failed: " + firstLine(err.Error()))
			} else {
				m.statusMsg = "stashed"
				if note != "" {
					m.logGit("stash push: " + note)
				} else {
					m.logGit("stash push")
				}
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

func (m gitModel) updateIgnorePathInput(msg tea.Msg) (gitModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.ignorePathInputMode = false
			m.ignorePathInputText = ""
			m.statusMsg = "ignore cancelled"
			return m, nil
		case "enter":
			path := strings.TrimSpace(m.ignorePathInputText)
			m.ignorePathInputMode = false
			m.ignorePathInputText = ""
			if path == "" {
				m.statusMsg = "ignore path required"
				return m, nil
			}
			return m.ignorePath(path)
		case "backspace":
			runes := []rune(m.ignorePathInputText)
			if len(runes) > 0 {
				m.ignorePathInputText = string(runes[:len(runes)-1])
			}
			return m, nil
		default:
			if len(msg.String()) == 1 {
				m.ignorePathInputText += msg.String()
			}
			return m, nil
		}
	}
	return m, nil
}

func (m gitModel) handleKey(msg tea.KeyPressMsg, w, h int) (gitModel, tea.Cmd) {
	key := msg.String()

	// Global filter activation — works from any panel
	if m.filterActive {
		return m.handleFilesKey(key)
	}

	switch key {
	case "tab":
		m.panel = (m.panel + 1) % 3
		return m, nil
	case "/":
		if m.section == gitSectionChanges {
			m.filterActive = true
			m.filterQuery = ""
			m.filesCursor = 0
			m.panel = gitPanelFiles
			return m, nil
		}
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

func (m *gitModel) ensureSelectedFiles() {
	if m.selectedFiles == nil {
		m.selectedFiles = make(map[int]bool)
	}
}

func (m gitModel) handleFilesKey(key string) (gitModel, tea.Cmd) {
	// Filter mode: handle typing and exit
	if m.filterActive {
		switch key {
		case "esc":
			m.filterActive = false
			m.filterQuery = ""
			m.filesCursor = 0
			m.loadDiff()
		case "backspace":
			if len(m.filterQuery) > 0 {
				m.filterQuery = m.filterQuery[:len(m.filterQuery)-1]
				m.filesCursor = 0
				m.loadDiff()
			}
		case "enter":
			m.filterActive = false
		default:
			if len(key) == 1 {
				m.filterQuery += key
				m.filesCursor = 0
				m.loadDiff()
			}
		}
		return m, nil
	}

	// Clear pending confirmation when user presses any key other than the confirm key
	if key != "d" && key != "x" {
		m.pendingAction = gitPendingNone
	}
	// esc clears filter first, then multi-selection
	if key == "esc" && m.filterQuery != "" {
		m.filterActive = false
		m.filterQuery = ""
		m.filesCursor = 0
		m.loadDiff()
		return m, nil
	}
	// esc clears multi-selection if active
	if key == "esc" && len(m.selectedFiles) > 0 {
		m.selectedFiles = nil
		return m, nil
	}
	switch key {
	case " ":
		if m.section == gitSectionChanges && m.filesCursor >= 0 {
			m.ensureSelectedFiles()
			if m.selectedFiles[m.filesCursor] {
				delete(m.selectedFiles, m.filesCursor)
			} else {
				m.selectedFiles[m.filesCursor] = true
			}
		}
		return m, nil
	case "shift+down":
		if m.section == gitSectionChanges && m.filesCursor >= 0 {
			m.ensureSelectedFiles()
			files := m.currentFileList()
			if m.filesCursor < len(files)-1 {
				m.selectedFiles[m.filesCursor] = true
				m.filesCursor++
				m.selectedFiles[m.filesCursor] = true
				m.loadDiff()
			}
		}
		return m, nil
	case "shift+up":
		if m.section == gitSectionChanges && m.filesCursor >= 0 {
			m.ensureSelectedFiles()
			if m.filesCursor > 0 {
				m.selectedFiles[m.filesCursor] = true
				m.filesCursor--
				m.selectedFiles[m.filesCursor] = true
				m.loadDiff()
			}
		}
		return m, nil
	case "j", "down":
		if m.section == gitSectionChanges {
			m.selectedFiles = nil // clear selection on plain nav
		}
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
		if m.section == gitSectionChanges {
			m.selectedFiles = nil // clear selection on plain nav
		}
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
		if m.section == gitSectionChanges {
			if len(m.selectedFiles) > 0 {
				unstaged := m.allUnstagedAndUntracked()
				var staged []string
				for idx := range m.selectedFiles {
					unstagedIdx := idx - len(m.stagedFiles)
					if unstagedIdx >= 0 && unstagedIdx < len(unstaged) {
						staged = append(staged, unstaged[unstagedIdx].path)
					}
				}
				if len(staged) > 0 {
					args := append([]string{"add", "--"}, staged...)
					if _, err := m.gitRun(args...); err != nil {
						m.statusMsg = "stage failed: " + err.Error()
						m.logGit("stage failed: " + firstLine(err.Error()))
					} else {
						m.statusMsg = fmt.Sprintf("staged %d files", len(staged))
						m.logGit(fmt.Sprintf("staged %d files", len(staged)))
						m.selectedFiles = nil
						return m, m.cmdRefresh()
					}
				}
			} else if m.filesCursor >= 0 && m.filesCursor >= len(m.stagedFiles) {
				idx := m.filesCursor - len(m.stagedFiles)
				unstaged := m.allUnstagedAndUntracked()
				if idx < len(unstaged) {
					f := unstaged[idx]
					if _, err := m.gitRun("add", "--", f.path); err != nil {
						m.statusMsg = "stage failed: " + err.Error()
						m.logGit("stage failed: " + firstLine(err.Error()))
					} else {
						m.statusMsg = "staged " + f.path
						m.logGit("stage: " + f.path)
						return m, m.cmdRefresh()
					}
				}
			}
		}
	case "u":
		if m.section == gitSectionChanges {
			if len(m.selectedFiles) > 0 {
				var unstaged []string
				for idx := range m.selectedFiles {
					if idx < len(m.stagedFiles) {
						unstaged = append(unstaged, m.stagedFiles[idx].path)
					}
				}
				if len(unstaged) > 0 {
					args := append([]string{"restore", "--staged", "--"}, unstaged...)
					if _, err := m.gitRun(args...); err != nil {
						m.statusMsg = "unstage failed: " + err.Error()
						m.logGit("unstage failed: " + firstLine(err.Error()))
					} else {
						m.statusMsg = fmt.Sprintf("unstaged %d files", len(unstaged))
						m.logGit(fmt.Sprintf("unstaged %d files", len(unstaged)))
						m.selectedFiles = nil
						return m, m.cmdRefresh()
					}
				}
			} else if m.filesCursor >= 0 && m.filesCursor < len(m.stagedFiles) {
				f := m.stagedFiles[m.filesCursor]
				if _, err := m.gitRun("restore", "--staged", "--", f.path); err != nil {
					m.statusMsg = "unstage failed: " + err.Error()
					m.logGit("unstage failed: " + firstLine(err.Error()))
				} else {
					m.statusMsg = "unstaged " + f.path
					m.logGit("unstage: " + f.path)
					return m, m.cmdRefresh()
				}
			}
		}
	case "d":
		if m.section == gitSectionChanges {
			if m.filesCursor >= 0 && m.filesCursor >= len(m.stagedFiles) {
				idx := m.filesCursor - len(m.stagedFiles)
				unstaged := m.allUnstagedAndUntracked()
				if idx < len(unstaged) {
					if m.pendingAction == gitPendingDiscard {
						m.pendingAction = gitPendingNone
						f := unstaged[idx]
						if f.status == "?" {
							m.statusMsg = "cannot discard untracked file"
							return m, nil
						}
						if _, err := m.gitRun("restore", "--", f.path); err != nil {
							m.statusMsg = "discard failed: " + err.Error()
							m.logGit("discard failed: " + firstLine(err.Error()))
						} else {
							m.statusMsg = "discarded " + f.path
							m.logGit("discard: " + f.path)
							return m, m.cmdRefresh()
						}
					} else {
						m.pendingAction = gitPendingDiscard
						m.statusMsg = "press d again to discard"
					}
				}
			} else {
				m.statusMsg = "cannot discard staged file — unstage first"
			}
		} else if m.section == gitSectionStash && m.stashCursor < len(m.stashes) {
			if m.pendingAction == gitPendingDropStash {
				m.pendingAction = gitPendingNone
				ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
				if _, err := m.gitRun("stash", "drop", ref); err != nil {
					m.statusMsg = "drop failed: " + err.Error()
					m.logGit("stash drop failed: " + firstLine(err.Error()))
				} else {
					m.statusMsg = "stash dropped"
					m.logGit("stash drop: " + ref)
					return m, m.cmdRefresh()
				}
			} else {
				m.pendingAction = gitPendingDropStash
				m.statusMsg = "press d again to drop stash"
			}
		}
	case "c":
		if m.section == gitSectionChanges {
			m.committing = true
			m.commitInput.Reset()
			m.commitInput.Focus()
			m.Resize(m.width, m.height)
		}
	case "a":
		if m.section == gitSectionStash && m.stashCursor < len(m.stashes) {
			ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
			if _, err := m.gitRun("stash", "apply", ref); err != nil {
				m.statusMsg = "stash apply failed: " + err.Error()
				m.logGit("stash apply failed: " + firstLine(err.Error()))
			} else {
				m.statusMsg = "stash applied"
				m.logGit("stash apply: " + ref)
				return m, m.cmdRefresh()
			}
		}
	case "i":
		if m.section == gitSectionChanges {
			files := m.currentFileList()
			if len(files) == 0 || m.filesCursor < 0 || m.filesCursor >= len(files) {
				m.statusMsg = "no file selected"
				return m, nil
			}
			return m.ignorePath(files[m.filesCursor].path)
		}
	case "I":
		if m.section == gitSectionChanges {
			m.ignorePathInputMode = true
			m.ignorePathInputText = ""
			m.statusMsg = "ignore path:"
			return m, nil
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
			if m.pendingAction == gitPendingDeleteBranch {
				m.pendingAction = gitPendingNone
				if _, err := m.gitRun("branch", "-d", branch); err != nil {
					m.statusMsg = "delete failed: " + err.Error()
					m.logGit("delete branch failed: " + firstLine(err.Error()))
				} else {
					m.statusMsg = "deleted " + branch
					m.logGit("delete branch: " + branch)
				}
				return m, m.cmdRefresh()
			}
			m.pendingAction = gitPendingDeleteBranch
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
			if m.filesCursor >= 0 && m.filesCursor < len(files) {
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
					m.logGit("checkout failed: " + firstLine(err.Error()))
				} else {
					m.statusMsg = "switched to " + branch
					m.logGit("checkout: " + branch)
					return m, m.cmdRefresh()
				}
			}
		case gitSectionStash:
			if m.stashCursor < len(m.stashes) {
				ref := fmt.Sprintf("stash@{%d}", m.stashCursor)
				if _, err := m.gitRun("stash", "pop", ref); err != nil {
					m.statusMsg = "pop failed: " + err.Error()
					m.logGit("stash pop failed: " + firstLine(err.Error()))
				} else {
					m.statusMsg = "stash popped"
					m.logGit("stash pop: " + ref)
					return m, m.cmdRefresh()
				}
			}
		}
	}
	return m, nil
}

func (m gitModel) openInEditor(path string) tea.Cmd {
	if isBinaryFile(path) {
		log.Printf("[editor] git openInEditor: using system opener for binary file=%q", path)
		return openFileWithOSDefault(path)
	}
	if m.editorOpener != nil {
		log.Printf("[editor] git openInEditor: delegating to editorOpener for file=%q", path)
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
		log.Printf("[editor] git openInEditor: no valid editor command configured")
		return func() tea.Msg { return editorFinishedMsg{err: os.ErrInvalid} }
	}
	cmdParts = append(cmdParts, path)
	c := exec.Command(cmdParts[0], cmdParts[1:]...)
	log.Printf("[editor] git openInEditor fallback: editor=%q file=%q full_cmd=%v", editor, path, cmdParts)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		log.Printf("[editor] git openInEditor fallback finished: editor=%q file=%q err=%v", editor, path, err)
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
		m.logGit("hunk apply failed: " + firstLine(err.Error()))
		return m, nil
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(patch); err != nil {
		m.statusMsg = "hunk apply: " + err.Error()
		m.logGit("hunk apply failed: " + firstLine(err.Error()))
		return m, nil
	}
	tmp.Close()
	args := []string{"apply", "--cached", tmp.Name()}
	if reverse {
		args = []string{"apply", "--cached", "--reverse", tmp.Name()}
	}
	if _, err := m.gitRun(args...); err != nil {
		m.statusMsg = "hunk apply failed: " + err.Error()
		m.logGit("hunk apply failed: " + firstLine(err.Error()))
		return m, nil
	}
	action := "staged hunk"
	if reverse {
		action = "unstaged hunk"
	}
	m.statusMsg = action
	m.logGit(action)
	return m, m.cmdRefresh()
}

func (m *gitModel) currentFileList() []gitFile {
	switch m.section {
	case gitSectionChanges:
		var all []gitFile
		all = append(all, m.stagedFiles...)
		all = append(all, m.allUnstagedAndUntracked()...)
		if m.filterQuery != "" {
			all = fuzzyFilterFunc(all, m.filterQuery, func(f gitFile) string { return f.path })
		}
		return all
	}
	return nil
}

func (m gitModel) ignorePath(path string) (gitModel, tea.Cmd) {
	path = strings.TrimSpace(path)
	if path == "" {
		m.statusMsg = "ignore path required"
		return m, nil
	}
	if err := appendUniqueLine(filepath.Join(m.workDir, ".gitignore"), path+"\n"); err != nil {
		m.statusMsg = "ignore failed: " + err.Error()
		m.logGit("ignore failed: " + firstLine(err.Error()))
		return m, nil
	}
	m.statusMsg = "ignored " + path
	m.logGit("ignore: " + path)
	return m, m.cmdRefresh()
}

func appendUniqueLine(path, line string) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	trimmedLine := strings.TrimRight(line, "\n")
	if len(content) > 0 {
		for _, existing := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
			if strings.TrimSpace(existing) == trimmedLine {
				return nil
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(line)
	return err
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

func (m *gitModel) clearActiveFile() {
	m.filesCursor = -1
	m.setDiffContent("")
	m.hunks = nil
	m.hunkCursor = 0
	m.diffHeader = ""
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
		if m.filesCursor < 0 || m.filesCursor >= len(files) {
			m.setDiffContent("")
			m.hunks = nil
			m.hunkCursor = 0
			m.diffHeader = ""
			return
		}
		f := files[m.filesCursor]
		if f.status == "?" && !f.staged {
			m.loadFilePreview(f)
			return
		}
		var out string
		var err error
		if f.staged {
			out, err = m.gitRun("diff", "--no-color", "--cached", "--", f.path)
		} else {
			out, err = m.gitRun("diff", "--no-color", "--", f.path)
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
		genHint := ""
		if m.generateCommitMsg != nil {
			genHint = "  ctrl+g ✨generate"
		}
		return "ctrl+enter commit" + genHint + "  esc cancel"
	}
	if m.branchInputMode || m.stashInputMode || m.ignorePathInputMode {
		return "enter confirm  esc cancel"
	}
	base := "tab next panel  "
	switch m.panel {
	case gitPanelSections:
		return base + "j/k navigate  enter focus files"
	case gitPanelFiles:
		switch m.section {
		case gitSectionChanges:
			if len(m.selectedFiles) > 0 {
				return base + fmt.Sprintf("%d selected — s stage  u unstage  space toggle  esc clear", len(m.selectedFiles))
			}
			return base + "s stage  u unstage  i ignore file  I ignore path  space/shift+↑↓ select  d discard  E edit  S stash  c commit  / filter  f fetch  p pull  P push"
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

	// Reserve space for commit input when active (textarea height + border)
	commitInputRows := 0
	if m.committing {
		commitInputRows = m.commitInput.Height() + 2
	}
	panelH := h - 4 - commitInputRows
	if panelH < 2 {
		panelH = 2
	}

	focusBorder := func(focused bool) lipgloss.Style {
		if focused {
			return styles.Border.BorderForeground(styles.Selected.GetBackground())
		}
		return styles.Border
	}

	sectionNames := []string{"Changes", "Log", "Stash", "Branches"}
	sectionLines := make([]string, len(sectionNames))
	for i, name := range sectionNames {
		if gitSection(i) == m.section {
			sectionLines[i] = styles.Selected.Width(sectW - 4).Render(name)
		} else {
			sectionLines[i] = name
		}
	}
	sectPane := focusBorder(m.panel == gitPanelSections).Width(sectW - 2).Height(panelH).Render(
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
	if m.filterActive || m.filterQuery != "" {
		cursor := ""
		if m.filterActive {
			cursor = "█"
		}
		filterLine := styles.Hint.Render("/ " + m.filterQuery + cursor)
		filesContent = filterLine + "\n" + filesContent
	}
	filesPane := focusBorder(m.panel == gitPanelFiles).Width(filesW - 2).Height(panelH).Render(filesContent)

	diffSB := renderScrollbar(m.diff.Height(), m.diff.TotalLineCount(), m.diff.VisibleLineCount(), m.diff.YOffset())
	diffContent := lipgloss.JoinHorizontal(lipgloss.Top, m.diff.View(), diffSB)
	diffPane := focusBorder(m.panel == gitPanelDiff).Width(diffW - 2).Height(panelH).Render(diffContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, sectPane, filesPane, diffPane)

	tabBar := renderTabBar(tabGit, chatUnread)
	var exitBtn string
	if exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("✕ exit?")
	} else {
		exitBtn = styles.Hint.Padding(0, 1).Render("✕ exit")
	}
	ab := ""
	if m.aheadBehind != "" {
		ab = "  " + m.aheadBehind
	}
	headerLeft := appHeaderLeftPad + styles.Header.Render("\u25c6 ocode  Git"+ab) + appHeaderHintGap + hintStyle.Render("opencode clone")
	headerPad := w - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar) - lipgloss.Width(exitBtn)
	if headerPad < 0 {
		headerPad = 0
	}
	// Add the standard top-pad row + left gap + thin title/hint gap. Use the
	// chat tab's "opencode clone" hint to stay consistent with model.go.
	renderedHeader := appHeaderTopPad + headerLeft + strings.Repeat(" ", headerPad) + tabBar + exitBtn

	var parts []string
	parts = append(parts, renderedHeader, row)
	if m.committing {
		parts = append(parts, styles.Border.Width(w-2).Render(m.commitInput.View()))
	}
	if m.branchInputMode {
		prompt := styles.Hint.Render("New branch: ") + m.branchInputText + "█"
		parts = append(parts, styles.Border.Width(sectW+filesW-2).Render(prompt))
	}
	if m.stashInputMode {
		prompt := styles.Hint.Render("Stash message: ") + m.stashInputText + "█"
		parts = append(parts, styles.Border.Width(sectW+filesW-2).Render(prompt))
	}
	if m.ignorePathInputMode {
		prompt := styles.Hint.Render("Ignore path: ") + m.ignorePathInputText + "█"
		parts = append(parts, styles.Border.Width(sectW+filesW-2).Render(prompt))
	}
	hints := styles.Hint.Render(m.renderHints())
	var statusBar string
	if m.filterActive || m.filterQuery != "" {
		cursor := ""
		if m.filterActive {
			cursor = "█"
		}
		filterStr := styles.Selected.Render("/ "+m.filterQuery+cursor) + "  " + styles.Hint.Render("esc clear")
		statusBar = filterStr + "   " + hints
	} else {
		statusBar = hints
		if m.statusMsg != "" {
			statusBar = hints + "   " + errorStyle.Render(m.statusMsg)
		}
	}
	parts = append(parts, statusBar)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m gitModel) renderFileList(width int) []string {
	var lines []string
	switch m.section {
	case gitSectionChanges:
		if m.filterQuery != "" {
			// Filtered: flat list, no staged/unstaged headers
			filtered := m.currentFileList()
			for i, f := range filtered {
				line := "  " + f.status + " " + f.path
				if i == m.filesCursor && m.panel == gitPanelFiles {
					line = selectedStyle.Width(width).Render(line)
				}
				lines = append(lines, line)
			}
			break
		}
		idx := 0
		checkmark := func(i int) string {
			if m.selectedFiles[i] {
				return "◆ "
			}
			return "  "
		}
		if len(m.stagedFiles) > 0 {
			lines = append(lines, hintStyle.Render("● staged"))
			for _, f := range m.stagedFiles {
				line := checkmark(idx) + f.status + " " + f.path
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
				line := checkmark(idx) + f.status + " " + f.path
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
