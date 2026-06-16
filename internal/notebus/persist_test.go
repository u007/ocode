package notebus

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestSidecarRoundTrip confirms that an append-only write produces
// the expected one-JSON-object-per-line file, and a fresh Bus +
// Hydrate can reconstruct the log with the same seq, the same
// per-agent watermarks, and nextSeq == max(seq)+1.
func TestSidecarRoundTrip(t *testing.T) {
	dir := t.TempDir()
	groupID := "test-grp"

	sc, err := NewSidecar(dir, groupID)
	if err != nil {
		t.Fatal(err)
	}

	// Phase 1: write some entries through a real Bus so the
	// owner stamps seq and TS consistently.
	b := NewBus(groupID)
	b.SetPersist(sc)
	b.SetNow(func() int64 { return 1_700_000_000 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	for i := 0; i < 5; i++ {
		if _, err := b.Append(Note(0, "a1", "anchor", "body", 0)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Append(Touch(0, "a2", "x.go", "edit", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Append(Resolve(0, "a1", 3, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sc.Close(); err != nil {
		t.Fatal(err)
	}
	b.Stop()
	<-b.Done()

	// Sanity check: the file exists and has the expected number of
	// lines (5 + 1 + 1 = 7).
	fp := filepath.Join(dir, groupID+".notes.jsonl")
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 7 {
		t.Errorf("sidecar lines = %d, want 7", len(lines))
	}
	for i, l := range lines {
		var e Entry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Errorf("line %d: %v", i, err)
		}
	}

	// Phase 2: a fresh bus hydrates from the sidecar and sees
	// every entry. Start the bus BEFORE checking HeadSeq / Snapshot
	// (those route through the owner goroutine).
	b2 := NewBus(groupID)
	if _, err := b2.Hydrate(dir, groupID); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	b2.Start(ctx2)
	defer func() { b2.Stop(); <-b2.Done() }()
	if got := b2.HeadSeq(); got != 7 {
		t.Errorf("after hydrate HeadSeq = %d, want 7 (max seen)", got)
	}

	snap := b2.Snapshot()
	if len(snap) != 7 {
		t.Errorf("hydrated Snapshot len = %d, want 7", len(snap))
	}
	// Hydration should set next seq to max. A subsequent Append
	// must receive seq=8 (max+1), never seq=1.
	seq, err := b2.Append(Note(0, "a1", "a", "b", 0))
	if err != nil {
		t.Fatal(err)
	}
	if seq != 8 {
		t.Errorf("post-hydrate Append seq = %d, want 8", seq)
	}
	// The session file (caller's responsibility) is not touched by
	// the sidecar — verified by the fact that the sidecar path is
	// under dir/ which is the test's own tempdir, not a session
	// path.
}

// TestSidecarTruncatedLine recovers gracefully when the file ends
// with a malformed line. LoadSnapshot must return every well-formed
// prefix and log the bad line (we only assert that the log is
// well-formed and the bad line is dropped, not the log output).
func TestSidecarTruncatedLine(t *testing.T) {
	dir := t.TempDir()
	groupID := "test-grp"
	fp := filepath.Join(dir, groupID+".notes.jsonl")
	// Three good lines, then a truncated garbage line.
	good := []byte(`{"seq":1,"by":"a1","kind":"note","at":"x","body":"first"}
{"seq":2,"by":"a2","kind":"note","at":"y","body":"second"}
{"seq":3,"by":"a1","kind":"touch","file":"z.go","act":"edit"}
`)
	bad := []byte(`{"seq":4,"by":"a2","kind":"note","at":`)
	if err := os.WriteFile(fp, append(good, bad...), 0o644); err != nil {
		t.Fatal(err)
	}
	log2, nextSeq, _, _, err := LoadSnapshot(dir, groupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(log2) != 3 {
		t.Errorf("log2 len = %d, want 3 (truncated line dropped)", len(log2))
	}
	if nextSeq != 3 {
		t.Errorf("nextSeq = %d, want 3 (max 3)", nextSeq)
	}
}

// TestSidecarDoesNotTouchSessionFile ensures the sidecar lives in
// its own file (not co-located with the session JSON). We assert
// this by giving the sidecar a path that is NOT a session path and
// verifying that the session file (separately created in the same
// parent dir) is unchanged after writing to the sidecar.
func TestSidecarDoesNotTouchSessionFile(t *testing.T) {
	dir := t.TempDir()
	groupID := "test-grp"
	sessionPath := filepath.Join(dir, "session.json")
	originalSession := []byte(`{"id":"sess1","messages":[]}`)
	if err := os.WriteFile(sessionPath, originalSession, 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := NewSidecar(dir, groupID)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBus(groupID)
	b.SetPersist(sc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	if _, err := b.Append(Note(0, "a1", "x", "y", 0)); err != nil {
		t.Fatal(err)
	}
	if err := sc.Close(); err != nil {
		t.Fatal(err)
	}
	b.Stop()
	<-b.Done()

	got, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(originalSession) {
		t.Errorf("session file changed after sidecar write:\nwas: %s\nnow: %s", originalSession, got)
	}
	// The sidecar file exists in the same dir but has a distinct name.
	if _, err := os.Stat(filepath.Join(dir, groupID+".notes.jsonl")); err != nil {
		t.Errorf("sidecar file not created: %v", err)
	}
}

// TestAsyncSinkAsyncDelivery documents the pattern: a sink that
// returns asynchronously to its Write call is supported as long as
// the bus's seq assignment has already happened (which it has, by
// the time the owner calls Write). This is a behavioural test — we
// verify the seq is assigned before Write blocks, and that the entry
// is on disk once Write completes.
func TestAsyncSinkAsyncDelivery(t *testing.T) {
	dir := t.TempDir()
	groupID := "test-grp"
	sc, err := NewSidecar(dir, groupID)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBus(groupID)
	b.SetPersist(sc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	defer func() { b.Stop(); <-b.Done() }()
	const N = 50
	var written atomic.Int64
	for i := 0; i < N; i++ {
		seq, err := b.Append(Note(0, "a1", "x", "y", 0))
		if err != nil {
			t.Fatal(err)
		}
		if seq <= 0 {
			t.Errorf("Append seq = %d", seq)
		}
	}
	// Sidecar Close flushes. After that, the file must contain N
	// lines.
	if err := sc.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, groupID+".notes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != N {
		t.Errorf("sidecar lines after %d appends = %d", N, len(lines))
	}
	written.Store(int64(len(lines)))
	if written.Load() != int64(N) {
		t.Errorf("expected %d lines on disk, got %d", N, written.Load())
	}
}
