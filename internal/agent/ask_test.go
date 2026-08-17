package agent

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// scriptedSideQueryClient returns a fixed sequence of responses: first a tool
// call (so the loop exercises tool execution), then plain text answers. It
// also records every received message so tests can assert the main agent's
// client was never touched.
type scriptedSideQueryClient struct {
	responses []*Message
	idx       int
	seen      [][]Message
}

func (c *scriptedSideQueryClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	c.seen = append(c.seen, append([]Message(nil), messages...))
	if c.idx >= len(c.responses) {
		return &Message{Role: "assistant", Content: "final"}, nil
	}
	r := c.responses[c.idx]
	c.idx++
	return r, nil
}

func (c *scriptedSideQueryClient) GetProvider() string { return "mock" }
func (c *scriptedSideQueryClient) GetModel() string    { return "mock-model" }

// TestAskLoopAsyncRunsIndependentToolCallLoop verifies the /btw side-query
// runs a REAL agent loop: a tool call is dispatched and executed, then the
// follow-up answer is concatenated and delivered — not a one-shot completion.
func TestAskLoopAsyncRunsIndependentToolCallLoop(t *testing.T) {
	var calls int32
	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "count"
	tc.Function.Arguments = `{}`
	childClient := &scriptedSideQueryClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{tc}},
	}}
	mainClient := &scriptedSideQueryClient{}

	main := NewAgent(mainClient, []tool.Tool{countingTool{calls: &calls}}, nil, nil)
	// The count tool is ASK-level by default in normal mode; a side query
	// cannot show a permission dialog, so the loop needs an explicit allow
	// (mirrors auto-permission allowing read-only tools in the real flow).
	main.permissions.SetRule("count", PermissionAllow)

	var gotText string
	var gotErr error
	done := make(chan struct{})
	cancel := main.AskLoopAsync(
		[]Message{{Role: "user", Content: "run count twice"}},
		AskLoopOptions{
			Client:   childClient, // isolated from the main client
			Tools:    []tool.Tool{countingTool{calls: &calls}},
			MaxSteps: 8,
		},
		func(content string, err error) {
			gotText, gotErr = content, err
			close(done)
		},
	)
	defer cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AskLoopAsync did not deliver a result")
	}
	if gotErr != nil {
		t.Fatalf("err = %v", gotErr)
	}
	if !strings.Contains(gotText, "final") {
		t.Fatalf("answer = %q, want the follow-up assistant text", gotText)
	}
	// The tool loop actually executed the tool call.
	if atomic.LoadInt32(&calls) == 0 {
		t.Fatal("tool call was never executed — the loop did not run")
	}
	// The main agent's client saw nothing: the side query ran on its own
	// client and its own message list.
	if len(mainClient.seen) != 0 {
		t.Fatalf("main client received %d chats, want 0 (side query must be isolated)", len(mainClient.seen))
	}
}

// TestAskLoopAsyncCancellation verifies cancel() stops the loop promptly and
// the result callback still fires (with whatever was produced before cancel).
func TestAskLoopAsyncCancellation(t *testing.T) {
	mkCall := func() ToolCall {
		tc := ToolCall{ID: "c1", Type: "function"}
		tc.Function.Name = "count"
		tc.Function.Arguments = `{}`
		return tc
	}
	client := &blockingToolCallClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
		resp:    &Message{Role: "assistant", ToolCalls: []ToolCall{mkCall()}},
	}
	var calls int32
	main := NewAgent(&MockClient{}, []tool.Tool{countingTool{calls: &calls}}, nil, nil)
	main.permissions.SetRule("count", PermissionAllow)

	done := make(chan struct{})
	cancel := main.AskLoopAsync(
		[]Message{{Role: "user", Content: "go"}},
		AskLoopOptions{Client: client, Tools: []tool.Tool{countingTool{calls: &calls}}},
		func(content string, err error) { close(done) },
	)

	<-client.started
	cancel() // must interrupt the loop, not hang
	close(client.release)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("AskLoopAsync did not return after cancel")
	}
}

