package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/snapshot"
)

// TestChangesSnapshotRehydratesJournalWithoutAgent is the regression guard for
// the Changes tab going blank on a session switch: a restored/idle-evicted
// session has no live agent, but its backups are still journaled, so
// changesSnapshot must reconstruct the list from snapshots.sqlite instead of
// returning [].
//
// Mutation check: reverting the journalChangesSnapshot fallback makes this
// test fail with an empty list.
func TestChangesSnapshotRehydratesJournalWithoutAgent(t *testing.T) {
	// Isolate GlobalDataDir (~/.local/share/opencode on darwin) so the test
	// writes its journal into a temp home rather than the real user store.
	t.Setenv("HOME", t.TempDir())

	workDir := t.TempDir()
	proj := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(workDir)
	h.projects = newTestProjectStore(t, workDir, proj)

	id := "ses_journal_changes"
	saveSessionToDir(t, proj, id)

	// Seed the journal exactly where the handler will look for it.
	base, err := paths.GlobalDataDir()
	if err != nil {
		t.Fatalf("GlobalDataDir: %v", err)
	}
	snapDir := filepath.Join(base, "project", paths.ProjectSlug(proj), "snapshots")
	store := snapshot.NewStore(snapshot.NewAgentID(), snapDir)
	store.SetSessionID(id)

	file := filepath.Join(proj, "foo.txt")
	if err := os.WriteFile(file, []byte("v1 before edit"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := store.Backup(file, "call-1"); err != nil {
		t.Fatalf("backup: %v", err)
	}
	// Post-backup content differs from the backup, so the row is "modified".
	if err := os.WriteFile(file, []byte("v2 after edit, longer"), 0644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	// No live agent registered for this session — the whole point.
	h.mu.Lock()
	if _, ok := h.agents[id]; ok {
		h.mu.Unlock()
		t.Fatalf("test precondition: session %s unexpectedly has a live agent", id)
	}
	h.mu.Unlock()

	list := h.changesSnapshot(id)
	if len(list) != 1 {
		t.Fatalf("changesSnapshot = %d rows, want 1 (journal rehydration)", len(list))
	}
	got := list[0]
	if got.OriginalPath != file {
		t.Errorf("OriginalPath = %q, want %q", got.OriginalPath, file)
	}
	if got.Status != "modified" {
		t.Errorf("Status = %q, want modified", got.Status)
	}
	if got.FirstBackupPath == "" {
		t.Error("FirstBackupPath is empty; diff would 404")
	}
	if got.Undoable {
		t.Error("Undoable = true for a rehydrated (no live agent) row; want false")
	}
	if got.UndoAllTCID != "" {
		t.Errorf("UndoAllTCID = %q, want empty for a rehydrated row", got.UndoAllTCID)
	}
}
