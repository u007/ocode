package agent

import (
	"testing"

	"github.com/jamesmercstudio/ocode/internal/tool"
)

// TestTaskTool_HiddenAgentsExcludedFromEnum guards against the regression where
// hidden agents (title, compaction) leaked into the JSON Schema enum and were
// callable by the model via task(agent="title"). The description already
// filtered them out; the enum did not.
func TestTaskTool_HiddenAgentsExcludedFromEnum(t *testing.T) {
	registry := NewAgentRegistry()
	tool := TaskTool{registry: registry}

	def := tool.Definition()
	params, ok := def["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("definition missing parameters object")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters missing properties")
	}

	for _, field := range []string{"agent", "subagent_type"} {
		spec, ok := props[field].(map[string]interface{})
		if !ok {
			t.Fatalf("missing %q property", field)
		}
		enum, ok := spec["enum"].([]string)
		if !ok {
			t.Fatalf("%q enum is not []string: %T", field, spec["enum"])
		}
		for _, banned := range []string{"title", "compaction"} {
			for _, v := range enum {
				if v == banned {
					t.Errorf("%q enum contains hidden agent %q (full enum: %v)", field, banned, enum)
				}
			}
		}
		// Sanity: visible agents must still be there.
		var hasGeneral bool
		for _, v := range enum {
			if v == "general" {
				hasGeneral = true
			}
		}
		if !hasGeneral {
			t.Errorf("%q enum missing visible agent 'general': %v", field, enum)
		}
	}
}

// TestSamplingTunable_SkipsReasoningModels guards against the regression where
// temperature/top_p were sent to OpenAI reasoning families (o1/o3/o4/gpt-5)
// that reject the sampling tunables.
func TestSamplingTunable_SkipsReasoningModels(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-4o", true},
		{"gpt-4-turbo", true},
		{"openai/gpt-4o", true},
		{"claude-sonnet-4-6", true},
		{"o1", false},
		{"o1-preview", false},
		{"o3", false},
		{"o3-mini", false},
		{"o4-mini", false},
		{"gpt-5", false},
		{"gpt-5-codex", false},
		{"openai/gpt-5.1", false},
		{"openai:gpt-5.1-codex", false},
	}
	for _, c := range cases {
		gc := &GenericClient{Model: c.model}
		got := gc.samplingTunable()
		if got != c.want {
			t.Errorf("samplingTunable(%q) = %v, want %v", c.model, got, c.want)
		}
	}
}

// TestSamplingTunable_SkipsWhenThinking guards the Anthropic thinking-mode
// branch — extended-thinking sessions must not receive temperature/top_p.
func TestSamplingTunable_SkipsWhenThinking(t *testing.T) {
	gc := &GenericClient{Model: "claude-sonnet-4-6", ThinkingBudget: 8000}
	if gc.samplingTunable() {
		t.Errorf("samplingTunable should be false when ThinkingBudget > 0")
	}
}

// TestApplyGenerationParams_OmitsForReasoningModel asserts that calling
// applyGenerationParams on a reasoning-family client leaves the payload clean.
func TestApplyGenerationParams_OmitsForReasoningModel(t *testing.T) {
	temp, top := 0.4, 0.9
	gc := &GenericClient{Model: "gpt-5.1", Temperature: &temp, TopP: &top}
	payload := map[string]interface{}{"model": "gpt-5.1"}
	gc.applyGenerationParams(payload)
	if _, ok := payload["temperature"]; ok {
		t.Errorf("payload should NOT carry temperature for reasoning model, got: %v", payload)
	}
	if _, ok := payload["top_p"]; ok {
		t.Errorf("payload should NOT carry top_p for reasoning model, got: %v", payload)
	}
}

// TestSubAgentSpec_InheritsModelAndSamplingParams guards against the
// regression where subagent construction built an AgentSpec literal that
// omitted Model/Temperature/TopP/Color and assigned to subAgent.spec directly
// (bypassing SetSpec → applySpecModel).
func TestSubAgentSpec_InheritsModelAndSamplingParams(t *testing.T) {
	// Build a parent agent with a vanilla GenericClient.
	parent := &Agent{
		client: &GenericClient{Provider: "anthropic", Model: "claude-haiku-4-5"},
		tools:  map[string]tool.Tool{},
	}
	// Construct the spec we want the subagent to receive — these fields were
	// previously dropped in subagent.go.
	temp, top := 0.2, 0.5
	subAgentSpec := AgentSpec{
		Name:        "explore",
		Description: "test",
		Color:       "#123456",
		Temperature: &temp,
		TopP:        &top,
	}
	// Mirror what TaskTool.Execute does after the fix:
	sub := NewAgent(parent.client, nil, nil)
	sub.SetSpec(&subAgentSpec)

	if sub.spec == nil {
		t.Fatal("subagent spec was not set")
	}
	if sub.spec.Color != "#123456" {
		t.Errorf("subagent spec missing Color: %q", sub.spec.Color)
	}
	if sub.spec.Temperature == nil || *sub.spec.Temperature != temp {
		t.Errorf("subagent spec missing Temperature: %v", sub.spec.Temperature)
	}
	if sub.spec.TopP == nil || *sub.spec.TopP != top {
		t.Errorf("subagent spec missing TopP: %v", sub.spec.TopP)
	}
	// applySpecModel must have pushed sampling params onto the client.
	gc, ok := sub.client.(*GenericClient)
	if !ok {
		t.Fatalf("expected *GenericClient on subagent, got %T", sub.client)
	}
	if gc.Temperature == nil || *gc.Temperature != temp {
		t.Errorf("subagent client did not receive Temperature: %v", gc.Temperature)
	}
	if gc.TopP == nil || *gc.TopP != top {
		t.Errorf("subagent client did not receive TopP: %v", gc.TopP)
	}
}
