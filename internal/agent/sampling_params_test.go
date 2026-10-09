package agent

import (
	"testing"

	"github.com/u007/ocode/internal/config"
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

// TestApplySpecModel_CarriesDebugSessionIDOntoSwappedClient is the regression
// guard for the subagent Logs-tab leak: when a purpose/small-model spec swaps
// the client, the replacement carries the debug-log sessionID (not just the
// opencode affinity id), so its TOKENS rows are attributed to the owning chat
// instead of falling back to the process-global sink. Reverting the
// sessionIDValue re-tag in applySpecModel makes this fail.
func TestApplySpecModel_CarriesDebugSessionIDOntoSwappedClient(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	a := &Agent{
		client: &GenericClient{Provider: "openai", Model: "gpt-4o-mini"},
		config: &config.Config{},
	}
	a.SetSessionID("ses_swap_attr")

	a.applySpecModel(&AgentSpec{Name: "swap", Model: "openai/gpt-4o"})

	gc, ok := a.client.(*GenericClient)
	if !ok {
		t.Fatalf("client swapped to %T, want *GenericClient", a.client)
	}
	if got := gc.sessionIDValue(); got != "ses_swap_attr" {
		t.Fatalf("swapped client debug sessionID = %q, want ses_swap_attr", got)
	}
}

// TestDefaultTemperature pins the per-family default temperature table. A nil
// want means the model gets no default (the provider's own default applies).
func TestDefaultTemperature(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		model string
		want  *float64
	}{
		{"minimax/minimax-m2.5", f(1.0)}, {"minimax/minimax-m2.7", f(1.0)}, {"minimax/minimax-m2", f(1.0)},
		{"qwen/qwen3.7-max", f(0.55)},
		{"claude-sonnet-4-6", nil},
		{"north/north-mini-code", f(1.0)},
		{"deepseek/deepseek-v4-pro", f(0.6)}, {"deepseek/deepseek-v4-flash", f(0.6)},
		{"xiaomi/mimo-v2-flash", f(0.6)}, {"xiaomi/mimo-v2-pro", f(0.6)}, {"xiaomi/mimo-v2.5", f(0.6)}, {"xiaomi/mimo-v2.5-pro", f(0.6)},
		{"x-ai/grok-4-fast-non-reasoning", f(0.7)}, {"x-ai/grok-4-1-fast-non-reasoning", f(0.7)},
		{"x-ai/grok-4", nil}, {"x-ai/grok-4.3", nil}, {"x-ai/grok-4-fast-reasoning", nil},
		{"google/gemma-4-31b-it", f(0.8)},
		{"mistral/codestral-latest", f(0.7)}, {"mistral/devstral-latest", f(0.7)}, {"mistral/mistral-large-latest", f(0.7)},
		{"cohere/command-a-03-2025", f(0.75)}, {"cohere/command-r-08-2024", f(0.75)},
		{"meta/llama-3.3-70b-instruct", f(0.7)},
		{"nvidia/nemotron-3-nano-30b-a3b", f(0.7)},
		{"gemini/gemini-2.0-flash", f(1.0)},
		{"zhipu/glm-4.5", f(1.0)}, {"zhipu/glm-4.6", f(1.0)}, {"zhipu/glm-4.7", f(1.0)}, {"zai/glm-5", f(1.0)}, {"zai/glm-5.1", f(1.0)}, {"zai/glm-5.2", f(1.0)},
		{"kimi/kimi-k2-thinking", f(1.0)}, {"kimi/kimi-k2.5", f(1.0)}, {"kimi/kimi-k2p5", f(1.0)}, {"kimi/kimi-k2-5", f(1.0)}, {"kimi/kimi-k2.6", f(1.0)}, {"moonshotai/kimi-k2.7-code", f(1.0)},
		{"kimi/kimi-k2", f(0.6)},
	}
	for _, tc := range cases {
		checkSamplingDefault(t, "defaultTemperature", tc.model, defaultTemperature(tc.model), tc.want)
	}
}

func TestDefaultTopP(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		model string
		want  *float64
	}{
		{"minimax/minimax-m2.5", f(0.95)}, {"gemini/gemini-2.0-flash", f(0.95)},
		{"kimi/kimi-k2.5", f(0.95)}, {"kimi/kimi-k2p5", f(0.95)}, {"kimi/kimi-k2-5", f(0.95)}, {"kimi/kimi-k2.6", f(0.95)}, {"moonshotai/kimi-k2.7-code", f(0.95)},
		{"qwen/qwen3.7-max", f(1)},
		{"claude-sonnet-4-6", nil},
	}
	for _, tc := range cases {
		checkSamplingDefault(t, "defaultTopP", tc.model, defaultTopP(tc.model), tc.want)
	}
}

func TestDefaultTopK(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		model string
		want  *float64
	}{
		{"minimax/minimax-m2.5", f(40)}, {"minimax/minimax-m2.1", f(40)}, {"minimax/minimax-m2.7", f(40)}, {"minimax/minimax-m25", f(40)}, {"minimax/minimax-m21", f(40)},
		{"minimax/minimax-m2", f(20)},
		{"gemini/gemini-2.0-flash", f(64)},
		{"claude-sonnet-4-6", nil},
	}
	for _, tc := range cases {
		checkSamplingDefault(t, "defaultTopK", tc.model, defaultTopK(tc.model), tc.want)
	}
}

func checkSamplingDefault(t *testing.T, fn, model string, got, want *float64) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s(%q) = %v, want nil", fn, model, *got)
	case want != nil && (got == nil || *got != *want):
		t.Errorf("%s(%q) = %v, want %v", fn, model, got, *want)
	}
}
