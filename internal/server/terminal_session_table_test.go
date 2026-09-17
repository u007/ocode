package server

import "testing"

func TestTerminalSessionTableInvalidateProject(t *testing.T) {
	table := newTerminalSessionTable()
	old := &terminalSession{id: "old", project: "alice@host:/srv/app", resumable: true}
	other := &terminalSession{id: "other", project: "other:/srv/app", resumable: true}
	if !table.put(old.id, old) || !table.put(other.id, other) {
		t.Fatal("failed to seed terminal sessions")
	}
	invalidated := table.invalidateProject("alice@host:/srv/app")
	if len(invalidated) != 1 || invalidated[0] != old {
		t.Fatalf("invalidated = %+v", invalidated)
	}
	if table.lookup(old.id) != nil {
		t.Fatal("old project session remained reattachable")
	}
	if table.lookup(other.id) != other {
		t.Fatal("unrelated project session was invalidated")
	}
}

func TestTerminalSessionTableInvalidatesInFlightProjectReservation(t *testing.T) {
	table := newTerminalSessionTable()
	_, created, done := table.reserveForProject("old", "alice@host:/srv/app")
	if !created || done == nil {
		t.Fatal("expected project reservation")
	}
	if got := table.invalidateProject("alice@host:/srv/app"); len(got) != 0 {
		t.Fatalf("unexpected live sessions: %d", len(got))
	}
	if table.completeCreate("old", &terminalSession{id: "old", project: "alice@host:/srv/app", resumable: true}) {
		t.Fatal("invalidated reservation was accepted")
	}
	select {
	case <-done:
	default:
		t.Fatal("reservation waiters were not released")
	}
	if table.lookup("old") != nil {
		t.Fatal("stale reservation became reattachable")
	}
}

// TestTerminalSessionTableAbandonReleasesReservation pins the safety net that
// makes a leaked reservation impossible: if the create owner never publishes
// (a panic between reserve and completeCreate), abandon must wake every waiter
// and free the id so the next socket can spawn a fresh shell instead of
// blocking on the done channel forever.
func TestTerminalSessionTableAbandonReleasesReservation(t *testing.T) {
	table := newTerminalSessionTable()
	_, created, done := table.reserveForProject("leaked", "host:/srv/app")
	if !created || done == nil {
		t.Fatal("expected a reservation")
	}
	// A second socket for the same id waits on done.
	if _, created2, done2 := table.reserveForProject("leaked", "host:/srv/app"); created2 || done2 != done {
		t.Fatal("second reserve did not join the existing reservation")
	}

	table.abandon("leaked")

	select {
	case <-done:
	default:
		t.Fatal("abandon did not release the reservation waiters")
	}
	// The id is free again: the next socket owns a fresh create.
	if _, created3, _ := table.reserveForProject("leaked", "host:/srv/app"); !created3 {
		t.Fatal("abandon left the id reserved; a later socket can never spawn")
	}
}

// TestTerminalSessionTableAbandonAfterCompleteIsNoop verifies the deferred
// abandon racing a successful publish cannot close the done channel twice
// (which would panic) and cannot evict the published session.
func TestTerminalSessionTableAbandonAfterCompleteIsNoop(t *testing.T) {
	table := newTerminalSessionTable()
	_, created, done := table.reserveForProject("done", "host:/srv/app")
	if !created {
		t.Fatal("expected a reservation")
	}
	sess := &terminalSession{id: "done", project: "host:/srv/app", resumable: true}
	if !table.completeCreate("done", sess) {
		t.Fatal("completeCreate refused a free table")
	}
	select {
	case <-done:
	default:
		t.Fatal("completeCreate did not release waiters")
	}
	table.abandon("done") // must be a no-op, not a double close
	if table.lookup("done") != sess {
		t.Fatal("abandon after completeCreate evicted the published session")
	}
}
