package server

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/session"
)

// TestAsyncTurnPersistsReplyAfterPrePersistedUserMessage covers the
// production async web/desktop path: dispatchTurn pre-persists the user
// message to disk (persistUserMessage) BEFORE runTurn appends its own
// in-memory copy. The two copies must serialize identically, or every live
// save is dropped as a "diverged overlap", the turn-end save conflicts, and
// the in-memory transcript is re-synced to the one-message disk copy — the
// reply vanishes from both the UI and the session file.
func TestAsyncTurnPersistsReplyAfterPrePersistedUserMessage(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour
	proj := t.TempDir()
	id := session.NewSessionID()
	entry := h.sessions.Register(id, proj)
	if err := h.persistUserMessage(entry, "hello?"); err != nil {
		t.Fatalf("persistUserMessage: %v", err)
	}
	as := newTestSession(h, id, instantClient{})

	if _, err := h.runTurn(id, as, "hello?", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	if len(as.messages) != 2 || as.messages[1].Content != "hi" {
		t.Fatalf("in-memory transcript lost the reply: %+v", as.messages)
	}
	if err := session.FlushForDir(proj, id, 10*time.Second); err != nil {
		t.Fatalf("flush: %v", err)
	}
	loaded, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].Content != "hi" {
		t.Fatalf("expected user+reply on disk, got %+v", loaded.Messages)
	}
}
