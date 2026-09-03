package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TestGitFileListKeyboardScrollsToBottom is the regression test for the git
// tab file list failing to scroll: keyboard navigation must be able to reach
// the last file, and the rendered tab must fit the terminal height.
func TestGitFileListKeyboardScrollsToBottom(t *testing.T) {
	ApplyThemeColors("tokyonight")
	dir := t.TempDir() // not a git repo: no background refresh to pollute counts
	m, _ := newGitModel(dir)
	m.section = gitSectionChanges
	m.panel = gitPanelFiles
	for i := 0; i < 40; i++ {
		m.unstagedFiles = append(m.unstagedFiles, gitFile{status: "M", path: fmt.Sprintf("file_%02d.go", i)})
	}
	const w, h = 100, 24
	m.Resize(w, h)
	// Mimic production: every keypress is followed by a re-render, which is
	// what feeds the ListBox its item count for EnsureVisible.
	innerW, _ := m.filesListInnerDims()
	_ = m.renderFileList(innerW)
	for i := 0; i < 60; i++ {
		nm, _ := m.handleFilesKey("j")
		m = nm
		_ = m.renderFileList(innerW)
	}
	if m.filesCursor != 39 {
		t.Fatalf("expected cursor on last file (39), got %d", m.filesCursor)
	}
	lb := m.changesList
	wantOffset := 39 - lb.ContentHeight() + 1
	if lb.ScrollOffset() != wantOffset {
		t.Fatalf("expected scroll offset %d, got %d", wantOffset, lb.ScrollOffset())
	}
	view := m.View(w, h, ApplyThemeColors("tokyonight"), false, false)
	if rows := len(strings.Split(view, "\n")); rows > h {
		t.Fatalf("git tab overflows terminal: %d rows for height %d", rows, h)
	}
	if !strings.Contains(stripANSI(view), "file_39.go") {
		t.Fatal("expected last file to be visible after scrolling to bottom")
	}
}

// TestGitFileListLongPathsStaySingleLine guards the 1:1 row/scroll mapping:
// long file names must be truncated (with scrollbar cell reserved), never
// wrapped onto extra physical lines that push the list past the terminal.
func TestGitFileListLongPathsStaySingleLine(t *testing.T) {
	ApplyThemeColors("tokyonight")
	dir := t.TempDir()
	m, _ := newGitModel(dir)
	m.section = gitSectionChanges
	m.panel = gitPanelFiles
	for i := 0; i < 40; i++ {
		m.unstagedFiles = append(m.unstagedFiles, gitFile{status: "M", path: fmt.Sprintf("some/rather/long/directory/prefix/file_%02d_with_long_name.go", i)})
	}
	const w, h = 100, 24
	m.Resize(w, h)
	innerW, _ := m.filesListInnerDims()
	_ = m.renderFileList(innerW)
	for i := 0; i < 60; i++ {
		nm, _ := m.handleFilesKey("j")
		m = nm
		_ = m.renderFileList(innerW)
	}
	if m.filesCursor != 39 {
		t.Fatalf("expected cursor on last file (39), got %d", m.filesCursor)
	}
	for i, line := range m.renderFileList(innerW) {
		if got := lipgloss.Width(line); got > innerW {
			t.Fatalf("row %d exceeds list width: %d > %d (%q)", i, got, innerW, stripANSI(line))
		}
	}
	lb := m.changesList
	if off := lb.ScrollOffset(); m.filesCursor < off || m.filesCursor >= off+lb.ContentHeight() {
		t.Fatalf("cursor %d not in visible window [offset %d, height %d]", m.filesCursor, off, lb.ContentHeight())
	}
	view := m.View(w, h, ApplyThemeColors("tokyonight"), false, false)
	if rows := len(strings.Split(view, "\n")); rows > h {
		t.Fatalf("git tab overflows terminal with long paths: %d rows for height %d", rows, h)
	}
}

// TestGitListBoxForSection verifies the wheel/click target follows the
// visible section instead of always hitting the changes list.
func TestGitListBoxForSection(t *testing.T) {
	m := gitModel{
		changesList:  NewListBox(10, 10),
		logList:      NewListBox(10, 10),
		stashList:    NewListBox(10, 10),
		branchesList: NewListBox(10, 10),
	}
	cases := []struct {
		section gitSection
		want    *ListBox
	}{
		{gitSectionChanges, m.changesList},
		{gitSectionLog, m.logList},
		{gitSectionStash, m.stashList},
		{gitSectionBranches, m.branchesList},
	}
	for _, c := range cases {
		m.section = c.section
		if got := m.listBoxForSection(); got != c.want {
			t.Errorf("section %d: wrong list box", int(c.section))
		}
	}
	// Nil-safe for hand-built models.
	empty := gitModel{section: gitSectionLog}
	if empty.listBoxForSection() != nil {
		t.Error("expected nil list box for uninitialized model")
	}
}

// TestGitWheelScrollsVisibleSection ensures a mouse wheel over the files
// column scrolls the active section's list (here: Log commits), not the
// hidden changes list and not the diff.
func TestGitWheelScrollsVisibleSection(t *testing.T) {
	ApplyThemeColors("tokyonight")
	dir := t.TempDir()
	gm, _ := newGitModel(dir)
	gm.section = gitSectionLog
	gm.panel = gitPanelFiles
	for i := 0; i < 40; i++ {
		gm.commits = append(gm.commits, gitCommit{hash: fmt.Sprintf("abc%02d", i), subject: fmt.Sprintf("commit subject %02d", i), author: "a", age: "1h"})
	}
	const w, h = 100, 30
	gm.Resize(w, h)
	// Populate the ListBox counts the way a render frame would.
	_ = gm.View(w, h, ApplyThemeColors("tokyonight"), false, false)

	m := model{
		width:     w,
		height:    h,
		activeTab: tabGit,
		styles:    ApplyThemeColors("tokyonight"),
		git:       gm,
	}
	filesX := w*20/100 + 2 // inside the files column
	updated, _ := m.Update(tea.MouseWheelMsg{
		X: filesX, Y: appHeaderHeight + 5, Button: tea.MouseWheelDown,
	})
	got := derefTestModel(t, updated)
	if got.git.logList.ScrollOffset() == 0 {
		t.Fatal("expected log list to scroll on wheel down over files column")
	}
	if got.git.changesList.ScrollOffset() != 0 {
		t.Fatal("wheel over files column must not scroll the hidden changes list")
	}
}
