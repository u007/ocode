package agent

import (
	"strings"
	"testing"
)

func TestProviderPrompt_KnownProviders(t *testing.T) {
	cases := []struct {
		provider string
		want     string
	}{
		{"anthropic", "Claude"},
		{"claude", "Claude"},
		{"openai", "GPT"},
		{"gpt", "GPT"},
		{"google", "Gemini"},
		{"gemini", "Gemini"},
		{"vertex", "Gemini"},
		{"copilot", "Copilot"},
		{"moonshot", "Kimi"},
	}
	for _, c := range cases {
		got := providerPrompt(c.provider)
		if got == "" {
			t.Errorf("providerPrompt(%q) returned empty", c.provider)
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("providerPrompt(%q) missing %q: %s", c.provider, c.want, got)
		}
	}
}

func TestProviderPrompt_UnknownReturnsEmpty(t *testing.T) {
	if got := providerPrompt("acme-cloud"); got != "" {
		t.Errorf("expected empty for unknown provider, got %q", got)
	}
	if got := providerPrompt(""); got != "" {
		t.Errorf("expected empty for empty provider, got %q", got)
	}
}

func TestModelFamilyPrompt_ModelIDRouting(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		model    string
		want     string // substring expected in the returned fragment
	}{
		{"claude opus", "anthropic", "claude-opus-4-7", "Claude"},
		{"claude haiku", "anthropic", "claude-haiku-4-5", "Claude"},
		{"claude via openai-like provider", "openrouter", "claude-3.5-sonnet", "Claude"},
		{"o1 reasoning", "openai", "o1", "reasoning"},
		{"o1-mini reasoning", "openai", "o1-mini", "reasoning"},
		{"o3-mini reasoning", "openai", "o3-mini", "reasoning"},
		{"o4-mini reasoning", "openai", "o4-mini", "reasoning"},
		{"claude thinking reasoning", "anthropic", "claude-opus-4-thinking", "reasoning"},
		{"gpt-4o", "openai", "gpt-4o", "GPT"},
		{"gpt-5", "openai", "gpt-5", "GPT"},
		{"gemini 2", "google", "gemini-2.0-flash", "Gemini"},
		{"kimi k2", "moonshot", "kimi-k2", "Kimi"},
		{"copilot provider only", "copilot", "", "Copilot"},
		{"small model deepseek flash", "opencode-go", "deepseek-v4-flash", "Intent Analysis"},
		{"small model mimo", "opencode", "mimo-v2.5-free", "Intent Analysis"},
		{"small model qwen", "opencode-go", "qwen-3.5-plus", "Intent Analysis"},
		{"small model deepseek chat", "deepseek", "deepseek-chat", "Intent Analysis"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := modelFamilyPrompt(c.provider, c.model)
			if got == "" {
				t.Fatalf("modelFamilyPrompt(%q, %q) returned empty", c.provider, c.model)
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("modelFamilyPrompt(%q, %q) missing %q: first line = %q", c.provider, c.model, c.want, strings.SplitN(got, "\n", 2)[0])
			}
		})
	}
}

func TestModelFamilyPrompt_UnknownReturnsEmpty(t *testing.T) {
	if got := modelFamilyPrompt("acme", "unknown-model"); got != "" {
		t.Errorf("expected empty for unknown provider+model, got %q", got)
	}
	if got := modelFamilyPrompt("", ""); got != "" {
		t.Errorf("expected empty for empty inputs, got %q", got)
	}
}

func TestIsReasoningModel(t *testing.T) {
	yes := []string{"o1", "o1-mini", "o1-preview", "o3", "o3-mini", "o4-mini", "claude-opus-4-thinking", "some-thinking-model"}
	no := []string{"", "gpt-4o", "gpt-5", "claude-opus-4-7", "gemini-2.0-flash", "kimi-k2", "o", "open-model"}
	for _, m := range yes {
		if !isReasoningModel(m) {
			t.Errorf("isReasoningModel(%q) = false, want true", m)
		}
	}
	for _, m := range no {
		if isReasoningModel(m) {
			t.Errorf("isReasoningModel(%q) = true, want false", m)
		}
	}
}

