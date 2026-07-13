package tui

import (
	"reflect"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

func TestGitDiffDoubleClickOpensEditorAtLine(t *testing.T) {
	m := model{
		width:     100,
		height:    30,
		activeTab: tabGit,
		styles:    ApplyThemeColors("tokyonight"),
		git: gitModel{
			section:       gitSectionChanges,
			workDir:       "/repo",
			filesCursor:   0,
			unstagedFiles: []gitFile{{status: "M", path: "a.go"}},
			diff:          viewport.New(viewport.WithWidth(45), viewport.WithHeight(10)),
		},
	}
	// Untracked-style content: no hunk headers, so each line maps to its
	// 1-based index in the file.
	m.git.setDiffContent("line one\nline two\nline three")

	var openedPath string
	var openedLine int
	m.git.editorOpenerAtLine = func(p string, l int) tea.Cmd {
		openedPath = p
		openedLine = l
		return func() tea.Msg { return editorFinishedMsg{} }
	}

	panelW := m.panelWidth()
	diffLeft := panelW*20/100 + panelW*30/100 + 1
	gitBodyTop := appHeaderHeight + 1
	click := tea.Mouse{Button: tea.MouseLeft, X: diffLeft, Y: gitBodyTop + 1}

	// First click (press + release): starts a selection, not a double-click.
	var up tea.Model
	up, _, ok := m.handleMouseAction(click, true)
	if !ok {
		t.Fatal("expected first press handled")
	}
	m = up.(model)
	// The release of a simple click is not reported as "handled" but still
	// clears the selection; capture the returned model so state carries over.
	up, _, _ = m.handleMouseAction(click, false)
	m = up.(model)

	// Second click at the same spot within the double-click window.
	up, cmd, ok := m.handleMouseAction(click, true)
	m = up.(model)
	if !ok {
		t.Fatal("expected second press handled")
	}
	if cmd == nil {
		t.Fatal("expected an editor command from double-click")
	}
	if openedPath != "/repo/a.go" {
		t.Fatalf("opened path = %q, want %q", openedPath, "/repo/a.go")
	}
	if openedLine != 1 {
		t.Fatalf("opened line = %d, want 1 (first diff line)", openedLine)
	}
	// The stray selection from the first click must be cleared.
	if m.gitSel.active || m.gitSel.dragging {
		t.Fatalf("expected selection cleared after double-click, got %#v", m.gitSel)
	}
}

func TestGitDiffDoubleClickTrackedMapsToSourceLine(t *testing.T) {
	m := model{
		width:     100,
		height:    30,
		activeTab: tabGit,
		styles:    ApplyThemeColors("tokyonight"),
		git: gitModel{
			section:       gitSectionChanges,
			workDir:       "/repo",
			filesCursor:   0,
			unstagedFiles: []gitFile{{status: "M", path: "a.go"}},
			diff:          viewport.New(viewport.WithWidth(60), viewport.WithHeight(10)),
		},
	}
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	m.git.setDiffContent(diff)

	var openedLine int
	m.git.editorOpenerAtLine = func(p string, l int) tea.Cmd {
		openedLine = l
		return func() tea.Msg { return editorFinishedMsg{} }
	}

	panelW := m.panelWidth()
	diffLeft := panelW*20/100 + panelW*30/100 + 1
	gitBodyTop := appHeaderHeight + 1

	// Click the added line ("+B") which is the 6th diff line (index 5).
	clickB := tea.Mouse{Button: tea.MouseLeft, X: diffLeft, Y: gitBodyTop + 6}

	// Double-click on the added line: should map to source line 2.
	var up tea.Model
	up, _, _ = m.handleMouseAction(clickB, true)
	m = up.(model)
	up, _, _ = m.handleMouseAction(clickB, false)
	m = up.(model)
	up, cmd, ok := m.handleMouseAction(clickB, true)
	m = up.(model)
	if !ok || cmd == nil {
		t.Fatal("expected double-click on added line to open editor")
	}
	if openedLine != 2 {
		t.Fatalf("opened line = %d, want 2 (the 'B' line in the new file)", openedLine)
	}
}

func TestBuildDiffLineMap(t *testing.T) {
	raw := []string{
		"diff --git a/foo.go b/foo.go",
		"index 111..222 100644",
		"--- a/foo.go",
		"+++ b/foo.go",
		"@@ -1,3 +1,3 @@",
		" package main",
		"-func old() {",
		"+func new() {",
		"",
		" func main() {",
	}
	got := buildDiffLineMap(raw)
	want := []int{0, 0, 0, 0, 0, 1, 2, 2, 0, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildDiffLineMap = %v, want %v", got, want)
	}
}

func TestBuildDiffLineMapMultipleRemovals(t *testing.T) {
	// Several consecutive removed lines should all map to the same next
	// surviving new-file line.
	raw := []string{
		"@@ -10,6 +10,4 @@",
		" keep",
		"-drop1",
		"-drop2",
		"-drop3",
		"+added",
		" keep2",
	}
	got := buildDiffLineMap(raw)
	want := []int{0, 10, 11, 11, 11, 11, 12}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildDiffLineMap = %v, want %v", got, want)
	}
}

