package tui

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	gitPendingMergeBranch  gitPendingAction = "merge-branch"
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

type diffReadyMsg struct {
	seq     int
	workDir string
	content string
	hunks   []diffHunk
	header  string
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
	// diffLineMap maps a diff viewport line index (0-based) to the
	// corresponding source file line number (1-based), or 0 if the line has
	// no source counterpart (headers, removed lines, notices).
	diffLineMap []int
	// external editor integration
	editor       string
	editorOpener func(string) tea.Cmd
	// editorOpenerAtLine is like editorOpener but opens the file at a line.
	editorOpenerAtLine func(string, int) tea.Cmd
	// ai commit message generation
	generateCommitMsg func(diff string) tea.Cmd
	generatingMsg     bool
	// async diff loading
	diffLoadSeq int
	diffLoading bool
	// log section: toggle between stat and full diff
	logFullDiff bool
	// file list filter
	filterActive bool
	filterQuery  string
	// ListBox instances for each section (replaces fileListScroll, commitViewport scroll, etc.)
	changesList  *ListBox
	logList      *ListBox
	stashList    *ListBox
	branchesList *ListBox
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

// activeDiffLineMap mirrors the source-line map of the currently displayed git
// diff so the diff gutter (which only receives a GutterContext, not the model)
// can render real source line numbers. The TUI renders at most one git diff at
// a time, so a single package-level mirror is safe; it is rewritten on every
// diff content load.
var activeDiffLineMap []int

// gitDiffLineGutter renders the source-file line number for each diff viewport
// line. Lines with no source counterpart (headers, removed lines, notices)
// show a blank gutter so they don't imply a jumpable line.
func gitDiffLineGutter(ctx viewport.GutterContext) string {
	if ctx.Soft {
		return "     │ "
	}
	if ctx.Index >= len(activeDiffLineMap) {
		return "   ~ │ "
	}
	ln := activeDiffLineMap[ctx.Index]
	if ln <= 0 {
		return "     │ "
	}
	return fmt.Sprintf("%4d │ ", ln)
}

func newGitModel(workDir string) (gitModel, tea.Cmd) {
	m := gitModel{workDir: workDir}
	m.diff = viewport.New()
	m.diff.SoftWrap = true
	m.diff.LeftGutterFunc = gitDiffLineGutter
	m.commitViewport = viewport.New()
	ci := textarea.New()
	ci.Placeholder = "Commit message..."
	ci.SetHeight(5)
	m.commitInput = ci
	// Initialize ListBox instances for each section
	m.changesList = NewListBox(0, 0)   // size set in Resize
	m.logList = NewListBox(0, 0)
	m.stashList = NewListBox(0, 0)
	m.branchesList = NewListBox(0, 0)
	if _, err := m.gitRun("rev-parse", "--git-dir"); err != nil {
		m.statusMsg = "not a git repository"
		return m, nil
	}
	m.refresh()
	st := currentStyles()
	return m, m.startLoadDiff(st)
}

func (m *gitModel) SetEditor(e string) { m.editor = e }

func (m *gitModel) SetEditorOpener(fn func(string) tea.Cmd) { m.editorOpener = fn }

func (m *gitModel) SetEditorOpenerAtLine(fn func(string, int) tea.Cmd) { m.editorOpenerAtLine = fn }

// setChangesListScrollOffset safely sets the scroll offset on changesList if it's not nil.
func (m *gitModel) setChangesListScrollOffset(offset int) {
	if m.changesList != nil {
		m.changesList.SetScrollOffset(offset)
	}
}

// ensureChangesListCursorVisible safely ensures the cursor is visible in changesList if it's not nil.
func (m *gitModel) ensureChangesListCursorVisible() {
	if m.changesList != nil {
		m.changesList.EnsureVisible(m.filesCursor)
	}
}

// ensureLogListCursorVisible safely ensures the cursor is visible in logList if it's not nil.
func (m *gitModel) ensureLogListCursorVisible() {
	if m.logList != nil {
		m.logList.EnsureVisible(m.commitCursor)
	}
}

// ensureStashListCursorVisible safely ensures the cursor is visible in stashList if it's not nil.
func (m *gitModel) ensureStashListCursorVisible() {
	if m.stashList != nil {
		m.stashList.EnsureVisible(m.stashCursor)
	}
}

// ensureBranchesListCursorVisible safely ensures the cursor is visible in branchesList if it's not nil.
func (m *gitModel) ensureBranchesListCursorVisible() {
	if m.branchesList != nil {
		m.branchesList.EnsureVisible(m.branchCursor)
	}
}

// getOrCreateListBox returns the ListBox for the given section, creating it if necessary.
func (m *gitModel) getOrCreateListBox(section gitSection, width, height int) *ListBox {
	var lb *ListBox
	switch section {
	case gitSectionChanges:
		if m.changesList == nil {
			m.changesList = NewListBox(width, height)
		}
		lb = m.changesList
	case gitSectionLog:
		if m.logList == nil {
			m.logList = NewListBox(width, height)
		}
		lb = m.logList
	case gitSectionStash:
		if m.stashList == nil {
			m.stashList = NewListBox(width, height)
		}
		lb = m.stashList
	case gitSectionBranches:
		if m.branchesList == nil {
			m.branchesList = NewListBox(width, height)
		}
		lb = m.branchesList
	}
	return lb
}

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
	// The diff/commit viewport widths must match the inner content width of
	// their bordered panes so lipgloss does not word-wrap diff lines. Each
	// pane's outer width is sectW/filesW/diffW; the inner content area is that
	// minus the 1-char border (each side) and 1-char padding (each side) = 4,
	// and the scrollbar takes 1 more char. So inner text width = paneW - 5.
	diffH := h - 6 - commitInputRows
	if diffH < 1 {
		diffH = 1
	}
	m.diff.SetWidth(diffW - 5)
	m.diff.SetHeight(diffH)
	m.commitViewport.SetWidth(filesW - 5)
	m.commitViewport.SetHeight(h - 5 - commitInputRows)
	// full width minus border
	m.commitInput.SetWidth(w - 4)
	
	// Set ListBox sizes for all sections
	// Files pane content width: filesW - 4 (border 2 + padding 2)
	// Height: h - 4 (header) - commitInputRows
	filesListW := filesW - 4
	filesListH := h - 4 - commitInputRows
	if filesListH < 1 {
		filesListH = 1
	}
	if m.changesList != nil {
		m.changesList.SetSize(filesListW, filesListH)
	}
	if m.logList != nil {
		m.logList.SetSize(filesListW, filesListH)
	}
	if m.stashList != nil {
		m.stashList.SetSize(filesListW, filesListH)
	}
	if m.branchesList != nil {
		m.branchesList.SetSize(filesListW, filesListH)
	}
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
	return gitRunInDir(m.workDir, args...)
}

