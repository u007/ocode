package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// TestFirstAndLastMessageTexts covers the input extraction used by title
// generation: first non-empty user message + latest assistant reply.
func TestFirstAndLastMessageTexts(t *testing.T) {
	msgs := []agent.Message{
		{Role: "user", Content: ""},
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: ""},
		{Role: "user", Content: "second question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "assistant", Content: "final answer"},
	}
	userMsg, assistantMsg := firstAndLastMessageTexts(msgs)
	if userMsg != "first question" {
		t.Errorf("userMsg = %q, want %q", userMsg, "first question")
	}
	if assistantMsg != "final answer" {
		t.Errorf("assistantMsg = %q, want %q", assistantMsg, "final answer")
	}

	// No user content at all → empty.
	userMsg, _ = firstAndLastMessageTexts([]agent.Message{{Role: "assistant", Content: "hi"}})
	if userMsg != "" {
		t.Errorf("userMsg with no user content = %q, want empty", userMsg)
	}
}

// TestTruncateSessionTitle verifies the ellipsis truncation used for
// generated titles.
func TestTruncateSessionTitle(t *testing.T) {
	short := "short title"
	if got := truncateSessionTitle(short, 80); got != short {
		t.Errorf("short title was modified: %q", got)
	}
	long := strings.Repeat("x", 100)
	got := truncateSessionTitle(long, 10)
	if len(got) != 10 || !strings.HasSuffix(got, "...") {
		t.Errorf("truncate = %q (len %d), want 10 runes ending in ...", got, len(got))
	}
	if truncateSessionTitle("abc", 0) != "" {
		t.Error("expected empty for maxLen 0")
	}
}

// TestTryClaimTitleGen verifies the per-session in-flight guard: the first
// claim wins, duplicates are rejected, and releasing (finishSessionTitle)
// frees the slot.
func TestTryClaimTitleGen(t *testing.T) {
	h := NewHandler()
	h.titleGen = newTitleGenState()

	if !h.tryClaimTitleGen("sess-1") {
		t.Fatal("first claim should succeed")
	}
	if h.tryClaimTitleGen("sess-1") {
		t.Fatal("duplicate claim should fail")
	}
	if !h.tryClaimTitleGen("sess-2") {
		t.Fatal("claim for a different session should succeed")
	}

	// Release via finishSessionTitle (empty title still releases the slot).
	h.finishSessionTitle("sess-1", "")
	if !h.tryClaimTitleGen("sess-1") {
		t.Fatal("claim after release should succeed")
	}
}

func TestSessionTitleGenerationAllowedForAutoTitle(t *testing.T) {
	sessID := seedSession(t, "", "", []agent.Message{{Role: "user", Content: "hello there"}})
	s, err := session.Load(sessID)
	if err != nil {
		t.Fatalf("load auto-titled session: %v", err)
	}
	if s.Title == "" || s.TitleGenerated {
		t.Fatalf("seed did not create an auto-title fallback: %+v", s)
	}
	if !sessionTitleGenerationAllowed(*s) {
		t.Fatal("auto-title fallback should still allow generated title")
	}
	s.TitleGenerated = true
	if sessionTitleGenerationAllowed(*s) {
		t.Fatal("explicit/generated title should suppress another generation")
	}
}

// TestMaybeGenerateSessionTitleSkipsTitledAndBridge verifies the no-op paths:
// RC-bridge attached (TUI owns titles) and sessions that already have a title
// must never start generation.
func TestMaybeGenerateSessionTitleSkipsTitledAndBridge(t *testing.T) {
	h := NewHandler()
	h.titleGen = newTitleGenState()

	// RC bridge attached → generation must not start at all.
	h.rc = &RCBridge{RcCh: make(chan RCRequest, 1), SessionID: "sess-bridge", Model: "m"}
	h.maybeGenerateSessionTitle("sess-bridge", &agentSession{agent: &agent.Agent{}})
	if h.tryClaimTitleGen("sess-bridge") == false {
		t.Error("generation started with an RC bridge attached (slot claimed)")
	}

	// Headless + titled session on disk → no generation.
	h.rc = nil
	seedSession(t, "sess-titled", "Existing title", []agent.Message{{Role: "user", Content: "hi"}})
	h.maybeGenerateSessionTitle("sess-titled", &agentSession{agent: &agent.Agent{}})
	h.titleGen.mu.Lock()
	_, started := h.titleGen.active["sess-titled"]
	h.titleGen.mu.Unlock()
	if started {
		t.Error("title generation started for an already-titled session")
	}
}

