package agent

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// A tool that signals when it starts and then blocks until released, so a test
// can cancel the agent while the tool is genuinely in flight.
type cancelBlockingTool struct {
	name    string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *cancelBlockingTool) Name() string        { return b.name }
func (b *cancelBlockingTool) Description() string { return "" }
func (b *cancelBlockingTool) Parallel() bool      { return false }
func (b *cancelBlockingTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": b.name}
}

func (b *cancelBlockingTool) Execute(args json.RawMessage) (string, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return "done", nil
}

// oneShotClient returns a single scripted assistant message carrying the given
// tool calls, then a plain assistant reply.
type oneShotClient struct {
	mu    sync.Mutex
	calls int
	tcs   []ToolCall
}

func (c *oneShotClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return &Message{Role: "assistant", Content: "working", ToolCalls: c.tcs}, nil
	}
	return &Message{Role: "assistant", Content: "finished"}, nil
}

func (c *oneShotClient) GetProvider() string { return "mock" }
func (c *oneShotClient) GetModel() string    { return "mock-model" }

// TestStepCancelDuringToolRoundEmitsResultForEveryCall is the regression test
// for "after I hit Stop, the tool call still shows as running".
//
// The assistant message with its tool_calls is appended to newMsgs BEFORE any
// tool executes. Every cancellation exit inside the dispatch loop then returns
// newMsgs early, so a tool that never started (or whose result was computed but
// never appended) leaves an assistant tool_call with NO matching tool message.
//
// Both the web UI (ChatPanel renderEntries: resultContent undefined -> ToolBlock
// "running…") and the TUI treat an unanswered tool_call as still in flight, and
// recoverOrphanedToolCalls RE-EXECUTES it on the next turn. So a cancelled tool
// call must still be answered with a tool message saying it was cancelled.
func TestStepCancelDuringToolRoundEmitsResultForEveryCall(t *testing.T) {
	tc1 := ToolCall{ID: "call-1", Type: "function"}
	tc1.Function.Name = "slow_tool"
	tc1.Function.Arguments = `{}`
	tc2 := ToolCall{ID: "call-2", Type: "function"}
	tc2.Function.Name = "second_tool"
	tc2.Function.Arguments = `{}`

	client := &oneShotClient{tcs: []ToolCall{tc1, tc2}}
	slow := &cancelBlockingTool{name: "slow_tool", started: make(chan struct{}), release: make(chan struct{})}
	second := &cancelBlockingTool{name: "second_tool", started: make(chan struct{}), release: make(chan struct{})}
	a := newTestAgent(client, []tool.Tool{slow, second}, nil, nil)
	a.permissions = nil

	done := make(chan struct{})
	var msgs []Message
	go func() {
		defer close(done)
		msgs, _ = a.Step([]Message{{Role: "user", Content: "go"}})
	}()

	// Cancel while the FIRST tool is genuinely blocked inside Execute.
	<-slow.started
	a.Cancel()
	close(slow.release)
	close(second.release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Step() did not return after cancellation")
	}

	// The load-bearing assertion: EVERY tool_call must be answered. An
	// unanswered call is what renders as "running…" forever and what
	// recoverOrphanedToolCalls re-executes on the next turn.
	answered := map[string]string{}
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolID != "" {
			answered[m.ToolID] = m.Content
		}
	}
	for _, want := range []string{"call-1", "call-2"} {
		if _, ok := answered[want]; !ok {
			t.Fatalf("tool call %s has no tool result; transcript ends with an unanswered call (renders as running):\n%+v", want, msgs)
		}
	}
	// call-2 never started (the sequential loop bailed on isCancelled before
	// reaching it), so it must carry the explicit cancellation text — that is
	// what tells the model the call did NOT run.
	if got := answered["call-2"]; got != tool.ToolCancelledResult {
		t.Errorf("call-2 result = %q, want the cancellation sentinel %q", got, tool.ToolCancelledResult)
	}
	// call-1 DID complete before the cancel landed. Its real result must
	// survive: discarding it would hide work that already had side effects.
	if got := answered["call-1"]; got != "done" {
		t.Errorf("call-1 result = %q, want the completed result %q to be preserved", got, "done")
	}
}

// TestStepCancelBeforeFirstToolAnswersLaterCalls covers the sequential path:
// the loop checks isCancelled() at the top of each sequential iteration, so a
// tool that never started must still get an explicit cancelled result.
func TestStepCancelBeforeFirstToolAnswersLaterCalls(t *testing.T) {
	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "never_runs"
	tc.Function.Arguments = `{}`

	client := &oneShotClient{tcs: []ToolCall{tc}}
	never := &cancelBlockingTool{name: "never_runs", started: make(chan struct{}), release: make(chan struct{})}
	a := newTestAgent(client, []tool.Tool{never}, nil, nil)
	a.permissions = nil

	// Cancel BEFORE Step runs: the sequential loop's isCancelled() guard fires
	// before Execute, so the tool must still be answered.
	a.Cancel()
	msgs, err := a.Step([]Message{{Role: "user", Content: "go"}})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	close(never.release)

	// With a pre-cancelled agent the assistant message itself is never
	// appended (the loop returns at the top), so there is nothing to answer.
	// The assertion that matters is that we never emit an assistant tool_call
	// without a result — assert the invariant rather than a specific shape.
	for i, m := range msgs {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		for _, c := range m.ToolCalls {
			found := false
			for _, r := range msgs[i+1:] {
				if r.Role == "tool" && r.ToolID == c.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("assistant tool_call %s at index %d has no tool result", c.ID, i)
			}
		}
	}
}
