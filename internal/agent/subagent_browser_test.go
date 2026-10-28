package agent

import (
	"encoding/json"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// scopedBrowserTool is a fake AgentScopedTool: ForAgent returns a new instance,
// and the instance records which one it is and the agent type it was made for.
type scopedBrowserTool struct {
	id    int
	agent string
}

var scopedBrowserNext int

func (s *scopedBrowserTool) Name() string        { return "browser" }
func (s *scopedBrowserTool) Description() string { return "fake" }
func (s *scopedBrowserTool) Parallel() bool      { return false }
func (s *scopedBrowserTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "browser"}
}
func (s *scopedBrowserTool) Execute(json.RawMessage) (string, error) { return "", nil }
func (s *scopedBrowserTool) ForAgent(agentName string) tool.Tool {
	scopedBrowserNext++
	return &scopedBrowserTool{id: scopedBrowserNext, agent: agentName}
}

// TestSubagentBrowserInstanceIsScopedToItsAgentType pins that the agent type's
// name reaches ForAgent for every subagent, so a type's browser policy (see
// tool.RefusingTool) is the one that was declared for it.
func TestSubagentBrowserInstanceIsScopedToItsAgentType(t *testing.T) {
	parent := &scopedBrowserTool{}
	ag := newTestAgent(nil, []tool.Tool{parent, &stubTool{name: "read"}}, &config.Config{}, nil)
	task := TaskTool{mainAgent: ag}

	for _, spec := range []*AgentDefinition{
		{Name: "explore", Tools: []string{"browser"}},
		{Name: "context", Tools: []string{"browser"}},
		{Name: "general"},
	} {
		var child *scopedBrowserTool
		for _, tl := range task.getToolsForDef(spec) {
			if b, ok := tl.(*scopedBrowserTool); ok {
				child = b
			}
		}
		if child == nil || child.agent != spec.Name {
			t.Fatalf("%s: browser instance agent = %v, want %q", spec.Name, child, spec.Name)
		}
	}
}

// TestSubagentGetsItsOwnBrowserInstance pins that a subagent whose definition
// lists browser receives its own instance, not the parent's, so the screenshot
// cache and opened tabs are never shared.
func TestSubagentGetsItsOwnBrowserInstance(t *testing.T) {
	parent := &scopedBrowserTool{}
	ag := newTestAgent(nil, []tool.Tool{parent, &stubTool{name: "read"}}, &config.Config{}, nil)
	task := TaskTool{mainAgent: ag}

	for _, spec := range []*AgentDefinition{
		{Name: "explore", Tools: []string{"browser", "read"}},
		{Name: "general"}, // no explicit list: every main tool
	} {
		got := task.getToolsForDef(spec)
		var child *scopedBrowserTool
		for _, tl := range got {
			if b, ok := tl.(*scopedBrowserTool); ok {
				child = b
			}
		}
		if child == nil {
			t.Fatalf("%s: subagent got no browser tool", spec.Name)
		}
		if child == parent {
			t.Fatalf("%s: subagent shares the parent's browser instance", spec.Name)
		}
	}
}

// TestSubagentWithoutBrowserInListGetsNone pins that an explicit tool list is
// still an allowlist: browser reaches a subagent only when its definition names
// it.
func TestSubagentWithoutBrowserInListGetsNone(t *testing.T) {
	ag := newTestAgent(nil, []tool.Tool{&scopedBrowserTool{}, &stubTool{name: "read"}}, &config.Config{}, nil)
	task := TaskTool{mainAgent: ag}
	for _, tl := range task.getToolsForDef(&AgentDefinition{Name: "scout", Tools: []string{"read"}}) {
		if tl.Name() == "browser" {
			t.Fatal("explicit allowlist without browser still received it")
		}
	}
}

// TestBuiltinSubagentsListBrowser pins that every built-in definition with an
// explicit tool list names browser, so each can drive the browser.
func TestBuiltinSubagentsListBrowser(t *testing.T) {
	for _, def := range DefaultSubAgents {
		if len(def.Tools) == 0 {
			continue
		}
		found := false
		for _, name := range def.Tools {
			if name == "browser" {
				found = true
			}
		}
		if !found {
			t.Errorf("built-in subagent %q has an explicit tool list without browser", def.Name)
		}
	}
}
