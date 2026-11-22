package boxsession

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var jt0 = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

func newTestJournal(t *testing.T) (*Journal, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upstash-boxes.json")
	return NewJournal(path), path
}

func ownerLease(pid int, started time.Time, hb time.Time) Lease {
	return Lease{PID: pid, ProcessStart: started, Heartbeat: hb}
}

func mustList(t *testing.T, j *Journal) []Entry {
	t.Helper()
	entries, err := j.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return entries
}

func TestJournalMissingFileListsEmpty(t *testing.T) {
	j, _ := newTestJournal(t)
	if got := mustList(t, j); len(got) != 0 {
		t.Fatalf("List on missing file = %d entries, want 0", len(got))
	}
}

func TestJournalAddListAndDuplicate(t *testing.T) {
	j, _ := newTestJournal(t)
	e := Entry{BoxID: "box-a", AccountID: "acc", SessionID: "s1", Kind: KindEphemeral, CreatedAt: jt0}
	if err := j.Add(e); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := j.Add(e); err == nil {
		t.Fatal("second Add of the same box id succeeded, want an error")
	}
	got := mustList(t, j)
	if len(got) != 1 || got[0].BoxID != "box-a" || got[0].SessionID != "s1" {
		t.Fatalf("List = %+v, want the one box-a entry", got)
	}
}

func TestJournalAddValidatesFields(t *testing.T) {
	j, _ := newTestJournal(t)
	if err := j.Add(Entry{Kind: KindEphemeral}); err == nil {
		t.Error("Add with empty box id succeeded")
	}
	if err := j.Add(Entry{BoxID: "box-x", Kind: "forever"}); err == nil {
		t.Error("Add with unknown kind succeeded")
	}
}

func TestJournalListSortsByCreatedThenID(t *testing.T) {
	j, _ := newTestJournal(t)
	for _, e := range []Entry{
		{BoxID: "c", Kind: KindEphemeral, CreatedAt: jt0.Add(time.Minute)},
		{BoxID: "b", Kind: KindEphemeral, CreatedAt: jt0},
		{BoxID: "a", Kind: KindEphemeral, CreatedAt: jt0},
	} {
		if err := j.Add(e); err != nil {
			t.Fatalf("Add %s: %v", e.BoxID, err)
		}
	}
	got := mustList(t, j)
	want := []string{"a", "b", "c"}
	for i, id := range want {
		if got[i].BoxID != id {
			t.Fatalf("order = %v, want %v", ids(got), want)
		}
	}
}

func ids(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.BoxID
	}
	return out
}

func TestJournalRemoveExistingAndMissing(t *testing.T) {
	j, _ := newTestJournal(t)
	if err := j.Add(Entry{BoxID: "box-a", Kind: KindEphemeral, CreatedAt: jt0}); err != nil {
		t.Fatal(err)
	}
	if err := j.Remove("box-a"); err != nil {
		t.Fatalf("Remove existing: %v", err)
	}
	if err := j.Remove("box-a"); err != nil {
		t.Fatalf("Remove missing id returned %v, want nil", err)
	}
	if got := mustList(t, j); len(got) != 0 {
		t.Fatalf("after Remove, List = %+v, want empty", got)
	}
}