func TestIsSmallModel(t *testing.T) {
	// Note: GetModel() returns just the model part (e.g., "deepseek-v4-flash"),
	// not the full "provider/model" string. The isSmallModel function should
	// match on just the model part.
	yes := []string{
		"deepseek-v4-flash",       // from "opencode-go/deepseek-v4-flash"
		"mimo-v2.5-free",          // from "opencode/mimo-v2.5-free"
		"qwen-3.5-plus",           // from "opencode-go/qwen-3.5-plus"
		"deepseek-chat",           // from "deepseek/deepseek-chat"
		"MiMo-V2.5",               // from "xiaomi-token-plan-sgp/MiMo-V2.5"
	}
	no := []string{
		"",
		"gpt-4o",
		"gpt-5",
		"claude-opus-4-7",
		"claude-3-haiku",
		"gemini-2.0-flash",
		"gemini-2.0-pro",
		"kimi-k2",
		"o1",
		"random-model",
		"opencode-go/deepseek-v4-flash",  // Full string should NOT match (model part only)
		"opencode/mimo-v2.5-free",        // Full string should NOT match
	}
	for _, m := range yes {
		if !isSmallModel(m) {
			t.Errorf("isSmallModel(%q) = false, want true", m)
		}
	}
	for _, m := range no {
		if isSmallModel(m) {
			t.Errorf("isSmallModel(%q) = true, want false", m)
		}
	}
}

type providerStubClient struct{ provider, model string }

func (c providerStubClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	return &Message{Role: "assistant"}, nil
}
func (c providerStubClient) GetProvider() string { return c.provider }
func (c providerStubClient) GetModel() string    { return c.model }
func (c providerStubClient) StreamChat(messages []Message, tools []map[string]interface{}, onChunk func(string)) (*Message, error) {
	return c.Chat(messages, tools)
}

func TestBasePromptIncludesProviderFragment(t *testing.T) {
	a := &Agent{client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"}}
	msgs := a.BasePromptMessages("")
	var found bool
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, promptProviderMarker) {
			found = true
			if !strings.Contains(m.Content, "Claude") {
				t.Errorf("provider fragment present but missing Claude tuning: %s", m.Content)
			}
		}
	}
	if !found {
		t.Fatalf("expected %s fragment in BasePromptMessages; got %d messages", promptProviderMarker, len(msgs))
	}
}

func TestBasePromptOmitsProviderFragmentForUnknown(t *testing.T) {
	a := &Agent{client: providerStubClient{provider: "acme", model: "x"}}
	for _, m := range a.BasePromptMessages("") {
		if strings.HasPrefix(m.Content, promptProviderMarker) {
			t.Fatalf("unexpected provider fragment for unknown provider: %s", m.Content)
		}
	}
}

func TestBasePromptReasoningFragmentForOSeries(t *testing.T) {
	a := &Agent{client: providerStubClient{provider: "openai", model: "o3-mini"}}
	msgs := a.BasePromptMessages("")
	var found bool
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, promptProviderMarker) && strings.Contains(m.Content, "reasoning") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected reasoning fragment for o3-mini")
	}
}

// TestAgentModelFamilyPrompt_NilSafe locks in the nil-agent and nil-client
// behavior of the Agent.ModelFamilyPrompt() accessor. The TUI /context
// command relies on this not panicking when called during early startup
// before the LLM client is wired up.
func TestAgentModelFamilyPrompt_NilSafe(t *testing.T) {
	if got := (*Agent)(nil).ModelFamilyPrompt(); got != "" {
		t.Errorf("nil agent: got %q, want empty", got)
	}
	a := &Agent{} // nil client
	if got := a.ModelFamilyPrompt(); got != "" {
		t.Errorf("nil client: got %q, want empty", got)
	}
}

// TestAgentModelFamilyPrompt_MatchesHelper verifies the method returns the
// same value as the unexported modelFamilyPrompt helper. This is the
// contract the TUI /context command depends on.
func TestAgentModelFamilyPrompt_MatchesHelper(t *testing.T) {
	cases := []struct {
		provider, model string
	}{
		{"anthropic", "claude-opus-4-7"},
		{"openai", "gpt-5"},
		{"openai", "o1"},
		{"opencode-go", "deepseek-v4-flash"},
		{"acme", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.provider+"/"+c.model, func(t *testing.T) {
			a := &Agent{client: providerStubClient{provider: c.provider, model: c.model}}
			gotMethod := a.ModelFamilyPrompt()
			gotHelper := modelFamilyPrompt(c.provider, c.model)
			if gotMethod != gotHelper {
				t.Errorf("Agent.ModelFamilyPrompt() = %q, want %q (match modelFamilyPrompt)", gotMethod, gotHelper)
			}
		})
	}
}
