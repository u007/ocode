package agent

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// commitMessageStubClient is a minimal LLMClient for commit-message generation
// tests. Every Chat call records its model and prompt.
type commitMessageStubClient struct {
	model   string
	reply   string
	err     error
	usage   *TokenUsage
	spend   *float64
	callLog *[]string
	prompts *[]string
}

func (c *commitMessageStubClient) Chat(msgs []Message, _ []map[string]interface{}) (*Message, error) {
	if c.callLog != nil {
		*c.callLog = append(*c.callLog, c.model)
	}
	if c.prompts != nil && len(msgs) > 0 {
		*c.prompts = append(*c.prompts, msgs[0].Content)
	}
	if c.err != nil {
		return nil, c.err
	}
	return &Message{Role: "assistant", Content: c.reply, Model: c.model, Usage: c.usage, Spend: c.spend}, nil
}

func (c *commitMessageStubClient) GetProvider() string { return "stub" }
func (c *commitMessageStubClient) GetModel() string    { return c.model }

// stubCommitMessageClients replaces both client factories for one test.
func stubCommitMessageClients(t *testing.T, build func(model string) LLMClient) {
	t.Helper()
	prev := newClientFn
	prevProfile := newClientWithProfileFn
	newClientFn = func(_ *config.Config, model string) LLMClient { return build(model) }
	newClientWithProfileFn = func(_ *config.Config, model, _ string) LLMClient { return build(model) }
	t.Cleanup(func() {
		newClientFn = prev
		newClientWithProfileFn = prevProfile
	})
}

func commitMessageTestCfg(model, small, explicit string) *config.Config {
	return &config.Config{
		Model: model,
		Ocode: config.OcodeConfig{SmallModel: small, CommitMsgModel: explicit},
	}
}

func TestCommitMessageModelChain(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		small    string
		main     string
		want     []string
	}{
		{"explicit model is authoritative", "explicit/model", "small/model", "main/model", []string{"explicit/model"}},
		{"small model falls back to main", "", "small/model", "main/model", []string{"small/model", "main/model"}},
		{"main model when no small model", "", "", "main/model", []string{"main/model"}},
		{"duplicate collapsed", "", "same/model", "same/model", []string{"same/model"}},
		{"whitespace entries dropped", "", "  ", " main/model ", []string{"main/model"}},
		{"nothing configured", "", "", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CommitMessageModelChain(tc.explicit, tc.small, tc.main)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("CommitMessageModelChain(%q, %q, %q) = %v, want %v", tc.explicit, tc.small, tc.main, got, tc.want)
			}
		})
	}
}

func TestResolveCommitMessageModelChain(t *testing.T) {
	t.Run("explicit model skips small-model probing", func(t *testing.T) {
		probes := 0
		prev := newClientFn
		newClientFn = func(_ *config.Config, _ string) LLMClient { probes++; return nil }
		t.Cleanup(func() { newClientFn = prev })

		cfg := commitMessageTestCfg("main/model", "small/model", "explicit/model")
		if got := ResolveCommitMessageModelChain(cfg); !slices.Equal(got, []string{"explicit/model"}) {
			t.Fatalf("chain = %v, want [explicit/model]", got)
		}
		if probes != 0 {
			t.Fatalf("resolved %d provider clients despite an explicit commit_msg_model", probes)
		}
	})

	t.Run("small then main", func(t *testing.T) {
		cfg := commitMessageTestCfg("main/model", "small/model", "")
		if got := ResolveCommitMessageModelChain(cfg); !slices.Equal(got, []string{"small/model", "main/model"}) {
			t.Fatalf("chain = %v, want [small/model main/model]", got)
		}
	})

	t.Run("main when no small model resolves", func(t *testing.T) {
		stubCommitMessageClients(t, func(string) LLMClient { return nil })
		cfg := commitMessageTestCfg("main/model", "", "")
		if got := ResolveCommitMessageModelChain(cfg); !slices.Equal(got, []string{"main/model"}) {
			t.Fatalf("chain = %v, want [main/model]", got)
		}
	})

	t.Run("nil config", func(t *testing.T) {
		if got := ResolveCommitMessageModelChain(nil); got != nil {
			t.Fatalf("chain = %v, want nil", got)
		}
	})
}

func TestGenerateCommitMessagePrefersSmallModel(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, reply: "  fix: small model wins\n", callLog: &calls}
	})

	res, err := GenerateCommitMessage(commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "fix: small model wins" || res.Model != "small/model" {
		t.Fatalf("result = %+v, want the trimmed small-model reply", res)
	}
	if !slices.Equal(calls, []string{"small/model"}) {
		t.Fatalf("calls = %v, want only the small model", calls)
	}
}

func TestGenerateCommitMessageFallsBackToMainModelOnError(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		if model == "small/model" {
			return &commitMessageStubClient{model: model, err: errors.New("rate limited"), callLog: &calls}
		}
		return &commitMessageStubClient{model: model, reply: "feat: fallback works", callLog: &calls}
	})

	res, err := GenerateCommitMessage(commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "feat: fallback works" || res.Model != "main/model" {
		t.Fatalf("result = %+v, want the main-model reply", res)
	}
	if !slices.Equal(calls, []string{"small/model", "main/model"}) {
		t.Fatalf("calls = %v, want small then main", calls)
	}
}

