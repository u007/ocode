package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// attrSubagentClient returns the same scripted two-step shape the other
// sub-agent permission tests use: one tool call, then a plain reply.
func attrSubagentClient() *scriptedSubagentClient {
	return &scriptedSubagentClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{makeAskToolCall()}},
		{Role: "assistant", Content: "done"},
	}}
}

func attrAskTool() *countingMockTool {
	return &countingMockTool{name: "ask_tool", result: "executed"}
}

func attrRegistry(name string, reg *AgentRegistry) {
	reg.defs = append(reg.defs, AgentDefinition{
		Name:        name,
		Description: "test",
		Mode:        AgentModeSubagent,
		Tools:       []string{"ask_tool"},
		Source:      "test",
	})
}

// TestSubagentPermissionAskCarriesAgentName pins the attribution the web/desktop
// prompt depends on: without a name a dialog can only say "a sub-agent asked",
// which is useless when several run at once. The sub-agent's ask must arrive at
// the shared callback stamped with the DISPATCHING agent's name.
func TestSubagentPermissionAskCarriesAgentName(t *testing.T) {
	askTool := attrAskTool()
	parent := NewAgent(attrSubagentClient(), []tool.Tool{askTool}, nil, nil)
	parent.Permissions().SetRule("task", PermissionAllow)
	// ask_tool has no rule, so Decide returns Ask and the callback runs.

	var seen []PermissionRequest
	parent.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		seen = append(seen, req)
		return PermissionResponse{Level: PermissionDeny}
	})

	reg := NewAgentRegistry()
	attrRegistry("named-writer", reg)
	if _, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(json.RawMessage(`{"prompt":"run","agent":"named-writer"}`)); err != nil {
		t.Fatalf("task execute: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("permission callback called %d times, want 1", len(seen))
	}
	if seen[0].AgentName != "named-writer" {
		t.Fatalf("ask AgentName = %q, want %q", seen[0].AgentName, "named-writer")
	}
	if askTool.calls != 0 {
		t.Fatalf("ask_tool ran %d times on a denied ask, want 0", askTool.calls)
	}
}

// TestAttributePermAskerKeepsExistingAgentName guards the nesting rule: a
// grandchild's wrapper stamps the grandchild's name first, and the
// intermediate levels must not overwrite it with their own.
func TestAttributePermAskerKeepsExistingAgentName(t *testing.T) {
	var seen PermissionRequest
	base := func(req PermissionRequest) PermissionResponse {
		seen = req
		return PermissionResponse{Level: PermissionDeny}
	}

	parentLevel := attributePermAsker(base, "parent-child")
	grandchildLevel := attributePermAsker(parentLevel, "grandchild")

	// The request as the GRANDCHILD's asker produces it: already stamped.
	grandchildLevel(PermissionRequest{ToolName: "bash", AgentName: "grandchild"})
	if seen.AgentName != "grandchild" {
		t.Fatalf("AgentName = %q, want the grandchild's own name to survive", seen.AgentName)
	}

	// An unstamped request gets filled by whichever level sees it first.
	parentLevel(PermissionRequest{ToolName: "bash"})
	if seen.AgentName != "parent-child" {
		t.Fatalf("AgentName = %q, want the wrapping level's name", seen.AgentName)
	}
}

// TestAttributePermAskerNilStaysNil: a nil callback must stay nil, so a host
// that installed no asker keeps Agent.Step on the PERMISSION_ASK sentinel path.
// Wrapping nil into a non-nil func that panics on call would be a crash.
func TestAttributePermAskerNilStaysNil(t *testing.T) {
	if got := attributePermAsker(nil, "someone"); got != nil {
		t.Fatalf("attributePermAsker(nil) = non-nil, want nil so Step keeps the sentinel path")
	}
	// An empty name returns the callback unwrapped (nothing to stamp, and a
	// closure would only add an indirection).
	f := func(req PermissionRequest) PermissionResponse { return PermissionResponse{Level: PermissionDeny} }
	if got := attributePermAsker(f, ""); got == nil {
		t.Fatalf("attributePermAsker(f, \"\") = nil, want the callback itself")
	}
}

// TestAttributePermAskerForwardsTheDecision: the wrapper is a pass-through for
// everything except the stamp. A broken wrapper that swallowed the level would
// silently deny every sub-agent call.
func TestAttributePermAskerForwardsTheDecision(t *testing.T) {
	want := PermissionResponse{Level: PermissionAllow, PersistRule: true}
	f := attributePermAsker(func(req PermissionRequest) PermissionResponse { return want }, "child")
	got := f(PermissionRequest{ToolName: "bash"})
	if got != want {
		t.Fatalf("response = %+v, want %+v forwarded unchanged", got, want)
	}
}

// TestSubagentAskStampsAgentNameOnlyWithCallback: with no asker installed the
// sub-agent must still take the sentinel path (the main agent has no callback
// either), and the sentinel payload must carry no agent_name key at all — the
// field is omitempty so every pre-existing frame stays byte-identical.
func TestSubagentAskStampsAgentNameOnlyWithCallback(t *testing.T) {
	askTool := attrAskTool()
	parent := NewAgent(attrSubagentClient(), []tool.Tool{askTool}, nil, nil)
	parent.Permissions().SetRule("task", PermissionAllow)

	reg := NewAgentRegistry()
	attrRegistry("sentinel-writer", reg)
	if _, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(json.RawMessage(`{"prompt":"run","agent":"sentinel-writer"}`)); err != nil {
		t.Fatalf("task execute: %v", err)
	}
	if askTool.calls != 0 {
		t.Fatalf("ask_tool ran %d times with no asker installed, want 0", askTool.calls)
	}
}

// TestPermissionRequestAgentNameOmittedWhenEmpty pins wire stability: an
// unattributed ask must marshal exactly as it did before the field existed.
func TestPermissionRequestAgentNameOmittedWhenEmpty(t *testing.T) {
	req := PermissionRequest{ToolName: "bash", Command: "ls", Rule: "bash.unknown"}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "agent_name") {
		t.Fatalf("empty AgentName leaked into the wire payload: %s", raw)
	}

	req.AgentName = "context"
	raw, err = json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back PermissionRequest
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.AgentName != "context" {
		t.Fatalf("round-tripped AgentName = %q, want %q", back.AgentName, "context")
	}
}
