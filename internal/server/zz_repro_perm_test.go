package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// fakeClientAskBash: first call returns a bash tool call, second returns a plain assistant message.
type fakeClientAskBash struct{ calls int }

func (f *fakeClientAskBash) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	f.calls++
	if f.calls == 1 {
		return &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "bash", Arguments: `{"command":"rm -rf build"}`}}}}, nil
	}
	return &agent.Message{Role: "assistant", Content: "done"}, nil
}
func (f *fakeClientAskBash) GetProvider() string { return "fake" }
func (f *fakeClientAskBash) GetModel() string    { return "fake-model" }

func TestReproPermissionResolveHeadless(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "fake-model"
	}
	ag := agent.NewAgent(&fakeClientAskBash{}, nil, nil, nil)
	as := &agentSession{agent: ag, model: "fake-model", messages: nil}
	h.agents["sess-1"] = as

	body := `{"content":"list files","sessionId":"sess-1"}`
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleChat(rec, req)
	if rec.Code != 200 {
		t.Fatalf("chat failed: %d %s", rec.Code, rec.Body.String())
	}
	last := as.messages[len(as.messages)-1]
	if !strings.HasPrefix(last.Content, tool.SentinelPermissionAsk) {
		t.Fatalf("expected PERMISSION_ASK at tail, got %q", last.Content)
	}
	permReqID := last.ToolID

	body2, _ := json.Marshal(map[string]interface{}{"request_id": permReqID, "session_id": "sess-1", "approved": true})
	req2 := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(string(body2)))
	rec2 := httptest.NewRecorder()
	h.HandleResolvePermission(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("resolve failed: %d %s", rec2.Code, rec2.Body.String())
	}
	t.Logf("headless resolve OK")
}

// TestSecondSyncChatRefusedWhilePermissionPending is the regression test for
// the "server keeps looping while a permission decision is pending" bug: once
// a turn pauses on PERMISSION_ASK, a second turn for the same session (a
// second browser tab, the desktop shell, a scheduler/Telegram dispatch) must
// be refused instead of stepping the agent again on top of the unresolved
// ask.
func TestSecondSyncChatRefusedWhilePermissionPending(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "fake-model"
	}
	ag := agent.NewAgent(&fakeClientAskBash{}, nil, nil, nil)
	as := &agentSession{agent: ag, model: "fake-model", messages: nil}
	h.agents["sess-pending"] = as

	body := `{"content":"list files","sessionId":"sess-pending"}`
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleChat(rec, req)
	if rec.Code != 200 {
		t.Fatalf("first chat failed: %d %s", rec.Code, rec.Body.String())
	}
	countBefore := len(as.messages)
	tailBefore := as.messages[len(as.messages)-1].Content
	if !strings.HasPrefix(tailBefore, tool.SentinelPermissionAsk) {
		t.Fatalf("expected PERMISSION_ASK at tail, got %q", tailBefore)
	}

	body2 := `{"content":"second message","sessionId":"sess-pending"}`
	req2 := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body2))
	rec2 := httptest.NewRecorder()
	h.HandleChat(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 while permission is pending, got %d %s", rec2.Code, rec2.Body.String())
	}
	if len(as.messages) != countBefore {
		t.Fatalf("second turn mutated the transcript: before=%d after=%d", countBefore, len(as.messages))
	}
	if as.messages[len(as.messages)-1].Content != tailBefore {
		t.Fatalf("transcript tail changed after refused second turn: %q", as.messages[len(as.messages)-1].Content)
	}
}

// TestAsyncTurnRefusedWhilePermissionPending covers the same regression on
// the async (web/desktop) dispatch path: a second async message queued while
// the first turn is paused on PERMISSION_ASK must not be turned — it should
// surface as an error event and stay in the pending queue for retry, rather
// than being shifted away and silently dropped.
func TestAsyncTurnRefusedWhilePermissionPending(t *testing.T) {
	h := NewHandler()
	as := newTestSession(h, "sess-async-pending", &fakeClientAskBash{})

	rec := chatRequest(t, h, map[string]any{"content": "list files", "sessionId": "sess-async-pending", "async": true, "model": "fake-model"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		as.mu.Lock()
		paused := len(as.messages) > 0 && strings.HasPrefix(as.messages[len(as.messages)-1].Content, tool.SentinelPermissionAsk)
		as.mu.Unlock()
		if paused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first turn never paused on permission ask")
		}
		time.Sleep(5 * time.Millisecond)
	}

	as.mu.Lock()
	countBefore := len(as.messages)
	tailBefore := as.messages[len(as.messages)-1].Content
	as.mu.Unlock()

	// Subscribe only now: a fresh channel never sees the first turn's own
	// backlog of tool_start/permission/messages/turn_done events.
	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	rec2 := chatRequest(t, h, map[string]any{"content": "second message", "sessionId": "sess-async-pending", "async": true, "model": "fake-model"})
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("expected 202 (the guard rejects after the persist ack), got %d %s", rec2.Code, rec2.Body.String())
	}

	found := false
	deadline = time.Now().Add(5 * time.Second)
	for !found && time.Now().Before(deadline) {
		select {
		case ev := <-sub:
			if ev.Event != "error" {
				continue
			}
			if data, ok := ev.Data.(map[string]string); ok && strings.Contains(data["error"], "permission decision is pending") {
				found = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !found {
		t.Fatal("expected an error event refusing the second turn")
	}

	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.messages) != countBefore {
		t.Fatalf("second turn mutated the transcript: before=%d after=%d", countBefore, len(as.messages))
	}
	if as.messages[len(as.messages)-1].Content != tailBefore {
		t.Fatalf("transcript tail changed: %q", as.messages[len(as.messages)-1].Content)
	}
	if _, ok := h.sessions.PendingFront("sess-async-pending"); !ok {
		t.Fatal("expected the refused message to remain pending for retry")
	}
}

// TestReproPermissionResolveRCBridge simulates the web->TUI bridge path.
func TestReproPermissionResolveRCBridge(t *testing.T) {
	resolveCh := make(chan RCResolution, 4)
	h := NewHandler()
	h.rc = &RCBridge{SessionID: "tui-sess", ResolveCh: resolveCh}

	// Subscribe a headless sub (the web browser connects via rc.Subscribe in
	// HandleSessionMessages, but for the resolve broadcast the server uses
	// broadcastEvent -> headlessSubs only when rc==nil; in RC mode the TUI
	// broadcasts. Simulate the browser mirror via bridge subscribe.)
	sub := h.rc.Subscribe()
	defer h.rc.Unsubscribe(sub)

	body := `{"request_id":"call-1","approved":true}`
	req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	if rec.Code != 200 {
		t.Fatalf("resolve failed: %d %s", rec.Code, rec.Body.String())
	}
	t.Logf("rc-bridge resolve returned 200")

	// The web closes the dialog on POST 200 (resolvePermission dispatches
	// PERMISSION_RESOLVED). The SSE permission_resolved is a secondary signal.
	select {
	case res := <-resolveCh:
		t.Logf("resolution forwarded to TUI: %+v", res)
	case <-time.After(time.Second):
		t.Fatalf("no resolution forwarded to TUI")
	}
}