// TestFinishSessionTitleBroadcastsStatus verifies a generated title is
// persisted (session file gets the title) and a "status" SSE event carrying
// session_id + session_title is broadcast to headless subscribers.
func TestFinishSessionTitleBroadcastsStatus(t *testing.T) {
	h := NewHandler()
	h.titleGen = newTitleGenState()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}

	// Seed the session on disk so the title save has a base to append to.
	sessID := seedSession(t, "", "", []agent.Message{
		{Role: "user", Content: "help me"},
		{Role: "assistant", Content: "sure"},
	})

	// Subscribe to the headless bus.
	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	h.finishSessionTitle(sessID, "A very long generated title that exceeds the maximum allowed length for a session tab label so it must be truncated")

	// Wait for the broadcast.
	select {
	case ev := <-sub:
		if ev.Event != "status" {
			t.Fatalf("event = %q, want status", ev.Event)
		}
		snap, ok := ev.Data.(TUIStatus)
		if !ok {
			t.Fatalf("status data type = %T, want TUIStatus", ev.Data)
		}
		if snap.SessionID != sessID {
			t.Errorf("SessionID = %q, want %q", snap.SessionID, sessID)
		}
		if snap.SessionTitle == "" || len([]rune(snap.SessionTitle)) > maxGeneratedTitleLen {
			t.Errorf("SessionTitle = %q, want non-empty ≤ %d runes", snap.SessionTitle, maxGeneratedTitleLen)
		}
		// The snapshot must carry the session's context fields — the title
		// broadcast lands right after the first turn, and a context-less
		// status event zeroes the web sidebar's Context gauge (fields are
		// omitempty on the wire).
		if snap.ContextCurrentTokens <= 0 {
			t.Errorf("ContextCurrentTokens = %d, want > 0 from the seeded transcript", snap.ContextCurrentTokens)
		}
		if snap.ContextModel == "" || snap.ContextMaxTokens <= 0 {
			t.Errorf("context = %q/%d, want model + window", snap.ContextModel, snap.ContextMaxTokens)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no status event broadcast")
	}

	// The title must be persisted on disk.
	loaded, err := session.Load(sessID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if loaded.Title == "" {
		t.Error("generated title was not persisted")
	}
	if loaded.TitleGenerated != true {
		t.Error("TitleGenerated not set after save")
	}

	// In-flight entry must be cleared.
	h.titleGen.mu.Lock()
	_, stillActive := h.titleGen.active[sessID]
	h.titleGen.mu.Unlock()
	if stillActive {
		t.Error("in-flight title-gen entry not cleared after finish")
	}
}

// TestSSEEventsFilter verifies the ?events= allowlist only forwards matching
// events on the headless mirror stream.
func TestSSEEventsFilter(t *testing.T) {
	h := NewHandler()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/chat/messages?events=status", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		h.HandleSessionMessages(w, r)
		close(done)
	}()

	time.Sleep(60 * time.Millisecond)

	// Broadcast a mix: only "status" should be forwarded.
	h.broadcastEvent(SSEEvent{Event: "status", Data: TUIStatus{SessionID: "s1", SessionTitle: "T"}})
	h.broadcastEvent(SSEEvent{Event: "messages", Data: []agent.Message{{Role: "user", Content: "x"}}})
	h.broadcastEvent(SSEEvent{Event: "turn_done", Data: DoneEvent{SessionID: "s1", Model: "m"}})

	time.Sleep(120 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not return after context cancel")
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: status") {
		t.Errorf("expected status event forwarded, body:\n%s", body)
	}
	if strings.Contains(body, "event: messages") || strings.Contains(body, "event: turn_done") {
		t.Errorf("non-status events leaked through the filter:\n%s", body)
	}
}

// seedSession saves a session under a temp workdir-scoped storage dir and
// returns its ID. The storage dir is cleaned up after the test via
// session.SetWorkDir reset.
func seedSession(t *testing.T, id, title string, msgs []agent.Message) string {
	t.Helper()
	wd := t.TempDir()
	session.SetWorkDir(wd)
	t.Cleanup(func() { session.SetWorkDir("") })

	if id == "" {
		id = session.NewSessionID()
	}
	if err := session.Save(id, title, msgs, nil); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return id
}
