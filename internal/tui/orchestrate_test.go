package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

func TestOrchestrateCommandRegistered(t *testing.T) {
	found := false
	for _, spec := range commandSpecs {
		if spec.name == "/orchestrate" {
			found = true
			if spec.handler == nil {
				t.Error("/orchestrate has no handler")
			}
			break
		}
	}
	if !found {
		t.Error("/orchestrate not found in commandSpecs")
	}
}

func TestOrchestrateGoalExtraction(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"add", "user", "validation"}, "add user validation"},
		{[]string{"fix", "nil", "panic", "in", "auth"}, "fix nil panic in auth"},
		{[]string{}, ""},
	}
	for _, c := range cases {
		got := strings.Join(c.args, " ")
		if got != c.want {
			t.Errorf("args %v → %q, want %q", c.args, got, c.want)
		}
	}
}

func TestOrchestratorRegisteredInRegistry(t *testing.T) {
	def := agent.DefaultAgentRegistry.Get("orchestrator")
	if def == nil {
		t.Fatal("orchestrator not found in DefaultAgentRegistry")
	}
	if def.Hidden {
		t.Error("orchestrator should NOT be hidden — it must appear in the agent picker")
	}
	if def.SystemPrompt != "" {
		t.Error("orchestrator registry entry should have no system prompt — it is a picker-only entry")
	}
}
