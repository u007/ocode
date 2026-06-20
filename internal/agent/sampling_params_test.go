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

func TestDefaultTemperatureMinimaxM2(t *testing.T) {
	tests := []string{
		"minimax/minimax-m2.5",
		"minimax/minimax-m2.7",
		"minimax/minimax-m2",
	}
	for _, m := range tests {
		v := defaultTemperature(m)
		if v == nil || *v != 1.0 {
			t.Errorf("defaultTemperature(%q) = %v, want 1.0", m, v)
		}
	}
}

func TestDefaultTemperatureQwen(t *testing.T) {
	v := defaultTemperature("qwen/qwen3.7-max")
	if v == nil || *v != 0.55 {
		t.Errorf("defaultTemperature(qwen/qwen3.7-max) = %v, want 0.55", v)
	}
}

func TestDefaultTemperatureUnset(t *testing.T) {
	if v := defaultTemperature("claude-sonnet-4-6"); v != nil {
		t.Errorf("defaultTemperature(claude) = %v, want nil", v)
	}
}

func TestDefaultTemperatureNorthMiniCode(t *testing.T) {
	v := defaultTemperature("north/north-mini-code")
	if v == nil || *v != 1.0 {
		t.Errorf("defaultTemperature(north-mini-code) = %v, want 1.0", v)
	}
}

func TestDefaultTemperatureDeepseekV4(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"deepseek/deepseek-v4-pro", 0.6},
		{"deepseek/deepseek-v4-flash", 0.6},
	}
	for _, tc := range tests {
		v := defaultTemperature(tc.model)
		if v == nil || *v != tc.want {
			t.Errorf("defaultTemperature(%q) = %v, want %v", tc.model, v, tc.want)
		}
	}
}

func TestDefaultTemperatureMiMo(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"xiaomi/mimo-v2-flash", 0.6},
		{"xiaomi/mimo-v2-pro", 0.6},
		{"xiaomi/mimo-v2.5", 0.6},
		{"xiaomi/mimo-v2.5-pro", 0.6},
	}
	for _, tc := range tests {
		v := defaultTemperature(tc.model)
		if v == nil || *v != tc.want {
			t.Errorf("defaultTemperature(%q) = %v, want %v", tc.model, v, tc.want)
		}
	}
}

func TestDefaultTemperatureGrok(t *testing.T) {
	// Non-reasoning variants get a moderate temp
	nonReasoning := []string{
		"x-ai/grok-4-fast-non-reasoning",
		"x-ai/grok-4-1-fast-non-reasoning",
	}
	for _, m := range nonReasoning {
		v := defaultTemperature(m)
		if v == nil || *v != 0.7 {
			t.Errorf("defaultTemperature(%q) = %v, want 0.7", m, v)
		}
	}
	// Reasoning variants get nil (use reasoning_effort)
	reasoning := []string{
		"x-ai/grok-4",
		"x-ai/grok-4.3",
		"x-ai/grok-4-fast-reasoning",
	}
	for _, m := range reasoning {
		if v := defaultTemperature(m); v != nil {
			t.Errorf("defaultTemperature(%q) = %v, want nil", m, v)
		}
	}
}

func TestDefaultTemperatureGemma(t *testing.T) {
	v := defaultTemperature("google/gemma-4-31b-it")
	if v == nil || *v != 0.8 {
		t.Errorf("defaultTemperature(gemma) = %v, want 0.8", v)
	}
}

func TestDefaultTemperatureMistral(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"mistral/codestral-latest", 0.7},
		{"mistral/devstral-latest", 0.7},
		{"mistral/mistral-large-latest", 0.7},
	}
	for _, tc := range tests {
		v := defaultTemperature(tc.model)
		if v == nil || *v != tc.want {
			t.Errorf("defaultTemperature(%q) = %v, want %v", tc.model, v, tc.want)
		}
	}
}

func TestDefaultTemperatureCohere(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"cohere/command-a-03-2025", 0.75},
		{"cohere/command-r-08-2024", 0.75},
	}
	for _, tc := range tests {
		v := defaultTemperature(tc.model)
		if v == nil || *v != tc.want {
			t.Errorf("defaultTemperature(%q) = %v, want %v", tc.model, v, tc.want)
		}
	}
}

