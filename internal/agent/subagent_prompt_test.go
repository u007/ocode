package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

// TestSubagentDoesNotInheritParentModePrompt is the regression for the bug
// where TaskTool spawned subagents without setting subAgent.spec, causing the
// prompt assembler to fall through to Mode.SystemPrompt() and prepend BUILD
// (or PLAN/etc.) mode instructions on top of the subagent's own prompt.
func TestSubagentDoesNotInheritParentModePrompt(t *testing.T) {
	reg := NewAgentRegistry()
	cap := &captureClient{}
	parent := &Agent{
		client:      cap,
		tools:       make(map[string]tool.Tool),
		config:      &config.Config{Ocode: config.OcodeConfig{Permissions: config.PermissionConfig{Mode: "yolo"}}},
		permissions: NewPermissionManager(),
		mode:        ModeBuild,
	}
	parent.permissions.SetMode(PermissionModeYOLO)
	taskTool := TaskTool{mainAgent: parent, registry: reg}
	parent.tools["task"] = taskTool

	if _, err := taskTool.Execute(json.RawMessage(`{"prompt":"find me something","agent":"general"}`)); err != nil {
		t.Fatalf("task execute: %v", err)
	}

	if len(cap.Messages) == 0 {
		t.Fatal("capture client saw no messages")
	}

	var modeFragment, agentFragment string
	for _, m := range cap.Messages {
		if m.Role != "system" {
			continue
		}
		if strings.HasPrefix(m.Content, promptModeMarker) {
			modeFragment = m.Content
		}
	}
	agentFragment = modeFragment
	if agentFragment == "" {
		t.Fatal("subagent prompt missing entirely")
	}
	// The mode-marker slot must carry the SUBAGENT prompt, not the parent's
	// BUILD mode prompt. Two cheap signals: (1) it should mention something
	// from generalSubAgentPrompt, (2) it should NOT contain "BUILD mode".
	if strings.Contains(agentFragment, "BUILD mode") {
		t.Fatalf("subagent inherited parent BUILD mode prompt:\n%s", agentFragment)
	}
	if !strings.Contains(agentFragment, "general-purpose sub-agent") {
		t.Errorf("expected subagent prompt to contain general agent's identity, got:\n%s", agentFragment)
	}
}
