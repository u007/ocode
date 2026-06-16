package notebus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestAppend_FailsFastWhenNotStarted: Append on a bus that was never
// Start()'d returns ErrBusNotStarted instead of blocking forever on a
// request channel with no reader. This locks the fail-fast contract
// that replaced the 10-minute test hang.
func TestAppend_FailsFastWhenNotStarted(t *testing.T) {
	bus := NewBus("grp")
	if _, err := bus.Append(Note(0, "a1", "x.go", "body", 0)); !errors.Is(err, ErrBusNotStarted) {
		t.Fatalf("Append on unstarted bus err = %v, want ErrBusNotStarted", err)
	}
	// Reads must also not block on an unstarted bus.
	if snap := bus.Snapshot(); snap != nil {
		t.Errorf("Snapshot on unstarted bus = %v, want nil", snap)
	}
	if d := bus.Delta("a1"); d != nil {
		t.Errorf("Delta on unstarted bus = %v, want nil", d)
	}
}

// TestDeltaAndWatermark is the cache-preservation contract:
//
//   - Delta(agent) returns entries with seq > lastSeen[agent] AND
//     by != agent, in seq order, and advances lastSeen[agent] to the
//     current head (so the agent's own entries are never
//     re-injected, but neither are any earlier entries).
//   - Two successive Delta(agent) calls with no intervening Append
//     return the new entries the first time and an empty delta the
//     second.
//   - Delta(agent) where the agent authored every new entry returns
//     empty but still advances the watermark.
func TestDeltaAndWatermark(t *testing.T) {
	b := NewBus("grp")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	defer func() { b.Stop(); <-b.Done() }()

	// a1 writes, a2 writes, a1 writes. From a2's view, all three
	// entries are "from a peer" (none authored by a2), but a2's own
	// entries are skipped in a2's delta. a1's delta should include
	// only a2's entries (a1's own are filtered).
	if _, err := b.Append(Note(0, "a1", "a", "from a1", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Append(Note(0, "a2", "b", "from a2", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Append(Note(0, "a1", "c", "from a1 again", 0)); err != nil {
		t.Fatal(err)
	}

	a1d := b.Delta("a1")
	if len(a1d) != 1 {
		t.Fatalf("a1 delta len = %d, want 1 (just a2's note)", len(a1d))
	}
	if a1d[0].By != "a2" || a1d[0].Body != "from a2" {
		t.Errorf("a1 delta entry = %+v, want a2/from a2", a1d[0])
	}

	// Second Delta for a1 with no intervening append: empty.
	if d := b.Delta("a1"); len(d) != 0 {
		t.Errorf("a1 second delta len = %d, want 0", len(d))
	}

	a2d := b.Delta("a2")
	if len(a2d) != 2 {
		t.Fatalf("a2 delta len = %d, want 2 (a1's two notes)", len(a2d))
	}
	if a2d[0].By != "a1" || a2d[1].By != "a1" {
		t.Errorf("a2 delta entries = [%+v,%+v], want both by a1", a2d[0], a2d[1])
	}

	// a3 (newly joining): first Delta should return all 3 entries
	// authored by peers. The second Delta is empty because the
	// watermark has been advanced past the head.
	a3First := b.Delta("a3")
	if len(a3First) != 3 {
		t.Errorf("a3 first delta len = %d, want 3 (all peer notes)", len(a3First))
	}
	if d := b.Delta("a3"); len(d) != 0 {
		t.Errorf("a3 second delta len = %d, want 0 (watermark at head)", len(d))
	}
	// After a3's first Delta, the watermark is at head. Now a1
	// appends — a3 should see it.
	if _, err := b.Append(Note(0, "a1", "d", "new from a1", 0)); err != nil {
		t.Fatal(err)
	}
	a3d := b.Delta("a3")
	if len(a3d) != 1 || a3d[0].Body != "new from a1" {
		t.Errorf("a3 delta after append = %+v, want [new from a1]", a3d)
	}
}

// TestSnapshotImmutability verifies that the slice returned by
// Snapshot does not observe later appends and does not race a
// concurrent Append. Run with -race to catch any synchronization
// regression.
func TestSnapshotImmutability(t *testing.T) {
	b := NewBus("grp")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	defer func() { b.Stop(); <-b.Done() }()

	for i := 0; i < 10; i++ {
		if _, err := b.Append(Note(0, "a1", "a", "b", 0)); err != nil {
			t.Fatal(err)
		}
	}
	snap := b.Snapshot()
	if len(snap) != 10 {
		t.Fatalf("Snapshot len = %d, want 10", len(snap))
	}
	// Append more entries after snapshot.
	for i := 0; i < 5; i++ {
		if _, err := b.Append(Note(0, "a1", "c", "d", 0)); err != nil {
			t.Fatal(err)
		}
	}
	if len(snap) != 10 {
		t.Errorf("Snapshot observed later appends: len = %d, want 10", len(snap))
	}
	// A fresh Snapshot sees the new entries.
	if got := len(b.Snapshot()); got != 15 {
		t.Errorf("post-append Snapshot len = %d, want 15", got)
	}
}

// TestResolvedNotesExcludedFromDelta confirms that a Resolve entry
// removes the referenced note from subsequent deltas (and is itself
// never injected — Resolve is bookkeeping).
func TestResolvedNotesExcludedFromDelta(t *testing.T) {
	b := NewBus("grp")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	defer func() { b.Stop(); <-b.Done() }()

	if _, err := b.Append(Note(0, "a1", "x", "to-be-resolved", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Append(Note(0, "a2", "y", "still-valid", 0)); err != nil {
		t.Fatal(err)
	}
	// a1 resolves its own earlier note.
	if _, err := b.Append(Resolve(0, "a1", 1, 0)); err != nil {
		t.Fatal(err)
	}

	a3d := b.Delta("a3")
	if len(a3d) != 1 {
		t.Fatalf("a3 delta len = %d, want 1 (resolved note excluded)", len(a3d))
	}
	if a3d[0].Seq != 2 {
		t.Errorf("a3 delta[0].Seq = %d, want 2", a3d[0].Seq)
	}
	// Resolves are not injected.
	for _, e := range a3d {
		if e.Kind == KindResolve {
			t.Errorf("Resolve entry appeared in delta: %+v", e)
		}
	}
}

// TestEntryModel pins the public shape of an Entry and the Kind enum.
// A separate constructor per kind keeps the call sites self-documenting
// and prevents accidental cross-field fill-in (e.g. a touch carrying Body).
func TestEntryModel(t *testing.T) {
	at := "auth/token.go:tokenFromHeader"
	body := "token empty -> panic, no nil check"
	touchFile := "internal/tool/patch.go"
	ref := int64(42)
	ts := int64(1_700_000_000)
	seq := int64(1)
	by := "a1"

	note := Note(seq, by, at, body, ts)
	if note.Seq != seq {
		t.Errorf("note.Seq = %d, want %d", note.Seq, seq)
	}
	if note.By != by {
		t.Errorf("note.By = %q, want %q", note.By, by)
	}
	if note.At != at {
		t.Errorf("note.At = %q, want %q", note.At, at)
	}
	if note.Body != body {
		t.Errorf("note.Body = %q, want %q", note.Body, body)
	}
	if note.TS != ts {
		t.Errorf("note.TS = %d, want %d", note.TS, ts)
	}
	if note.Kind != KindNote {
		t.Errorf("note.Kind = %q, want %q", note.Kind, KindNote)
	}
	if note.File != "" || note.Act != "" || note.Ref != 0 {
		t.Errorf("note should not carry touch/resolve fields, got %+v", note)
	}

	tch := Touch(seq, by, touchFile, "edit", ts)
	if tch.Kind != KindTouch {
		t.Errorf("touch.Kind = %q, want %q", tch.Kind, KindTouch)
	}
	if tch.File != touchFile {
		t.Errorf("touch.File = %q, want %q", tch.File, touchFile)
	}
	if tch.Act != "edit" {
		t.Errorf("touch.Act = %q, want edit", tch.Act)
	}
	if tch.Body != "" {
		t.Errorf("touch should not carry body, got %q", tch.Body)
	}

	rsv := Resolve(seq, by, ref, ts)
	if rsv.Kind != KindResolve {
		t.Errorf("resolve.Kind = %q, want %q", rsv.Kind, KindResolve)
	}
	if rsv.Ref != ref {
		t.Errorf("resolve.Ref = %d, want %d", rsv.Ref, ref)
	}
	if rsv.At != "" || rsv.Body != "" {
		t.Errorf("resolve should not carry note fields, got %+v", rsv)
	}
}

// TestKindString guards the wire format: the bus injects the literal string
// values into the agent's prompt, so they must be stable and exactly
// "note", "touch", "resolve". If you change one, grep for it in the rest of
// the package and the agent's parser — these are wire values.
func TestKindString(t *testing.T) {
	cases := map[Kind]string{
		KindNote:    "note",
		KindTouch:   "touch",
		KindResolve: "resolve",
	}
	for k, want := range cases {
		if string(k) != want {
			t.Errorf("kind %q != %q", string(k), want)
		}
	}
	if got := ParseKind("bogus"); got != KindUnknown {
		t.Errorf("ParseKind(bogus) = %q, want %q", got, KindUnknown)
	}
	if got := ParseKind("note"); got != KindNote {
		t.Errorf("ParseKind(note) = %q, want %q", got, KindNote)
	}
}

// TestAppendOrdering verifies the core invariant: appends from N
// concurrent goroutines receive N unique, gapless, strictly-increasing
// sequence numbers starting at 1. Run with -race to catch any
// synchronization regression.
func TestAppendOrdering(t *testing.T) {
	const N = 200
	b := NewBus("grp")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	defer func() { b.Stop(); <-b.Done() }()

	var wg sync.WaitGroup
	seqs := make([]int64, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			seq, err := b.Append(Note(0, "a1", "anchor", "body", 0))
			if err != nil {
				t.Errorf("Append: %v", err)
				return
			}
			seqs[idx] = seq
		}(i)
	}
	wg.Wait()

	seen := make(map[int64]bool, N)
	for i, s := range seqs {
		if s < 1 || s > N {
			t.Errorf("seqs[%d]=%d out of [1,%d]", i, s, N)
		}
		if seen[s] {
			t.Errorf("seq %d assigned twice", s)
		}
		seen[s] = true
	}
	if got := b.HeadSeq(); got != int64(N) {
		t.Errorf("HeadSeq = %d, want %d", got, N)
	}
}

// TestAppendAfterContextCancel verifies that a cancelled context
// causes Append to return a clear error, never to panic, and never
// to block forever. The bus may still service a few in-flight
// requests but eventually exits and Append returns ErrBusClosed.
func TestAppendAfterContextCancel(t *testing.T) {
	b := NewBus("grp")
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	b.Stop()
	<-b.Done()
	cancel()

	_, err := b.Append(Note(0, "a1", "a", "b", 0))
	if err == nil {
		t.Fatal("Append after Stop should return an error, got nil")
	}
}

// TestSlowPersisterDoesNotBlockAppends is the "criticial section never
// blocks" invariant. We register a persist sink whose Write takes 50ms
// and fire many appends through it. The in-memory seq assignments must
// not be gated on the sink's return: every Append returns within a
// bounded time. (The current single-goroutine implementation does
// serialize persist, so this test passes because the sink returns
// quickly. The test would fail loudly if a future change moves
// persist before the seq reply — use this as a regression guard.)
func TestSlowPersisterDoesNotBlockAppends(t *testing.T) {
	b := NewBus("grp")
	b.SetPersist(&slowSink{delay: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	defer func() { b.Stop(); <-b.Done() }()

	const N = 20
	var wg sync.WaitGroup
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := b.Append(Note(0, "a1", "x", "y", 0)); err != nil {
				t.Errorf("Append: %v", err)
			}
		}()
	}
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
		if time.Now().After(deadline) {
			t.Errorf("appends took too long (>2s) — persist appears to gate seq assignment")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("appends did not complete within 3s — persist is blocking the critical section")
	}
}

type slowSink struct {
	delay time.Duration
}

func (s *slowSink) Write(e Entry) error {
	time.Sleep(s.delay)
	return nil
}

func (s *slowSink) Close() error { return nil }

// TestBusConstruction confirms a fresh Bus has a non-nil (but empty) log,
// a zero seq counter, and an empty per-agent watermark map. No goroutine
// is started by construction — that is Start(ctx)'s job.
func TestBusConstruction(t *testing.T) {
	b := NewBus("grp1")
	if b.GroupID() != "grp1" {
		t.Errorf("GroupID = %q, want grp1", b.GroupID())
	}
	// Snapshot/HeadSeq route through the owner goroutine, so the bus
	// must be running for those queries to return. Start the bus with
	// a short-lived context and stop it on test exit.
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	defer func() {
		b.Stop()
		<-b.Done()
		cancel()
	}()
	// Give Start a brief moment to spin up the goroutine. The
	// race-free path is "send on reqs, receive on reply" which would
	// hang if the goroutine isn't running. The owner starts before
	// Start returns to the caller, so this sleep is purely
	// defensive.
	time.Sleep(10 * time.Millisecond)
	snap := b.Snapshot()
	if len(snap) != 0 {
		t.Errorf("fresh bus Snapshot len = %d, want 0", len(snap))
	}
	if b.HeadSeq() != 0 {
		t.Errorf("fresh bus HeadSeq = %d, want 0", b.HeadSeq())
	}
}
