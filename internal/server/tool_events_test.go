package server

import (
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// recvEvent reads the next broadcast event of the given type, failing if none
// arrives. Other event kinds (user_message, permission_check, ...) are skipped
// so the assertions stay focused on tool framing.
func recvEvent(t *testing.T, ch chan SSEEvent, want string) SSEEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Event == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q event", want)
		}
	}
}

// TestToolEventsCarryCallID pins the correlation id onto the tool lifecycle
// frames. Without it the browser can only guess which bubble a result belongs
// to — the live view attaches a result to the most recent tool part still
// awaiting output, which mis-pairs whenever a turn dispatches tool calls in
// parallel (agent.go runs them concurrently).
//
// The ids come straight from the model's own tool-call plumbing: ToolCall.ID on
// the assistant message, echoed back as Message.ToolID on the tool result.
func TestToolEventsCarryCallID(t *testing.T) {
	h := NewHandler()
	ch := h.subscribeHeadless()
	defer h.unsubscribeHeadless(ch)

	ag := &agent.Agent{}
	h.wireHeadlessAgentCallbacks("sess-callid", ag)

	tc := agent.ToolCall{ID: "call-abc"}
	tc.Function.Name = "bash"
	tc.Function.Arguments = `{"command":"echo hi"}`
	ag.OnMessage(agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}})

	start := recvEvent(t, ch, "tool_start")
	startData, ok := start.Data.(ToolStartEvent)
	if !ok {
		t.Fatalf("tool_start payload was %T, want ToolStartEvent", start.Data)
	}
	if startData.CallID != "call-abc" {
		t.Fatalf("tool_start CallID = %q, want %q", startData.CallID, "call-abc")
	}

	ag.OnMessage(agent.Message{Role: "tool", ToolID: "call-abc", Content: "hi"})

	result := recvEvent(t, ch, "tool_result")
	resultData, ok := result.Data.(ToolResultEvent)
	if !ok {
		t.Fatalf("tool_result payload was %T, want ToolResultEvent", result.Data)
	}
	if resultData.CallID != "call-abc" {
		t.Fatalf("tool_result CallID = %q, want %q", resultData.CallID, "call-abc")
	}
}

// TestParallelToolResultsPairByCallID is the case the positional heuristic gets
// wrong: two tool calls in flight, results arriving in the opposite order. Each
// result must still identify its own call.
func TestParallelToolResultsPairByCallID(t *testing.T) {
	h := NewHandler()
	ch := h.subscribeHeadless()
	defer h.unsubscribeHeadless(ch)

	ag := &agent.Agent{}
	h.wireHeadlessAgentCallbacks("sess-parallel", ag)

	first := agent.ToolCall{ID: "call-first"}
	first.Function.Name = "bash"
	second := agent.ToolCall{ID: "call-second"}
	second.Function.Name = "read"
	ag.OnMessage(agent.Message{
		Role:      "assistant",
		ToolCalls: []agent.ToolCall{first, second},
	})

	starts := map[string]string{}
	for range 2 {
		ev := recvEvent(t, ch, "tool_start")
		data := ev.Data.(ToolStartEvent)
		starts[data.CallID] = data.Tool
	}
	if starts["call-first"] != "bash" || starts["call-second"] != "read" {
		t.Fatalf("tool_start events did not carry distinct call ids: %#v", starts)
	}

	// Results come back reversed — the positional heuristic would mis-pair here.
	ag.OnMessage(agent.Message{Role: "tool", ToolID: "call-second", Content: "file body"})
	ag.OnMessage(agent.Message{Role: "tool", ToolID: "call-first", Content: "shell out"})

	got := map[string]string{}
	for range 2 {
		ev := recvEvent(t, ch, "tool_result")
		data := ev.Data.(ToolResultEvent)
		got[data.CallID] = data.Output
	}
	if got["call-second"] != "file body" {
		t.Fatalf("call-second output = %q, want %q", got["call-second"], "file body")
	}
	if got["call-first"] != "shell out" {
		t.Fatalf("call-first output = %q, want %q", got["call-first"], "shell out")
	}
}

// TestServerWiresToolOutput is the gap this feature closes: OnToolOutput was
// wired only in the TUI, so a browser saw nothing between tool_start and
// tool_result and a slow tool looked identical to a hung one.
func TestServerWiresToolOutput(t *testing.T) {
	h := NewHandler()
	ch := h.subscribeHeadless()
	defer h.unsubscribeHeadless(ch)

	ag := &agent.Agent{}
	h.wireHeadlessAgentCallbacks("sess-output", ag)

	if ag.OnToolOutput == nil {
		t.Fatal("server must wire OnToolOutput so the browser gets live tool progress")
	}

	// Cross the byte threshold so the coalescer forwards immediately.
	ag.OnToolOutput("call-1", strings.Repeat("z", toolOutputFlushBytes+1))

	ev := recvEvent(t, ch, "tool_output")
	data, ok := ev.Data.(ToolOutputEvent)
	if !ok {
		t.Fatalf("tool_output payload was %T, want ToolOutputEvent", ev.Data)
	}
	if data.CallID != "call-1" {
		t.Fatalf("tool_output CallID = %q, want %q", data.CallID, "call-1")
	}
	if len(data.Chunk) != toolOutputFlushBytes+1 {
		t.Fatalf("tool_output Chunk length = %d, want %d", len(data.Chunk), toolOutputFlushBytes+1)
	}
}

// TestToolResultFlushesPendingOutput covers the tail of a short command: output
// below every flush threshold must still reach the UI when the call completes,
// and the call's buffer must be released rather than retained for the life of
// the server process.
func TestToolResultFlushesPendingOutput(t *testing.T) {
	h := NewHandler()
	ch := h.subscribeHeadless()
	defer h.unsubscribeHeadless(ch)

	ag := &agent.Agent{}
	h.wireHeadlessAgentCallbacks("sess-flush", ag)

	ag.OnToolOutput("call-tail", "short output")
	ag.OnMessage(agent.Message{Role: "tool", ToolID: "call-tail", Content: "short output"})

	ev := recvEvent(t, ch, "tool_output")
	data := ev.Data.(ToolOutputEvent)
	if data.Chunk != "short output" {
		t.Fatalf("expected the buffered tail to flush, got %q", data.Chunk)
	}
	if n := h.toolOutput.activeCalls(); n != 0 {
		t.Fatalf("completing a tool call must release its buffer, %d still tracked", n)
	}
}
