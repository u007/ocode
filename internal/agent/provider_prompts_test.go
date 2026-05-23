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
