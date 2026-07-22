package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/u007/ocode/internal/changes"
	"github.com/u007/ocode/internal/snapshot"
)

// newTestChangesModel returns a changesModel pre-populated with the given
// files and whose getRegistry returns nil (so refreshFiles is a no-op and
// the caller's file list is preserved). The ListBox count is synced so
// selection works without first calling View().
func newTestChangesModel(files []changes.FileChange) changesModel {
	m := NewChangesModel()
	m.files = files
	m.list.SetData(len(files), nil)
	return m
}

// TestChangesModelEmptyState verifies the empty-state strings appear when
// the model has no files.
func TestChangesModelEmptyState(t *testing.T) {
	m := NewChangesModel()
	styles := ApplyThemeColors("tokyonight")
	view := m.View(80, 24, styles)
	clean := stripANSI(view)
	if !strings.Contains(clean, "no changes in this") {
		t.Errorf("expected empty state message in left pane, got:\n%s", clean)
	}
	if !strings.Contains(clean, "files the agent edits will appear") {
		t.Errorf("expected empty state hint in right pane, got:\n%s", clean)
	}
}

// TestChangesModelCursor verifies that j/k/g/G keys move the selection.
func TestChangesModelCursor(t *testing.T) {
	files := []changes.FileChange{
		{OriginalPath: "/tmp/a.txt", Status: changes.FileModified, Undoable: true, ChangeCount: 1},
		{OriginalPath: "/tmp/b.txt", Status: changes.FileAdded, Undoable: true, ChangeCount: 1},
		{OriginalPath: "/tmp/c.txt", Status: changes.FileModified, Undoable: true, ChangeCount: 2},
	}
	m := newTestChangesModel(files)

	// Initial selection should be 0.
	if got := m.list.Selected(); got != 0 {
		t.Fatalf("expected initial selection 0, got %d", got)
	}

	// Press j to move down.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := m.list.Selected(); got != 1 {
		t.Fatalf("expected selection 1 after j, got %d", got)
	}

	// Press j again.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := m.list.Selected(); got != 2 {
		t.Fatalf("expected selection 2 after second j, got %d", got)
	}

	// Press j again wraps to 0.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := m.list.Selected(); got != 0 {
		t.Fatalf("expected selection 0 after wrap, got %d", got)
	}

	// Press k moves up (wraps to 2).
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if got := m.list.Selected(); got != 2 {
		t.Fatalf("expected selection 2 after k, got %d", got)
	}

	// Press g goes to top.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if got := m.list.Selected(); got != 0 {
		t.Fatalf("expected selection 0 after g, got %d", got)
	}

	// Press G goes to end.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if got := m.list.Selected(); got != 2 {
		t.Fatalf("expected selection 2 after G, got %d", got)
	}

	// Press down arrow key.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.list.Selected(); got != 0 {
		t.Fatalf("expected selection 0 after down arrow wrap, got %d", got)
	}

	// Press up arrow key.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.list.Selected(); got != 2 {
		t.Fatalf("expected selection 2 after up arrow wrap, got %d", got)
	}

	// Press home.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyHome})
	if got := m.list.Selected(); got != 0 {
		t.Fatalf("expected selection 0 after home, got %d", got)
	}

	// Press end.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	if got := m.list.Selected(); got != 2 {
		t.Fatalf("expected selection 2 after end, got %d", got)
	}
}

// TestChangesModelKeybindings verifies that ? toggles showDetails.
func TestChangesModelKeybindings(t *testing.T) {
	files := []changes.FileChange{
		{OriginalPath: "/tmp/a.txt", Status: changes.FileModified, Undoable: true, ChangeCount: 1},
	}
	m := newTestChangesModel(files)

	// Initial state: showDetails is false.
	if m.showDetails {
		t.Fatal("expected showDetails=false initially")
	}

	// Press ? to toggle on.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: '?'})
	if !m.showDetails {
		t.Fatal("expected showDetails=true after ?")
	}

	// Press ? to toggle off.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: '?'})
	if m.showDetails {
		t.Fatal("expected showDetails=false after second ?")
	}

	// Toggle on again, then esc turns it off.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: '?'})
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showDetails {
		t.Fatal("expected showDetails=false after esc when on")
	}

	// Esc with showDetails already off — should still be off.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showDetails {
		t.Fatal("expected showDetails=false after second esc")
	}
}

