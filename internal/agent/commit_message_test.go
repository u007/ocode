package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/u007/ocode/internal/config"
)

// commitMessageStubClient is a minimal LLMClient for commit-message generation
// tests. Every call records its model and prompt.
type commitMessageStubClient struct {
	model   string
	reply   string
	err     error
	usage   *TokenUsage
	spend   *float64
	block   bool // ChatWithContext waits for ctx to end instead of replying
	callLog *[]string
	prompts *[]string
}

func (c *commitMessageStubClient) Chat(msgs []Message, tools []map[string]interface{}) (*Message, error) {
	return c.ChatWithContext(context.Background(), msgs, tools)
}

func (c *commitMessageStubClient) ChatWithContext(ctx context.Context, msgs []Message, _ []map[string]interface{}) (*Message, error) {
	if c.callLog != nil {
		*c.callLog = append(*c.callLog, c.model)
	}
	if c.prompts != nil && len(msgs) > 0 {
		*c.prompts = append(*c.prompts, msgs[0].Content)
	}
	if c.block {
		<-ctx.Done()
		return nil, ctx.Err()
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

func TestCommitMessageModel(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		small    string
		main     string
		want     string
	}{
		{"explicit model is authoritative", "explicit/model", "small/model", "main/model", "explicit/model"},
		{"small model before main", "", "small/model", "main/model", "small/model"},
		{"main model when no small model", "", "", "main/model", "main/model"},
		{"blank entries skipped", "  ", "", " main/model ", "main/model"},
		{"nothing configured", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CommitMessageModel(tc.explicit, tc.small, tc.main); got != tc.want {
				t.Fatalf("CommitMessageModel(%q, %q, %q) = %q, want %q", tc.explicit, tc.small, tc.main, got, tc.want)
			}
		})
	}
}

func TestResolveCommitMessageModel(t *testing.T) {
	t.Run("explicit model skips small-model probing", func(t *testing.T) {
		probes := 0
		prev := newClientFn
		newClientFn = func(_ *config.Config, _ string) LLMClient { probes++; return nil }
		t.Cleanup(func() { newClientFn = prev })

		cfg := commitMessageTestCfg("main/model", "", "explicit/model")
		if got := ResolveCommitMessageModel(cfg, ""); got != "explicit/model" {
			t.Fatalf("model = %q, want explicit/model", got)
		}
		if probes != 0 {
			t.Fatalf("resolved %d provider clients despite an explicit commit_msg_model", probes)
		}
	})

	t.Run("configured small model wins without probing", func(t *testing.T) {
		stubCommitMessageClients(t, func(string) LLMClient { t.Fatal("probed a client"); return nil })
		cfg := commitMessageTestCfg("main/model", "small/model", "")
		if got := ResolveCommitMessageModel(cfg, ""); got != "small/model" {
			t.Fatalf("model = %q, want small/model", got)
		}
	})

	t.Run("main when no small model resolves", func(t *testing.T) {
		stubCommitMessageClients(t, func(string) LLMClient { return nil })
		cfg := commitMessageTestCfg("main/model", "", "")
		if got := ResolveCommitMessageModel(cfg, ""); got != "main/model" {
			t.Fatalf("model = %q, want main/model", got)
		}
	})

	t.Run("probe uses the profile", func(t *testing.T) {
		var gotProfiles []string
		prev := newClientFn
		prevProfile := newClientWithProfileFn
		newClientFn = func(_ *config.Config, _ string) LLMClient {
			t.Error("a non-empty profile must probe through the profile-aware factory")
			return nil
		}
		newClientWithProfileFn = func(_ *config.Config, model, profile string) LLMClient {
			gotProfiles = append(gotProfiles, profile)
			return &commitMessageStubClient{model: model}
		}
		t.Cleanup(func() {
			newClientFn = prev
			newClientWithProfileFn = prevProfile
		})

		cfg := commitMessageTestCfg("main/model", "", "")
		if got := ResolveCommitMessageModel(cfg, "work"); got != SmallModelPriority[0] {
			t.Fatalf("model = %q, want first priority candidate %q", got, SmallModelPriority[0])
		}
		if !slices.Equal(gotProfiles, []string{"work"}) {
			t.Fatalf("probed profiles = %v, want [work]", gotProfiles)
		}
	})

	t.Run("nil config", func(t *testing.T) {
		if got := ResolveCommitMessageModel(nil, ""); got != "" {
			t.Fatalf("model = %q, want empty", got)
		}
	})
}

