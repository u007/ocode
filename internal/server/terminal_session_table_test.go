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
