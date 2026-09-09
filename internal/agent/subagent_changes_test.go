package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// A task sub-agent's bash writes must land in the PARENT's changes
// registry — the one the Changes tab reads. The sub-agent shares the
// parent's snapshot store (so write/edit tool edits show up), but its bash
// recorder was built by NewAgent against the sub-agent's own throwaway
// registry, so files touched only by sub-agent shell commands never
// appeared on the tab.
func TestTaskSubagentBashWritesReachParentChangesRegistry(t *testing.T) {
	workDir := t.TempDir()
	bashCall := ToolCall{ID: "call-bash", Type: "function"}
	bashCall.Function.Name = "bash"
	bashCall.Function.Arguments = `{"command":"echo hi > ./out.txt"}`
	client := &scriptedSubagentClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{bashCall}},
		{Role: "assistant", Content: "done"},
	}}
	parent := NewAgent(client, nil, nil, nil)
	parent.SetWorkDir(workDir)
	parent.Permissions().SetRule("task", PermissionAllow)
	parent.Permissions().SetRule("bash", PermissionAllow)
	parent.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Level: PermissionAllow}
	})

	reg := NewAgentRegistry()
	reg.defs = append(reg.defs, AgentDefinition{
		Name:        "sheller",
		Description: "test",
		Mode:        AgentModeSubagent,
		Tools:       []string{"bash"},
		Source:      "test",
	})

	if _, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(json.RawMessage(`{"prompt":"run","agent":"sheller"}`)); err != nil {
		t.Fatalf("task execute: %v", err)
	}

	want := filepath.Join(workDir, "out.txt")
	for _, f := range parent.Changes().List() {
		if f.OriginalPath == want {
			return
		}
	}
	t.Fatalf("parent changes registry missing %s: got %+v", want, parent.Changes().List())
}