// TestChangesModelEnter verifies that enter doesn't crash and selection stays.
func TestChangesModelEnter(t *testing.T) {
	files := []changes.FileChange{
		{OriginalPath: "/tmp/a.txt", Status: changes.FileModified, Undoable: true, ChangeCount: 1},
		{OriginalPath: "/tmp/b.txt", Status: changes.FileAdded, Undoable: true, ChangeCount: 1},
	}
	m := newTestChangesModel(files)

	// Move to index 1.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := m.list.Selected(); got != 1 {
		t.Fatalf("expected selection 1, got %d", got)
	}

	// Press enter — selection stays at 1.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.list.Selected(); got != 1 {
		t.Fatalf("expected selection 1 after enter, got %d", got)
	}
}

// TestChangesModelListboxHitTest verifies that list.HitTest returns the
// correct row index for a given y coordinate after rendering.
func TestChangesModelListboxHitTest(t *testing.T) {
	files := []changes.FileChange{
		{OriginalPath: "/tmp/a.txt", Status: changes.FileModified, Undoable: true, ChangeCount: 1},
		{OriginalPath: "/tmp/b.txt", Status: changes.FileAdded, Undoable: true, ChangeCount: 1},
		{OriginalPath: "/tmp/c.txt", Status: changes.FileDeleted, Undoable: true, ChangeCount: 3},
	}
	m := newTestChangesModel(files)

	// Render with known dimensions so the ListBox computes layout.
	_ = m.View(80, 24, ApplyThemeColors("tokyonight"))

	// Content area starts after chrome (header rows + filter row = 0 for us).
	contentTopY := m.list.ContentTopY()
	contentH := m.list.ContentHeight()

	t.Logf("contentTopY=%d contentH=%d count=%d", contentTopY, contentH, m.list.Count())

	// Hit test at the first item row.
	firstIdx := m.list.HitTest(0, contentTopY)
	if firstIdx != 0 {
		t.Errorf("expected hit test row 0 at y=%d, got %d", contentTopY, firstIdx)
	}

	// Hit test at the second item row.
	secondIdx := m.list.HitTest(0, contentTopY+1)
	if secondIdx != 1 {
		t.Errorf("expected hit test row 1 at y=%d, got %d", contentTopY+1, secondIdx)
	}

	// Hit test above the content area (should return -1).
	aboveIdx := m.list.HitTest(0, contentTopY-1)
	if aboveIdx != -1 {
		t.Errorf("expected -1 above content area, got %d", aboveIdx)
	}

	// Hit test below the content area (should return -1).
	belowIdx := m.list.HitTest(0, contentTopY+contentH)
	if belowIdx != -1 {
		t.Errorf("expected -1 below content area, got %d", belowIdx)
	}
}

// TestChangesModelUndo verifies that pressing u then y performs an undo.
func TestChangesModelUndo(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "undo.txt")
	if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	if err := store.Backup(f, "tc-undo-test"); err != nil {
		t.Fatal(err)
	}
	// Modify the file so there's something to undo.
	if err := os.WriteFile(f, []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r := changes.NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	// Create the model and wire the registry.
	m := NewChangesModel()
	m.getRegistry = func() *changes.Registry { return r }
	m = m.refreshFiles()
	m.list.SetData(len(m.files), nil)

	if len(m.files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(m.files))
	}
	if !m.files[0].Undoable {
		t.Fatal("expected file to be undoable")
	}

	// Press u to initiate undo.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.confirm == nil {
		t.Fatal("expected confirm dialog after u")
	}
	if m.confirm.action != "undo-file" {
		t.Errorf("expected action 'undo-file', got %q", m.confirm.action)
	}

	// Press y to confirm.
	m, _ = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}, 80, 24)
	if m.confirm != nil {
		t.Fatal("expected confirm to be nil after y")
	}
	if m.statusMsg != "undo complete" {
		t.Errorf("expected 'undo complete', got %q", m.statusMsg)
	}

	// Verify the file was restored.
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original\n" {
		t.Errorf("expected file content 'original\\n', got %q", string(data))
	}
}

// TestChangesModelUndoCancel verifies that pressing n cancels the undo.
func TestChangesModelUndoCancel(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "cancel.txt")
	if err := os.WriteFile(f, []byte("data\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	if err := store.Backup(f, "tc-cancel"); err != nil {
		t.Fatal(err)
	}

	r := changes.NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	m := NewChangesModel()
	m.getRegistry = func() *changes.Registry { return r }
	m = m.refreshFiles()
	m.list.SetData(len(m.files), nil)

	// Press u, then n to cancel.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.confirm == nil {
		t.Fatal("expected confirm dialog after u")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"}, 80, 24)
	if m.confirm != nil {
		t.Fatal("expected confirm to be nil after n")
	}
}

