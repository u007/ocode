package agent

import (
	"testing"

	"github.com/jamesmercstudio/ocode/internal/config"
)

func TestParseOptionalFloat(t *testing.T) {
	cases := []struct {
		in   string
		want *float64
	}{
		{"", nil},
		{"  ", nil},
		{"not a number", nil},
		{"0.3", floatPtr(0.3)},
		{"\"0.7\"", floatPtr(0.7)},
		{"1.0", floatPtr(1.0)},
	}
	for _, c := range cases {
		got := parseOptionalFloat(c.in)
		switch {
		case got == nil && c.want == nil:
			// ok
		case got == nil || c.want == nil:
			t.Errorf("parseOptionalFloat(%q): got=%v want=%v", c.in, got, c.want)
		case *got != *c.want:
			t.Errorf("parseOptionalFloat(%q): got=%v want=%v", c.in, *got, *c.want)
		}
	}
}

func TestParseAgentContent_ParsesTemperatureAndTopP(t *testing.T) {
	src := "---\ndescription: test\nmode: primary\ntemperature: 0.3\ntop_p: 0.9\n---\nbody"
	def, diags := parseAgentContent(src, "fake.md")
	if def == nil {
		t.Fatalf("expected def, diags: %+v", diags)
	}
	if def.Temperature == nil || *def.Temperature != 0.3 {
		t.Errorf("Temperature = %v, want 0.3", def.Temperature)
	}
	if def.TopP == nil || *def.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", def.TopP)
	}
	// No more "not yet applied" warnings — they now flow through to the client.
	for _, d := range diags {
		if d.Level == "warning" {
			t.Errorf("did not expect a warning for valid numeric tuning fields, got: %+v", d)
		}
	}
}

func TestParseAgentContent_WarnsOnInvalidTuning(t *testing.T) {
	src := "---\ndescription: test\nmode: primary\ntemperature: lukewarm\n---\nbody"
	_, diags := parseAgentContent(src, "fake.md")
	var sawWarn bool
	for _, d := range diags {
		if d.Level == "warning" && containsAny(d.Message, "temperature", "not a number") {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Errorf("expected a warning about invalid temperature, got: %+v", diags)
	}
}

func TestApplySpecModel_PushesSamplingParamsOntoClient(t *testing.T) {
	gc := &GenericClient{Provider: "anthropic", Model: "claude-haiku-4-5"}
	a := &Agent{client: gc, config: &config.Config{}}
	temp := 0.2
	topP := 0.5
	spec := &AgentSpec{Name: "tuned", Temperature: &temp, TopP: &topP}
	a.applySpecModel(spec)
	if gc.Temperature == nil || *gc.Temperature != temp {
		t.Errorf("Temperature not applied: %v", gc.Temperature)
	}
	if gc.TopP == nil || *gc.TopP != topP {
		t.Errorf("TopP not applied: %v", gc.TopP)
	}
}

func TestApplySpecModel_ClearsSamplingParamsWhenSpecHasNone(t *testing.T) {
	temp := 0.2
	gc := &GenericClient{Provider: "anthropic", Model: "claude-haiku-4-5", Temperature: &temp}
	a := &Agent{client: gc, config: &config.Config{}}
	a.applySpecModel(&AgentSpec{Name: "plain"})
	if gc.Temperature != nil {
		t.Errorf("Temperature should be cleared when next spec has none, got %v", *gc.Temperature)
	}
}

func floatPtr(v float64) *float64 { return &v }

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

func TestApplySpecModel_ClearsPreloadedModelContextOnClientSwap(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	a := &Agent{
		client:                &MockClient{},
		config:                &config.Config{},
		preloadedModelContext: "stale model context",
	}
	a.applySpecModel(&AgentSpec{Name: "swap", Model: "openai/gpt-4o"})
	if a.preloadedModelContext != "" {
		t.Fatalf("expected preloadedModelContext to be cleared on model swap, got %q", a.preloadedModelContext)
	}
}
