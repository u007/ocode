package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRekeySessionMigratesJournalAndKeepsLiveSnapshots pins the /reset-id
// snapshot contract: a rekey moves the session's journaled rows to the new id
// (so a rebuilt store rehydrates them) AND keeps the live in-memory undo
// history — unlike SwitchSession, which drops it.
func TestRekeySessionMigratesJournalAndKeepsLiveSnapshots(t *testing.T) {
	clearJournalCache()
	t.Cleanup(clearJournalCache)
	dir := t.TempDir()
	work := t.TempDir()

	target := filepath.Join(work, "a.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldID := "ses_old"
	newID := "ses_new"
	s := NewStore("agent1", dir)
	s.SetSessionID(oldID)
	if err := s.Backup(target, "tc_1"); err != nil {
		t.Fatalf("backup: %v", err)
	}
	// The live store must keep its snapshot across the rekey.
	s.mu.Lock()
	before := len(s.snapshots)
	s.mu.Unlock()
	if before != 1 {
		t.Fatalf("live snapshots before rekey = %d, want 1", before)
	}

	s.RekeySession(oldID, newID)
	if got := s.SessionID(); got != newID {
		t.Fatalf("bound session = %q, want %q", got, newID)
	}
	s.mu.Lock()
	after := len(s.snapshots)
	s.mu.Unlock()
	if after != 1 {
		t.Fatalf("live snapshots after rekey = %d, want 1 (must not reset)", after)
	}

	// A rebuilt store for the NEW id rehydrates the journaled snapshot.
	s2 := NewStore("agent2", dir)
	s2.SetSessionID(newID)
	s2.Rehydrate()
	s2.mu.Lock()
	n := len(s2.snapshots)
	s2.mu.Unlock()
	if n != 1 {
		t.Fatalf("new id rehydrated %d snapshots, want 1", n)
	}

	// The OLD id now sees nothing — the rows moved, they were not copied.
	s3 := NewStore("agent3", dir)
	s3.SetSessionID(oldID)
	s3.Rehydrate()
	s3.mu.Lock()
	oldN := len(s3.snapshots)
	s3.mu.Unlock()
	if oldN != 0 {
		t.Fatalf("old id rehydrated %d snapshots, want 0 (rows must move)", oldN)
	}
}

// TestRekeySessionNoopWhenAlreadyBound pins idempotence: rekeying to the bound
// id is a no-op and never errors or drops history.
func TestRekeySessionNoopWhenAlreadyBound(t *testing.T) {
	clearJournalCache()
	t.Cleanup(clearJournalCache)
	dir := t.TempDir()
	work := t.TempDir()
	target := filepath.Join(work, "a.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore("agent1", dir)
	s.SetSessionID("ses_x")
	if err := s.Backup(target, "tc_1"); err != nil {
		t.Fatal(err)
	}
	s.RekeySession("ses_x", "ses_x")
	s.mu.Lock()
	n := len(s.snapshots)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("snapshots = %d, want 1", n)
	}
}