// gitRunInDir is a package-level helper used by async goroutines that cannot
// safely access the gitModel receiver. It mirrors gitRun's behaviour exactly.
func gitRunInDir(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\r\n"), err
}

func (m *gitModel) gitRunTimeout(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.workDir
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\r\n"), err
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
	case diffReadyMsg:
		if msg.seq != m.diffLoadSeq || msg.workDir != m.workDir {
			return m, nil // stale, discard
		}
		m.diffLoading = false
		m.setDiffContent(msg.content)
		m.diff.GotoTop()
		m.hunks = msg.hunks
		m.hunkCursor = 0
		m.diffHeader = msg.header
		return m, nil
	case gitCommitMsgMsg:
		m.generatingMsg = false
		if msg.err != nil {
			m.statusMsg = "generate failed: " + msg.err.Error()
		} else {
			m.commitInput.SetValue(msg.text)
			m.statusMsg = "✦ generated"
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
		m.clampFileListScroll()
		// During auto-refresh, skip diff reload to preserve scroll position
		// and any active text selection.
		if !msg.autoRefresh {
			st := currentStyles()
			return m, m.startLoadDiff(st)
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
				m.statusMsg = "✦ generating..."
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
	case "ctrl+r":
		return m, m.cmdRefresh()
	case "ctrl+f":
		if m.section == gitSectionChanges {
			m.filterActive = true
			m.filterQuery = ""
			m.filesCursor = 0
			m.setChangesListScrollOffset(0)
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
	if m.changesList != nil {
		m.setChangesListScrollOffset(0)
	}
	m.commitCursor = 0
	m.stashCursor = 0
	m.branchCursor = 0
	if m.logList != nil {
		m.logList.SetScrollOffset(0)
	}
	if m.stashList != nil {
		m.stashList.SetScrollOffset(0)
	}
	if m.branchesList != nil {
		m.branchesList.SetScrollOffset(0)
	}
}

func (m gitModel) handleSectionKey(key string) (gitModel, tea.Cmd) {
	sections := []gitSection{gitSectionChanges, gitSectionLog, gitSectionStash, gitSectionBranches}
	cur := int(m.section)
	switch key {
	case "j", "down":
		if cur < len(sections)-1 {
			m.section = sections[cur+1]
			m.resetCursors()
			st := currentStyles()
			return m, m.startLoadDiff(st)
		}
	case "k", "up":
		if cur > 0 {
			m.section = sections[cur-1]
			m.resetCursors()
			st := currentStyles()
			return m, m.startLoadDiff(st)
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
			m.setChangesListScrollOffset(0)
			st := currentStyles()
			return m, m.startLoadDiff(st)
		case "backspace":
			if len(m.filterQuery) > 0 {
				m.filterQuery = m.filterQuery[:len(m.filterQuery)-1]
				m.filesCursor = 0
				m.setChangesListScrollOffset(0)
				st := currentStyles()
				return m, m.startLoadDiff(st)
			}
		case "enter":
			m.filterActive = false
		default:
			if len(key) == 1 {
				m.filterQuery += key
				m.filesCursor = 0
				m.setChangesListScrollOffset(0)
				st := currentStyles()
				return m, m.startLoadDiff(st)
			}
		}
		return m, nil
	}

	// Clear pending confirmation when user presses any key other than the confirm key
	if key != "ctrl+d" && key != "ctrl+x" {
		m.pendingAction = gitPendingNone
	}
	// esc clears filter first, then multi-selection
	if key == "esc" && m.filterQuery != "" {
		m.filterActive = false
		m.filterQuery = ""
		m.filesCursor = 0
		m.setChangesListScrollOffset(0)
		st := currentStyles()
		return m, m.startLoadDiff(st)
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
				st := currentStyles()
				return m, m.startLoadDiff(st)
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
				st := currentStyles()
				return m, m.startLoadDiff(st)
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
				m.clampFileListScroll()
				st := currentStyles()
				return m, m.startLoadDiff(st)
			}
		case gitSectionLog:
			if m.commitCursor < len(m.commits)-1 {
				m.commitCursor++
				m.logFullDiff = false
				if m.commitCursor >= m.commitViewport.YOffset()+m.commitViewport.VisibleLineCount() {
					m.commitViewport.ScrollDown(1)
				}
				m.ensureLogListCursorVisible()
				st := currentStyles()
				cmd := m.startLoadDiff(st)
				if !m.loadingLog && m.logsMore && m.commitCursor >= len(m.commits)-5 {
					m.loadingLog = true
					return m, tea.Batch(cmd, m.cmdLoadMoreLog())
				}
				return m, cmd
			}
		case gitSectionStash:
			if m.stashCursor < len(m.stashes)-1 {
				m.stashCursor++
				m.ensureStashListCursorVisible()
				st := currentStyles()
				return m, m.startLoadDiff(st)
			}
		case gitSectionBranches:
			if m.branchCursor < len(m.branches)-1 {
				m.branchCursor++
				m.ensureBranchesListCursorVisible()
				st := currentStyles()
				return m, m.startLoadDiff(st)
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
				m.clampFileListScroll()
				st := currentStyles()
				return m, m.startLoadDiff(st)
			}
		case gitSectionLog:
			if m.commitCursor > 0 {
				m.commitCursor--
				m.logFullDiff = false
				if m.commitCursor < m.commitViewport.YOffset() {
					m.commitViewport.ScrollUp(1)
				}
				m.ensureLogListCursorVisible()
				st := currentStyles()
				return m, m.startLoadDiff(st)
			}
		case gitSectionStash:
			if m.stashCursor > 0 {
				m.stashCursor--
				m.ensureStashListCursorVisible()
				st := currentStyles()
				return m, m.startLoadDiff(st)
			}
		case gitSectionBranches:
			if m.branchCursor > 0 {
				m.branchCursor--
				m.ensureBranchesListCursorVisible()
				st := currentStyles()
				return m, m.startLoadDiff(st)
			}
		}
	case "ctrl+s":
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
	case "ctrl+u":
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
	case "ctrl+d":
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
						m.statusMsg = "press ctrl+d again to discard"
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
				m.statusMsg = "press ctrl+d again to drop stash"
			}
		}
	case "ctrl+\\":
		if m.section == gitSectionChanges {
			m.committing = true
			m.commitInput.Reset()
			m.commitInput.Focus()
			m.Resize(m.width, m.height)
		}
	case "ctrl+a":
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
	case "ctrl+l":
		if m.section == gitSectionChanges {
			files := m.currentFileList()
			if len(files) == 0 || m.filesCursor < 0 || m.filesCursor >= len(files) {
				m.statusMsg = "no file selected"
				return m, nil
			}
			return m.ignorePath(files[m.filesCursor].path)
		}
	case "ctrl+_":
		if m.section == gitSectionChanges {
			m.ignorePathInputMode = true
			m.ignorePathInputText = ""
			m.statusMsg = "ignore path:"
			return m, nil
		}
	case "ctrl+g":
		if m.section == gitSectionChanges || m.section == gitSectionBranches {
			m.statusMsg = "fetching..."
			return m, m.cmdNetworkOp("fetched", "fetch failed", "fetch", "--all")
		}
	case "ctrl+p":
		if m.section == gitSectionChanges || m.section == gitSectionBranches {
			m.statusMsg = "pulling..."
			return m, m.cmdNetworkOp("pulled", "pull failed", "pull")
		}
	case "ctrl+o":
		if m.section == gitSectionChanges || m.section == gitSectionBranches {
			m.statusMsg = "pushing..."
			return m, m.cmdNetworkOp("pushed", "push failed", "push")
		}
	case "ctrl+n":
		if m.section == gitSectionBranches {
			m.branchInputMode = true
			m.branchInputText = ""
			m.statusMsg = "new branch name:"
		}
	case "ctrl+x":
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
			m.statusMsg = "press ctrl+x again to delete " + branch
		}
	case "ctrl+z":
		if m.section == gitSectionChanges {
			m.stashInputMode = true
			m.stashInputText = ""
			m.statusMsg = "stash message (optional, enter to confirm):"
		}
	case "ctrl+e":
		if m.section == gitSectionChanges {
			files := m.currentFileList()
			if m.filesCursor >= 0 && m.filesCursor < len(files) {
				path := filepath.Join(m.workDir, files[m.filesCursor].path)
				m.statusMsg = "opening editor..."
				return m, m.openInEditor(path)
			}
		}
	case "ctrl+m":
		if m.section == gitSectionBranches && m.branchCursor < len(m.branches) {
			branch := m.branches[m.branchCursor]
			if branch == m.currentBranch {
				m.statusMsg = "cannot merge current branch into itself"
				return m, nil
			}
			if m.pendingAction == gitPendingMergeBranch {
				m.pendingAction = gitPendingNone
				if _, err := m.gitRun("merge", branch); err != nil {
					m.statusMsg = "merge failed: " + err.Error()
					m.logGit("merge failed: " + firstLine(err.Error()))
				} else {
					m.statusMsg = "merged " + branch
					m.logGit("merge: " + branch)
					return m, m.cmdRefresh()
				}
			} else {
				m.pendingAction = gitPendingMergeBranch
				m.statusMsg = "press ctrl+m again to merge " + branch + " into " + m.currentBranch
			}
		}
	case "enter":
		switch m.section {
		case gitSectionLog:
			m.logFullDiff = !m.logFullDiff
			st := currentStyles()
			return m, m.startLoadDiff(st)
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

// openInEditorAtLine opens the file in the external editor jump to lineNo.
// lineNo <= 0 falls back to a plain open (best-effort per editor support).
func (m gitModel) openInEditorAtLine(path string, lineNo int) tea.Cmd {
	if isBinaryFile(path) {
		log.Printf("[editor] git openInEditorAtLine: binary file=%q, using system opener", path)
		return openFileWithOSDefault(path)
	}
	if m.editorOpenerAtLine != nil {
		log.Printf("[editor] git openInEditorAtLine: delegating to editorOpenerAtLine for file=%q line=%d", path, lineNo)
		return m.editorOpenerAtLine(path, lineNo)
	}
	// Fallback: build the command directly with line support.
	editor := m.editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	args := editorArgsWithLine(editor, path, lineNo)
	if args == nil {
		args = append(strings.Fields(editor), path)
	}
	c := exec.Command(args[0], args[1:]...)
	log.Printf("[editor] git openInEditorAtLine fallback: editor=%q file=%q line=%d full_cmd=%v", editor, path, lineNo, args)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		log.Printf("[editor] git openInEditorAtLine fallback finished: file=%q line=%d err=%v", path, lineNo, err)
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
	case "ctrl+s":
		if m.section == gitSectionChanges && m.hunkCursor < len(m.hunks) {
			return m.applyHunk(false)
		}
	case "ctrl+u":
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
	m.diffLineMap = buildDiffLineMap(m.diffRawLines)
	activeDiffLineMap = m.diffLineMap
}

// buildDiffLineMap maps each displayed diff line (index into rawLines) to the
// corresponding source file line number (1-based). For added/context lines the
// number comes from the enclosing hunk header's new-file start; removed lines
// map to the next surviving new-file line; everything else maps to 0.
func buildDiffLineMap(rawLines []string) []int {
	out := make([]int, len(rawLines))
	newLine := 0
	seenHeader := false
	for i, line := range rawLines {
		switch {
		case strings.HasPrefix(line, "@@"):
			if n, ok := parseHunkNewStart(line); ok {
				newLine = n
			}
			seenHeader = true
			out[i] = 0
		case strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"),
			strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "Binary "), strings.HasPrefix(line, "old mode"),
			strings.HasPrefix(line, "new mode"), strings.HasPrefix(line, "rename "),
			strings.HasPrefix(line, "similarity "), strings.HasPrefix(line, "dissimilarity "):
			out[i] = 0
		case !seenHeader:
			// Preamble or an untracked-file preview (raw file content): there is
			// no hunk structure, so treat each line as a plain file line.
			out[i] = i + 1
		case len(line) == 0:
			out[i] = 0
		case line[0] == '+' || line[0] == ' ':
			out[i] = newLine
			newLine++
		case line[0] == '-':
			// Removed line: jump to the next surviving new-file line.
			out[i] = newLine
		default:
			// e.g. "\ No newline at end of file"
			out[i] = 0
		}
	}
	return out
}

