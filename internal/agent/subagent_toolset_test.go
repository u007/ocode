package agent

import (
	"encoding/json"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestSubagentToolSetExcludesWriteClassTodo(t *testing.T) {
	// Simulate the parent's tool set including the todo tools (which is what
	// NewAgent wires in practice) and verify the subagent filter strips the
	// write-class todo tools while keeping todoread.
	parentTools := []tool.Tool{
		&tool.TodoReadTool{},
		&tool.TodoWriteTool{},
		&tool.TodoUpdateTool{},
		&stubTool{name: "bash"},
	}
	got := filterMainOnlyTools(parentTools)

	var hasRead, hasWrite, hasUpdate bool
	for _, t := range got {
		switch t.Name() {
		case "todoread":
			hasRead = true
		case "todowrite":
			hasWrite = true
		case "todo_update":
			hasUpdate = true
		}
	}
	if !hasRead {
		t.Fatal("subagent tool set must include todoread")
	}
	if hasWrite {
		t.Fatal("subagent tool set must not include todowrite")
	}
	if hasUpdate {
		t.Fatal("subagent tool set must not include todo_update")
	}
}

// stubTool is a minimal tool.Tool used by subagent filter tests. Other tests
// in this file define richer fakes; this one is just to assert the filter
// passes non-main-agent-only tools through.
type stubTool struct{ name string }

func (s stubTool) Name() string        { return s.name }
func (s stubTool) Description() string { return "stub" }
func (s stubTool) Parallel() bool      { return true }
func (s stubTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": s.name, "description": "stub"}
}
func (s stubTool) Execute(args json.RawMessage) (string, error) { return "", nil }
