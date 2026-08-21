package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// blockingClient holds its Chat call until release is closed, so a test can
// keep one session's turn "in flight" while it exercises another session.
type blockingClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingClient() *blockingClient {
	return &blockingClient{started: make(chan struct{}), release: make(chan struct{})}
}

func (c *blockingClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return &agent.Message{Role: "assistant", Content: "done"}, nil
}
func (c *blockingClient) GetProvider() string { return "fake" }
func (c *blockingClient) GetModel() string    { return "fake-model" }

// instantClient answers immediately.
type instantClient struct{}

func (instantClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return &agent.Message{Role: "assistant", Content: "hi"}, nil
}
func (instantClient) GetProvider() string { return "fake" }
func (instantClient) GetModel() string    { return "fake-model" }

func newTestSession(h *Handler, id string, client agent.LLMClient) *agentSession {
	as := &agentSession{agent: agent.NewAgent(client, nil, nil, nil), model: "fake-model"}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()
	return as
}

func chatRequest(t *testing.T, h *Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := httptest.NewRecorder()
	h.HandleChat(rec, httptest.NewRequest("POST", "/api/chat", bytes.NewReader(raw)))
	return rec
}

// TestChatSessionsRunConcurrently is the regression test for the "a session
// won't run while another session is running" bug: one session's in-flight
// turn must not block a turn on a different session. Previously the handler
// held h.mu across agent construction and the whole turn, so the second
// session's request queued behind the first.
func TestChatSessionsRunConcurrently(t *testing.T) {
	h := NewHandler()

	blocking := newBlockingClient()
	newTestSession(h, "sess-busy", blocking)
	newTestSession(h, "sess-free", instantClient{})

	busyDone := make(chan int, 1)
	go func() {
		rec := chatRequest(t, h, map[string]any{"content": "slow", "sessionId": "sess-busy", "model": "fake-model"})
		busyDone <- rec.Code
	}()

	// Wait until the first session is genuinely inside its LLM call.
	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first session never started its turn")
	}

	freeDone := make(chan int, 1)
	go func() {
		rec := chatRequest(t, h, map[string]any{"content": "quick", "sessionId": "sess-free", "model": "fake-model"})
		freeDone <- rec.Code
	}()

	select {
	case code := <-freeDone:
		if code != http.StatusOK {
			t.Fatalf("second session: expected 200, got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second session blocked behind the first session's turn")
	}

	close(blocking.release)
	if code := <-busyDone; code != http.StatusOK {
		t.Fatalf("first session: expected 200, got %d", code)
	}
}

// TestHandlerEndpointsStayResponsiveDuringTurn guards the other half of the
// same bug: a running turn must not hold h.mu, or every unrelated endpoint
// (session list, run states, the desktop dock badge) freezes with it.
func TestHandlerEndpointsStayResponsiveDuringTurn(t *testing.T) {
	h := NewHandler()

	blocking := newBlockingClient()
	newTestSession(h, "sess-busy", blocking)

	done := make(chan struct{})
	go func() {
		chatRequest(t, h, map[string]any{"content": "slow", "sessionId": "sess-busy", "model": "fake-model"})
		close(done)
	}()

	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn never started")
	}

	responded := make(chan struct{})
	go func() {
		h.RunStates()
		close(responded)
	}()

	select {
	case <-responded:
	case <-time.After(5 * time.Second):
		t.Fatal("RunStates blocked on the handler lock during a turn")
	}

	close(blocking.release)
	<-done
}

// TestCompactDoesNotBlockOtherSessions is the regression test for the
// compaction half of the bug: HandleCompactSession used to hold h.mu across
// the compaction LLM call, so while one session compacted — which happens on
// its own in long sessions — no other session could start a turn.
func TestCompactDoesNotBlockOtherSessions(t *testing.T) {
	h := NewHandler()

	blocking := newBlockingClient()
	compacting := &agentSession{
		agent: agent.NewAgent(blocking, nil, &config.Config{}, nil),
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "one"},
			{Role: "assistant", Content: "two"},
			{Role: "user", Content: "three"},
			{Role: "assistant", Content: "four"},
		},
	}
	h.mu.Lock()
	h.agents["sess-compacting"] = compacting
	h.mu.Unlock()

	newTestSession(h, "sess-other", instantClient{})

	compactDone := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/sess-compacting/compact", nil), "sess-compacting")
		close(compactDone)
	}()

	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("compaction never reached its LLM call")
	}

	chatDone := make(chan int, 1)
	go func() {
		rec := chatRequest(t, h, map[string]any{"content": "quick", "sessionId": "sess-other", "model": "fake-model"})
		chatDone <- rec.Code
	}()

	select {
	case code := <-chatDone:
		if code != http.StatusOK {
			t.Fatalf("expected 200 for the unrelated session, got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unrelated session blocked behind another session's compaction")
	}

	close(blocking.release)
	<-compactDone
}

// TestChatAsyncReturnsBeforeTurnCompletes covers the async dispatch the web UI
// uses so a browser connection is not pinned for the whole turn (HTTP/1.1
// caps concurrent connections per origin, which starved other sessions).
func TestChatAsyncReturnsBeforeTurnCompletes(t *testing.T) {
	h := NewHandler()

	blocking := newBlockingClient()
	as := newTestSession(h, "sess-async", blocking)

	rec := chatRequest(t, h, map[string]any{"content": "slow", "sessionId": "sess-async", "async": true, "model": "fake-model"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d %s", rec.Code, rec.Body.String())
	}

	select {
	case <-blocking.started:
	case <-time.After(30 * time.Second):
		t.Fatal("async turn never started")
	}

	close(blocking.release)

	// The turn completes on its own goroutine and commits to the session.
	// The deadlines here are liveness bounds only — under a parallel package
	// run (e.g. `go test ./internal/tui/ ./internal/server/`) goroutine
	// scheduling can stall for seconds, so these are deliberately generous
	// rather than asserting any real latency property.
	deadline := time.Now().Add(30 * time.Second)
	for {
		as.mu.Lock()
		n := len(as.messages)
		as.mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("async turn never committed its messages")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRegisterAgentSessionDedupes covers the race that building agents outside
// h.mu introduces: two concurrent first messages for the same session must end
// up sharing one agent.
func TestRegisterAgentSessionDedupes(t *testing.T) {
	h := NewHandler()

	first := &agentSession{agent: agent.NewAgent(instantClient{}, nil, nil, nil), model: "fake-model"}
	second := &agentSession{agent: agent.NewAgent(instantClient{}, nil, nil, nil), model: "fake-model"}

	if got := h.registerAgentSession("sess-dup", first, ""); got != first {
		t.Fatal("first registration should win")
	}
	if got := h.registerAgentSession("sess-dup", second, ""); got != first {
		t.Fatal("second registration should return the already-registered session")
	}
	if got := h.lookupAgentSession("sess-dup"); got != first {
		t.Fatal("lookup should return the winner")
	}
}