func TestDefaultTemperatureLlama(t *testing.T) {
	v := defaultTemperature("meta/llama-3.3-70b-instruct")
	if v == nil || *v != 0.7 {
		t.Errorf("defaultTemperature(llama) = %v, want 0.7", v)
	}
}

func TestDefaultTemperatureNemotron(t *testing.T) {
	v := defaultTemperature("nvidia/nemotron-3-nano-30b-a3b")
	if v == nil || *v != 0.7 {
		t.Errorf("defaultTemperature(nemotron) = %v, want 0.7", v)
	}
}

func TestDefaultTemperatureGemini(t *testing.T) {
	v := defaultTemperature("gemini/gemini-2.0-flash")
	if v == nil || *v != 1.0 {
		t.Errorf("defaultTemperature(gemini) = %v, want 1.0", v)
	}
}

func TestDefaultTemperatureGLM(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"zhipu/glm-4.5", 1.0},
		{"zhipu/glm-4.6", 1.0},
		{"zhipu/glm-4.7", 1.0},
		{"zai/glm-5", 1.0},
		{"zai/glm-5.1", 1.0},
		{"zai/glm-5.2", 1.0},
	}
	for _, tc := range tests {
		v := defaultTemperature(tc.model)
		if v == nil || *v != tc.want {
			t.Errorf("defaultTemperature(%q) = %v, want %v", tc.model, v, tc.want)
		}
	}
}

func TestDefaultTemperatureKimiK2(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"kimi/kimi-k2-thinking", 1.0},
		{"kimi/kimi-k2.5", 1.0},
		{"kimi/kimi-k2p5", 1.0},
		{"kimi/kimi-k2-5", 1.0},
		{"kimi/kimi-k2.6", 1.0},
		{"moonshotai/kimi-k2.7-code", 1.0},
		{"kimi/kimi-k2", 0.6},
	}
	for _, tc := range tests {
		v := defaultTemperature(tc.model)
		if v == nil || *v != tc.want {
			t.Errorf("defaultTemperature(%q) = %v, want %v", tc.model, v, tc.want)
		}
	}
}

func TestDefaultTopP(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"minimax/minimax-m2.5", 0.95},
		{"gemini/gemini-2.0-flash", 0.95},
		{"kimi/kimi-k2.5", 0.95},
		{"kimi/kimi-k2p5", 0.95},
		{"kimi/kimi-k2-5", 0.95},
		{"kimi/kimi-k2.6", 0.95},
		{"moonshotai/kimi-k2.7-code", 0.95},
		{"qwen/qwen3.7-max", 1},
	}
	for _, tc := range tests {
		v := defaultTopP(tc.model)
		if v == nil || *v != tc.want {
			t.Errorf("defaultTopP(%q) = %v, want %v", tc.model, v, tc.want)
		}
	}
}

func TestDefaultTopPUnset(t *testing.T) {
	if v := defaultTopP("claude-sonnet-4-6"); v != nil {
		t.Errorf("defaultTopP(claude) = %v, want nil", v)
	}
}

func TestDefaultTopKMinimaxM2Dot(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"minimax/minimax-m2.5", 40},
		{"minimax/minimax-m2.1", 40},
		{"minimax/minimax-m2.7", 40},
		{"minimax/minimax-m25", 40},
		{"minimax/minimax-m21", 40},
	}
	for _, tc := range tests {
		v := defaultTopK(tc.model)
		if v == nil || *v != tc.want {
			t.Errorf("defaultTopK(%q) = %v, want %v", tc.model, v, tc.want)
		}
	}
}

func TestDefaultTopKMinimaxM2Other(t *testing.T) {
	v := defaultTopK("minimax/minimax-m2")
	if v == nil || *v != 20 {
		t.Errorf("defaultTopK(minimax/minimax-m2) = %v, want 20", v)
	}
}

func TestDefaultTopKGemini(t *testing.T) {
	v := defaultTopK("gemini/gemini-2.0-flash")
	if v == nil || *v != 64 {
		t.Errorf("defaultTopK(gemini) = %v, want 64", v)
	}
}

func TestDefaultTopKUnset(t *testing.T) {
	if v := defaultTopK("claude-sonnet-4-6"); v != nil {
		t.Errorf("defaultTopK(claude) = %v, want nil", v)
	}
}
