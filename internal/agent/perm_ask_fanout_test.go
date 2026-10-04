package agent

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// Regression, agent half: the host can only queue asks if the agent really
// emits several in one round. This pins that contract at the source instead of
// assuming it from reading the code.
//
// One assistant message carrying two ask-capable PARALLEL tool calls is enough:
// webfetch is Parallel() == true (internal/tool/web.go:38) and defaults to ask
// (NewPermissionManager's defaultRules, "webfetch" in the ask list). Step
// dispatches the round in goroutines, joins them, then walks the results calling
// OnMessage for EACH — so two asks reach the host in one frame, back to back.
//
// If a future change made a parallel ask unreachable, or collapsed the result
// fan-out to a single OnMessage, this test fails instead of the queue silently
// becoming dead code.

// askParallelTool stands in for webfetch: parallel-capable, and registered with
// an `ask` rule so the permission gate raises a sentinel rather than running it.
type askParallelTool struct {
	name   string
	execs  *int32
	muSync *sync.Mutex
}

func (t askParallelTool) Name() string        { return t.name }
func (t askParallelTool) Description() string { return "test ask-capable parallel tool" }
func (t askParallelTool) Parallel() bool      { return true }
func (t askParallelTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": t.name, "description": t.Description()}
}
func (t askParallelTool) Execute(args json.RawMessage) (string, error) {
	t.muSync.Lock()
	*t.execs++
	t.muSync.Unlock()
	return "executed", nil
}

type askFanoutClient struct {
	mu        sync.Mutex
	responses []*Message
	calls     int
}

func (c *askFanoutClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls >= len(c.responses) {
		return &Message{Role: "assistant", Content: "done"}, nil
	}
	r := c.responses[c.calls]
	c.calls++
	return r, nil
}

func (c *askFanoutClient) GetProvider() string { return "mock" }
func (c *askFanoutClient) GetModel() string    { return "mock-model" }

func TestStepDeliversEveryAskOfAParallelRoundThroughOnMessage(t *testing.T) {
	var first, second ToolCall
	first.ID, first.Type = "call_a", "function"
	first.Function.Name = "ask_parallel_a"
	first.Function.Arguments = `{"url":"https://a.example"}`
	second.ID, second.Type = "call_b", "function"
	second.Function.Name = "ask_parallel_b"
	second.Function.Arguments = `{"url":"https://b.example"}`

	client := &askFanoutClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{first, second}},
		{Role: "assistant", Content: "done"},
	}}

	var execs int32
	mu := &sync.Mutex{}
	a := NewAgent(client,
		[]tool.Tool{askParallelTool{name: "ask_parallel_a", execs: &execs, muSync: mu},
			askParallelTool{name: "ask_parallel_b", execs: &execs, muSync: mu}},
		nil, nil)
	// Explicit `ask` for both, mirroring the webfetch/websearch defaults the
	// real dispatch relies on.
	a.Permissions().SetRule("ask_parallel_a", PermissionAsk)
	a.Permissions().SetRule("ask_parallel_b", PermissionAsk)

	// Capture the OnMessage fan-out — the exact channel the TUI's
	// `case []agent.Message` handler consumes.
	var muMsgs sync.Mutex
	var delivered []Message
	a.OnMessage = func(m Message) {
		muMsgs.Lock()
		defer muMsgs.Unlock()
		delivered = append(delivered, m)
	}

	if _, err := a.Step([]Message{{Role: "user", Content: "fetch both"}}); err != nil {
		t.Fatalf("Step() error = %v", err)
	}

	// Collect the asks in delivery order.
	muMsgs.Lock()
	var asks []string // tool-call ids, in the order OnMessage delivered them
	for _, m := range delivered {
		if m.Role == "tool" && strings.HasPrefix(m.Content, tool.SentinelPermissionAsk) {
			asks = append(asks, m.ToolID)
		}
	}
	muMsgs.Unlock()

	if len(asks) != 2 {
		t.Fatalf("OnMessage delivered %d permission asks (%v), want 2 — a host queue over these is only correct if the round really produces several", len(asks), asks)
	}
	if asks[0] != "call_a" || asks[1] != "call_b" {
		t.Errorf("ask delivery order = %v, want [call_a call_b]; the host queue presents them in this order", asks)
	}

	// Neither tool may have run: an ask gate means the decision is the user's.
	mu.Lock()
	got := execs
	mu.Unlock()
	if got != 0 {
		t.Errorf("tool executions = %d, want 0 (both calls must stop at the ask gate)", got)
	}

	// The agent must STOP at the round that raised the asks — it must not spend
	// its next model call while a decision is outstanding. `pauseAfterResults`
	// returns from Step as soon as any result is an unanswered ask, so the
	// canned "done" response is never requested. This is the agent-side half of
	// the hold the TUI adds on the host side; if either regressed, an
	// unanswered ask would be resolved behind the user's back.
	client.mu.Lock()
	modelCalls := client.calls
	client.mu.Unlock()
	if modelCalls != 1 {
		t.Errorf("model calls = %d, want 1 — Step must not continue past a round holding unanswered asks", modelCalls)
	}
}
