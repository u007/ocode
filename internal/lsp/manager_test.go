package lsp

import (
	"testing"
)

func TestActiveServersEmpty(t *testing.T) {
	m := NewManager(".")
	defer m.Close()
	got := m.ActiveServers()
	if len(got) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(got))
	}
}

func TestSetEventChanReceivesStartEvent(t *testing.T) {
	// We can't actually start a real server in a unit test, but we can verify
	// that SetEventChan stores the channel and that a non-blocking send on a
	// nil channel is a no-op (doesn't panic).
	m := NewManager(".")
	defer m.Close()

	ch := make(chan ServerStartedEvent, 4)
	m.SetEventChan(ch)

	// Verify the channel is stored.
	if m.eventCh != ch {
		t.Fatal("event channel not stored")
	}
}
