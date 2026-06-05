package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"
)

func TestFilesPreviewShowsMetadataAndLanguage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	view := m.View(100, 30, ApplyThemeColors("tokyonight"), false, false)
	for _, want := range []string{"main.go", "go", "13 B"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected preview view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestFilesActionsCreateRenameAndDelete(t *testing.T) {
	dir := t.TempDir()
	m := newFilesModel(dir)
	m.Resize(100, 30)

	m.startCreateFile()
	m.promptInput.SetValue("draft.txt")
	m, _ = m.submitPrompt()
	created := filepath.Join(dir, "draft.txt")
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}

	m.navigateTo("draft.txt")
	m.startRename()
	m.promptInput.SetValue("final.txt")
	m, _ = m.submitPrompt()
	renamed := filepath.Join(dir, "final.txt")
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("expected file to be renamed: %v", err)
	}

	m.navigateTo("final.txt")
	m.startDelete()
	m, _ = m.Update(tea.KeyPressMsg{Code: 'y'}, 100, 30)
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, stat err=%v", err)
	}
}

func TestParseGitStatusShortMapsPaths(t *testing.T) {
	got := parseGitStatusShort(" M internal/tui/files_model.go\n?? new.txt\nR  old.txt -> renamed.txt\n")

	if got["internal/tui/files_model.go"] != "M" {
		t.Fatalf("expected modified badge, got %#v", got)
	}
	if got["new.txt"] != "?" {
		t.Fatalf("expected untracked badge, got %#v", got)
	}
	if got["renamed.txt"] != "R" {
		t.Fatalf("expected renamed badge, got %#v", got)
	}
}

// TestFilesDoubleClickFolderOpensExplorer verifies that a second click on a
// directory row inside the Files tab tree (within 400ms and at the same X/Y)
// routes through the cross-platform openInFileExplorer command instead of the
// default toggleDir. The returned tea.Cmd is the only thing the click path
// produces; its inner cmd.Start() is the real OS action and is not exercised
// in unit tests.
func TestFilesDoubleClickFolderOpensExplorer(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := model{
		ready:     true,
		width:     100,
		height:    30,
		activeTab: tabFiles,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.files = newFilesModel(dir)
	m.files.Resize(100, 30)

	// Locate the directory node as it appears in m.files.nodes.
	dirIdx := -1
	for i, n := range m.files.nodes {
		if n.isDir && n.name == "sub" {
			dirIdx = i
			break
		}
	}
	if dirIdx < 0 {
		t.Fatalf("expected a directory node named 'sub' in tree, got %#v", m.files.nodes)
	}
	// Y of node dirIdx inside the tree panel: appHeaderHeight + 1 (border) + hintHeight + dirIdx.
	treeW := 100 * 35 / 100
	hintHeight := lipgloss.Height(hintStyle.Width(treeW - 6).Render(m.files.treeHint()))
	clickY := appHeaderHeight + 1 + hintHeight + dirIdx
	clickX := 2

	// First click selects the node and toggles the directory open — it should
	// return no cmd (toggleDir has no async work).
	updated, cmd1 := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	m = derefTestModel(t, updated)
	if cmd1 != nil {
		t.Fatalf("first click on folder should not return a cmd, got %#v", cmd1)
	}
	if m.files.cursor != dirIdx {
		t.Fatalf("expected cursor at dir index %d after first click, got %d", dirIdx, m.files.cursor)
	}

	// Second click at the same X/Y within 400ms should hit the new
	// double-click-on-folder branch and return the openInFileExplorer cmd.
	// Note: the first click also calls toggleDir which expands the node and
	// inserts children; the directory row stays at the same index in
	// m.files.nodes, so clickY is still correct.
	updated, cmd2 := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	m = derefTestModel(t, updated)
	if cmd2 == nil {
		t.Fatal("expected double-click on folder to return an openInFileExplorer cmd, got nil")
	}
	// We deliberately do NOT execute cmd2() — the closure spawns a real
	// subprocess (`open`/`explorer`/`xdg-open`) via cmd.Start(), which on a
	// developer's local macOS box would open a Finder window during `go test`.
	// The non-nil assertion is sufficient to prove the cmd is wired.

	// A third click at the same X/Y but more than 400ms after the second must
	// be treated as a fresh single click — the new double-click branch must
	// not steal it. We backdate lastClickTime to simulate "slow" and check
	// that the directory's expanded flag (set by the first click's toggleDir)
	// flips back to false.
	m.lastClickTime = time.Now().Add(-time.Second)
	updated, cmd3 := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	m = derefTestModel(t, updated)
	if cmd3 != nil {
		t.Fatalf("slow second click on folder should fall back to toggleDir (no cmd), got %#v", cmd3)
	}
	// The directory was expanded by the first click, so this slow click should
	// collapse it: the directory's expanded flag must be false again.
	if m.files.nodes[dirIdx].expanded {
		t.Fatalf("expected slow click to collapse expanded folder; nodes[dirIdx]=%+v",
			m.files.nodes[dirIdx])
	}
}

func TestFilesTabMouseWheelScrollsPreview(t *testing.T) {
	m := model{
		ready:     true,
		width:     100,
		height:    30,
		activeTab: tabFiles,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.files = newFilesModel(t.TempDir())
	m.files.Resize(100, 30)
	m.files.preview.SetContent(strings.Repeat("line\n", 100))

	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 80, Y: 5})
	got := derefTestModel(t, updated)
	if got.files.preview.YOffset() == 0 {
		t.Fatal("expected files preview to scroll down on mouse wheel")
	}
}