// TestChangesModelUndoBashOnly verifies that pressing u on a bash-only entry
// (Undoable: false) skips the confirm dialog and sets the status message.
func TestChangesModelUndoBashOnly(t *testing.T) {
	files := []changes.FileChange{
		{
			OriginalPath:    "/tmp/bash.txt",
			Status:          changes.FileModified,
			Undoable:        false,
			LastBashCommand: "echo test >> bash.txt",
			ChangeCount:     1,
		},
	}
	m := newTestChangesModel(files)

	// Press u — should NOT show confirm, should set statusMsg.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.confirm != nil {
		t.Fatal("expected NO confirm dialog for bash-only entry")
	}
	const wantMsg = "this file's only change came from a bash command and cannot be undone from the changes tab"
	if m.statusMsg != wantMsg {
		t.Errorf("unexpected statusMsg:\n  got:  %q\n  want: %q", m.statusMsg, wantMsg)
	}
}

// TestChangesModelUndoBlock verifies that pressing U initiates block undo.
func TestChangesModelUndoBlock(t *testing.T) {
	files := []changes.FileChange{
		{
			OriginalPath: "/tmp/block.txt",
			Status:       changes.FileModified,
			Undoable:     true,
			ChangeCount:  1,
			Authors:      []changes.ChangeAuthor{{AgentID: "main", AgentName: "build", Changes: 1}},
		},
	}
	m := newTestChangesModel(files)

	// Press U — should show confirm with action "undo-block".
	m, _ = m.handleKey(tea.KeyPressMsg{Code: 'U', Text: "U"})
	if m.confirm == nil {
		t.Fatal("expected confirm dialog after U")
	}
	if m.confirm.action != "undo-block" {
		t.Errorf("expected action 'undo-block', got %q", m.confirm.action)
	}
}

// TestChangesModelNavigation wraps cursor test with multiple files and
// verifies all navigation keys work.
func TestChangesModelNavigation(t *testing.T) {
	files := make([]changes.FileChange, 5)
	for i := range files {
		files[i] = changes.FileChange{
			OriginalPath: filepath.Join("/tmp", string(rune('a'+i))+".txt"),
			Status:       changes.FileModified,
			Undoable:     true,
		}
	}
	m := newTestChangesModel(files)

	// Navigate down with down arrow.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.list.Selected(); got != 1 {
		t.Fatalf("selection should be 1 after down, got %d", got)
	}

	// Navigate up with up arrow.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.list.Selected(); got != 0 {
		t.Fatalf("selection should be 0 after up, got %d", got)
	}

	// Navigate down 5 times wraps around.
	for i := 0; i < 5; i++ {
		m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if got := m.list.Selected(); got != 0 {
		t.Fatalf("selection should wrap to 0 after 5 downs, got %d", got)
	}
}

// TestChangesModelHomeEndKeys verifies home/end navigation.
func TestChangesModelHomeEndKeys(t *testing.T) {
	files := make([]changes.FileChange, 4)
	for i := range files {
		files[i] = changes.FileChange{
			OriginalPath: filepath.Join("/tmp", string(rune('a'+i))+".txt"),
			Status:       changes.FileModified,
			Undoable:     true,
		}
	}
	m := newTestChangesModel(files)

	// Move to bottom with end.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	if got := m.list.Selected(); got != 3 {
		t.Fatalf("expected selection 3 after end, got %d", got)
	}

	// Move to top with home.
	m, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyHome})
	if got := m.list.Selected(); got != 0 {
		t.Fatalf("expected selection 0 after home, got %d", got)
	}
}

// TestChangesModelLayoutWithinHeight verifies that the rendered changes tab
// does not exceed the available terminal height, preventing the bottom chrome
// from being pushed off-screen. Modeled on TestActivityRowGrowthStaysWithinHeight.
func TestChangesModelLayoutWithinHeight(t *testing.T) {
	styles := ApplyThemeColors("tokyonight")
	m := NewChangesModel()
	m.files = []changes.FileChange{
		{OriginalPath: "/tmp/a.txt", Status: changes.FileAdded, Undoable: true},
		{OriginalPath: "/tmp/b.txt", Status: changes.FileModified, Undoable: true},
		{OriginalPath: "/tmp/c.txt", Status: changes.FileModified, Undoable: false},
	}
	m.list.SetData(len(m.files), nil)

	w, h := 80, 13 // short terminal, same as the overflow_repro_test
	out := m.View(w, h, styles)
	if got := lipgloss.Height(out); got > h {
		t.Errorf("changes tab output height %d exceeds terminal height %d\n%s", got, h, out)
	}
}
