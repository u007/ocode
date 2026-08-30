package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// clearJournalCache resets the process-wide journal cache so each test's
// TempDir gets a fresh Journal.
func clearJournalCache() {
	journalCacheMu.Lock()
	for k, j := range journalCache {
		if j != nil {
			j.db.Close()
		}
		delete(journalCache, k)
	}
	journalCacheMu.Unlock()
}

func TestJournalAppendAndRehydrate(t *testing.T) {
	clearJournalCache()
	t.Cleanup(clearJournalCache)
	dir := t.TempDir()
	work := t.TempDir()

	target := filepath.Join(work, "a.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore("agent1", dir)
	s.SetSessionID("ses_test1")
	if err := s.Backup(target, "tc_1"); err != nil {
		t.Fatalf("backup: %v", err)
	}

	// A rebuilt store for the same session rehydrates the snapshot.
	s2 := NewStore("agent2", dir)
	s2.SetSessionID("ses_test1")
	s2.Rehydrate()
	s2.mu.Lock()
	got := append([]Snapshot(nil), s2.snapshots...)
	s2.mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("rehydrated %d snapshots, want 1", len(got))
	}
	if got[0].OriginalPath != target || got[0].ToolCallID != "tc_1" {
		t.Fatalf("rehydrated snapshot mismatch: %+v", got[0])
	}
	if got[0].BackupPath == "" {
		t.Fatal("rehydrated snapshot lost its backup path")
	}
	if _, err := os.Stat(got[0].BackupPath); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}

	// A different session sees nothing.
	s3 := NewStore("agent3", dir)
	s3.SetSessionID("ses_other")
	s3.Rehydrate()
	s3.mu.Lock()
	n := len(s3.snapshots)
	s3.mu.Unlock()
	if n != 0 {
		t.Fatalf("other session rehydrated %d snapshots, want 0", n)
	}
}

func TestRehydrateOnlyFillsEmptyStore(t *testing.T) {
	clearJournalCache()
	t.Cleanup(clearJournalCache)
	dir := t.TempDir()
	work := t.TempDir()
	target := filepath.Join(work, "b.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore("agent1", dir)
	s.SetSessionID("ses_fill")
	if err := s.Backup(target, "tc_1"); err != nil {
		t.Fatal(err)
	}

	// The live store already has the snapshot; Rehydrate must not duplicate it.
	s.Rehydrate()
	s.mu.Lock()
	n := len(s.snapshots)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("live store has %d snapshots after Rehydrate, want 1", n)
	}
}

func TestRehydrateSkipsMissingBackups(t *testing.T) {
	clearJournalCache()
	t.Cleanup(clearJournalCache)
	dir := t.TempDir()
	work := t.TempDir()
	target := filepath.Join(work, "c.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore("agent1", dir)
	s.SetSessionID("ses_gone")
	if err := s.Backup(target, "tc_1"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	backup := s.snapshots[0].BackupPath
	s.mu.Unlock()
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}

	s2 := NewStore("agent2", dir)
	s2.SetSessionID("ses_gone")
	s2.Rehydrate()
	s2.mu.Lock()
	n := len(s2.snapshots)
	s2.mu.Unlock()
	if n != 0 {
		t.Fatalf("rehydrated %d snapshots with deleted backup, want 0", n)
	}
}

func TestJournalGC(t *testing.T) {
	clearJournalCache()
	t.Cleanup(clearJournalCache)
	dir := t.TempDir()

	j, err := openJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.db.Close()

	oldBackup := filepath.Join(dir, "1000_old_a.txt")
	newBackup := filepath.Join(dir, "2000_new_b.txt")
	orphan := filepath.Join(dir, "1500_orphan_c.txt")
	for _, p := range []string{oldBackup, newBackup, orphan} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(orphan, past, past); err != nil {
		t.Fatal(err)
	}

	if err := j.append("ses_old", "a1", Snapshot{OriginalPath: "/x/a.txt", BackupPath: oldBackup, Timestamp: past}); err != nil {
		t.Fatal(err)
	}
	if err := j.append("ses_new", "a1", Snapshot{OriginalPath: "/x/b.txt", BackupPath: newBackup, Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}

	j.gc(time.Now().Add(-24 * time.Hour))

	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Fatalf("expired backup survived gc: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("old orphan survived gc: %v", err)
	}
	if _, err := os.Stat(newBackup); err != nil {
		t.Fatalf("fresh referenced backup was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, journalFileName)); err != nil {
		t.Fatalf("journal db itself was deleted: %v", err)
	}

	snaps, err := j.loadSession("ses_old")
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("expired session still has %d rows", len(snaps))
	}
	snaps, err = j.loadSession("ses_new")
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("fresh session has %d rows, want 1", len(snaps))
	}
}

func TestUnjournaledStoreWritesNoJournal(t *testing.T) {
	clearJournalCache()
	t.Cleanup(clearJournalCache)
	dir := t.TempDir()
	work := t.TempDir()
	target := filepath.Join(work, "d.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore("agent1", dir) // no SetSessionID
	if err := s.Backup(target, "tc_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, journalFileName)); !os.IsNotExist(err) {
		t.Fatalf("journal created for sessionless store: %v", err)
	}
}
