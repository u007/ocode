package server

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/session"
)

func TestSessionManagerTurnError(t *testing.T) {
	m := NewSessionManager(time.Hour, nil, nil)
	id := session.NewSessionID()
	m.Register(id, t.TempDir())

	if e, _ := m.SnapshotEntry(id); e.lastTurnErr != "" {
		t.Fatalf("fresh session lastTurnErr = %q, want empty", e.lastTurnErr)
	}

	m.setTurnError(id, "compaction failed: summary timed out")
	e, ok := m.SnapshotEntry(id)
	if !ok {
		t.Fatal("entry vanished")
	}
	if e.lastTurnErr != "compaction failed: summary timed out" {
		t.Errorf("lastTurnErr = %q, want the recorded message", e.lastTurnErr)
	}

	// Starting a new turn supersedes the previous failure: the dashboard shows
	// a session as "error" only while its most recent turn is the failed one.
	m.setTurnActive(id, true)
	if e, _ := m.SnapshotEntry(id); e.lastTurnErr != "" {
		t.Errorf("lastTurnErr = %q after a new turn started, want cleared", e.lastTurnErr)
	}

	// Ending a turn does NOT clear it — that is the whole point of the field.
	m.setTurnError(id, "llm 500")
	m.setTurnActive(id, false)
	if e, _ := m.SnapshotEntry(id); e.lastTurnErr != "llm 500" {
		t.Errorf("lastTurnErr = %q after the failing turn ended, want it kept", e.lastTurnErr)
	}
}

func TestSessionManagerTurnErrorSurvivesSnapshot(t *testing.T) {
	// The Pulse handler reads the bulk Snapshot(), not SnapshotEntry(), so the
	// field has to be copied there too or every row silently reads "no error".
	m := NewSessionManager(time.Hour, nil, nil)
	id := session.NewSessionID()
	m.Register(id, t.TempDir())
	m.setTurnError(id, "boom")

	found := false
	for _, e := range m.Snapshot() {
		if e.SessionID == id {
			found = true
			if e.lastTurnErr != "boom" {
				t.Errorf("Snapshot() lastTurnErr = %q, want %q", e.lastTurnErr, "boom")
			}
		}
	}
	if !found {
		t.Error("session missing from Snapshot()")
	}
}

func TestSessionManagerTurnErrorUnknownSessionIsNoOp(t *testing.T) {
	m := NewSessionManager(time.Hour, nil, nil)
	// Must not panic, and must not conjure an entry for a session nobody
	// registered — an orphan entry would show up on the dashboard as a session
	// with no project.
	m.setTurnError("ses_never_registered", "boom")

	if _, ok := m.SnapshotEntry("ses_never_registered"); ok {
		t.Error("setTurnError created an entry for an unknown session")
	}
	if len(m.Snapshot()) != 0 {
		t.Errorf("Snapshot() has %d entries, want 0", len(m.Snapshot()))
	}
}

func TestSessionManagerSetTurnErrorEmptyMessageClears(t *testing.T) {
	// Callers must be able to clear the field without inventing a sentinel
	// message that the dashboard would render as an error.
	m := NewSessionManager(time.Hour, nil, nil)
	id := session.NewSessionID()
	m.Register(id, t.TempDir())
	m.setTurnError(id, "boom")
	m.setTurnError(id, "")
	if e, _ := m.SnapshotEntry(id); e.lastTurnErr != "" {
		t.Errorf("lastTurnErr = %q, want cleared by an empty message", e.lastTurnErr)
	}
}
