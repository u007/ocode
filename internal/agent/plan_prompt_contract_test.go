package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func planFragment(a *Agent) string {
	for _, m := range a.BasePromptMessages() {
		if promptMarker(m.Content) == promptPlanMarker {
			return m.Content
		}
	}
	return ""
}

func TestPlanContract_PrimaryAgentCarriesItAfterRecap(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	if !a.PlanPromptEnabled() {
		t.Fatal("primary agent must carry the plan fragment by default")
	}
	got := planFragment(a)
	if got == "" {
		t.Fatalf("[%s] fragment missing from base prompt", promptPlanMarker)
	}
	for _, want := range []string{"## Plan then execute", "todo_update set_deps", "task(todo_item=tN)", "Skip the plan for one-step requests"} {
		if !strings.Contains(got, want) {
			t.Errorf("plan fragment dropped %q", want)
		}
	}
	recapAt, planAt := -1, -1
	for i, m := range a.BasePromptMessages() {
		switch promptMarker(m.Content) {
		case promptRecapMarker:
			recapAt = i
		case promptPlanMarker:
			planAt = i
			if m.Role != "system" {
				t.Errorf("plan fragment role = %q, want system", m.Role)
			}
		}
	}
	if planAt != recapAt+1 {
		t.Errorf("plan fragment at %d, want directly after recap at %d", planAt, recapAt)
	}
}

func TestPlanContract_ByteStableAcrossCalls(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	if first, second := planFragment(a), planFragment(a); first == "" || first != second {
		t.Fatalf("plan fragment not byte-stable:\n%q\nvs\n%q", first, second)
	}
}

func TestPlanContract_Toggle(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetPlanPromptEnabled(false)
	if planFragment(a) != "" {
		t.Error("plan fragment present after SetPlanPromptEnabled(false)")
	}
}

func TestPlanContract_HelpersOptOut(t *testing.T) {
	parent := NewAgent(&MockClient{}, nil, nil, nil)
	child, err := parent.newSideQueryAgent(AskLoopOptions{Client: &MockClient{}})
	if err != nil {
		t.Fatalf("newSideQueryAgent: %v", err)
	}
	if child.PlanPromptEnabled() || planFragment(child) != "" {
		t.Error("ask helper must opt out of the plan fragment")
	}
}

func TestPlanContract_TaskSubagentOptsOut(t *testing.T) {
	client := &captureClient{}
	parent := NewAgent(client, nil, nil, nil)
	parent.Permissions().SetRule("task", PermissionAllow)
	parent.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Level: PermissionAllow}
	})
	reg := NewAgentRegistry()
	reg.defs = append(reg.defs, AgentDefinition{
		Name:        "prober",
		Description: "test",
		Mode:        AgentModeSubagent,
		Tools:       []string{"read"},
		Source:      "test",
	})
	if _, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(json.RawMessage(`{"prompt":"look","agent":"prober"}`)); err != nil {
		t.Fatalf("task execute: %v", err)
	}
	if len(client.Messages) == 0 {
		t.Fatal("sub-agent never called the client; the assertion below would be vacuous")
	}
	if strings.Contains(joinContents(client.Messages), promptPlanMarker) {
		t.Error("sub-agent received the plan fragment; it must opt out")
	}
}

func TestTaskDefinition_DescribesIsolatedReport(t *testing.T) {
	def := TaskTool{}.Definition()
	desc, _ := def["description"].(string)
	for _, want := range []string{"Run a sub-agent in its own context and return ONLY its final report", "todo_item (links it to a plan item)", "Available agents: "} {
		if !strings.Contains(desc, want) {
			t.Errorf("task description missing %q: %s", want, desc)
		}
	}
}

func TestGeneralSubAgentPrompt_ReportFormat(t *testing.T) {
	for _, want := range []string{"the FIRST line is one sentence stating the outcome", "DONE:", "VERIFIED:", "GAPS:", "User Expectation Checklist"} {
		if !strings.Contains(generalSubAgentPrompt, want) {
			t.Errorf("general prompt missing %q", want)
		}
	}
}
