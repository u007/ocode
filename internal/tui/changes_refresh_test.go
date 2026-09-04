package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/changes"
	"github.com/u007/ocode/internal/snapshot"
)

// TestChangesRefreshEvictsStaleDiff is a regression test for the "sometimes
// not working" changes tab: the right-pane diff cache was never invalidated,
// so after the agent edited a file a second time the tab kept showing the
// previous diff. refreshFiles must evict entries whose UpdatedAt moved.
func TestChangesRefreshEvictsStaleDiff(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := snapshot.NewStore("main", filepath.Join(dir, "snaps"))
	if err := store.Backup(a, "tc-1"); err != nil {
		t.Fatal(err)
	}
	// Simulate the agent's edit landing after the backup.
	if err := os.WriteFile(a, []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r := changes.NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	m := NewChangesModel()
	m.getRegistry = func() *changes.Registry { return r }
	m = m.refreshFiles()
	if len(m.files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(m.files))
	}
	m.ensureDiffCached(0)
	first, ok := m.diffCache[a]
	if !ok || first == "" {
		t.Fatalf("expected cached diff, got %q (present=%v)", first, ok)
	}

	// Second edit: new snapshot => UpdatedAt moves.
	if err := store.Backup(a, "tc-2"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("v3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m = m.refreshFiles()
	if _, ok := m.diffCache[a]; ok {
		t.Error("stale diff survived a newer edit; refreshFiles must evict it")
	}

	// Re-rendering caches the fresh diff (must differ from the first).
	m.ensureDiffCached(0)
	second, ok := m.diffCache[a]
	if !ok || second == "" {
		t.Fatalf("expected re-cached diff, got %q (present=%v)", second, ok)
	}
	if second == first {
		t.Error("expected a different diff after the second edit")
	}
}

// TestChangesRefreshEvictsStaleDiffOnSameSizeAndMtime is the deterministic
// regression test for diff-cache identity not depending on wall-clock or
// stat uniqueness: a content change that preserves BOTH the file size and
// its mtime, with FileChange.UpdatedAt frozen (no new Backup, plus
// os.Chtimes restoring the timestamp), must still evict the cached diff via
// the content hash. Under the old timestamp-only (or stat-only) stamp this
// refresh saw equal keys and kept the stale diff forever.
func TestChangesRefreshEvictsStaleDiffOnSameSizeAndMtime(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := snapshot.NewStore("main", filepath.Join(dir, "snaps"))
	if err := store.Backup(a, "tc-1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r := changes.NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	m := NewChangesModel()
	m.getRegistry = func() *changes.Registry { return r }
	m = m.refreshFiles()
	m.ensureDiffCached(0)
	if _, ok := m.diffCache[a]; !ok {
		t.Fatal("expected cached diff before the trap")
	}

	// Trap: rewrite same-length content ("v3\n" is also 3 bytes) and force
	// the mtime back to the value the cached entry was stamped with, so a
	// stat-only fingerprint sees no change at all. Only the sha256 of the
	// content differs.
	before, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("v3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(a, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("test setup: size/mtime must be preserved (size %d==%d, mtime %v==%v)",
			before.Size(), after.Size(), before.ModTime(), after.ModTime())
	}

	m = m.refreshFiles()
	if _, ok := m.diffCache[a]; ok {
		t.Error("stale diff survived a same-size/same-mtime content change; the diff key must include a content hash")
	}

	// Re-rendering caches the fresh diff.
	m.ensureDiffCached(0)
	if _, ok := m.diffCache[a]; !ok {
		t.Fatal("expected the fresh diff to be re-cached")
	}
}

// TestChangesRefreshClampsSelection verifies the selection never points past
// a shrunk file list (e.g. after undoing the last file while it was
// selected).
func TestChangesRefreshClampsSelection(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("v1\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	store := snapshot.NewStore("main", filepath.Join(dir, "snaps"))
	if err := store.Backup(a, "tc-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(b, "tc-b"); err != nil {
		t.Fatal(err)
	}

	r := changes.NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	m := NewChangesModel()
	m.getRegistry = func() *changes.Registry { return r }
	m = m.refreshFiles()
	if len(m.files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(m.files))
	}
	m.list.SetSelected(1)

	if err := r.UndoFile(b); err != nil {
		t.Fatalf("UndoFile: %v", err)
	}
	m = m.refreshFiles()
	if len(m.files) != 1 {
		t.Fatalf("expected 1 file after undo, got %d", len(m.files))
	}
	if got := m.list.Selected(); got != 0 {
		t.Errorf("expected selection clamped to 0, got %d", got)
	}
}

// TestChangesClickAfterSameTabRemoval verifies the mouse hit-test path
// refreshes the persistent model before resolving rows: when a file is
// undone/removed while the Changes tab is already visible (no tab re-entry),
// a click on the now-stale row must not panic, open the wrong file, or leave
// the selection pointing past the shrunk list. View() refreshes only a
// throwaway copy, so the press handler must sync first.
func TestChangesClickAfterSameTabRemoval(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("v1\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	store := snapshot.NewStore("main", filepath.Join(dir, "snaps"))
	if err := store.Backup(a, "tc-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(b, "tc-b"); err != nil {
		t.Fatal(err)
	}

	r := changes.NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	cm := NewChangesModel()
	cm.getRegistry = func() *changes.Registry { return r }
	cm = cm.refreshFiles()
	if len(cm.files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(cm.files))
	}
	styles := ApplyThemeColors("tokyonight")
	// Render once so the ListBox geometry (size/line map) is laid out, as it
	// is on a live visible tab.
	_ = cm.View(120, 30, styles)
	cm.list.SetSelected(1)
	// Cache b's diff so we can verify eviction after removal.
	cm.ensureDiffCached(1)
	if _, ok := cm.diffCache[b]; !ok {
		t.Fatalf("expected cached diff for %q", b)
	}

	m := model{
		activeTab: tabChanges,
		width:     120,
		height:    30,
		changes:   cm,
		styles:    styles,
	}

	// Same-tab removal: no tab re-entry, so the persistent model is stale
	// (2 files, selection 1) while the registry has 1.
	if err := r.UndoFile(b); err != nil {
		t.Fatalf("UndoFile: %v", err)
	}

	// contentTop mirrors the press handler (appHeaderHeight+1).
	const contentTop = appHeaderHeight + 1
	click := func(x, y int) model {
		updated, _, _ := m.handleMouseAction(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}, true)
		mm, ok := updated.(model)
		if !ok {
			if pm, ok := updated.(*model); ok {
				return *pm
			}
			t.Fatalf("handleMouseAction returned %T, want model", updated)
		}
		return mm
	}

	// Click the now-stale second row: must refresh, clamp, and evict — no
	// panic, no selection past the end, no removal-target diff left behind.
	m = click(2, contentTop+1)
	if len(m.changes.files) != 1 {
		t.Fatalf("expected 1 file after refresh, got %d", len(m.changes.files))
	}
	if got := m.changes.list.Selected(); got != 0 {
		t.Errorf("expected selection clamped to 0 after same-tab removal, got %d", got)
	}
	if _, ok := m.changes.diffCache[b]; ok {
		t.Error("stale diff for removed file survived the click-path refresh")
	}

	// Click the remaining first row: selects it without opening the editor
	// (single click) and caches its diff.
	m = click(2, contentTop)
	if got := m.changes.list.Selected(); got != 0 {
		t.Errorf("expected selection 0 after clicking first row, got %d", got)
	}
	if _, ok := m.changes.diffCache[a]; !ok {
		t.Errorf("expected cached diff for %q after clicking its row", a)
	}
}
