package agent

import (
	"strings"
	"testing"
)

func TestHiddenAgentsRegistered(t *testing.T) {
	reg := NewAgentRegistry()
	for _, name := range []string{"title", "compaction"} {
		def := reg.Get(name)
		if def == nil {
			t.Errorf("expected hidden agent %q to be registered", name)
			continue
		}
		if !def.Hidden {
			t.Errorf("agent %q should be Hidden=true", name)
		}
		if strings.TrimSpace(def.SystemPrompt) == "" {
			t.Errorf("agent %q must have a non-empty SystemPrompt", name)
		}
	}
}

func TestHiddenAgentsExcludedFromPrimaryListings(t *testing.T) {
	reg := NewAgentRegistry()
	// SubAgents() intentionally returns hidden agents (TaskTool exposes them in
	// the enum but not in the user-visible description). Only PrimaryAgents and
	// PrimaryAgentSpecs should strip them, since /agent and Tab list those.
	for _, d := range reg.PrimaryAgents() {
		if d.Hidden {
			t.Errorf("PrimaryAgents() leaked hidden agent %q", d.Name)
		}
	}
}

func TestLookupHiddenAgent(t *testing.T) {
	saved := DefaultAgentRegistry
	DefaultAgentRegistry = NewAgentRegistry()
	defer func() { DefaultAgentRegistry = saved }()

	if def := lookupHiddenAgent("title"); def == nil {
		t.Fatal("lookupHiddenAgent(title) returned nil")
	}
	// Visible agents must NOT be reachable via lookupHiddenAgent.
	if def := lookupHiddenAgent("explore"); def != nil {
		t.Errorf("lookupHiddenAgent should refuse non-hidden agents, got %+v", def)
	}
	if def := lookupHiddenAgent("does-not-exist"); def != nil {
		t.Errorf("expected nil for missing name, got %+v", def)
	}
}