// TestAskLoopAsyncPanicRecovered verifies a panicking client is recovered in
// the goroutine and surfaced as an error through onResult (no deadlock).
func TestAskLoopAsyncPanicRecovered(t *testing.T) {
	main := NewAgent(&MockClient{}, nil, nil, nil)
	done := make(chan struct{})
	var gotErr error
	cancel := main.AskLoopAsync(
		[]Message{{Role: "user", Content: "boom"}},
		AskLoopOptions{Client: &panicClient{}},
		func(content string, err error) { gotErr = err; close(done) },
	)
	defer cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("AskLoopAsync hung on panic")
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "panic") {
		t.Fatalf("err = %v, want panic error", gotErr)
	}
}

// TestAskLoopAsyncPermissionAskDeniedNonBlocking verifies a tool call whose
// permission decision is ASK is denied by the child's non-blocking deny
// callback — the loop continues to the next step instead of pausing on a
// sentinel the popup cannot answer.
func TestAskLoopAsyncPermissionAskDeniedNonBlocking(t *testing.T) {
	tc := ToolCall{ID: "c1", Type: "function"}
	tc.Function.Name = "count"
	tc.Function.Arguments = `{}`
	// First chat asks for the ASK-level tool; second chat is the follow-up.
	childClient := &scriptedSideQueryClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{tc}},
	}}
	var calls int32
	main := NewAgent(&MockClient{}, []tool.Tool{countingTool{calls: &calls}}, nil, nil)
	// Make the count tool ASK-level so Decide returns PermissionAsk.
	main.permissions.SetRule("count", PermissionAsk)

	var gotText string
	var gotErr error
	done := make(chan struct{})
	cancel := main.AskLoopAsync(
		[]Message{{Role: "user", Content: "use count"}},
		AskLoopOptions{Client: childClient, Tools: []tool.Tool{countingTool{calls: &calls}}},
		func(content string, err error) { gotText, gotErr = content, err; close(done) },
	)
	defer cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AskLoopAsync hung on a permission ask — deny must be non-blocking")
	}
	if gotErr != nil {
		t.Fatalf("err = %v", gotErr)
	}
	if !strings.Contains(gotText, "final") {
		t.Fatalf("answer = %q, want the follow-up text after a denied tool call", gotText)
	}
	// The denied tool must not have executed.
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("denied tool executed %d times, want 0", atomic.LoadInt32(&calls))
	}
}

// TestAskLoopAsyncStreamsActivity verifies OnMessage (tool activity) fires
// during the loop. (OnDelta is only exercisable through a *GenericClient,
// which mocks do not implement; the TUI side covers live delta rendering.)
func TestAskLoopAsyncStreamsActivity(t *testing.T) {
	tc := ToolCall{ID: "c1", Type: "function"}
	tc.Function.Name = "count"
	tc.Function.Arguments = `{}`
	childClient := &scriptedSideQueryClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{tc}},
	}}
	var calls int32
	main := NewAgent(&MockClient{}, []tool.Tool{countingTool{calls: &calls}}, nil, nil)
	main.permissions.SetRule("count", PermissionAllow)

	var activityMsgs int
	done := make(chan struct{})
	cancel := main.AskLoopAsync(
		[]Message{{Role: "user", Content: "run"}},
		AskLoopOptions{
			Client: childClient,
			Tools:  []tool.Tool{countingTool{calls: &calls}},
			OnMessage: func(am Message) {
				if am.Role == "assistant" && len(am.ToolCalls) > 0 {
					activityMsgs++
				}
			},
		},
		func(content string, err error) { close(done) },
	)
	defer cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	if activityMsgs == 0 {
		t.Fatal("OnMessage never fired for a tool-call assistant message")
	}
}