func TestParseHunkNewStart(t *testing.T) {
	cases := []struct {
		header string
		want   int
		ok     bool
	}{
		{"@@ -3,3 +5,2 @@", 5, true},
		{"@@ -0,0 +1,5 @@", 1, true},
		{"@@ -12,7 +9,8 @@ func main", 9, true},
		{"no plus sign here", 0, false},
	}
	for _, c := range cases {
		got, ok := parseHunkNewStart(c.header)
		if ok != c.ok || got != c.want {
			t.Fatalf("parseHunkNewStart(%q) = (%d,%v), want (%d,%v)", c.header, got, ok, c.want, c.ok)
		}
	}
}

func TestGitDiffLineGutter(t *testing.T) {
	prev := activeDiffLineMap
	defer func() { activeDiffLineMap = prev }()
	activeDiffLineMap = []int{0, 5, 0, 12}

	if got := gitDiffLineGutter(viewport.GutterContext{Index: 1, Soft: false}); got != "   5 │ " {
		t.Fatalf("gutter line 1 = %q, want %q", got, "   5 │ ")
	}
	if got := gitDiffLineGutter(viewport.GutterContext{Index: 3, Soft: false}); got != "  12 │ " {
		t.Fatalf("gutter line 3 = %q, want %q", got, "  12 │ ")
	}
	if got := gitDiffLineGutter(viewport.GutterContext{Index: 0, Soft: false}); got != "     │ " {
		t.Fatalf("gutter line 0 = %q, want %q", got, "     │ ")
	}
	if got := gitDiffLineGutter(viewport.GutterContext{Index: 0, Soft: true}); got != "     │ " {
		t.Fatalf("gutter soft = %q, want %q", got, "     │ ")
	}
}

func TestGitOpenInEditorAtLineDelegates(t *testing.T) {
	m := gitModel{workDir: "/tmp", editor: "vi"}
	var called bool
	var gotPath string
	var gotLine int
	m.editorOpenerAtLine = func(p string, l int) tea.Cmd {
		called = true
		gotPath = p
		gotLine = l
		return func() tea.Msg { return editorFinishedMsg{} }
	}
	cmd := m.openInEditorAtLine("/tmp/foo.go", 42)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	if !called {
		t.Fatal("editorOpenerAtLine was not invoked")
	}
	if gotPath != "/tmp/foo.go" || gotLine != 42 {
		t.Fatalf("editorOpenerAtLine called with path=%q line=%d", gotPath, gotLine)
	}
}

func TestGitCurrentDiffFilePath(t *testing.T) {
	files := []gitFile{
		{status: "M", path: "a.go"},
		{status: "M", path: "b.go"},
	}
	m := gitModel{
		section:       gitSectionChanges,
		workDir:       "/repo",
		filesCursor:   1,
		unstagedFiles: files,
	}
	want := "/repo/b.go"
	got := m.currentDiffFilePath()
	if got != want {
		t.Fatalf("currentDiffFilePath = %q, want %q", got, want)
	}

	// Non-changes sections have no single resolvable file.
	m.section = gitSectionLog
	if p := m.currentDiffFilePath(); p != "" {
		t.Fatalf("expected empty path for log section, got %q", p)
	}

	// Out-of-range cursor yields empty path.
	m.section = gitSectionChanges
	m.filesCursor = 99
	if p := m.currentDiffFilePath(); p != "" {
		t.Fatalf("expected empty path for out-of-range cursor, got %q", p)
	}
}
