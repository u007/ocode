package agent

import (
	"context"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestBriefSeeding_AppendsBeforeSpawn confirms that when an
// orchestrator-supplied brief is provided to the group-spawn
// path, the brief's entries are appended to the bus BEFORE
// the children are spawned. Each child's first-loop delta
// therefore contains the brief (by="main", seq < any child
// entry).
func TestBriefSeeding_AppendsBeforeSpawn(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	// The brief is a list of note entries authored as
	// by="main". The orchestrator computes it once; the
	// group-spawn path just appends them.
	brief := []notebus.Entry{
		notebus.Note(0, "main", "change-set:summary", "5 files changed, 3 partitions", 0),
		notebus.Note(0, "main", "partition:a1", "core/auth + token validation", 0),
		notebus.Note(0, "main", "partition:a2", "internal/agent + notes bus", 0),
		notebus.Note(0, "main", "partition:a3", "tools/patch + apply_patch", 0),
	}
	if err := seedBrief(bus, brief); err != nil {
		t.Fatalf("seedBrief: %v", err)
	}

	// A new child (a1) wiring onto this bus must see the
	// brief in its first delta.
	delta := bus.Delta("a1")
	if len(delta) != len(brief) {
		t.Fatalf("a1 delta len = %d, want %d (the brief)", len(delta), len(brief))
	}
	for i, e := range delta {
		if e.By != "main" {
			t.Errorf("delta[%d].By = %q, want main", i, e.By)
		}
	}
}

// TestBriefSeeding_NilBriefStillWorks confirms a group with no
// brief starts empty. The non-brief case is the common one
// (callers that do not pre-compute a brief get a bus that
// works normally — no entries, no errors).
func TestBriefSeeding_NilBriefStillWorks(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	// No brief → no seed. The bus is just empty.
	if err := seedBrief(bus, nil); err != nil {
		t.Errorf("nil brief: %v", err)
	}
	if len(bus.Snapshot()) != 0 {
		t.Errorf("bus not empty after nil brief: %d entries", len(bus.Snapshot()))
	}
}

// TestBriefSeeding_EmptyBriefStillWorks confirms an empty
// (non-nil) brief is equivalent to a nil one. This protects
// callers that compute a brief and end up with no entries
// (e.g. a change set with no partitions).
func TestBriefSeeding_EmptyBriefStillWorks(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	if err := seedBrief(bus, []notebus.Entry{}); err != nil {
		t.Errorf("empty brief: %v", err)
	}
	if len(bus.Snapshot()) != 0 {
		t.Errorf("bus not empty after empty brief: %d entries", len(bus.Snapshot()))
	}
}

// TestBriefSeeding_SeqIsStable confirms the brief entries
// receive seq 1, 2, 3, ... in order. This is the same
// sequence children will see in their first delta. The
// stability is needed so a child's <oc-resolve ref="N">
// references a deterministic seq (e.g. an orchestrator that
// wants to seed a "rule 42 says…" brief entry and then
// resolve it from a child).
func TestBriefSeeding_SeqIsStable(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	brief := []notebus.Entry{
		notebus.Note(0, "main", "x", "first", 0),
		notebus.Note(0, "main", "y", "second", 0),
		notebus.Note(0, "main", "z", "third", 0),
	}
	if err := seedBrief(bus, brief); err != nil {
		t.Fatal(err)
	}
	snap := bus.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(snap))
	}
	for i, e := range snap {
		wantSeq := int64(i + 1)
		if e.Seq != wantSeq {
			t.Errorf("snap[%d].Seq = %d, want %d", i, e.Seq, wantSeq)
		}
	}
}

// TestBriefSeeding_PersistsToSidecar confirms the brief
// entries hit the sidecar (so a crash mid-group can recover
// them). This is the same path every other entry takes —
// the brief is no different from a child's note in terms
// of persistence.
func TestBriefSeeding_PersistsToSidecar(t *testing.T) {
	dir := t.TempDir()
	bus := notebus.NewBus("grp")
	sc, err := notebus.NewSidecar(dir, "grp")
	if err != nil {
		t.Fatal(err)
	}
	bus.SetPersist(sc)
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	brief := []notebus.Entry{
		notebus.Note(0, "main", "x", "first", 0),
		notebus.Note(0, "main", "y", "second", 0),
	}
	if err := seedBrief(bus, brief); err != nil {
		t.Fatal(err)
	}
	// Stopping the bus flushes the persist sink.
	bus.Stop()
	<-bus.Done()
	sc.Close()

	// Reload and confirm the brief entries are on disk.
	sc2, err := notebus.NewSidecar(dir, "grp")
	if err != nil {
		t.Fatal(err)
	}
	defer sc2.Close()
	entries, _, _, _, err := notebus.LoadSnapshot(dir, "grp")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("reloaded entries = %d, want 2", len(entries))
	}
	for i, e := range entries {
		if e.By != "main" {
			t.Errorf("entries[%d].By = %q, want main", i, e.By)
		}
	}
}

// TestBriefSeeding_OverwritesSeqZero confirms a child that
// later appends a note gets a seq higher than the brief's.
// The brief occupies the low seqs; children get the high
// ones. The bus stamps Seq on Append — the brief's entries
// come in with seq=0 in the Entry literal, and the bus
// assigns them real seqs.
func TestBriefSeeding_OverwritesSeqZero(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	brief := []notebus.Entry{
		notebus.Note(0, "main", "x", "first", 0),
		notebus.Note(0, "main", "y", "second", 0),
	}
	if err := seedBrief(bus, brief); err != nil {
		t.Fatal(err)
	}
	// Now a child appends.
	seq, err := bus.Append(notebus.Note(0, "a1", "z.go", "child-1", 0))
	if err != nil {
		t.Fatal(err)
	}
	if seq != 3 {
		t.Errorf("child seq = %d, want 3 (after 2 brief entries)", seq)
	}
}
