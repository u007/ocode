package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUIOnlyFieldsDoNotReachProviderPayloads pins the contract stated on
// Message.ToolDurationMs: UI-only state (tool_duration_ms, mcp_app,
// suggestions, user_seq, display-only Notice) must never appear in a
// provider-bound payload. The builders construct maps from explicit fields,
// so this can only break if someone later marshals Message wholesale — which
// is what this test guards.
func TestUIOnlyFieldsDoNotReachProviderPayloads(t *testing.T) {
	c := &GenericClient{}

	toolCall := ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "bash", Arguments: `{"command":"ls"}`},
	}
	msgs := []Message{
		{Role: "user", Content: "run ls", UserSeq: 42},
		{Role: "assistant", ToolCalls: []ToolCall{toolCall}, Suggestions: []string{"run again"}},
		{
			Role:           "tool",
			ToolID:         "call-1",
			Content:        "total 0",
			DisplayContent: "total 0 (full)",
			ToolDurationMs: 4200,
			MCPApp:         &MCPAppRef{Server: "srv", Tool: "t", ResourceURI: "ui://res"},
			Notice:         "ui notice",
		},
	}

	for _, build := range []struct {
		name  string
		build func() (interface{}, error)
	}{
		{"openai", func() (interface{}, error) { return c.convertToOpenAIMessages(msgs) }},
		{"anthropic", func() (interface{}, error) { return c.buildAnthropicMessages(msgs) }},
	} {
		payload, err := build.build()
		if err != nil {
			t.Fatalf("%s: %v", build.name, err)
		}
		blob, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("%s: %v", build.name, err)
		}
		for _, forbidden := range []string{
			"tool_duration_ms", "ToolDurationMs", "mcp_app", "MCPApp",
			"suggestions", "Suggestions", "user_seq", "UserSeq",
			"display_content", "DisplayContent", "total 0 (full)",
			"ui notice",
		} {
			if strings.Contains(string(blob), forbidden) {
				t.Errorf("%s payload contains forbidden UI-only fragment %q: %s", build.name, forbidden, blob)
			}
		}
	}
}