func TestNumberKeysNoLongerSwitchTabsFromFilesTab(t *testing.T) {
	m := model{
		ready:     true,
		width:     100,
		height:    30,
		activeTab: tabFiles,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.files = newFilesModel(t.TempDir())
	m.files.Resize(100, 30)

	updated, _ := m.Update(tea.KeyPressMsg{Code: '3'})
	got := derefTestModel(t, updated)
	if got.activeTab != tabFiles {
		t.Fatalf("expected number key to not switch tabs, got %d", got.activeTab)
	}
}

func TestNumberKeysStillTypeInChat(t *testing.T) {
	m := model{
		ready:     true,
		width:     100,
		height:    30,
		activeTab: tabChat,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	got := derefTestModel(t, updated)
	if got.activeTab != tabChat {
		t.Fatalf("expected number key to stay on chat tab, got %d", got.activeTab)
	}
	if got.input.Value() != "3" {
		t.Fatalf("expected number key to type into chat input, got %q", got.input.Value())
	}
}

func TestFilesEditorErrorShowsFilesStatus(t *testing.T) {
	m := model{
		ready:     true,
		width:     100,
		height:    30,
		activeTab: tabFiles,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.files = newFilesModel(t.TempDir())

	updated, _ := m.Update(editorFinishedMsg{err: os.ErrNotExist})
	got := derefTestModel(t, updated)
	if !strings.Contains(got.files.statusMsg, "Editor error") {
		t.Fatalf("expected Files status to show editor error, got %q", got.files.statusMsg)
	}
	if len(got.messages) != 0 {
		t.Fatalf("expected no chat message for Files editor error, got %#v", got.messages)
	}
}

func TestFilesEditorOpenerExternalMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, []byte("package main\n"), 0644)

	m := newFilesModel(dir)
	m.editor = "echo"

	cmd1 := m.openInEditor(path)
	if cmd1 == nil {
		t.Fatal("expected non-nil cmd for default opener")
	}

	m.editorOpener = createEditorOpener("cat", "external", nil, nil)
	cmd2 := m.openInEditor(path)
	if cmd2 == nil {
		t.Fatal("expected non-nil cmd for external opener")
	}
}

func TestFilesOpenBinaryUsesSystemOpener(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.bin")
	if err := os.WriteFile(path, []byte{0, 1, 2, 3}, 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	called := false
	m.editorOpener = func(string) tea.Cmd {
		called = true
		return nil
	}

	if !isBinaryFile(path) {
		t.Fatal("expected binary detection to return true")
	}
	if cmd := m.openInEditor(path); cmd == nil {
		t.Fatal("expected binary open to return a system-opener command")
	}
	if called {
		t.Fatal("expected binary open to bypass editorOpener")
	}
}

func TestFilesSearchEmptyQueryDoesNotStart(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.mode = filesModeContentSearch
	m.contentSearchQuery = ""
	m.contentSearchLoading = false
	m.contentSearchDone = false

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)
	got := updated
	if cmd != nil {
		t.Fatal("expected empty search query to not start a search")
	}
	if got.contentSearchLoading {
		t.Fatal("expected empty search query to leave loading disabled")
	}
	if got.statusMsg != "type a query first" {
		t.Fatalf("expected empty query status, got %q", got.statusMsg)
	}
}

func TestFilesSearchIgnoresStaleDoneMessage(t *testing.T) {
	m := newFilesModel(t.TempDir())
	current := make(chan struct{})
	stale := make(chan struct{})
	m.contentSearchCancel = current
	m.contentSearchLoading = true
	m.contentSearchDone = false
	m.contentSearchResults = []filesContentSearchResult{{path: "/tmp/keep", relPath: "keep", line: 1, text: "keep"}}

	updated, _ := m.Update(filesContentSearchDoneMsg{cancel: stale}, 100, 30)
	got := updated
	if !got.contentSearchLoading {
		t.Fatal("expected stale done message to be ignored")
	}
	if got.contentSearchDone {
		t.Fatal("expected stale done message not to mark search done")
	}
	if got.contentSearchCancel != current {
		t.Fatal("expected current search token to remain unchanged")
	}
	if len(got.contentSearchResults) != 1 {
		t.Fatalf("expected results to remain untouched, got %#v", got.contentSearchResults)
	}
}

func TestFilesEnterUsesEditorOpener(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, []byte("package main\n"), 0644)

	m := newFilesModel(dir)
	m.editor = "echo"
	m.editorOpener = createEditorOpener("echo", "external", nil, nil)

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)
	if cmd == nil {
		t.Fatal("expected cmd from opening file via opener")
	}
}

func TestFilesHintsExternalMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	os.WriteFile(path, []byte("hello\n"), 0644)

	m := newFilesModel(dir)
	m.Resize(100, 30)
	m.editorMode = "external"
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	view := m.View(100, 30, ApplyThemeColors("tokyonight"), false, false)
	if !strings.Contains(view, "tab jump") {
		t.Fatalf("expected tab jump hint, got:\n%s", view)
	}
	if !strings.Contains(view, "e external") {
		t.Fatalf("expected external editor hint, got:\n%s", view)
	}
	if !strings.Contains(view, "choose editor") {
		t.Fatalf("expected choose editor hint, got:\n%s", view)
	}
}

func TestFilesSpaceTogglesSelection(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.nodes = []fileNode{{path: "/a/foo.go", name: "foo.go"}}
	m.cursor = 0
	m.panel = filesPanelPicker

	got, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "}, 100, 30)
	if !got.selectedFiles[0] {
		t.Fatal("expected space to select file at cursor")
	}

	got, _ = got.Update(tea.KeyPressMsg{Code: ' ', Text: " "}, 100, 30)
	if got.selectedFiles[0] {
		t.Fatal("expected second space to deselect file at cursor")
	}
}

func TestFilesSpaceOnDirTogglesExpand(t *testing.T) {
	dir := t.TempDir()
	m := newFilesModel(dir)
	m.nodes = []fileNode{{path: filepath.Join(dir, "sub"), name: "sub", isDir: true}}
	m.cursor = 0
	m.panel = filesPanelPicker

	got, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "}, 100, 30)
	// Space now selects/deselects directories (for multi-select delete)
	if !got.selectedFiles[0] {
		t.Fatal("expected space on directory to select it")
	}
	// Directories should NOT be expanded by space anymore (use enter for that)
	if got.nodes[0].expanded {
		t.Fatal("expected space on directory to NOT toggle expansion")
	}
}

func TestFilesShiftDownExtendsSelection(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.nodes = []fileNode{{path: "/a/a.go", name: "a.go"}, {path: "/a/b.go", name: "b.go"}, {path: "/a/c.go", name: "c.go"}}
	m.cursor = 0
	m.panel = filesPanelPicker

	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, 100, 30)
	if !got.selectedFiles[0] || !got.selectedFiles[1] {
		t.Fatalf("expected shift+down to select 0 and 1, got %#v", got.selectedFiles)
	}
	if got.cursor != 1 {
		t.Fatalf("expected cursor to move to 1, got %d", got.cursor)
	}
}

