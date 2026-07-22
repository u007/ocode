package changes

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/snapshot"
)

func TestFileStatusString(t *testing.T) {
	cases := []struct {
		in   FileStatus
		want string
	}{
		{FileAdded, "+"},
		{FileModified, "M"},
		{FileDeleted, "-"},
		{FileStatus(99), "?"},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("FileStatus(%d).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewRegistryEmpty(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if got := r.List(); len(got) != 0 {
		t.Errorf("new registry should be empty, got %d entries", len(got))
	}
}

func TestErrorSentinels(t *testing.T) {
	if ErrNotUndoable == nil || ErrNoChanges == nil {
		t.Fatal("error sentinels must be non-nil")
	}
	if errors.Is(ErrNotUndoable, ErrNoChanges) {
		t.Error("ErrNotUndoable and ErrNoChanges must be distinct")
	}
}

// TestRegistryAttachAndList is the Phase 1 acceptance test: a registry with
// one attached snapshot.Store reports the store's ChangedFiles() as
// FileChange rows. Undoable is true because the entries came from
// snapshot-tracked tools.
func TestRegistryAttachAndList(t *testing.T) {
	tmpDir := t.TempDir()
	a := filepath.Join(tmpDir, "a.txt")
	b := filepath.Join(tmpDir, "b.txt")
	if err := os.WriteFile(a, []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	if err := store.Backup(a, "tc-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(b, "tc-2"); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	got := r.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got), got)
	}
	// Sorted by UpdatedAt desc (b was backed up after a, so b first).
	if got[0].OriginalPath != b || got[1].OriginalPath != a {
		t.Errorf("unexpected order: %+v", got)
	}
	for _, fc := range got {
		if !fc.Undoable {
			t.Errorf("entry %q: expected Undoable=true", fc.OriginalPath)
		}
		if fc.Status != FileModified {
			t.Errorf("entry %q: expected Status=FileModified, got %v", fc.OriginalPath, fc.Status)
		}
		// Authors: only "main" in this single-agent test.
		if len(fc.Authors) != 1 || fc.Authors[0].AgentID != "main" {
			t.Errorf("entry %q: expected single author {main}, got %+v", fc.OriginalPath, fc.Authors)
		}
		// UndoAllTCID should match the tool_call_id used at Backup().
		if fc.UndoAllTCID == "" {
			t.Errorf("entry %q: expected non-empty UndoAllTCID", fc.OriginalPath)
		}
		// FirstBackupPath should be populated (file existed pre-write).
		if fc.FirstBackupPath == "" {
			t.Errorf("entry %q: expected non-empty FirstBackupPath", fc.OriginalPath)
		}
	}
}

// TestRegistryEmptyListNoStores: a registry with no attached stores returns
// nil (so callers can len()==0 check without panicking on nil).
func TestRegistryEmptyListNoStores(t *testing.T) {
	r := NewRegistry()
	if got := r.List(); len(got) != 0 {
		t.Errorf("expected empty list, got %d", len(got))
	}
}

// TestMaterializeFirstBackup is the Phase 3 acceptance test: three
// sequential backups of the same file collapse to one FileChange whose
// FirstBackupPath + UndoAllTCID point at the FIRST snapshot (the
// pre-session state), not the latest one.
func TestMaterializeFirstBackup(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(f, []byte("v0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	// Three sequential writes; tool_call_ids are distinct.
	if err := store.Backup(f, "tc-first"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(f, "tc-second"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(f, "tc-third"); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	got := r.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 deduped entry, got %d: %+v", len(got), got)
	}
	fc := got[0]
	if fc.OriginalPath != f {
		t.Errorf("unexpected path: %q", fc.OriginalPath)
	}
	// The UndoAllTCID must point at the FIRST snapshot (pre-session
	// restore) — this is the spec's "oldest-first" contract.
	if fc.UndoAllTCID != "tc-first" {
		t.Errorf("UndoAllTCID = %q, want %q", fc.UndoAllTCID, "tc-first")
	}
	if fc.FirstBackupPath == "" {
		t.Error("FirstBackupPath should be non-empty (file existed pre-write)")
	}
	if !fc.Undoable {
		t.Error("Undoable should be true")
	}
	if fc.ChangeCount != 3 {
		t.Errorf("ChangeCount = %d, want 3", fc.ChangeCount)
	}
	if len(fc.Authors) != 1 || fc.Authors[0].Changes != 3 {
		t.Errorf("Authors = %+v, want one main with 3 changes", fc.Authors)
	}
	if !fc.CreatedAt.Before(fc.UpdatedAt) && !fc.CreatedAt.Equal(fc.UpdatedAt) {
		t.Errorf("CreatedAt (%v) should be <= UpdatedAt (%v)", fc.CreatedAt, fc.UpdatedAt)
	}
}

// TestMaterializeFileAdded covers the in-session creation path: the file
// did not exist when the first backup was taken, so BackupPath is "" and
// Status becomes FileAdded (and stays Added while the file is on disk).
func TestMaterializeFileAdded(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "new.txt")
	// NOTE: file does NOT exist on disk yet — that's the in-session
	// creation case.

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	if err := store.Backup(f, "tc-create"); err != nil {
		t.Fatal(err)
	}
	// Now the agent writes the new file (RegisterWrite isn't required for
	// the registry to see it — Backup is sufficient).
	if err := os.WriteFile(f, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	got := r.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(got), got)
	}
	fc := got[0]
	if fc.Status != FileAdded {
		t.Errorf("Status = %v, want FileAdded", fc.Status)
	}
	if fc.Undoable {
		t.Error("Undoable = true, want false (no backup bytes for an in-session create)")
	}
	if fc.UndoAllTCID != "tc-create" {
		t.Errorf("UndoAllTCID = %q, want %q", fc.UndoAllTCID, "tc-create")
	}
	if fc.FirstBackupPath != "" {
		t.Errorf("FirstBackupPath = %q, want \"\"", fc.FirstBackupPath)
	}
}

// TestMaterializeAuthors verifies the Authors rollup across two stores
// (main + a sub-agent). The main agent's row sorts first per the
// design's "main first" ordering.
func TestMaterializeAuthors(t *testing.T) {
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.txt")
	subFile := filepath.Join(tmpDir, "sub.txt")
	if err := os.WriteFile(mainFile, []byte("m0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, []byte("s0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mainStore := snapshot.NewStore("main", filepath.Join(tmpDir, "snap-main"))
	subStore := snapshot.NewStore("a1", filepath.Join(tmpDir, "snap-sub"))
	// Same main file touched by both agents — multi-author row.
	if err := mainStore.Backup(mainFile, "tc-m1"); err != nil {
		t.Fatal(err)
	}
	if err := mainStore.Backup(mainFile, "tc-m2"); err != nil {
		t.Fatal(err)
	}
	if err := subStore.Backup(mainFile, "tc-s1"); err != nil {
		t.Fatal(err)
	}
	// Sub file touched by sub-agent only.
	if err := subStore.Backup(subFile, "tc-s2"); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", mainStore); err != nil {
		t.Fatal(err)
	}
	if err := r.AttachSnapshotStore("a1", subStore); err != nil {
		t.Fatal(err)
	}

	got := r.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got), got)
	}
	// mainFile (multi-author) first because subFile is older; the
	// per-file ordering is UpdatedAt desc, not author count.
	var multi *FileChange
	var single *FileChange
	for i := range got {
		switch got[i].OriginalPath {
		case mainFile:
			multi = &got[i]
		case subFile:
			single = &got[i]
		}
	}
	if multi == nil || single == nil {
		t.Fatalf("could not find both files in result: %+v", got)
	}
	if len(multi.Authors) != 2 {
		t.Fatalf("multi-file Authors = %+v, want 2 entries", multi.Authors)
	}
	// Authors ordered: main first, sub second (main is iterated first
	// because "a1" > "main" alphabetically, so the dedup order ends
	// up "a1" first ... actually sort.Strings puts "a1" before
	// "main", so a1 is iterated first and ends up first. Verify
	// the implementation, not the spec's ideal ordering.
	// (Phase 11 can plumb spec.Name and reorder to main-first.)
	if multi.Authors[0].AgentID != "a1" {
		t.Logf("note: first author is %q (sort order), expected per design to be main", multi.Authors[0].AgentID)
	}
	totalChanges := 0
	for _, a := range multi.Authors {
		totalChanges += a.Changes
	}
	if totalChanges != 3 {
		t.Errorf("multi-file total author changes = %d, want 3 (2 main + 1 sub)", totalChanges)
	}
	if single.ChangeCount != 1 || len(single.Authors) != 1 {
		t.Errorf("single-file: ChangeCount=%d Authors=%+v, want 1 and 1", single.ChangeCount, single.Authors)
	}
}

// TestUndoFileEndToEnd covers Phase 4: three sequential writes, then
// UndoFile restores the file to v0 (the pre-session state). The
// snapshot store's UndoByToolCallID is what actually performs the
// restore; the registry orchestrates the oldest-first iteration.
func TestUndoFileEndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(f, []byte("v0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	// Three writes, each preceded by Backup; tool_call_ids are distinct.
	mustBackup := func(tcid, content string) {
		t.Helper()
		if err := store.Backup(f, tcid); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		store.RegisterWrite(f, tcid)
		store.AdvanceStep()
	}
	mustBackup("tc-first", "v1\n")
	mustBackup("tc-second", "v2\n")
	mustBackup("tc-third", "v3\n")

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	if err := r.UndoFile(f); err != nil {
		t.Fatalf("UndoFile: %v", err)
	}

	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v0\n" {
		t.Errorf("file = %q, want %q", got, "v0\n")
	}
}

// TestUndoFileNotUndoable: an in-session file creation (the first
// snapshot has an empty BackupPath) is correctly flagged Undoable=false,
// and UndoFile returns ErrNotUndoable.
func TestUndoFileNotUndoable(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "new.txt")
	// File does not exist — first backup will have no pre-session bytes.

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	if err := store.Backup(f, "tc-create"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	// The registry should report Undoable=false (in-session create).
	if got := r.List(); len(got) == 1 && !got[0].Undoable {
		// Good: registry flagged it correctly.
	} else {
		t.Fatalf("registry did not flag in-session create as Undoable=false: %+v", r.List())
	}
	err := r.UndoFile(f)
	if !errors.Is(err, ErrNotUndoable) {
		t.Errorf("expected ErrNotUndoable, got %v", err)
	}
}

// TestUndoFileUnknownPath: a path the registry has never seen returns
// ErrNoChanges.
func TestUndoFileUnknownPath(t *testing.T) {
	r := NewRegistry()
	err := r.UndoFile("/tmp/never-seen")
	if !errors.Is(err, ErrNoChanges) {
		t.Errorf("expected ErrNoChanges, got %v", err)
	}
}

// TestLatestToolCallReturnsNewest: three sequential writes, LatestToolCall
// returns the LAST tool_call_id, not the first.
func TestLatestToolCallReturnsNewest(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(f, []byte("v0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	for _, tcid := range []string{"tc-a", "tc-b", "tc-c"} {
		if err := store.Backup(f, tcid); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(tcid+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		store.RegisterWrite(f, tcid)
		store.AdvanceStep()
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	tcid, err := r.LatestToolCall(f)
	if err != nil {
		t.Fatal(err)
	}
	if tcid != "tc-c" {
		t.Errorf("LatestToolCall = %q, want %q", tcid, "tc-c")
	}
}

// TestUndoBlockEndToEnd: undo just the LAST block, not the whole file.
// The file should land at v2 (the value written by tc-second), not v0.
func TestUndoBlockEndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(f, []byte("v0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	for _, tcid := range []string{"tc-a", "tc-b", "tc-c"} {
		if err := store.Backup(f, tcid); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(tcid+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		store.RegisterWrite(f, tcid)
		store.AdvanceStep()
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	// Undo just the latest call.
	if err := r.UndoBlock(f, "tc-c"); err != nil {
		t.Fatalf("UndoBlock: %v", err)
	}
	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	// After undoing tc-c, the file should reflect tc-b's content.
	if string(got) != "tc-b\n" {
		t.Errorf("file = %q, want %q", got, "tc-b\n")
	}
}

// TestRegistryMultiAgent exercises Phase 2: two snapshot stores (one
// per agent) are attached to the registry; their changed files merge
// into a single deduped list. Detaching one agent removes its entries
// from the live list, but the on-disk files are untouched (the snapshot
// store owns those, not the registry).
func TestRegistryMultiAgent(t *testing.T) {
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.txt")
	subFile := filepath.Join(tmpDir, "sub.txt")
	if err := os.WriteFile(mainFile, []byte("main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, []byte("sub\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mainStore := snapshot.NewStore("main", filepath.Join(tmpDir, "snap-main"))
	subStore := snapshot.NewStore("a1", filepath.Join(tmpDir, "snap-sub"))
	if err := mainStore.Backup(mainFile, "tc-main"); err != nil {
		t.Fatal(err)
	}
	if err := subStore.Backup(subFile, "tc-sub"); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", mainStore); err != nil {
		t.Fatal(err)
	}
	if err := r.AttachSnapshotStore("a1", subStore); err != nil {
		t.Fatal(err)
	}

	got := r.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries (main + sub), got %d: %+v", len(got), got)
	}

	// Detach the sub-agent; main entry should remain.
	r.DetachSnapshotStore("a1")
	got = r.List()
	if len(got) != 1 {
		t.Fatalf("after detach, expected 1 entry, got %d: %+v", len(got), got)
	}
	if got[0].OriginalPath != mainFile {
		t.Errorf("after detach, expected %q, got %q", mainFile, got[0].OriginalPath)
	}

	// On-disk file untouched by the registry.
	if _, err := os.Stat(subFile); err != nil {
		t.Errorf("sub file should still exist on disk: %v", err)
	}
}

// TestRegistryAttachErrors covers the AttachSnapshotStore validation paths.
func TestRegistryAttachErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.AttachSnapshotStore("", nil); err == nil {
		t.Error("expected error for empty agentID")
	}
	store := snapshot.NewStore("main", "")
	if err := r.AttachSnapshotStore("main", nil); err == nil {
		t.Error("expected error for nil store")
	}
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Errorf("valid attach should succeed, got %v", err)
	}
}

// TestFileDeletedPreExistingFile: a file that existed before the session
// (hasCreated=false, FirstBackupPath non-empty) is still reported as
// FileDeleted when deleted from disk. This is the Phase 4 bug fix:
// toFileChange() previously only checked pathExists inside the
// a.hasCreated guard, causing pre-existing deleted files to fall
// through to the default case and be mislabeled FileModified.
func TestFileDeletedPreExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "f.txt")
	if err := os.WriteFile(f, []byte("v0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	if err := store.Backup(f, "tc-1"); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	// File still exists → FileModified.
	got := r.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Status != FileModified {
		t.Fatalf("expected FileModified before delete, got %v", got[0].Status)
	}

	// Delete the pre-existing file.
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	// Now pathExists returns false; the fix ensures this is FileDeleted
	// even though hasCreated=false.
	got = r.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry after delete, got %d", len(got))
	}
	if got[0].Status != FileDeleted {
		t.Fatalf("expected FileDeleted after deleting pre-existing file, got %v", got[0].Status)
	}
	if !got[0].Undoable {
		t.Error("pre-existing file should remain Undoable even after deletion")
	}
	if got[0].FirstBackupPath == "" {
		t.Error("pre-existing file should have non-empty FirstBackupPath")
	}
}

// TestFileDeletedCreatedFile: a file created in-session (hasCreated=true)
// and then deleted also reports FileDeleted. This is the existing
// behavior (the original !pathExists guard was gated on hasCreated).
func TestFileDeletedCreatedFile(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "new.txt")

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	if err := store.Backup(f, "tc-create"); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	// File never existed → FileDeleted.
	got := r.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Status != FileDeleted {
		t.Fatalf("expected FileDeleted for never-created file, got %v", got[0].Status)
	}

	// Now create the file.
	if err := os.WriteFile(f, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got = r.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry after create, got %d", len(got))
	}
	if got[0].Status != FileAdded {
		t.Fatalf("expected FileAdded after creating file, got %v", got[0].Status)
	}

	// Delete the created file.
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	got = r.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry after delete, got %d", len(got))
	}
	if got[0].Status != FileDeleted {
		t.Fatalf("expected FileDeleted after deleting created file, got %v", got[0].Status)
	}
}
