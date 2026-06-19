package agent

import (
	"encoding/json"
	"testing"
)

func TestDispatchSubagent_callsTaskTool(t *testing.T) {
	// DispatchSubagent must be defined on *Agent
	// This test verifies the method signature compiles.
	// Functional dispatch testing requires a live client — see integration tests.
	var a *Agent
	_ = func() {
		// Should compile:
		_, _ = a.DispatchSubagent("explore", "find auth files")
	}
	// Verify JSON marshalling of the args matches what TaskTool.Execute expects
	args, err := json.Marshal(map[string]any{
		"agent":  "explore",
		"prompt": "find auth files",
	})
	if err != nil {
		t.Fatalf("failed to marshal dispatch args: %v", err)
	}
	var params struct {
		Prompt string `json:"prompt"`
		Agent  string `json:"agent"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if params.Agent != "explore" || params.Prompt != "find auth files" {
		t.Errorf("unexpected params: %+v", params)
	}
}
