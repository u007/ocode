package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// TestTaskToolBlocksHiddenAgentFromUnrelatedCaller verifies that a hidden
// agent (e.g. an orchestrator pipeline's internal subagent) cannot be
// dispatched by a caller loaded from a different source directory —
// including the main LLM, whose spec has no matching registry entry.
func TestTaskToolBlocksHiddenAgentFromUnrelatedCaller(t *testing.T) {
	client := &scriptedSubagentClient{responses: []*Message{{Role: "assistant", Content: "done"}}}
	parent := NewAgent(client, []tool.Tool{}, nil, nil)
	parent.SetSpec(&AgentSpec{Name: "unrelated-caller"})

	reg := NewAgentRegistry()
	reg.defs = append(reg.defs,
		AgentDefinition{
			Name:   "unrelated-caller",
			Mode:   AgentModePrimary,
			Source: "some/other/plugin/agents/unrelated-caller.md",
		},
		AgentDefinition{
			Name:   "orchestrator-developer",
			Mode:   AgentModeSubagent,
			Hidden: true,
			Source: ".opencode/plugins/orchestrator/agents/orchestrator-developer.md",
		},
	)

	_, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(
		json.RawMessage(`{"prompt":"do it","agent":"orchestrator-developer"}`))
	if err == nil {
		t.Fatalf("expected dispatch to be rejected, got no error")
	}
	if !strings.Contains(err.Error(), "internal-only") {
		t.Fatalf("expected internal-only rejection, got: %v", err)
	}
}

// TestTaskToolAllowsHiddenAgentFromSamePlugin verifies the same hidden
// agent CAN be dispatched by a caller loaded from the same plugin
// directory (e.g. orchestrator.md dispatching orchestrator-developer.md).
func TestTaskToolAllowsHiddenAgentFromSamePlugin(t *testing.T) {
	client := &scriptedSubagentClient{responses: []*Message{{Role: "assistant", Content: "done"}}}
	parent := NewAgent(client, []tool.Tool{}, nil, nil)
	parent.SetSpec(&AgentSpec{Name: "orchestrator-pipeline"})

	reg := NewAgentRegistry()
	reg.defs = append(reg.defs,
		AgentDefinition{
			Name:   "orchestrator-pipeline",
			Mode:   AgentModePrimary,
			Source: ".opencode/plugins/orchestrator/agents/orchestrator.md",
		},
		AgentDefinition{
			Name:   "orchestrator-developer",
			Mode:   AgentModeSubagent,
			Hidden: true,
			Source: ".opencode/plugins/orchestrator/agents/orchestrator-developer.md",
		},
	)

	result, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(
		json.RawMessage(`{"prompt":"do it","agent":"orchestrator-developer"}`))
	if err != nil {
		t.Fatalf("expected dispatch to succeed, got: %v", err)
	}
	if !strings.Contains(result, "done") {
		t.Fatalf("expected final sub-agent response, got %q", result)
	}
}