// TestAskLoopAsyncMaxStepsTerminates verifies the step cap ends a runaway
// loop instead of running forever.
func TestAskLoopAsyncMaxStepsTerminates(t *testing.T) {
	tc := ToolCall{ID: "c1", Type: "function"}
	tc.Function.Name = "count"
	tc.Function.Arguments = `{}`
	// Every response requests count again — an infinite tool loop.
	childClient := &scriptedSideQueryClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{tc}},
		{Role: "assistant", ToolCalls: []ToolCall{tc}},
		{Role: "assistant", ToolCalls: []ToolCall{tc}},
	}}
	var calls int32
	main := NewAgent(&MockClient{}, []tool.Tool{countingTool{calls: &calls}}, nil, nil)
	main.permissions.SetRule("count", PermissionAllow)

	done := make(chan struct{})
	cancel := main.AskLoopAsync(
		[]Message{{Role: "user", Content: "loop"}},
		AskLoopOptions{Client: childClient, Tools: []tool.Tool{countingTool{calls: &calls}}, MaxSteps: 3},
		func(content string, err error) { close(done) },
	)
	defer cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("step cap did not terminate the loop")
	}
}

// TestAskLoopAsyncExcludedToolsRemovedFromFinalMap verifies the advisor
// finding: NewAgent unconditionally registers the dispatch family (task,
// task_status, agent_status, task_cancel, wait) plus advisor/knowledge_lookup
// regardless of the Tools slice, so AskLoopOptions.ExcludedTools must delete
// them from the child's FINAL tool map — a slice filter alone is not enough.
func TestAskLoopAsyncExcludedToolsRemovedFromFinalMap(t *testing.T) {
	main := NewAgent(&MockClient{}, nil, nil, nil)

	// Mirror the TUI: the builtin tool set is passed in (read/grep/glob/...),
	// and NewAgent unconditionally adds the dispatch family on top — which
	// ExcludedTools must then remove.
	builtins := tool.InitBuiltinTools(nil, nil, nil)
	child, err := main.newSideQueryAgent(AskLoopOptions{
		Client: &MockClient{Response: &Message{Role: "assistant", Content: "ok"}},
		Tools:  builtins,
		ExcludedTools: []string{
			"task", "task_status", "agent_status", "task_cancel", "wait",
			"advisor", "knowledge_lookup", "question",
		},
	})
	if err != nil {
		t.Fatalf("newSideQueryAgent err: %v", err)
	}
	defer child.shutdownTransient()

	// The model sees GetTools() (filtered by isToolAllowed) — the exclusion
	// must hold there, not just in the internal map.
	seen := map[string]bool{}
	for _, tt := range child.GetTools() {
		seen[tt.Name()] = true
	}
	for _, excluded := range []string{"task", "task_status", "agent_status", "task_cancel", "wait", "advisor", "knowledge_lookup", "question"} {
		if seen[excluded] {
			t.Errorf("side-query agent still exposes excluded tool %q", excluded)
		}
	}
	// And the internal map must not contain them either.
	for _, excluded := range []string{"task", "task_status", "agent_status", "task_cancel", "wait", "advisor", "knowledge_lookup", "question"} {
		if _, ok := child.tools[excluded]; ok {
			t.Errorf("side-query agent internal map still contains excluded tool %q", excluded)
		}
	}
	// The exploration core survives.
	for _, want := range []string{"read", "grep", "glob", "list", "bash", "webfetch"} {
		if _, ok := child.tools[want]; !ok {
			t.Errorf("side-query agent missing expected tool %q", want)
		}
	}
}

func TestAskLoopAsyncFreshClientFailureIsStartupError(t *testing.T) {
	// Provider "mock" is not keyed: NewClient returns nil for the derived
	// "mock/mock-model" id, so the fresh-client build fails fast.
	main := NewAgent(&MockClient{}, nil, nil, nil)
	done := make(chan struct{})
	var gotErr error
	cancel := main.AskLoopAsync(
		[]Message{{Role: "user", Content: "hi"}},
		AskLoopOptions{}, // Client nil → fresh build path
		func(content string, err error) { gotErr = err; close(done) },
	)
	defer cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("no result")
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "side-query client") {
		t.Fatalf("err = %v, want a fresh-client build error", gotErr)
	}
}
