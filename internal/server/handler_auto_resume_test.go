package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// autoResumeSendAsync posts a message through the REAL HandleSendMessage
// handler — the same path the web/desktop composer uses.
func autoResumeSendAsync(t *testing.T, h *Handler, id, content string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"content": content, "async": true})
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/message", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleSendMessage(rec, req, id)
	return rec
}

// waitForSessionIdle blocks until the session has no in-flight turn job.
func waitForSessionIdle(t *testing.T, h *Handler, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		h.cancelMu.Lock()
		inFlight := h.turnInFlight[id]
		h.cancelMu.Unlock()
		if inFlight == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %s never went idle", id)
}

// waitForPendingDrained blocks until the session's persisted-but-un-turned
// queue is empty (the message was actually turned).
func waitForPendingDrained(t *testing.T, h *Handler, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h.sessions.PendingCount(id) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// transcriptHasUser reports whether the transcript contains a user row whose
// content contains substr.
func transcriptHasUser(msgs []agent.Message, substr string) bool {
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, substr) {
			return true
		}
	}
	return false
}

// transcriptHasAssistant reports whether the transcript contains an assistant
// reply.
func transcriptHasAssistant(msgs []agent.Message) bool {
	for _, m := range msgs {
		if m.Role == "assistant" {
			return true
		}
	}
	return false
}

// TestAutoResumeMessageAfterStopRuns is the core regression test for
// "sending a message after Stop resumes the conversation".
//
// The client used to queue the message behind the interrupt barrier, so it
// never reached the server at all. Now it sends. This test pins the SERVER
// half of the contract: a message dispatched after a cancelled turn must
// actually RUN (be appended to the transcript and answered), not be
// swallowed by the pendingCancel flag the Stop recorded.
func TestAutoResumeMessageAfterStopRuns(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	blocking := newBlockingClient()
	as := newTestSession(h, id, blocking)

	// Turn 1: a real dispatched job, so the whole production path runs.
	job1, err := h.dispatchTurn(id, "fake-model", "first", turnOptions{})
	if err != nil {
		t.Fatalf("dispatch 1: %v", err)
	}
	select {
	case <-job1.persistAck:
	case <-time.After(5 * time.Second):
		t.Fatal("persistAck 1 never arrived")
	}
	select {
	case <-blocking.started:
	case <-time.After(10 * time.Second):
		t.Fatal("turn 1 never started")
	}

	// The user presses Stop.
	cancelRequest(h, id)
	close(blocking.release)

	// Wait for the cancelled turn to unwind.
	waitForSessionIdle(t, h, id)

	// The user now sends a new message: the auto-resume path.
	rec := autoResumeSendAsync(t, h, id, "second")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}

	// The message must be TURNED, not left pending.
	waitForPendingDrained(t, h, id)

	if got := h.sessions.PendingCount(id); got != 0 {
		t.Fatalf("pending count = %d, want 0 — the message was swallowed", got)
	}
	if !transcriptHasAssistant(as.messages) {
		t.Fatalf("no assistant reply after the auto-resume turn: %+v", as.messages)
	}
	if !transcriptHasUser(as.messages, "second") {
		t.Fatalf("the resumed message never reached the transcript: %+v", as.messages)
	}
}

// TestAutoResumeSurvivesStopDuringAskContinuation pins the server-side gap
// this change fixes: a Stop recorded while an ask continuation (permission
// resolve / question answer) is in flight left the pendingCancel flag set,
// because that path never runs an executeTurnJob to consume it. The NEXT user
// message was then swallowed — persisted but never turned, so the
// conversation silently stalled.
func TestAutoResumeSurvivesStopDuringAskContinuation(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	as := newTestSession(h, id, &instantClient{})

	// Model an ask continuation: the session has a live in-flight unit, but
	// NO executeTurnJob is running for it.
	h.sessions.setTurnActive(id, true)
	h.cancelMu.Lock()
	h.turnInFlight[id]++
	h.cancelMu.Unlock()

	// The user presses Stop.
	cancelRequest(h, id)
	if !h.isPendingCancel(id) {
		t.Fatal("the Stop did not record pendingCancel — the premise of this test is broken")
	}

	// The continuation ends (this is what releases the in-flight count).
	h.releaseTurnInFlight(id)

	// The flag must be gone: no live unit can consume it any more, so it can
	// only poison the next message.
	if h.isPendingCancel(id) {
		t.Fatal("pendingCancel survived the end of the turn — it would swallow the next message")
	}

	// The user's next message must run.
	rec := autoResumeSendAsync(t, h, id, "after stop")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	waitForPendingDrained(t, h, id)
	if got := h.sessions.PendingCount(id); got != 0 {
		t.Fatalf("pending count = %d, want 0 — the message was swallowed", got)
	}
	if !transcriptHasUser(as.messages, "after stop") {
		t.Fatalf("the message never reached the transcript: %+v", as.messages)
	}
}