func TestGenerateCommitMessageFallsBackWhenSmallClientUnavailable(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		if model == "small/model" {
			return nil // provider not configured
		}
		return &commitMessageStubClient{model: model, reply: "chore: main only", callLog: &calls}
	})

	res, err := GenerateCommitMessage(commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "chore: main only" {
		t.Fatalf("text = %q, want the main-model reply", res.Text)
	}
	if !slices.Equal(calls, []string{"main/model"}) {
		t.Fatalf("calls = %v, want only the main model", calls)
	}
}

func TestGenerateCommitMessageFallsBackOnEmptyReply(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		if model == "small/model" {
			return &commitMessageStubClient{model: model, reply: "   \n", callLog: &calls}
		}
		return &commitMessageStubClient{model: model, reply: "docs: fallback", callLog: &calls}
	})

	res, err := GenerateCommitMessage(commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "docs: fallback" {
		t.Fatalf("text = %q, want the main-model reply", res.Text)
	}
	if !slices.Equal(calls, []string{"small/model", "main/model"}) {
		t.Fatalf("calls = %v, want small then main", calls)
	}
}

// An explicit commit_msg_model is the user's choice, so it must not silently
// run on a different model.
func TestGenerateCommitMessageExplicitModelDoesNotFallBack(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, err: errors.New("boom"), callLog: &calls}
	})

	res, err := GenerateCommitMessage(commitMessageTestCfg("main/model", "small/model", "explicit/model"), "", "diff")
	if err == nil {
		t.Fatalf("expected an error, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "explicit/model") {
		t.Fatalf("error = %v, want the failing model named", err)
	}
	if !slices.Equal(calls, []string{"explicit/model"}) {
		t.Fatalf("calls = %v, want only the explicit model", calls)
	}
}

// Usage/spend must describe the model that actually produced the message, not
// the one that failed first.
func TestGenerateCommitMessageReportsUsageFromFallbackModel(t *testing.T) {
	spend := 0.042
	stubCommitMessageClients(t, func(model string) LLMClient {
		if model == "small/model" {
			return &commitMessageStubClient{model: model, err: errors.New("rate limited")}
		}
		return &commitMessageStubClient{
			model: model,
			reply: "fix: usage",
			usage: &TokenUsage{PromptTokens: int64Ptr(11), CompletionTokens: int64Ptr(4)},
			spend: &spend,
		}
	})

	res, err := GenerateCommitMessage(commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Usage == nil || res.Usage.PromptTokens == nil || *res.Usage.PromptTokens != 11 {
		t.Fatalf("usage = %+v, want the fallback model's usage", res.Usage)
	}
	if res.Spend == nil || *res.Spend != spend {
		t.Fatalf("spend = %v, want %v", res.Spend, spend)
	}
}

func TestGenerateCommitMessageUsesProfile(t *testing.T) {
	var gotProfile string
	prev := newClientFn
	prevProfile := newClientWithProfileFn
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		t.Error("a non-empty profile must use the profile-aware factory")
		return nil
	}
	newClientWithProfileFn = func(_ *config.Config, model, profile string) LLMClient {
		gotProfile = profile
		return &commitMessageStubClient{model: model, reply: "fix: profile"}
	}
	t.Cleanup(func() {
		newClientFn = prev
		newClientWithProfileFn = prevProfile
	})

	if _, err := GenerateCommitMessage(commitMessageTestCfg("main/model", "small/model", ""), "work", "diff"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotProfile != "work" {
		t.Fatalf("profile = %q, want work", gotProfile)
	}
}

func TestGenerateCommitMessageCapsDiffAndUsesPrompt(t *testing.T) {
	var prompts []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, reply: "fix: prompt", prompts: &prompts}
	})

	cfg := commitMessageTestCfg("main/model", "small/model", "")
	cfg.Ocode.CommitMsgPrompt = "CUSTOM PROMPT:"
	if _, err := GenerateCommitMessage(cfg, "", strings.Repeat("x", commitMsgMaxDiffBytes+500)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "CUSTOM PROMPT:\n\n" + strings.Repeat("x", commitMsgMaxDiffBytes)
	if len(prompts) != 1 || prompts[0] != want {
		t.Fatalf("prompt = %q (len %d), want the custom prompt + the capped diff (len %d)", firstPrompt(prompts), len(firstPrompt(prompts)), len(want))
	}

	// Empty prompt falls back to the built-in default.
	prompts = nil
	cfg.Ocode.CommitMsgPrompt = ""
	if _, err := GenerateCommitMessage(cfg, "", "DIFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prompts) != 1 || prompts[0] != DefaultCommitMsgPrompt+"\n\nDIFF" {
		t.Fatalf("prompt = %q, want the default prompt", firstPrompt(prompts))
	}
}

func firstPrompt(prompts []string) string {
	if len(prompts) == 0 {
		return ""
	}
	return prompts[0]
}

func TestGenerateCommitMessageWithoutConfigOrModels(t *testing.T) {
	if _, err := GenerateCommitMessage(nil, "", "diff"); err == nil || !strings.Contains(err.Error(), "no LLM configured") {
		t.Fatalf("nil cfg error = %v, want no LLM configured", err)
	}

	stubCommitMessageClients(t, func(string) LLMClient { return nil })
	cfg := commitMessageTestCfg("", "", "")
	if _, err := GenerateCommitMessage(cfg, "", "diff"); err == nil || !strings.Contains(err.Error(), "no LLM configured") {
		t.Fatalf("no-model error = %v, want no LLM configured", err)
	}
}
