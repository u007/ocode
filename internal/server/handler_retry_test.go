package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// retryRequest hits HandleRetrySession for id and returns the recorder.
func retryRequest(h *Handler, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.HandleRetrySession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/retry", nil), id)
	return rec
}

// countUserMessages returns how many user-role rows a transcript holds.
func countUserMessages(msgs []agent.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "user" {
			n++
		}
	}
	return n
}

// TestHandleRetrySessionDoesNotDuplicateUserMessage is the core contract: a
// retry re-runs the existing transcript tail in place. It must NOT append a
// second user row (the duplicate the composer retry action exists to avoid),
// and it must land the assistant reply on disk.
func TestHandleRetrySessionDoesNotDuplicateUserMessage(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	as := newTestSession(h, id, instantClient{})
	as.messages = []agent.Message{{Role: "user", Content: "hello"}}

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	rec := retryRequest(h, id)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry status %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	// The retry runs on a per-session goroutine; wait for it to settle.
	if !waitForBusEvent(sub, "turn_done", 5*time.Second) {
		t.Fatal("retry turn never completed")
	}

	if got := countUserMessages(as.messages); got != 1 {
		t.Fatalf("retry duplicated the user message: %d user rows, want 1", got)
	}
	if len(as.messages) != 2 || as.messages[1].Role != "assistant" || as.messages[1].Content == "" {
		t.Fatalf("retry did not land an assistant reply: %#v", as.messages)
	}

	stored, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load stored transcript: %v", err)
	}
	if got := countUserMessages(stored.Messages); got != 1 {
		t.Fatalf("stored transcript has %d user rows after retry, want 1", got)
	}
	if len(stored.Messages) != 2 {
		t.Fatalf("stored transcript has %d rows after retry, want 2", len(stored.Messages))
	}
}

// TestHandleRetrySessionRefusesActiveTurn pins the 409 guard: retrying while a
// turn is already marked active would serialize a second Step behind the first.
func TestHandleRetrySessionRefusesActiveTurn(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	as.messages = []agent.Message{{Role: "user", Content: "hello"}}

	h.sessions.setTurnActive(id, true)

	rec := retryRequest(h, id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("retry with active turn: status %d, want 409", rec.Code)
	}
}

// TestHandleRetrySessionNothingToRetry pins the empty/assistant-only guard: a
// transcript with no user row must not step the model with no prompt.
func TestHandleRetrySessionNothingToRetry(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	newTestSession(h, id, instantClient{}) // no messages

	rec := retryRequest(h, id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("retry with no user turn: status %d, want 409", rec.Code)
	}
}

// TestHandleRetrySessionRefusesBridgedTUI pins the /rc guard: the TUI owns the
// bridged session, so a server-side retry would run a second agent against the
// same transcript. Mirrors the /reset-id refusal.
func TestHandleRetrySessionRefusesBridgedTUI(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	as.messages = []agent.Message{{Role: "user", Content: "hello"}}

	h.mu.Lock()
	h.rc = &RCBridge{SessionID: id}
	h.mu.Unlock()

	rec := retryRequest(h, id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("retry on bridged TUI session: status %d, want 409", rec.Code)
	}
}
