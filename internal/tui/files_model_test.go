package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
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

func TestVisibleNumberTabShortcutWorksFromFilesTab(t *testing.T) {
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
	if got.activeTab != tabGit {
		t.Fatalf("expected 3 key to switch to git tab, got %d", got.activeTab)
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

	m.editorOpener = createEditorOpener("cat", "external", nil)
	cmd2 := m.openInEditor(path)
	if cmd2 == nil {
		t.Fatal("expected non-nil cmd for external opener")
	}
}

func TestFilesEnterUsesEditorOpener(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, []byte("package main\n"), 0644)

	m := newFilesModel(dir)
	m.editor = "echo"
	m.editorOpener = createEditorOpener("echo", "external", nil)

	m, cmd := m.Update(tea.KeyPressMsg{Code: ' '}, 100, 30)
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

