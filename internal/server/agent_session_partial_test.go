package server

import (
	"errors"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// midTurnErrorClient answers the first LLM round with real work (assistant text
// plus a tool call the agent can execute without asking) and fails the second,
// which is what a dropped provider stream looks like from the server's side.
type midTurnErrorClient struct{ calls int }

func (c *midTurnErrorClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	c.calls++
	if c.calls > 1 {
		return nil, errors.New("stream closed by provider")
	}
	m := &agent.Message{Role: "assistant", Content: "looking at the diff"}
	tc := agent.ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "agent_status"
	tc.Function.Arguments = "{}"
	m.ToolCalls = []agent.ToolCall{tc}
	return m, nil
}

func (c *midTurnErrorClient) GetProvider() string { return "fake" }
func (c *midTurnErrorClient) GetModel() string    { return "fake-model" }

// TestTurnPersistsPartialTranscriptOnStepError is the regression test for
// "reopen the session and everything is gone except my first message": a turn
// that streamed assistant text and tool results into the browser and then hit
// an LLM error must persist what it produced. The user watched that work
// happen; a failed final round is no reason to erase it.
func TestTurnPersistsPartialTranscriptOnStepError(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour // no heartbeats in this test
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	newTestSession(h, id, &midTurnErrorClient{})

	as := h.lookupAgentSession(id)
	if _, err := h.runTurn(id, as, "review changes", turnOptions{}); err == nil {
		t.Fatal("expected runTurn to surface the LLM error")
	}

	// In-memory transcript: the work done before the error is still there.
	as.mu.Lock()
	msgs := append([]agent.Message(nil), as.messages...)
	as.mu.Unlock()
	var sawAssistant, sawToolResult bool
	for _, m := range msgs {
		if m.Role == "assistant" && m.Content == "looking at the diff" {
			sawAssistant = true
		}
		if m.Role == "tool" && m.ToolID == "call-1" {
			sawToolResult = true
		}
	}
	if !sawAssistant || !sawToolResult {
		t.Fatalf("in-memory transcript lost the completed round (assistant=%v tool=%v): %+v",
			sawAssistant, sawToolResult, msgs)
	}

	// On-disk transcript: reopening the session must show the same thing.
	loaded, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	var diskAssistant, diskToolResult bool
	for _, m := range loaded.Messages {
		if m.Role == "assistant" && m.Content == "looking at the diff" {
			diskAssistant = true
		}
		if m.Role == "tool" && m.ToolID == "call-1" {
			diskToolResult = true
		}
	}
	if !diskAssistant || !diskToolResult {
		t.Fatalf("failed turn was not persisted (assistant=%v tool=%v): %d messages on disk",
			diskAssistant, diskToolResult, len(loaded.Messages))
	}
}