func TestFilesPlainNavClearsSelection(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.nodes = []fileNode{{path: "/a/a.go", name: "a.go"}, {path: "/a/b.go", name: "b.go"}}
	m.cursor = 0
	m.panel = filesPanelPicker
	m.selectedFiles = map[int]bool{0: true}

	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown}, 100, 30)
	if len(got.selectedFiles) != 0 {
		t.Fatalf("expected plain down to clear selection, got %#v", got.selectedFiles)
	}
	if got.cursor != 1 {
		t.Fatalf("expected cursor to move to 1, got %d", got.cursor)
	}
}

func TestFilesSelectionHintShowsCount(t *testing.T) {
	m := newFilesModel(t.TempDir())
	m.selectedFiles = map[int]bool{0: true, 1: true}

	hint := m.selectionHint()
	if !strings.Contains(hint, "2 selected") {
		t.Fatalf("expected selection hint count, got %q", hint)
	}
	if !strings.Contains(hint, "D delete") {
		t.Fatalf("expected selection hint to mention delete, got %q", hint)
	}
}

func TestFilesTreeHintShowsSelectionFlow(t *testing.T) {
	m := newFilesModel(t.TempDir())

	hint := m.treeHint()
	for _, want := range []string{"space select", "shift+↑↓ extend", "D del"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("expected tree hint to contain %q, got %q", want, hint)
		}
	}
}

func TestFilesRenamePromptConfirmPersistsUntilNameChanges(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)

	for i, n := range m.nodes {
		if n.name == "a.txt" {
			m.cursor = i
			break
		}
	}
	m.startRename()
	m.promptInput.SetValue("b.txt")
	m, _ = m.submitPrompt()
	if !m.promptConfirm {
		t.Fatal("expected overwrite confirmation to be armed")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp}, 100, 30)
	if !m.promptConfirm {
		t.Fatal("expected non-editing key to keep overwrite confirmation armed")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, 100, 30)
	if m.promptConfirm {
		t.Fatal("expected typing to clear overwrite confirmation")
	}
}

func TestFilesInlineVimEditWriteQuit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	if m.mode != filesModeEdit {
		t.Fatalf("expected edit mode, got %v", m.mode)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'a'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: '!', Text: "!"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: ':'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}, 100, 30)
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)

	if m.mode != filesModeNormal {
		t.Fatalf("expected normal mode after :wq, got %v", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected refresh preview command after :wq")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello!\n" {
		t.Fatalf("expected saved edit, got %q", string(data))
	}
}

func TestFilesInlineVimQuitRefusesDirtyBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'a'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: '!', Text: "!"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: ':'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)

	if m.mode != filesModeEdit {
		t.Fatalf("expected to remain in edit mode, got %v", m.mode)
	}
	if !strings.Contains(m.statusMsg, "unsaved") {
		t.Fatalf("expected unsaved status, got %q", m.statusMsg)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: ':'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: '!', Text: "!"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)

	if m.mode != filesModeNormal {
		t.Fatalf("expected forced quit to return to normal mode, got %v", m.mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("expected file unchanged after :q!, got %q", string(data))
	}
}

func TestFilesInlineVimViewAndHints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	m.editorMode = "external"
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	view := m.View(100, 30, ApplyThemeColors("tokyonight"), false, false)
	if !strings.Contains(view, "i vim edit") {
		t.Fatalf("expected vim edit hint, got:\n%s", view)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	view = m.View(100, 30, ApplyThemeColors("tokyonight"), false, false)
	for _, want := range []string{"hello", "-- NORMAL --", ":w save"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected edit view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestFilesInlineVimRefusesDirectoryAndNonEditablePreview(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	if m.mode == filesModeEdit {
		t.Fatal("expected directory edit to be refused")
	}
	if !strings.Contains(m.statusMsg, "directory") {
		t.Fatalf("expected directory status, got %q", m.statusMsg)
	}

	filePath := filepath.Join(dir, "binary.bin")
	if err := os.WriteFile(filePath, []byte{0, 1, 2}, 0644); err != nil {
		t.Fatal(err)
	}
	m = newFilesModel(dir)
	m.navigateTo("binary.bin")
	if result, ok := loadPreviewCmd(m.nodes[m.cursor])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	if m.mode == filesModeEdit {
		t.Fatal("expected binary edit to be refused")
	}
	if !strings.Contains(m.statusMsg, "not editable") {
		t.Fatalf("expected not editable status, got %q", m.statusMsg)
	}
}

func TestFilesInlineVimSaveRefusesDiskChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'a'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: '!', Text: "!"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}, 100, 30)
	if err := os.WriteFile(path, []byte("external\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: ':'}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"}, 100, 30)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, 100, 30)

	if m.mode != filesModeEdit {
		t.Fatalf("expected stale save to remain in edit mode, got %v", m.mode)
	}
	if !strings.Contains(m.statusMsg, "changed on disk") {
		t.Fatalf("expected stale file status, got %q", m.statusMsg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external\n" {
		t.Fatalf("expected external content preserved, got %q", string(data))
	}
}

func TestFilesHintsTmuxSplitMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	os.WriteFile(path, []byte("hello\n"), 0644)

	m := newFilesModel(dir)
	m.Resize(100, 30)
	m.editor = "nvim"
	m.editorMode = "tmux-split"
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	view := m.View(100, 30, ApplyThemeColors("tokyonight"), false, false)
	if !strings.Contains(view, "tmux split: nvim") {
		t.Fatalf("expected tmux split hint, got:\n%s", view)
	}
	if !strings.Contains(view, "tab jump") {
		t.Fatalf("expected tab jump hint, got:\n%s", view)
	}
	if !strings.Contains(view, "choose") || !strings.Contains(view, "editor") {
		t.Fatalf("expected choose editor hint, got:\n%s", view)
	}
}

func TestFilesInFileSearch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	// Switch to preview panel
	m.panel = filesPanelPreview

	// Press / to start search
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"}, 100, 30)
	if m.mode != filesModeInFileSearch {
		t.Fatalf("expected filesModeInFileSearch, got %v", m.mode)
	}
	if !m.inFileSearchActive {
		t.Fatal("expected inFileSearchActive to be true")
	}

	// Type a search query
	for _, ch := range "Pri" {
		m, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)}, 100, 30)
	}
	if m.inFileSearchQuery != "Pri" {
		t.Fatalf("expected query 'Pri', got %q", m.inFileSearchQuery)
	}
	if len(m.inFileSearchMatches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.inFileSearchMatches))
	}

	// Press n to navigate to next match
	m, _ = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"}, 100, 30)

	// Press esc to cancel search
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}, 100, 30)
	if m.mode != filesModeNormal {
		t.Fatalf("expected filesModeNormal after esc, got %v", m.mode)
	}
	if m.inFileSearchActive {
		t.Fatal("expected inFileSearchActive to be false after esc")
	}
}

func TestFilesInFileSearchBackspace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("hello world\nhello again\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	m.panel = filesPanelPreview

	// Start search and type a query
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"}, 100, 30)
	for _, ch := range "hello" {
		m, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)}, 100, 30)
	}
	if len(m.inFileSearchMatches) != 2 {
		t.Fatalf("expected 2 matches for 'hello', got %d", len(m.inFileSearchMatches))
	}

	// Press backspace to delete last character
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Text: "\x7f"}, 100, 30)
	if m.inFileSearchQuery != "hell" {
		t.Fatalf("expected query 'hell' after backspace, got %q", m.inFileSearchQuery)
	}
	if len(m.inFileSearchMatches) != 2 {
		t.Fatalf("expected 2 matches for 'hell', got %d", len(m.inFileSearchMatches))
	}
}

func TestFilesInFileSearchEnterConfirms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	if result, ok := loadPreviewCmd(m.nodes[0])().(filesPreviewMsg); ok {
		m.applyPreview(result)
	}

	m.panel = filesPanelPreview

	// Start search
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"}, 100, 30)
	for _, ch := range "test" {
		m, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)}, 100, 30)
	}

	// Press enter to confirm search
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "\r"}, 100, 30)
	if m.mode != filesModeNormal {
		t.Fatalf("expected filesModeNormal after enter, got %v", m.mode)
	}
	if m.inFileSearchActive {
		t.Fatal("expected inFileSearchActive to be false after enter")
	}
}