func TestGenerateCommitMessagePrefersSmallModel(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, reply: "  fix: small model wins\n", callLog: &calls}
	})

	res, err := GenerateCommitMessage(context.Background(), commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
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

// A failing small model is the end of the attempt: the main model must not be
// tried in its place, and the error must name the model that failed.
func TestGenerateCommitMessageSmallModelErrorDoesNotFallBack(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, err: errors.New("rate limited"), callLog: &calls}
	})

	res, err := GenerateCommitMessage(context.Background(), commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if err == nil {
		t.Fatalf("expected an error, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "small/model") || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error = %v, want the failing model and its error", err)
	}
	if !slices.Equal(calls, []string{"small/model"}) {
		t.Fatalf("calls = %v, want only the small model", calls)
	}
}

func TestGenerateCommitMessageSmallClientUnavailableIsNotConfigured(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		if model == "small/model" {
			return nil // provider not configured
		}
		return &commitMessageStubClient{model: model, reply: "chore: main only", callLog: &calls}
	})

	res, err := GenerateCommitMessage(context.Background(), commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if !errors.Is(err, ErrCommitMessageNotConfigured) {
		t.Fatalf("error = %v (result %+v), want ErrCommitMessageNotConfigured", err, res)
	}
	if len(calls) != 0 {
		t.Fatalf("calls = %v, want none: the main model must not stand in", calls)
	}
}

func TestGenerateCommitMessageEmptyReplyIsAnError(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, reply: "   \n", callLog: &calls}
	})

	res, err := GenerateCommitMessage(context.Background(), commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if err == nil || !strings.Contains(err.Error(), "empty commit message") {
		t.Fatalf("result %+v, error %v, want an empty-message error", res, err)
	}
	if !slices.Equal(calls, []string{"small/model"}) {
		t.Fatalf("calls = %v, want only the small model", calls)
	}
}

// An explicit commit_msg_model is the user's choice, so it must not silently
// run on a different model.
func TestGenerateCommitMessageExplicitModelDoesNotFallBack(t *testing.T) {
	var calls []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, err: errors.New("boom"), callLog: &calls}
	})

	res, err := GenerateCommitMessage(context.Background(), commitMessageTestCfg("main/model", "small/model", "explicit/model"), "", "diff")
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

func TestGenerateCommitMessageReportsUsageAndSpend(t *testing.T) {
	spend := 0.042
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{
			model: model,
			reply: "fix: usage",
			usage: &TokenUsage{PromptTokens: int64Ptr(11), CompletionTokens: int64Ptr(4)},
			spend: &spend,
		}
	})

	res, err := GenerateCommitMessage(context.Background(), commitMessageTestCfg("main/model", "small/model", ""), "", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Usage == nil || res.Usage.PromptTokens == nil || *res.Usage.PromptTokens != 11 {
		t.Fatalf("usage = %+v, want the generating model's usage", res.Usage)
	}
	if res.Spend == nil || *res.Spend != spend {
		t.Fatalf("spend = %v, want %v", res.Spend, spend)
	}
}

// The deadline must reach the provider call: a stalled call returns once ctx
// ends, instead of blocking the generator forever.
func TestGenerateCommitMessageHonoursContextDeadline(t *testing.T) {
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, block: true}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := GenerateCommitMessage(ctx, commitMessageTestCfg("main/model", "", ""), "", "diff")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

// A client that cannot honour ctx is refused, not called past the deadline.
func TestGenerateCommitMessageRefusesClientWithoutContext(t *testing.T) {
	calls := 0
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &plainChatClient{model: model, calls: &calls}
	})

	_, err := GenerateCommitMessage(context.Background(), commitMessageTestCfg("main/model", "", ""), "", "diff")
	if err == nil || !strings.Contains(err.Error(), "cannot honour a request deadline") {
		t.Fatalf("error = %v, want the no-deadline refusal", err)
	}
	if calls != 0 {
		t.Fatalf("Chat was called %d times on a client that cannot honour ctx", calls)
	}
}

// plainChatClient offers only Chat, like a decision-only client.
type plainChatClient struct {
	model string
	calls *int
}