// TestAutoResumeDoesNotInjectIntoCancelledAgent pins the injection guard:
// a message that arrives while the cancelled agent is still unwinding must
// NOT be spliced into the dying agent's live loop (the agent's stop channel
// is closed, so it would never pick the message up). It must fall through
// to a fresh turn instead.
//
// The guard is exercised directly rather than through the HTTP handler: the
// handler takes the session's turn lock, which the still-unwinding first turn
// holds, so the request would (correctly) block until that turn unwinds.
func TestAutoResumeDoesNotInjectIntoCancelledAgent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	blocking := newBlockingClient()
	as := newTestSession(h, id, blocking)

	job1, err := h.dispatchTurn(id, "fake-model", "first", turnOptions{})
	if err != nil {
		t.Fatalf("dispatch 1: %v", err)
	}
	select {
	case <-job1.persistAck:
	case <-time.After(5 * time.Second):
		t.Fatal("persistAck 1 never arrived")
	}
	select {
	case <-blocking.started:
	case <-time.After(10 * time.Second):
		t.Fatal("turn 1 never started")
	}

	cancelRequest(h, id)
	// The cancelled turn is still unwinding: turnActive is still true and
	// the agent is cancelled. This is the exact window in which a message
	// sent right after the Stop arrives.
	h.sessions.setTurnActive(id, true)
	if !as.agent.Cancelled() {
		t.Fatal("precondition: the agent should be cancelled")
	}

	// The guard: a cancelled agent must not receive an injection.
	if h.tryEnqueueInjection(id, "second") {
		t.Fatal("the message was injected into a cancelled agent — it would never be picked up")
	}
	if injected := as.agent.DrainPendingInjections(); len(injected) != 0 {
		t.Fatalf("a cancelled agent holds %d pending injections", len(injected))
	}

	// Let the old turn unwind; the message must then run as its own turn.
	close(blocking.release)
	waitForSessionIdle(t, h, id)

	rec := autoResumeSendAsync(t, h, id, "second")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	waitForPendingDrained(t, h, id)

	if got := h.sessions.PendingCount(id); got != 0 {
		t.Fatalf("pending count = %d, want 0 — the message was swallowed", got)
	}
	if !transcriptHasUser(as.messages, "second") {
		t.Fatalf("the message never reached the transcript: %+v", as.messages)
	}
}

// TestCancelledTurnErrorIsFlagged pins the wire contract the client relies
// on: a cancelled turn publishes turn_error with cancelled:true, so the web
// client can tell a user Stop from a genuine failure and must not paint a
// red error banner or offer the Retry affordance.
func TestCancelledTurnErrorIsFlagged(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	ch := h.bus.Subscribe([]string{proj})
	defer h.bus.Unsubscribe(ch)

	h.publishTurnError(id, ErrTurnCancelled, "")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case env := <-ch:
			if env.Event != "turn_error" {
				continue
			}
			data, _ := env.Data.(map[string]any)
			if cancelled, _ := data["cancelled"].(bool); !cancelled {
				t.Fatalf("turn_error for a cancellation is not flagged cancelled:true (data=%v)", data)
			}
			if data["error"] != "cancelled" {
				t.Fatalf("turn_error error text = %v, want %q", data["error"], "cancelled")
			}
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("no turn_error event was published")
}

// TestGenuineTurnErrorIsNotFlaggedCancelled is the negative control: a REAL
// failure must not be flagged as a cancellation, or the client would swallow
// the error and hide the Retry affordance.
func TestGenuineTurnErrorIsNotFlaggedCancelled(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	ch := h.bus.Subscribe([]string{proj})
	defer h.bus.Unsubscribe(ch)

	// A GENUINE failure — not a cancellation.
	h.publishTurnError(id, errors.New("provider exploded"), "")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case env := <-ch:
			if env.Event != "turn_error" {
				continue
			}
			data, _ := env.Data.(map[string]any)
			if cancelled, _ := data["cancelled"].(bool); cancelled {
				t.Fatalf("a genuine failure was flagged as cancelled (data=%v)", data)
			}
			if data["error"] != "provider exploded" {
				t.Fatalf("turn_error error text = %v, want %q", data["error"], "provider exploded")
			}
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("no turn_error event was published")
}