func TestJournalRekeyMovesOnlyMatchingSession(t *testing.T) {
	j, _ := newTestJournal(t)
	for _, e := range []Entry{
		{BoxID: "box-1", SessionID: "old", Kind: KindEphemeral, CreatedAt: jt0},
		{BoxID: "box-2", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: jt0},
		{BoxID: "box-3", SessionID: "other", Kind: KindEphemeral, CreatedAt: jt0},
	} {
		if err := j.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Rekey("old", "new"); err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	got := map[string]string{}
	for _, e := range mustList(t, j) {
		got[e.BoxID] = e.SessionID
	}
	if got["box-1"] != "new" || got["box-2"] != "new" || got["box-3"] != "other" {
		t.Fatalf("sessions after Rekey = %v", got)
	}
	if err := j.Rekey("nobody", "x"); err != nil {
		t.Fatalf("Rekey with no matches: %v", err)
	}
	if err := j.Rekey("new", ""); err == nil {
		t.Fatal("Rekey to an empty session id succeeded")
	}
}

func TestJournalHeartbeatMatchesPIDAndProcessStart(t *testing.T) {
	j, _ := newTestJournal(t)
	me := ownerLease(100, jt0, time.Time{})
	reused := ownerLease(100, jt0.Add(time.Hour), time.Time{}) // same PID, later process
	if err := j.Add(Entry{BoxID: "mine", Kind: KindEphemeral, CreatedAt: jt0, Lease: me}); err != nil {
		t.Fatal(err)
	}
	if err := j.Add(Entry{BoxID: "reused-pid", Kind: KindEphemeral, CreatedAt: jt0, Lease: reused}); err != nil {
		t.Fatal(err)
	}
	now := jt0.Add(10 * time.Second)
	if err := j.Heartbeat(me, now); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	for _, e := range mustList(t, j) {
		switch e.BoxID {
		case "mine":
			if !e.Lease.Heartbeat.Equal(now) {
				t.Errorf("mine heartbeat = %v, want %v", e.Lease.Heartbeat, now)
			}
		case "reused-pid":
			if !e.Lease.Heartbeat.IsZero() {
				t.Errorf("reused-pid heartbeat = %v, want untouched zero", e.Lease.Heartbeat)
			}
		}
	}
}

func TestJournalOrphans(t *testing.T) {
	j, _ := newTestJournal(t)
	me := ownerLease(1, jt0, jt0)
	const ttl = 90 * time.Second
	now := jt0.Add(10 * time.Minute)
	stale := ownerLease(2, jt0, jt0)                   // dead owner: heartbeat long ago
	fresh := ownerLease(3, jt0, now.Add(-time.Second)) // live owner: heartbeat just now
	entries := []Entry{
		{BoxID: "stale-ephemeral", Kind: KindEphemeral, CreatedAt: jt0, Lease: stale},
		{BoxID: "stale-retained", Kind: KindRetained, LoginState: "work", CreatedAt: jt0, Lease: stale},
		{BoxID: "fresh-ephemeral", Kind: KindEphemeral, CreatedAt: jt0, Lease: fresh},
		{BoxID: "mine-stale", Kind: KindEphemeral, CreatedAt: jt0, Lease: me},
	}
	for _, e := range entries {
		if err := j.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := j.Orphans(ownerLease(1, jt0, now), now, ttl, allDead)
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	if len(got) != 1 || got[0].BoxID != "stale-ephemeral" {
		t.Fatalf("Orphans = %v, want only stale-ephemeral", ids(got))
	}
}

// allDead is a liveness check under which no PID is running.
func allDead(Lease) bool { return false }

// allAlive is a liveness check under which every PID is running.
func allAlive(Lease) bool { return true }

func TestJournalOrphansNeedBothStaleHeartbeatAndDeadPID(t *testing.T) {
	j, _ := newTestJournal(t)
	const ttl = 90 * time.Second
	now := jt0.Add(10 * time.Minute)
	staleLive := ownerLease(2, jt0, jt0)                   // stale heartbeat, PID alive: a sleeping or slow peer
	staleDead := ownerLease(3, jt0, jt0)                   // stale heartbeat, PID dead: a real orphan
	freshDead := ownerLease(4, jt0, now.Add(-time.Second)) // fresh heartbeat, PID dead: not yet
	for _, e := range []Entry{
		{BoxID: "stale-live", Kind: KindEphemeral, CreatedAt: jt0, Lease: staleLive},
		{BoxID: "stale-dead", Kind: KindEphemeral, CreatedAt: jt0, Lease: staleDead},
		{BoxID: "fresh-dead", Kind: KindEphemeral, CreatedAt: jt0, Lease: freshDead},
	} {
		if err := j.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	owner := ownerLease(1, jt0, now)
	alive := func(l Lease) bool { return l.PID == 2 }
	got, err := j.Orphans(owner, now, ttl, alive)
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	if len(got) != 1 || got[0].BoxID != "stale-dead" {
		t.Fatalf("Orphans = %v, want only stale-dead", ids(got))
	}
}

func TestJournalOrphansRequiresLivenessCheck(t *testing.T) {
	j, _ := newTestJournal(t)
	if _, err := j.Orphans(ownerLease(1, jt0, jt0), jt0, time.Second, nil); err == nil {
		t.Fatal("Orphans with a nil liveness check succeeded")
	}
}

func TestJournalOrphansBoundaryIsStrict(t *testing.T) {
	j, _ := newTestJournal(t)
	hb := jt0
	if err := j.Add(Entry{BoxID: "edge", Kind: KindEphemeral, CreatedAt: jt0, Lease: ownerLease(9, jt0, hb)}); err != nil {
		t.Fatal(err)
	}
	owner := ownerLease(1, jt0, jt0)
	at := hb.Add(90 * time.Second) // exactly the ttl: not yet older than it
	got, err := j.Orphans(owner, at, 90*time.Second, allDead)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("entry exactly leaseTTL old was returned as an orphan")
	}
	got, err = j.Orphans(owner, at.Add(time.Nanosecond), 90*time.Second, allDead)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("entry just past leaseTTL was not returned")
	}
}

func TestJournalCorruptFileIsErrorNotReset(t *testing.T) {
	j, path := newTestJournal(t)
	garbage := []byte("{not json")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := j.List(); err == nil {
		t.Fatal("List on a corrupt file succeeded")
	}
	if err := j.Add(Entry{BoxID: "box-a", Kind: KindEphemeral, CreatedAt: jt0}); err == nil {
		t.Fatal("Add on a corrupt file succeeded")
	}
	if _, err := j.Orphans(ownerLease(1, jt0, jt0), jt0, time.Second, allDead); err == nil {
		t.Fatal("Orphans on a corrupt file succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(garbage) {
		t.Fatalf("corrupt journal was rewritten: %q", after)
	}
}

func TestJournalSharedAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstash-boxes.json")
	a := NewJournal(path)
	b := NewJournal(path)
	if err := a.Add(Entry{BoxID: "from-a", Kind: KindEphemeral, CreatedAt: jt0}); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(Entry{BoxID: "from-b", Kind: KindEphemeral, CreatedAt: jt0}); err != nil {
		t.Fatal(err)
	}
	got := mustList(t, a)
	if len(got) != 2 {
		t.Fatalf("second journal view lost an entry: %v", ids(got))
	}
}

func TestJournalConcurrentAddsAllLand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstash-boxes.json")
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j := NewJournal(path)
			errs <- j.Add(Entry{BoxID: string(rune('a' + i)), Kind: KindEphemeral, CreatedAt: jt0})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Add: %v", err)
		}
	}
	if got := mustList(t, NewJournal(path)); len(got) != n {
		t.Fatalf("after %d concurrent adds, journal has %d entries", n, len(got))
	}
}

