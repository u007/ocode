package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
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
	parent := NewAgent(client, []tool.Tool{askTool}, nil, nil)
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
	parent := NewAgent(client, []tool.Tool{askTool}, nil, nil)
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

func TestTaskSubagentSharesParentPermissionManager(t *testing.T) {
	// Subagents now share the parent's PermissionManager rather than receiving a
	// clone. Grants applied to the parent at any time (before or after the
	// subagent is constructed) take effect immediately for in-flight subagents,
	// and "always allow" decisions made inside a subagent are visible to the
	// parent on the next tool call.
	client := &scriptedSubagentClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{makeAskToolCall()}},
		{Role: "assistant", Content: "done"},
	}}
	askTool := &countingMockTool{name: "ask_tool", result: "executed"}
	parent := NewAgent(client, []tool.Tool{askTool}, nil, nil)
	parent.Permissions().SetRule("task", PermissionAllow)
	parent.Permissions().SetRule("ask_tool", PermissionAllow)
	parent.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Level: PermissionDeny}
	})

	reg := NewAgentRegistry()
	reg.defs = append(reg.defs, AgentDefinition{
		Name:        "shared-pm",
		Description: "test",
		Mode:        AgentModeSubagent,
		Tools:       []string{"ask_tool"},
		// Spec adds an "addition to its own allowed" — grants apply to the
		// shared manager and extend the parent's allow-set.
		Permissions: map[string]interface{}{"webfetch": "allow"},
		Source:      "test",
	})

	parentPM := parent.Permissions()

	if _, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(json.RawMessage(`{"prompt":"run","agent":"shared-pm"}`)); err != nil {
		t.Fatalf("task execute: %v", err)
	}

	// The spec's webfetch allow must be visible on the shared (parent) manager.
	if got := parentPM.Check("webfetch"); got != PermissionAllow {
		t.Fatalf("after subagent ran, parent webfetch = %v, want Allow (spec rule should land on shared manager)", got)
	}
	if askTool.calls != 1 {
		t.Fatalf("ask_tool executed %d times, want 1", askTool.calls)
	}
}
