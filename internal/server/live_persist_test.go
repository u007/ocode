package server

// Integration tests for live async session persistence on the server path.
// Session keys are unique per test (fresh temp project + new session id), so
// the process-global live-writer registry needs no reset between tests.

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// TestWireLivePersistWritesIncrementally covers the wrapper mechanics
// without LLM plumbing: the base snapshot plus each OnMessage completion
// must reach the session file in order.
func TestWireLivePersistWritesIncrementally(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	as := &agentSession{agent: agent.NewAgent(instantClient{}, nil, nil, nil), model: "fake-model"}
	defer as.agent.Shutdown()

	base := []agent.Message{{Role: "user", Content: "hello"}}
	h.wireLivePersist(id, as, base)
	as.agent.OnMessage(agent.Message{Role: "assistant", Content: "hi"})
	as.agent.OnMessage(agent.Message{Role: "assistant", Content: "done", ToolCalls: []agent.ToolCall{}})

	if err := session.FlushForDir(proj, id, 10*time.Second); err != nil {
		t.Fatalf("flush: %v", err)
	}
	loaded, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("expected base + 2 live messages, got %+v", loaded.Messages)
	}
	if loaded.Messages[0].Content != "hello" || loaded.Messages[2].Content != "done" {
		t.Fatalf("live messages out of order: %+v", loaded.Messages)
	}
}

func waitForDiskUserMessage(t *testing.T, proj, id, content string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if loaded, err := session.LoadForDir(proj, id); err == nil {
			for _, m := range loaded.Messages {
				if m.Role == "user" && m.Content == content {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("live snapshot with %q never reached disk during turn", content)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRunTurnLivePersistsDuringTurn proves the production headless path
// persists while the turn is still running: the turn blocks inside its LLM
// call, yet the opening user message is already durable on disk.
func TestRunTurnLivePersistsDuringTurn(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour // no heartbeats in this test
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	blocking := newBlockingClient()
	as := newTestSession(h, id, blocking)

	done := make(chan error, 1)
	go func() {
		_, err := h.runTurn(id, as, "hello?", turnOptions{})
		done <- err
	}()

	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn never reached its LLM call")
	}
	waitForDiskUserMessage(t, proj, id, "hello?", 5*time.Second)

	close(blocking.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runTurn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runTurn never finished after release")
	}

	loaded, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].Content != "done" {
		t.Fatalf("expected completed turn on disk, got %+v", loaded.Messages)
	}
}