func TestJournalLeavesNoTempFiles(t *testing.T) {
	j, path := newTestJournal(t)
	if err := j.Add(Entry{BoxID: "box-a", Kind: KindEphemeral, CreatedAt: jt0}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".upstash-boxes-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestJournalAddOnFailedWriteReturnsError(t *testing.T) {
	// A journal path inside a regular file cannot be created, so the error must surface.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	j := NewJournal(filepath.Join(blocker, "upstash-boxes.json"))
	err := j.Add(Entry{BoxID: "box-a", Kind: KindEphemeral, CreatedAt: jt0})
	if err == nil {
		t.Fatal("Add into an impossible path succeeded")
	}
}

// TestLeaseAliveWithProbe pins the liveness rule over an injected probe: a PID that
// is gone is dead, a process created before the lease's heartbeat is its writer, one
// created after it is a reused PID, and a probe error counts as alive.
func TestLeaseAliveWithProbe(t *testing.T) {
	hb := jt0
	lease := Lease{PID: 7, ProcessStart: jt0.Add(-time.Hour), Heartbeat: hb}
	cases := []struct {
		name  string
		probe processProbe
		want  bool
	}{
		{"gone pid is dead", func(int) (bool, time.Time, error) { return false, time.Time{}, nil }, false},
		{"created before the heartbeat is the writer", func(int) (bool, time.Time, error) { return true, hb.Add(-time.Minute), nil }, true},
		{"created after the heartbeat is a reused pid", func(int) (bool, time.Time, error) { return true, hb.Add(time.Minute), nil }, false},
		{"unknown creation time counts as alive", func(int) (bool, time.Time, error) { return true, time.Time{}, nil }, true},
		{"probe error counts as alive", func(int) (bool, time.Time, error) { return false, time.Time{}, errors.New("probe failed") }, true},
	}
	for _, tc := range cases {
		if got := leaseAliveWith(lease, tc.probe); got != tc.want {
			t.Errorf("%s: leaseAliveWith = %v, want %v", tc.name, got, tc.want)
		}
	}
	if leaseAliveWith(Lease{}, func(int) (bool, time.Time, error) { return true, time.Time{}, nil }) {
		t.Error("a lease with no PID is alive")
	}
}
