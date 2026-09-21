package server

import (
	"net/http/httptest"
	"testing"
)

// The re-dispatch guard (agent.subagentDispatchLimit = 3) refuses a subagent
// type after 3 consecutive dispatches without new user input. The counter must
// be cleared by new user input, not accumulate across turns: the reset was wired
// into the TUI's send path only, so in the headless server (web/desktop) a
// resident agent that dispatched the same subagent 4 times was locked out for
// the rest of the agent's life, across every later user message.
//
// There are three server entry points that accept user input, and each must
// reset the counter: runTurn (HandleChat/HandleSendMessage), the legacy SSE
// stream endpoint (HandleChatStream, used by Telegram), and mid-turn injection
// (HandleSendMessage while a turn is already running).

// seedTrippedGuard drives the consecutive-dispatch counter past the limit so the
// guard is refusing — mimicking a runaway loop left behind by a previous turn.
func seedTrippedGuard(t *testing.T, as *agentSession) {
	t.Helper()
	for i := 0; i < 4; i++ {
		as.agent.NoteSubagentDispatch("context")
	}
	if n := as.agent.NoteSubagentDispatch("context"); n <= 3 {
		t.Fatalf("precondition: 5th consecutive dispatch counted as %d, want > 3 (guard tripped)", n)
	}
}

// assertReset fails unless the counter was cleared: the next dispatch of the
// same agent must start a fresh consecutive run at 1.
func assertReset(t *testing.T, as *agentSession, after string) {
	t.Helper()
	if n := as.agent.NoteSubagentDispatch("context"); n != 1 {
		t.Fatalf("consecutive-dispatch counter after %s = %d, want 1 (reset)", after, n)
	}
}

func TestChatTurnResetsSubagentDispatchGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate session storage from other tests
	h := NewHandler()
	h.sessions.Register("sess-dispatch-guard", t.TempDir())
	as := newTestSession(h, "sess-dispatch-guard", instantClient{})
	defer as.agent.Shutdown()
	// Suppress the background title-generation goroutine so it doesn't write
	// session files after the test body returns.
	h.tryClaimTitleGen("sess-dispatch-guard")
	seedTrippedGuard(t, as)

	rec := chatRequest(t, h, map[string]any{
		"content":   "hello",
		"sessionId": "sess-dispatch-guard",
		"model":     "fake-model",
	})
	if rec.Code != 200 {
		t.Fatalf("chat failed: %d %s", rec.Code, rec.Body.String())
	}

	assertReset(t, as, "a new user turn")
}

// The legacy streaming endpoint (GET /api/chat/stream) appends the user message
// and calls Step directly, bypassing runTurn. It is still used by the Telegram
// bot, so it must reset the counter too.
func TestChatStreamResetsSubagentDispatchGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	h.sessions.Register("sess-stream-guard", t.TempDir())
	as := newTestSession(h, "sess-stream-guard", instantClient{})
	defer as.agent.Shutdown()
	h.tryClaimTitleGen("sess-stream-guard")
	seedTrippedGuard(t, as)

	req := httptest.NewRequest("GET",
		"/api/chat/stream?session=sess-stream-guard&message=hello&model=fake-model", nil)
	rec := httptest.NewRecorder()
	h.HandleChatStream(rec, req)
	if rec.Code != 200 {
		t.Fatalf("chat stream failed: %d %s", rec.Code, rec.Body.String())
	}

	assertReset(t, as, "a streamed user turn")
}

// HandleSendMessage splices a message into the RUNNING turn via
// tryEnqueueInjection instead of starting a new turn. That is still new user
// input, so the guard must not keep claiming "without any new user input".
func TestMidTurnInjectionResetsSubagentDispatchGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	h.sessions.Register("sess-inject-guard", t.TempDir())
	as := newTestSession(h, "sess-inject-guard", instantClient{})
	defer as.agent.Shutdown()
	h.tryClaimTitleGen("sess-inject-guard")
	seedTrippedGuard(t, as)

	h.sessions.setTurnActive("sess-inject-guard", true)
	if !h.tryEnqueueInjection("sess-inject-guard", "hello") {
		t.Fatal("precondition: injection was not enqueued")
	}

	assertReset(t, as, "a mid-turn injection")
}
