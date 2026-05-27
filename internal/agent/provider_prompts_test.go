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
