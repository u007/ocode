package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newTempStore returns a fresh Store operating in tmpDir. Each test gets its
// own store and its own agentID so per-agent and global stores do not bleed.
func newTempStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return NewStore(NewAgentID()), dir
}

// resetGlobalStore wipes the package-level global store for tests that need a
// known-empty baseline.
func resetGlobalStore() {
	globalStore.mu.Lock()
	globalStore.snapshots = nil
	globalStore.redoStack = nil
	globalStore.step = 0
	globalStore.mu.Unlock()
}

func TestDiscardRecentRemovesBackups(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("file.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	resetGlobalStore()

	if err := Backup("file.txt"); err != nil {
		t.Fatal(err)
	}

	globalStore.mu.Lock()
	if len(globalStore.snapshots) != 1 {
		globalStore.mu.Unlock()
		t.Fatalf("expected 1 snapshot, got %d", len(globalStore.snapshots))
	}
	globalStore.mu.Unlock()

	if err := DiscardRecent(1); err != nil {
		t.Fatal(err)
	}

	globalStore.mu.Lock()
	defer globalStore.mu.Unlock()
	if len(globalStore.snapshots) != 0 {
		t.Fatalf("expected snapshots to be cleared, got %d", len(globalStore.snapshots))
	}
	entries, err := os.ReadDir(filepath.Join(tmpDir, ".opencode", "snapshots"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected snapshot files to be removed, got %d", entries)
	}
}

func TestChangedFilesReturnsUniquePaths(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("a.txt", []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("b.txt", []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	Reset()

	if err := Backup("a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Backup("b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Backup("a.txt"); err != nil {
		t.Fatal(err)
	}

	got := ChangedFiles()
	want := []string{"a.txt", "b.txt"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestResetClearsSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("a.txt", []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}

	Reset()
	if err := Backup("a.txt"); err != nil {
		t.Fatal(err)
	}
	Reset()
	if got := ChangedFiles(); len(got) != 0 {
		t.Fatalf("expected no changes after reset, got %v", got)
	}
}

func TestBackupTracksNewFiles(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	Reset()

	if err := Backup("newfile.txt"); err != nil {
		t.Fatal(err)
	}

	got := ChangedFiles()
	if len(got) != 1 || got[0] != "newfile.txt" {
		t.Fatalf("expected [newfile.txt], got %v", got)
	}

	// Verify the snapshot has empty BackupPath for new files.
	globalStore.mu.Lock()
	defer globalStore.mu.Unlock()
	if len(globalStore.snapshots) != 1 || globalStore.snapshots[0].BackupPath != "" {
		t.Fatalf("expected empty BackupPath for new file, got %q", globalStore.snapshots[0].BackupPath)
	}
}

func TestStoreBackupAndUndoByToolCallID_RoundTrip(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("a.txt", []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := s.Backup("a.txt", "tc1"); err != nil {
		t.Fatal(err)
	}
	s.RegisterWrite("a.txt", "tc1")

	if err := os.WriteFile("a.txt", []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	restored, err := s.UndoByToolCallID("tc1", 5)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if len(restored) != 1 || restored[0] != "a.txt" {
		t.Fatalf("expected [a.txt], got %v", restored)
	}
	got, err := os.ReadFile("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original\n" {
		t.Fatalf("file content after undo = %q, want %q", got, "original\n")
	}
}

func TestStoreUndoByToolCallID_RestoresNewFileDelete(t *testing.T) {
	s, _ := newTempStore(t)
	if err := s.Backup("new.txt", "tc1"); err != nil {
		t.Fatal(err)
	}
	s.RegisterWrite("new.txt", "tc1")

	if err := os.WriteFile("new.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	restored, err := s.UndoByToolCallID("tc1", 5)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if len(restored) != 1 || restored[0] != "new.txt" {
		t.Fatalf("expected [new.txt], got %v", restored)
	}
	if _, err := os.Stat("new.txt"); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted, stat err = %v", err)
	}
}

func TestStoreUndoByToolCallID_Expired(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("a.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup("a.txt", "tc1"); err != nil {
		t.Fatal(err)
	}
	s.AdvanceStep()
	s.AdvanceStep()
	s.AdvanceStep()
	s.AdvanceStep() // 4 steps past the backup

	_, err := s.UndoByToolCallID("tc1", 2)
	if err == nil {
		t.Fatal("expected expired error, got nil")
	}
}

func TestStoreUndoByToolCallID_CrossAgentConflict(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("a.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup("a.txt", "tc1"); err != nil {
		t.Fatal(err)
	}
	s.RegisterWrite("a.txt", "tc1")

	// Another agent writes the same file after our write.
	other := NewStore("other-agent")
	if err := other.Backup("a.txt", "tc-other"); err != nil {
		t.Fatal(err)
	}
	other.RegisterWrite("a.txt", "tc-other")
	t.Cleanup(other.Reset)

	_, err := s.UndoByToolCallID("tc1", 5)
	if err == nil {
		t.Fatal("expected cross-agent conflict error, got nil")
	}
}

func TestStoreUndoByToolCallID_SameAgentNewerBlocks(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("a.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup("a.txt", "tc1"); err != nil {
		t.Fatal(err)
	}
	s.RegisterWrite("a.txt", "tc1")
	if err := os.WriteFile("a.txt", []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup("a.txt", "tc2"); err != nil {
		t.Fatal(err)
	}
	s.RegisterWrite("a.txt", "tc2")

	_, err := s.UndoByToolCallID("tc1", 5)
	if err == nil {
		t.Fatal("expected same-agent newer change error, got nil")
	}
}

func TestStoreUndoByToolCallID_NotFound(t *testing.T) {
	s, _ := newTempStore(t)
	_, err := s.UndoByToolCallID("missing", 5)
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
}

func TestStoreResetUnregistersFromGlobalRegistry(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("a.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup("a.txt", "tc1"); err != nil {
		t.Fatal(err)
	}
	s.RegisterWrite("a.txt", "tc1")

	s.Reset()

	// After reset, our stale entry should be removed; a fresh "other agent"
	// write for the same path is unblocked.
	other := NewStore("other-agent")
	if err := other.Backup("a.txt", "tc-other"); err != nil {
		t.Fatal(err)
	}
	other.RegisterWrite("a.txt", "tc-other")
	if w := crossAgentWriteAfterSeq("a.txt", "other-agent", 0); w != nil && w.AgentID == s.agentID {
		t.Fatalf("expected stale entry to be removed, found %+v", w)
	}
	other.Reset() //nolint:errcheck
}

func TestSnapshotContextHelpers(t *testing.T) {
	_, dir := newTempStore(t)
	_ = dir
	s := NewStore(NewAgentID())
	ctx := WithStore(context.Background(), s)
	ctx = WithToolCallID(ctx, "tc-x")
	if got := FromContext(ctx); got != s {
		t.Fatalf("FromContext returned wrong store: %p vs %p", got, s)
	}
	if got := ToolCallIDFromContext(ctx); got != "tc-x" {
		t.Fatalf("ToolCallIDFromContext = %q, want %q", got, "tc-x")
	}
	// Bare context falls back to the global store.
	if FromContext(context.Background()) != globalStore {
		t.Fatal("FromContext on bare context should return global store")
	}
}

func TestStoreAdvanceStep(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("a.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup("a.txt", "tc1"); err != nil {
		t.Fatal(err)
	}
	// Sanity check: the backup file is on disk under .opencode/snapshots/ before undo.
	entries, err := os.ReadDir(filepath.Join(".opencode", "snapshots"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected backup file on disk before undo, err=%v entries=%d", err, len(entries))
	}
	backupPath := entries[0].Name()

	s.AdvanceStep()
	if err := os.WriteFile("a.txt", []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UndoByToolCallID("tc1", 5); err != nil {
		t.Fatalf("expected undo to succeed within window, got %v", err)
	}

	// After undo, restoreSnapshot has removed the backup file. Verify it's gone.
	if _, err := os.Stat(filepath.Join(".opencode", "snapshots", backupPath)); !os.IsNotExist(err) {
		t.Fatalf("expected backup file removed after undo, err = %v", err)
	}
}
