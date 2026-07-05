package agent

import (
	"strings"
	"testing"
)

// TestParseGoogleInteractionsToolCallName reproduces the bug where a Gemini
// function_call step finalized with an empty tool name, causing the agent to
// reject it ("tool \"\" is not allowed"). The name/id arrive in the step.start
// frame; arguments stream later via arguments_delta.
func TestParseGoogleInteractionsToolCallName(t *testing.T) {
	stream := strings.Join([]string{
		`event: step.start`,
		`data: {"index":0,"step":{"type":"function_call","id":"call_abc","name":"list_directory"}}`,
		``,
		`event: step.delta`,
		`data: {"index":0,"delta":{"type":"arguments_delta","arguments":"{\"path\":\"internal/tui\"}"}}`,
		``,
		`event: step.stop`,
		`data: {"index":0}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	msg, _, err := parseGoogleInteractionsStream(strings.NewReader(stream), nil, nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.Function.Name != "list_directory" {
		t.Errorf("tool name = %q, want %q", tc.Function.Name, "list_directory")
	}
	if tc.ID != "call_abc" {
		t.Errorf("tool id = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Function.Arguments != `{"path":"internal/tui"}` {
		t.Errorf("tool args = %q", tc.Function.Arguments)
	}
}
