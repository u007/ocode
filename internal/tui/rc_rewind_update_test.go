package tui

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/server"
)

// TestUpdateRCRequestRewindAcksCommittedSeq drives the real Update dispatch for
// a tokenized /rc send: it must commit, ack nil, and append the new user row
// carrying the durable committed UserSeq so the next live snapshot byte-matches
// disk (a missing seq reads as a diverged transcript and conflicts).
func TestUpdateRCRequestRewindAcksCommittedSeq(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "keep me", UserSeq: 1},
		{Role: "assistant", Content: "answer before target"},
		{Role: "user", Content: "replace me", UserSeq: 2},
		{Role: "assistant", Content: "discard me"},
	}
	m, _, token := rcRewindFixture(t, messages, 2, "replace me", 2)
	// No agent configured: askAgent must not be required for the commit path.
	m.agent = nil

	ack := make(chan error, 1)
	updated, _ := m.Update(rcRequestMsg{req: server.RCRequest{
		Content:     "edited bridge",
		RewindToken: token,
		AckCh:       ack,
		ResultCh:    make(chan server.RCResult, 1),
	}})
	got := asReplacementModel(t, updated)

	select {
	case err := <-ack:
		if err != nil {
			t.Fatalf("ack error = %v, want nil", err)
		}
	default:
		t.Fatal("commit did not acknowledge the bridged send")
	}

	// The new user row is the tail and carries the committed durable seq.
	last := got.messages[len(got.messages)-1]
	if last.role != roleUser || last.text != "edited bridge" {
		t.Fatalf("tail message = %+v, want user 'edited bridge'", last)
	}
	if last.raw == nil || last.raw.UserSeq != 2 {
		t.Fatalf("committed user row raw = %+v, want UserSeq 2", last.raw)
	}
}
