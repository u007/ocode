//go:build !windows

package server

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// terminalToolNames are the tools that read terminal state. They belong to the
// Pulse assistant alone: no other agent may be handed them.
var terminalToolNames = []string{"terminal_read", "terminal_tabs"}

func toolNamesOf(defs []map[string]interface{}) map[string]bool {
	out := make(map[string]bool, len(defs))
	for _, d := range defs {
		if name, ok := d["name"].(string); ok {
			out[name] = true
		}
	}
	return out
}

// TestTerminalToolsAreOnlyForThePulseAssistant pins the scope of the terminal
// tools. The built-in toolset that every ordinary session gets must not carry
// them, and a Pulse agent must expose only the Pulse set (which carries them).
func TestTerminalToolsAreOnlyForThePulseAssistant(t *testing.T) {
	cfg := &config.Config{}
	ordinary := agent.NewAgent(nil, tool.InitBuiltinToolsWithComputerDriver(nil, cfg, nil, nil, nil), cfg, nil)
	ordinaryNames := toolNamesOf(ordinary.GetToolDefinitions())
	for _, name := range terminalToolNames {
		if ordinaryNames[name] {
			t.Fatalf("ordinary agent exposes the Pulse-only tool %q", name)
		}
	}

	h := NewHandler()
	pulseAgent := agent.NewAgent(nil, tool.InitBuiltinToolsWithComputerDriver(nil, cfg, nil, nil, nil), cfg, nil)
	h.configurePulseAgent(pulseAgent, cfg)
	pulseNames := toolNamesOf(pulseAgent.GetToolDefinitions())
	for _, name := range terminalToolNames {
		if !pulseNames[name] {
			t.Fatalf("Pulse agent is missing %q", name)
		}
	}
	pulseSet := make(map[string]bool)
	for _, pt := range h.pulseTools() {
		pulseSet[pt.Name()] = true
	}
	for name := range pulseNames {
		if !pulseSet[name] {
			t.Fatalf("Pulse agent exposes %q, which is not in the Pulse tool set", name)
		}
	}
}