func (c *plainChatClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	*c.calls++
	return &Message{Content: "fix: plain"}, nil
}
func (c *plainChatClient) GetProvider() string { return "stub" }
func (c *plainChatClient) GetModel() string    { return c.model }

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

	if _, err := GenerateCommitMessage(context.Background(), commitMessageTestCfg("main/model", "small/model", ""), "work", "diff"); err != nil {
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
	if _, err := GenerateCommitMessage(context.Background(), cfg, "", strings.Repeat("x", commitMsgMaxDiffBytes+500)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "CUSTOM PROMPT:\n\n" + strings.Repeat("x", commitMsgMaxDiffBytes)
	if len(prompts) != 1 || prompts[0] != want {
		t.Fatalf("prompt = %q (len %d), want the custom prompt + the capped diff (len %d)", firstPrompt(prompts), len(firstPrompt(prompts)), len(want))
	}

	// Empty prompt falls back to the built-in default.
	prompts = nil
	cfg.Ocode.CommitMsgPrompt = ""
	if _, err := GenerateCommitMessage(context.Background(), cfg, "", "DIFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prompts) != 1 || prompts[0] != DefaultCommitMsgPrompt+"\n\nDIFF" {
		t.Fatalf("prompt = %q, want the default prompt", firstPrompt(prompts))
	}
}

// TestDefaultCommitMsgPromptAsksForABody pins the SHAPE the built-in prompt
// requests, because nothing downstream can recover it: both surfaces render
// whatever the model returns verbatim, so reverting to a subject-only prompt
// silently takes the body away from every user who has no custom prompt.
func TestDefaultCommitMsgPromptAsksForABody(t *testing.T) {
	prompt := DefaultCommitMsgPrompt
	for _, want := range []string{"subject", "blank line", "bullets"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default prompt no longer asks for %q: %q", want, prompt)
		}
	}
	if strings.Contains(strings.ToLower(prompt), "subject line only") {
		t.Fatalf("default prompt still restricts the reply to a subject line: %q", prompt)
	}
}

func TestCapDiffBytesKeepsRuneBoundary(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"short input unchanged", "abc", 10, "abc"},
		{"two-byte rune at the cut is dropped", "éé", 3, "é"},
		{"four-byte rune at the cut is dropped", "ab😀cd", 3, "ab"},
		{"four-byte rune ending exactly at the cut is kept", "ab😀cd", 6, "ab😀"},
		{"cut after a lead byte run of two-byte runes", "a" + strings.Repeat("é", 5000), commitMsgMaxDiffBytes, "a" + strings.Repeat("é", 3999)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := capDiffBytes(tc.in, tc.limit)
			if got != tc.want {
				t.Fatalf("capDiffBytes(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("capDiffBytes produced invalid UTF-8: %q", got)
			}
		})
	}

	// Non-UTF-8 input is backed up at most utf8.UTFMax-1 bytes, never to empty.
	bad := strings.Repeat("\x80", 100)
	got := capDiffBytes(bad, 10)
	if len(got) < 10-(utf8.UTFMax-1) || len(got) > 10 {
		t.Fatalf("capDiffBytes(non-UTF-8, 10) length = %d, want within [7, 10]", len(got))
	}
}

func TestGenerateCommitMessageCapsOnRuneBoundary(t *testing.T) {
	var prompts []string
	stubCommitMessageClients(t, func(model string) LLMClient {
		return &commitMessageStubClient{model: model, reply: "fix: runes", prompts: &prompts}
	})

	cfg := commitMessageTestCfg("main/model", "", "")
	cfg.Ocode.CommitMsgPrompt = "P:"
	if _, err := GenerateCommitMessage(context.Background(), cfg, "", strings.Repeat("é", 5000)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prompts) != 1 || !utf8.ValidString(prompts[0]) {
		t.Fatalf("prompt sent to the model is not valid UTF-8: %q", firstPrompt(prompts))
	}
	if got := len(prompts[0]) - len("P:\n\n"); got != commitMsgMaxDiffBytes {
		t.Fatalf("diff bytes sent = %d, want %d", got, commitMsgMaxDiffBytes)
	}
}

func firstPrompt(prompts []string) string {
	if len(prompts) == 0 {
		return ""
	}
	return prompts[0]
}

func TestGenerateCommitMessageWithoutConfigOrModels(t *testing.T) {
	if _, err := GenerateCommitMessage(context.Background(), nil, "", "diff"); !errors.Is(err, ErrCommitMessageNotConfigured) {
		t.Fatalf("nil cfg error = %v, want ErrCommitMessageNotConfigured", err)
	}

	stubCommitMessageClients(t, func(string) LLMClient { return nil })
	cfg := commitMessageTestCfg("", "", "")
	if _, err := GenerateCommitMessage(context.Background(), cfg, "", "diff"); !errors.Is(err, ErrCommitMessageNotConfigured) {
		t.Fatalf("no-model error = %v, want ErrCommitMessageNotConfigured", err)
	}
}
