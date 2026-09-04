package changes

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/snapshot"
)

// TestRegistryCrossStoreWritePlusUndo is a regression test for the stale
// changes-tab cache: the old signature summed snapshot counts, so a write
// in one store (+1) landing between two List() calls together with an undo
// in another store (-1) summed to the same total and the new file was
// hidden until some later unrelated change. Versions are monotonic, so the
// sum always moves.
func TestRegistryCrossStoreWritePlusUndo(t *testing.T) {
	tmpDir := t.TempDir()
	a := filepath.Join(tmpDir, "a.txt")
	b := filepath.Join(tmpDir, "b.txt")
	c := filepath.Join(tmpDir, "c.txt")
	for _, p := range []string{a, b, c} {
		if err := os.WriteFile(p, []byte("v1\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	storeA := snapshot.NewStore("main", filepath.Join(tmpDir, "snaps-a"))
	storeB := snapshot.NewStore("sub", filepath.Join(tmpDir, "snaps-b"))
	if err := storeA.Backup(a, "tc-a"); err != nil {
		t.Fatal(err)
	}
	if err := storeB.Backup(b, "tc-b"); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", storeA); err != nil {
		t.Fatal(err)
	}
	if err := r.AttachSnapshotStore("sub", storeB); err != nil {
		t.Fatal(err)
	}

	// Prime the aggregate cache.
	if got := r.List(); len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}

	// Between two List() calls: one store loses a snapshot (undo),
	// the other gains one (new write). Net snapshot-count delta is 0.
	if err := r.UndoFile(b); err != nil {
		t.Fatalf("UndoFile(b): %v", err)
	}
	if err := storeA.Backup(c, "tc-c"); err != nil {
		t.Fatal(err)
	}

	got := r.List()
	paths := make(map[string]bool, len(got))
	for _, fc := range got {
		paths[fc.OriginalPath] = true
	}
	if len(got) != 2 || !paths[a] || !paths[c] {
		t.Fatalf("expected entries {a, c}, got %v", got)
	}
	if paths[b] {
		t.Fatalf("undone file b must not be listed: %v", got)
	}
}

// TestRegistryBashMergeIntoSnapshotRow verifies that a bash touch on a
// snapshot-tracked file surfaces its command on the row instead of being
// dropped (finalize used to skip registry entries for covered paths).
func TestRegistryBashMergeIntoSnapshotRow(t *testing.T) {
	tmpDir := t.TempDir()
	a := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(a, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snaps"))
	if err := store.Backup(a, "tc-1"); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	r.NotifyBashWrite(BashWriteEvent{
		Command:  "sed -i s/v1/v2/ a.txt",
		WorkDir:  tmpDir,
		ExitCode: 0,
		Touches:  []BashTouch{{Path: a, Op: BashModified}},
	})

	got := r.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %v", got)
	}
	fc := got[0]
	if !fc.Undoable {
		t.Error("snapshot-backed row must stay undoable after a bash touch")
	}
	if fc.LastBashCommand != "sed -i s/v1/v2/ a.txt" {
		t.Errorf("expected bash command merged into row, got %q", fc.LastBashCommand)
	}

	// A repeat touch with a newer command must be visible too (in-place
	// update: entry count doesn't change, so the signature must still move).
	r.NotifyBashWrite(BashWriteEvent{
		Command:  "echo v3 >> a.txt",
		WorkDir:  tmpDir,
		ExitCode: 0,
		Touches:  []BashTouch{{Path: a, Op: BashModified}},
	})
	got = r.List()
	if len(got) != 1 || got[0].LastBashCommand != "echo v3 >> a.txt" {
		t.Fatalf("expected refreshed bash command, got %v", got)
	}
}

// TestStoreVersionMonotonic guards the contract signatureLocked relies on:
// every membership mutation strictly increases Version(). The registry's
// cache signature is the summed store versions, so a mutation that fails to
// bump the version (in any of the membership-changing paths) would leave
// the changes tab showing stale rows until some unrelated change.
func TestStoreVersionMonotonic(t *testing.T) {
	tmpDir := t.TempDir()
	a := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(a, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snaps"))

	prev := store.Version()
	bump := func(what string) {
		t.Helper()
		if got := store.Version(); got <= prev {
			t.Fatalf("%s: version %d did not increase from %d", what, got, prev)
		}
		prev = store.Version()
	}

	// Backup bumps.
	for i, tc := range []string{"tc-1", "tc-2"} {
		if err := store.Backup(a, tc); err != nil {
			t.Fatal(err)
		}
		bump(fmt.Sprintf("backup %d", i))
	}

	// Successful UndoByToolCallID bumps (removes snapshots).
	if _, err := store.UndoByToolCallID("tc-2", 1000); err != nil {
		t.Fatalf("undo: %v", err)
	}
	bump("UndoByToolCallID")

	// Undo (pop newest snapshot) bumps.
	if _, err := store.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	bump("Undo")

	// DiscardRecent bumps.
	for _, tc := range []string{"tc-3", "tc-4"} {
		if err := store.Backup(a, tc); err != nil {
			t.Fatal(err)
		}
		prev = store.Version()
	}
	if err := store.DiscardRecent(1); err != nil {
		t.Fatalf("DiscardRecent: %v", err)
	}
	bump("DiscardRecent")

	// Reset bumps.
	store.Reset()
	bump("Reset")

	// Rehydrate bumps an empty journal-backed store, and is a no-op for a
	// store that already holds snapshots (live history is never reordered).
	storeR := snapshot.NewStore("mainR", filepath.Join(tmpDir, "snaps-r"))
	storeR.SetSessionID("sess-r")
	if err := storeR.Backup(a, "tc-r"); err != nil {
		t.Fatal(err)
	}
	storeR2 := snapshot.NewStore("mainR2", filepath.Join(tmpDir, "snaps-r"))
	storeR2.SetSessionID("sess-r")
	prevR := storeR2.Version()
	storeR2.Rehydrate()
	if got := storeR2.Version(); got <= prevR {
		t.Fatalf("Rehydrate: version %d did not increase from %d", got, prevR)
	}
	if got := len(storeR2.Snapshots()); got != 1 {
		t.Fatalf("Rehydrate: expected 1 snapshot loaded, got %d", got)
	}
	// Non-empty store: Rehydrate must leave the version untouched.
	storeR3 := snapshot.NewStore("mainR3", filepath.Join(tmpDir, "snaps-r"))
	storeR3.SetSessionID("sess-r")
	if err := storeR3.Backup(a, "tc-r3"); err != nil {
		t.Fatal(err)
	}
	prevR3 := storeR3.Version()
	storeR3.Rehydrate()
	if got := storeR3.Version(); got != prevR3 {
		t.Fatalf("Rehydrate on non-empty store: version changed %d -> %d", prevR3, got)
	}

	// A partial UndoByToolCallID (restore error midway through the loop)
	// still bumps: the failures that succeeded removed their snapshots, so
	// the membership did change. Make the OLDER snapshot's file
	// un-restorable by replacing it with a directory — os.WriteFile on a
	// directory fails deterministically with EISDIR (unlike chmod, which a
	// root-like CI user could bypass).
	x2 := filepath.Join(tmpDir, "x2.txt")
	y2 := filepath.Join(tmpDir, "y2.txt")
	if err := os.WriteFile(x2, []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(y2, []byte("y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	storeP := snapshot.NewStore("mainP", filepath.Join(tmpDir, "snaps-p"))
	// Both snapshots share one tool call id; the loop restores newest-first,
	// so y2 (the newer) succeeds and x2 (the older) fails.
	if err := storeP.Backup(x2, "tc-p"); err != nil {
		t.Fatal(err)
	}
	if err := storeP.Backup(y2, "tc-p"); err != nil {
		t.Fatal(err)
	}
	prevP := storeP.Version()
	if err := os.Remove(x2); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(x2, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := storeP.UndoByToolCallID("tc-p", 1000); err == nil {
		t.Fatal("expected partial undo to fail restoring x2")
	}
	if got := storeP.Version(); got <= prevP {
		t.Fatalf("partial UndoByToolCallID: version %d did not increase from %d", got, prevP)
	}
	// The younger file must have been restored before the failure.
	if data, err := os.ReadFile(y2); err != nil || string(data) != "y\n" {
		t.Fatalf("expected y2 restored to %q, got %q (err %v)", "y\n", data, err)
	}
}

// TestRegistryBashSnapshotOrdering covers interleaved bash and snapshot
// operations on the same path. finalizeLocked merges bash metadata into
// snapshot rows only when the bash event is the most recent one, and the
// live filesystem wins for FileStatus. Each case asserts the row's Status,
// UpdatedAt, LastBashCommand, and Undoable after the whole sequence.
func TestRegistryBashSnapshotOrdering(t *testing.T) {
	newCase := func(t *testing.T) (*snapshot.Store, *Registry, string) {
		t.Helper()
		dir := t.TempDir()
		a := filepath.Join(dir, "a.txt")
		if err := os.WriteFile(a, []byte("v0\n"), 0644); err != nil {
			t.Fatal(err)
		}
		store := snapshot.NewStore("main", filepath.Join(dir, "snaps"))
		r := NewRegistry()
		if err := r.AttachSnapshotStore("main", store); err != nil {
			t.Fatal(err)
		}
		return store, r, a
	}
	bashEvent := func(cmd string, touch BashTouch) BashWriteEvent {
		return BashWriteEvent{Command: cmd, ExitCode: 0, Touches: []BashTouch{touch}}
	}

	// Snapshot write -> bash touch -> newer snapshot write. The bash
	// command must not claim the row once a snapshot write superseded it,
	// status must come from the live filesystem, and UpdatedAt must be the
	// newest snapshot's timestamp.
	t.Run("snapshot_write_bash_touch_newer_snapshot", func(t *testing.T) {
		store, r, a := newCase(t)
		if err := store.Backup(a, "tc-1"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(a, []byte("bash v2\n"), 0644); err != nil {
			t.Fatal(err)
		}
		r.NotifyBashWrite(bashEvent("sed -i s/v1/v2/ a.txt", BashTouch{Path: a, Op: BashModified}))
		if err := os.WriteFile(a, []byte("v3\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := store.Backup(a, "tc-2"); err != nil {
			t.Fatal(err)
		}

		got := r.List()
		if len(got) != 1 {
			t.Fatalf("expected 1 row, got %v", got)
		}
		fc := got[0]
		if fc.Status != FileModified {
			t.Errorf("status = %v, want FileModified (file exists)", fc.Status)
		}
		if fc.LastBashCommand != "" {
			t.Errorf("LastBashCommand = %q, want empty — a snapshot write is the most recent event", fc.LastBashCommand)
		}
		if !fc.Undoable {
			t.Error("row must stay undoable (snapshot backup exists)")
		}
		snaps := store.Snapshots()
		if len(snaps) != 2 || !fc.UpdatedAt.Equal(snaps[1].Timestamp) {
			t.Errorf("UpdatedAt = %v, want newest snapshot %v", fc.UpdatedAt, snaps[1].Timestamp)
		}
	})

	// Bash delete -> snapshot recreation. The stored FileDeleted must not
	// resurrect once a newer snapshot write (or undo) put the file back.
	t.Run("bash_delete_then_snapshot_recreate", func(t *testing.T) {
		store, r, a := newCase(t)
		if err := store.Backup(a, "tc-1"); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(a); err != nil {
			t.Fatal(err)
		}
		r.NotifyBashWrite(bashEvent("rm a.txt", BashTouch{Path: a, Op: BashDeleted}))
		if err := os.WriteFile(a, []byte("recreated\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := store.Backup(a, "tc-2"); err != nil {
			t.Fatal(err)
		}

		got := r.List()
		if len(got) != 1 {
			t.Fatalf("expected 1 row, got %v", got)
		}
		fc := got[0]
		if fc.Status != FileModified {
			t.Errorf("status = %v, want FileModified — file exists on disk", fc.Status)
		}
		if fc.LastBashCommand != "" {
			t.Errorf("LastBashCommand = %q, want empty — the rm is no longer the most recent event", fc.LastBashCommand)
		}
		if !fc.Undoable {
			t.Error("row must stay undoable")
		}
	})

	// Bash delete -> UndoFile. Undo consumes every snapshot for the path,
	// leaving a bash-only (non-undoable) row; the restored file must read
	// as Modified (live filesystem), not the stored FileDeleted, and the
	// bash command is retained because bash is now the most recent event.
	t.Run("bash_delete_then_undo_file", func(t *testing.T) {
		store, r, a := newCase(t)
		if err := store.Backup(a, "tc-1"); err != nil {
			t.Fatal(err)
		}
		preUndo := store.Snapshots()[0].Timestamp
		if err := os.WriteFile(a, []byte("v2\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(a); err != nil {
			t.Fatal(err)
		}
		r.NotifyBashWrite(bashEvent("rm a.txt", BashTouch{Path: a, Op: BashDeleted}))

		if err := r.UndoFile(a); err != nil {
			t.Fatalf("UndoFile: %v", err)
		}
		got := r.List()
		if len(got) != 1 {
			t.Fatalf("expected 1 row, got %v", got)
		}
		fc := got[0]
		if fc.Status != FileModified {
			t.Errorf("status = %v, want FileModified — undo restored the file", fc.Status)
		}
		if fc.Undoable {
			t.Error("row must be non-undoable now (no snapshot backups remain)")
		}
		if fc.LastBashCommand != "rm a.txt" {
			t.Errorf("LastBashCommand = %q, want the rm command retained", fc.LastBashCommand)
		}
		if fc.UpdatedAt.Before(preUndo) {
			t.Errorf("UpdatedAt = %v, want at or after the pre-undo snapshot %v", fc.UpdatedAt, preUndo)
		}
	})

	// Bash-only delete -> later bash add. The in-place update must refresh
	// the row: same path, new op and command, no snapshot involvement.
	t.Run("bash_only_delete_then_add", func(t *testing.T) {
		dir := t.TempDir()
		a := filepath.Join(dir, "a.txt")
		if err := os.WriteFile(a, []byte("v1\n"), 0644); err != nil {
			t.Fatal(err)
		}
		r := NewRegistry() // no store attached: fully bash-only file
		if err := os.Remove(a); err != nil {
			t.Fatal(err)
		}
		r.NotifyBashWrite(bashEvent("rm a.txt", BashTouch{Path: a, Op: BashDeleted}))
		if err := os.WriteFile(a, []byte("v2\n"), 0644); err != nil {
			t.Fatal(err)
		}
		r.NotifyBashWrite(bashEvent("echo v2 > a.txt", BashTouch{Path: a, Op: BashAdded}))

		got := r.List()
		if len(got) != 1 {
			t.Fatalf("expected 1 row, got %v", got)
		}
		fc := got[0]
		if fc.Status != FileAdded {
			t.Errorf("status = %v, want FileAdded", fc.Status)
		}
		if fc.Undoable {
			t.Error("bash-only row must stay non-undoable")
		}
		if fc.LastBashCommand != "echo v2 > a.txt" {
			t.Errorf("LastBashCommand = %q, want the latest bash command", fc.LastBashCommand)
		}
	})
}
