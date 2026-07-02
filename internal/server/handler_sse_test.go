package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// TestHandleSessionMessagesMirrorsBridge verifies the persistent mirror stream
// emits the bridge's current message list, which is how TUI-typed messages reach
// the /rc web UI.
func TestHandleSessionMessagesMirrorsBridge(t *testing.T) {
	h := NewHandler()
	bridge := &RCBridge{RcCh: make(chan RCRequest, 1), SessionID: "sess-1", Model: "test-model"}
	bridge.SetMessages([]agent.Message{
		{Role: "user", Content: "typed in the tui"},
		{Role: "assistant", Content: "answer from the agent"},
	})
	h.rc = bridge

	ctx, cancel := context.WithCancel(context.Background())
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/chat/messages?session=sess-1", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		h.HandleSessionMessages(w, r)
		close(done)
	}()

	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not return after context cancel")
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: messages") {
		t.Fatalf("expected an initial messages frame, got: %q", body)
	}
	if !strings.Contains(body, "typed in the tui") || !strings.Contains(body, "answer from the agent") {
		t.Fatalf("expected mirrored messages in frame, got: %q", body)
	}
}

// TestHandleSessionMessagesForwardsLiveEvents verifies that events broadcast by
// the TUI after a browser connects are forwarded over the live mirror stream.
func TestHandleSessionMessagesForwardsLiveEvents(t *testing.T) {
	h := NewHandler()
	bridge := &RCBridge{RcCh: make(chan RCRequest, 1), SessionID: "sess-1", Model: "test-model"}
	h.rc = bridge

	ctx, cancel := context.WithCancel(context.Background())
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/chat/messages?session=sess-1", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		h.HandleSessionMessages(w, r)
		close(done)
	}()

	// Let the handler subscribe, then broadcast a live token delta + tool event.
	time.Sleep(40 * time.Millisecond)
	bridge.Broadcast(SSEEvent{Event: "text", Data: TextDelta{Delta: "hello"}})
	bridge.Broadcast(SSEEvent{Event: "tool_start", Data: ToolStartEvent{Tool: "read_file", Command: "main.go"}})
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not return after context cancel")
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: text") || !strings.Contains(body, "hello") {
		t.Fatalf("expected forwarded text delta, got: %q", body)
	}
	if !strings.Contains(body, "event: tool_start") || !strings.Contains(body, "read_file") {
		t.Fatalf("expected forwarded tool_start, got: %q", body)
	}
}

// TestRCBridgeUnsubscribeStopsDelivery verifies an unsubscribed channel no longer
// receives broadcasts (no leak / no send to a dead consumer).
func TestRCBridgeUnsubscribeStopsDelivery(t *testing.T) {
	b := &RCBridge{}
	ch := b.Subscribe()
	b.Broadcast(SSEEvent{Event: "text", Data: TextDelta{Delta: "a"}})
	select {
	case ev := <-ch:
		if ev.Event != "text" {
			t.Fatalf("expected text event, got %q", ev.Event)
		}
	default:
		t.Fatal("subscriber did not receive broadcast")
	}

	b.Unsubscribe(ch)
	b.Broadcast(SSEEvent{Event: "text", Data: TextDelta{Delta: "b"}})
	select {
	case ev := <-ch:
		t.Fatalf("unsubscribed channel still received %q", ev.Event)
	default:
	}
}

// TestHandleSessionMessagesNoBridge verifies the endpoint returns an initial
// messages frame (may be empty) without blocking forever when no TUI session
// is bridged and the context is cancelled.
func TestHandleSessionMessagesNoBridge(t *testing.T) {
	h := NewHandler()
	ctx, cancel := context.WithCancel(context.Background())
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/chat/messages", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		h.HandleSessionMessages(w, r)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler blocked when no bridge was set")
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "event: messages") {
		t.Fatalf("expected messages frame, got: %q", w.Body.String())
	}
}
