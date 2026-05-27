package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/tool"
)

type scriptedSubagentClient struct {
	responses []*Message
	idx       int
}

func (c *scriptedSubagentClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	if c.idx >= len(c.responses) {
		return &Message{Role: "assistant", Content: "done"}, nil
	}
	msg := c.responses[c.idx]
	c.idx++
	return msg, nil
}

func (c *scriptedSubagentClient) GetProvider() string { return "mock" }
func (c *scriptedSubagentClient) GetModel() string    { return "mock-model" }

type countingMockTool struct {
	name   string
	result string
	calls  int
}

func (t *countingMockTool) Name() string        { return t.name }
func (t *countingMockTool) Description() string { return "" }
func (t *countingMockTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": t.name}
}
func (t *countingMockTool) Execute(args json.RawMessage) (string, error) {
	t.calls++
	return t.result, nil
}
func (t *countingMockTool) Parallel() bool { return true }

func makeAskToolCall() ToolCall {
	call := ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = "ask_tool"
	call.Function.Arguments = `{}`
	return call
}

func TestTaskSubagentInheritsParentPermissionRules(t *testing.T) {
	client := &scriptedSubagentClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{makeAskToolCall()}},
		{Role: "assistant", Content: "done"},
	}}
	askTool := &countingMockTool{name: "ask_tool", result: "executed"}
	parent := NewAgent(client, []tool.Tool{askTool}, nil)
	parent.Permissions().SetRule("task", PermissionAllow)
	parent.Permissions().SetRule("ask_tool", PermissionAllow)

	asks := 0
	parent.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		asks++
		return PermissionResponse{Level: PermissionDeny}
	})

	reg := NewAgentRegistry()
	reg.defs = append(reg.defs, AgentDefinition{
		Name:        "inherit-rule",
		Description: "test",
		Mode:        AgentModeSubagent,
		Tools:       []string{"ask_tool"},
		Source:      "test",
	})

	result, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(json.RawMessage(`{"prompt":"run","agent":"inherit-rule"}`))
	if err != nil {
		t.Fatalf("task execute: %v", err)
	}
	if asks != 0 {
		t.Fatalf("sub-agent permission callback called %d times, want 0", asks)
	}
	if !strings.Contains(result, "done") {
		t.Fatalf("expected final sub-agent response, got %q", result)
	}
	if askTool.calls != 1 {
		t.Fatalf("ask_tool executed %d times, want 1", askTool.calls)
	}
}

func TestTaskSubagentInheritsParentPermissionModeAcrossSpecOverrides(t *testing.T) {
	client := &scriptedSubagentClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{makeAskToolCall()}},
		{Role: "assistant", Content: "done"},
	}}
	askTool := &countingMockTool{name: "ask_tool", result: "executed"}
	parent := NewAgent(client, []tool.Tool{askTool}, nil)
	parent.Permissions().SetRule("task", PermissionAllow)
	parent.Permissions().SetMode(PermissionModeYOLO)

	asks := 0
	parent.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		asks++
		return PermissionResponse{Level: PermissionDeny}
	})

	reg := NewAgentRegistry()
	reg.defs = append(reg.defs, AgentDefinition{
		Name:        "inherit-mode",
		Description: "test",
		Mode:        AgentModeSubagent,
		Tools:       []string{"ask_tool"},
		Permissions: map[string]interface{}{"read": "allow"},
		Source:      "test",
	})

	result, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(json.RawMessage(`{"prompt":"run","agent":"inherit-mode"}`))
	if err != nil {
		t.Fatalf("task execute: %v", err)
	}
	if asks != 0 {
		t.Fatalf("sub-agent permission callback called %d times, want 0", asks)
	}
	if !strings.Contains(result, "done") {
		t.Fatalf("expected final sub-agent response, got %q", result)
	}
	if askTool.calls != 1 {
		t.Fatalf("ask_tool executed %d times, want 1", askTool.calls)
	}
}