// parseHunkNewStart extracts the new-file start line from a hunk header such as
// "@@ -3,3 +3,2 @@".
func parseHunkNewStart(header string) (int, bool) {
	plus := strings.IndexByte(header, '+')
	if plus < 0 {
		return 0, false
	}
	rest := header[plus+1:]
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		end = len(rest)
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0, false
	}
	return n, true
}

// currentDiffFilePath returns the absolute path of the file whose diff is
// currently shown, or "" when there is no single resolvable file (e.g. a commit
// full-diff spanning multiple files).
func (m *gitModel) currentDiffFilePath() string {
	if m.section != gitSectionChanges {
		return ""
	}
	files := m.currentFileList()
	if m.filesCursor < 0 || m.filesCursor >= len(files) {
		return ""
	}
	return filepath.Join(m.workDir, files[m.filesCursor].path)
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

// diffSizeLimit is the maximum byte length of a raw diff that will be fully
// rendered. Diffs larger than this are truncated before processing to prevent
// renderUnifiedDiff + viewport.SetContent from blocking the TUI event loop.
const diffSizeLimit = 256 * 1024 // 256 KB

// capDiff truncates raw diff output to diffSizeLimit and appends a notice.
func capDiff(out string) string {
	if len(out) <= diffSizeLimit {
		return out
	}
	// Find the last newline within the limit so we don't cut mid-line.
	cut := strings.LastIndexByte(out[:diffSizeLimit], '\n')
	if cut <= 0 {
		cut = diffSizeLimit
	}
	return out[:cut] + "\n\n[diff truncated — too large to display fully]"
}

// startLoadDiff replaces the old synchronous loadDiff. It increments the load
// sequence, shows a "loading…" placeholder immediately, and returns a tea.Cmd
// that runs the git/file-read work off the event loop. The result arrives as a
// diffReadyMsg which Update handles; stale messages (seq mismatch) are dropped.
func (m *gitModel) startLoadDiff(styles Styles) tea.Cmd {
	m.diffLoadSeq++
	m.diffLoading = true
	m.hunks = nil
	m.hunkCursor = 0
	m.diffHeader = ""
	m.setDiffContent(hintStyle.Render("loading…"))

	// Capture everything the goroutine needs from m before spawning.
	seq := m.diffLoadSeq
	section := m.section
	workDir := m.workDir
	filesCursor := m.filesCursor
	commitCursor := m.commitCursor
	stashCursor := m.stashCursor
	branchCursor := m.branchCursor
	logFullDiff := m.logFullDiff
	files := m.currentFileList()
	commits := m.commits
	stashes := m.stashes
	branches := m.branches

	return func() tea.Msg {
		switch section {
		case gitSectionChanges:
			if filesCursor < 0 || filesCursor >= len(files) {
				return diffReadyMsg{seq: seq, workDir: workDir, content: "", hunks: nil, header: ""}
			}
			f := files[filesCursor]
			if f.status == "?" && !f.staged {
				// File preview path — read at most 1MB to avoid blocking on huge files.
				const previewReadLimit = 1024 * 1024
				path := filepath.Join(workDir, f.path)
				fh, err := os.Open(path)
				if err != nil {
					return diffReadyMsg{seq: seq, workDir: workDir, content: "error: " + err.Error()}
				}
				data, err := io.ReadAll(io.LimitReader(fh, previewReadLimit+1))
				fh.Close()
				if err != nil {
					return diffReadyMsg{seq: seq, workDir: workDir, content: "error: " + err.Error()}
				}
				probe := data
				if len(probe) > 512 {
					probe = probe[:512]
				}
				if strings.ContainsRune(string(probe), '\x00') {
					return diffReadyMsg{seq: seq, workDir: workDir, content: "[binary file]"}
				}
				previewTooLarge := len(data) > previewReadLimit
				if previewTooLarge {
					data = data[:previewReadLimit]
				}
				// Cap syntax highlighting to 64KB; chroma is slow on large buffers.
				content := string(data)
				highlightSrc := content
				const highlightLimit = 64 * 1024
				if len(highlightSrc) > highlightLimit {
					highlightSrc = highlightSrc[:highlightLimit]
				}
				header := hintStyle.Render("Preview: "+f.path+"  (E edit)") + "\n\n"
				notice := ""
				if previewTooLarge || len(content) > highlightLimit {
					notice = styles.Hint.Render("[truncated — 1MB limit]") + "\n\n"
				}
				return diffReadyMsg{seq: seq, workDir: workDir, content: header + notice + highlightContent(highlightSrc, languageForPath(f.path))}
			}
			// Git diff path
			var out string
			var err error
			if f.staged {
				out, err = gitRunInDir(workDir, "diff", "--no-color", "--cached", "--", f.path)
			} else {
				out, err = gitRunInDir(workDir, "diff", "--no-color", "--", f.path)
			}
			if err != nil {
				out = "error: " + err.Error()
			}
			out = capDiff(out)
			rendered := renderUnifiedDiff(out, styles)
			hunks := parseHunks(out)
			hdr := extractDiffHeader(out)
			return diffReadyMsg{seq: seq, workDir: workDir, content: rendered, hunks: hunks, header: hdr}

		case gitSectionLog:
			if commitCursor >= len(commits) {
				return diffReadyMsg{seq: seq, workDir: workDir, content: ""}
			}
			c := commits[commitCursor]
			var out string
			var err error
			if logFullDiff {
				out, err = gitRunInDir(workDir, "show", "--no-color", c.hash)
			} else {
				out, err = gitRunInDir(workDir, "show", "--no-color", "--stat", c.hash)
			}
			if err != nil {
				out = "error: " + err.Error()
			}
			out = capDiff(out)
			rendered := renderUnifiedDiff(out, styles)
			var hunks []diffHunk
			var hdr string
			if logFullDiff {
				hunks = parseHunks(out)
				hdr = extractDiffHeader(out)
			}
			return diffReadyMsg{seq: seq, workDir: workDir, content: rendered, hunks: hunks, header: hdr}

		case gitSectionStash:
			if stashCursor >= len(stashes) {
				return diffReadyMsg{seq: seq, workDir: workDir, content: ""}
			}
			ref := fmt.Sprintf("stash@{%d}", stashCursor)
			out, err := gitRunInDir(workDir, "stash", "show", "--no-color", "-p", ref)
			if err != nil {
				out = "error: " + err.Error()
			}
			out = capDiff(out)
			rendered := renderUnifiedDiff(out, styles)
			return diffReadyMsg{seq: seq, workDir: workDir, content: rendered}

		case gitSectionBranches:
			if branchCursor >= len(branches) {
				return diffReadyMsg{seq: seq, workDir: workDir, content: ""}
			}
			out, err := gitRunInDir(workDir, "log", "--oneline", "-20", branches[branchCursor])
			if err != nil {
				out = "error: " + err.Error()
			}
			return diffReadyMsg{seq: seq, workDir: workDir, content: out}
		}
		return diffReadyMsg{seq: seq, workDir: workDir}
	}
}

func (m gitModel) renderHints() string {
	if m.committing {
		genHint := ""
		if m.generateCommitMsg != nil {
			genHint = "  ctrl+g ✦generate"
		}
		return "ctrl+\\ commit" + genHint + "  esc cancel"
	}
	if m.branchInputMode || m.stashInputMode || m.ignorePathInputMode {
		return "enter confirm  esc cancel"
	}
	base := "tab next panel  "
	switch m.panel {
	case gitPanelSections:
		return base + "j/k navigate  enter focus files  ctrl+r refresh"
	case gitPanelFiles:
		switch m.section {
		case gitSectionChanges:
			if len(m.selectedFiles) > 0 {
				return base + fmt.Sprintf("%d selected — ctrl+s stage  ctrl+u unstage  space toggle  esc clear  ctrl+r refresh", len(m.selectedFiles))
			}
			return base + "ctrl+s stage  ctrl+u unstage  ctrl+l ignore file  ctrl+_ ignore path  space/shift+↑↓ select  ctrl+d discard  ctrl+e edit  ctrl+z stash  ctrl+a apply  ctrl+\\ commit  ctrl+f filter  ctrl+r refresh  ctrl+g fetch  ctrl+p pull  ctrl+o push"
		case gitSectionLog:
			if m.logFullDiff {
				return base + "j/k navigate  enter stat view  ctrl+r refresh"
			}
			return base + "j/k navigate  enter full diff  ctrl+r refresh"
		case gitSectionStash:
			return base + "enter pop  ctrl+a apply  ctrl+d drop  ctrl+r refresh"
		case gitSectionBranches:
			return base + "enter checkout  ctrl+n new  ctrl+m merge  ctrl+x delete  ctrl+r refresh  ctrl+g fetch  ctrl+p pull  ctrl+o push"
		}
	case gitPanelDiff:
		if m.section == gitSectionChanges {
			return base + "j/k scroll  [/] prev/next hunk  ctrl+s stage hunk  ctrl+u unstage hunk  ctrl+r refresh"
		}
		return base + "j/k scroll  ctrl+r refresh"
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
	sectPane := focusBorder(m.panel == gitPanelSections).Width(sectW).Height(panelH).Render(
		strings.Join(sectionLines, "\n"),
	)

	var filesContent string
	fileLines := m.renderFileList(filesW - 4)
	filesContent = strings.Join(fileLines, "\n")
	filesPane := focusBorder(m.panel == gitPanelFiles).Width(filesW).Height(panelH).Render(filesContent)

	diffSB := renderScrollbar(m.diff.Height(), m.diff.TotalLineCount(), m.diff.VisibleLineCount(), m.diff.YOffset())
	diffContent := lipgloss.JoinHorizontal(lipgloss.Top, m.diff.View(), diffSB)
	diffPane := focusBorder(m.panel == gitPanelDiff).Width(diffW).Height(panelH).Render(diffContent)

	row := lipgloss.JoinHorizontal(lipgloss.Top, sectPane, filesPane, diffPane)

	tabBar := renderTabBar(tabGit, chatUnread)
	var exitBtn string
	if exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(errorStyle.GetForeground()).Padding(0, 1).Render("✕ exit?")
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
		filterStr := styles.Selected.Render("ctrl+f "+m.filterQuery+cursor) + "  " + styles.Hint.Render("esc clear")
		statusBar = filterStr + "   " + hints
	} else {
		statusBar = hints
		if m.statusMsg != "" {
			statusBar = hints + "   " + errorStyle.Render(m.statusMsg)
		}
	}
	statusBar = lipgloss.NewStyle().Width(w).MaxHeight(1).Render(statusBar)
	parts = append(parts, statusBar)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// fileListVisibleRows returns the number of content rows visible inside the
// files pane (borders and filter bar excluded). This is used by both
// renderFileList (to slice the output) and clampFileListScroll (to keep the
// cursor in view).
func (m gitModel) fileListVisibleRows() int {
	panelH := m.height - 4
	if panelH < 4 {
		panelH = 4
	}
	vis := panelH - 2 // top + bottom borders
	if m.filterQuery != "" {
		vis-- // filter bar
	}
	if vis < 1 {
		vis = 1
	}
	return vis
}

// clampFileListScroll ensures the cursor is visible in the changes list.
// This is now a thin wrapper around the ListBox's EnsureVisible.
func (m *gitModel) clampFileListScroll() {
	if m.section != gitSectionChanges {
		return
	}
	files := m.currentFileList()
	if len(files) == 0 {
		if m.changesList != nil {
			m.setChangesListScrollOffset(0)
		}
		return
	}
	// Ensure the cursor is visible
	if m.changesList != nil {
		m.ensureChangesListCursorVisible()
	}
}

func (m *gitModel) renderFileList(width int) []string {
	// Configure and render the appropriate ListBox for the current section
	var lb *ListBox
	switch m.section {
	case gitSectionChanges:
		lb = m.changesList
	case gitSectionLog:
		lb = m.logList
	case gitSectionStash:
		lb = m.stashList
	case gitSectionBranches:
		lb = m.branchesList
	}
	
	// Initialize ListBox if nil (for tests or uninitialized models)
	if lb == nil {
		lb = NewListBox(width, m.height-4)
		switch m.section {
		case gitSectionChanges:
			m.changesList = lb
		case gitSectionLog:
			m.logList = lb
		case gitSectionStash:
			m.stashList = lb
		case gitSectionBranches:
			m.branchesList = lb
		}
	}
	
	// Set up data and render based on section
	switch m.section {
	case gitSectionChanges:
		files := m.currentFileList()
		
		// Set filter bar if active
		if m.filterQuery != "" || m.filterActive {
			cursor := ""
			if m.filterActive {
				cursor = "█"
			}
			lb.SetFilterRow("ctrl+f " + m.filterQuery + cursor)
		} else {
			lb.SetFilterRow("")
		}
		
		if m.filterQuery != "" {
			// Filtered: flat list, no headers
			lb.SetHeaderRows(nil)
			lb.SetData(len(files), func(idx, w int, selected bool) string {
				line := "  " + files[idx].status + " " + files[idx].path
				if selected && m.panel == gitPanelFiles {
					line = selectedStyle.Width(w).Render(line)
				}
				return line
			})
		} else {
			// Unfiltered: headers for staged/unstaged
			var headers []string
			if len(m.stagedFiles) > 0 {
				headers = append(headers, hintStyle.Render("● staged"))
			}
			if len(m.unstagedFiles)+len(m.untrackedFiles) > 0 {
				headers = append(headers, hintStyle.Render("○ unstaged/untracked"))
			}
			lb.SetHeaderRows(headers)
			
			checkmark := func(i int) string {
				if m.selectedFiles[i] {
					return "◆ "
				}
				return "  "
			}
			
			lb.SetData(len(files), func(idx, w int, selected bool) string {
				line := checkmark(idx) + files[idx].status + " " + files[idx].path
				if selected && m.panel == gitPanelFiles {
					line = selectedStyle.Width(w).Render(line)
				}
				return line
			})
		}
		// SetSelectedForRender: this runs on every render (including
		// after a mouse-wheel scroll), so it must not force EnsureVisible
		// — that would snap independent wheel scroll back to the cursor.
		lb.SetSelectedForRender(m.filesCursor)

	case gitSectionLog:
		lb.SetHeaderRows(nil)
		lb.SetFilterRow("")
		lb.SetData(len(m.commits), func(idx, w int, selected bool) string {
			c := m.commits[idx]
			line := fmt.Sprintf("%s  %s  %s", c.hash, c.subject, c.age)
			if selected {
				line = selectedStyle.Width(w).Render(line)
			}
			return line
		})
		lb.SetSelectedForRender(m.commitCursor)

	case gitSectionStash:
		lb.SetHeaderRows(nil)
		lb.SetFilterRow("")
		lb.SetData(len(m.stashes), func(idx, w int, selected bool) string {
			line := m.stashes[idx]
			if selected {
				line = selectedStyle.Width(w).Render(line)
			}
			return line
		})
		lb.SetSelectedForRender(m.stashCursor)

	case gitSectionBranches:
		lb.SetHeaderRows(nil)
		lb.SetFilterRow("")
		lb.SetData(len(m.branches), func(idx, w int, selected bool) string {
			b := m.branches[idx]
			marker := "  "
			if b == m.currentBranch {
				marker = "* "
			}
			line := marker + b
			if selected {
				line = selectedStyle.Width(w).Render(line)
			}
			return line
		})
		lb.SetSelectedForRender(m.branchCursor)
	}
	
	// Render and split into lines
	rendered := lb.Render()
	return strings.Split(rendered, "\n")
}
