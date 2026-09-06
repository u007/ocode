package agent

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/pricing"
)

// runinfraSSE is a canned streamed /chat/completions response whose usage
// frame mirrors runinfra's real shape: prompt_tokens_details.cached_tokens,
// prompt_tokens_details.created_cache_tokens and the runinfra cost object.
const runinfraSSE = "data: {\"id\":\"cmpl-probe\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n" +
	"data: {\"id\":\"cmpl-probe\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n" +
	"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7962,\"completion_tokens\":182,\"total_tokens\":8144,\"prompt_tokens_details\":{\"cached_tokens\":4352,\"created_cache_tokens\":2176},\"cost\":0.0001529,\"runinfra\":{\"cost_microcents\":15290,\"cached_input_tokens\":4352}}}\n\n" +
	"data: [DONE]\n\n"

// newStreamCaptureServer returns an httptest server that records the raw
// request body and replies with runinfraSSE as a text/event-stream.
func newStreamCaptureServer(t *testing.T) (capturedBody func() []byte, url string) {
	t.Helper()
	var lastBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		lastBody = buf
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(runinfraSSE))
	}))
	t.Cleanup(srv.Close)
	return func() []byte { return lastBody }, srv.URL
}

func TestProviderNeedsStreamUsageOptIn(t *testing.T) {
	if !providerNeedsStreamUsageOptIn("runinfra") {
		t.Fatal("runinfra must be in the stream usage opt-in allowlist")
	}
	for _, p := range []string{"openai", "deepseek", "groq", "openrouter", "novita-ai", "mistral", "local", "lmstudio", ""} {
		if providerNeedsStreamUsageOptIn(p) {
			t.Fatalf("provider %q must NOT be in the stream usage opt-in allowlist", p)
		}
	}
}

// TestRuninfraStreamUsageOptInAndCachedUsage pins the runinfra contract:
// the streamed request carries stream_options.include_usage, the request
// never carries the runinfra-rejected prompt_cache_* fields, and the usage
// frame's cached/created tokens land in TokenUsage.
func TestRuninfraStreamUsageOptInAndCachedUsage(t *testing.T) {
	captured, url := newStreamCaptureServer(t)
	gc := &GenericClient{Provider: "runinfra", Model: "nvidia/NVIDIA-Nemotron-3.5-Lightning-30B-A3B-BF16", BaseURL: url}
	msgs := []Message{
		{Role: "system", Content: "You are a terse probe."},
		{Role: "user", Content: "Reply with one word: ok"},
	}
	out, err := gc.Chat(msgs, nil)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if strings.TrimSpace(out.Content) != "ok" {
		t.Fatalf("unexpected content %q", out.Content)
	}

	body := captured()
	if len(body) == 0 {
		t.Fatal("no request body captured")
	}
	var payload struct {
		StreamOptions *struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if payload.StreamOptions == nil || !payload.StreamOptions.IncludeUsage {
		t.Fatalf("runinfra request missing stream_options.include_usage: %s", body)
	}
	// runinfra rejects prompt_cache_key / prompt_cache_options /
	// prompt_cache_retention with 400 hosted_parameter_not_supported —
	// ocode must never send them.
	for _, forbidden := range []string{"prompt_cache_key", "prompt_cache_options", "prompt_cache_retention"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("request body contains forbidden field %q: %s", forbidden, body)
		}
	}

	if out.Usage == nil {
		t.Fatal("usage frame not parsed from stream")
	}
	if out.Usage.PromptTokens == nil || *out.Usage.PromptTokens != 7962 {
		t.Fatalf("prompt tokens = %v, want 7962", out.Usage.PromptTokens)
	}
	if out.Usage.CompletionTokens == nil || *out.Usage.CompletionTokens != 182 {
		t.Fatalf("completion tokens = %v, want 182", out.Usage.CompletionTokens)
	}
	if out.Usage.CacheReadTokens == nil || *out.Usage.CacheReadTokens != 4352 {
		t.Fatalf("cache read tokens = %v, want 4352", out.Usage.CacheReadTokens)
	}
	if out.Usage.CacheWriteTokens == nil || *out.Usage.CacheWriteTokens != 2176 {
		t.Fatalf("cache write tokens = %v, want 2176", out.Usage.CacheWriteTokens)
	}
	if !out.Usage.PromptIncludesCacheRead {
		t.Fatal("OpenAI-style usage must set PromptIncludesCacheRead")
	}
}

// TestOtherProvidersDoNotSendStreamOptions ensures the opt-in is scoped to
// the verified allowlist — other OpenAI-compatible providers must keep their
// exact previous payload shape.
func TestOtherProvidersDoNotSendStreamOptions(t *testing.T) {
	for _, provider := range []string{"deepseek", "openai", "groq"} {
		captured, url := newStreamCaptureServer(t)
		gc := &GenericClient{Provider: provider, Model: "test-model", BaseURL: url}
		if _, err := gc.Chat([]Message{{Role: "user", Content: "hi"}}, nil); err != nil {
			t.Fatalf("[%s] Chat failed: %v", provider, err)
		}
		if strings.Contains(string(captured()), "stream_options") {
			t.Fatalf("[%s] request body must not contain stream_options", provider)
		}
	}
}

// TestSpendCacheWriteNoDoubleCount pins the billing rule: for OpenAI-style
// payloads (PromptIncludesCacheRead) the cache-write tokens (runinfra's
// created_cache_tokens) are inside prompt_tokens and bill at the plain input
// rate, so no cache-write charge is added; Anthropic-style payloads, whose
// cache-creation tokens sit outside input_tokens, do get the charge.
func TestSpendCacheWriteNoDoubleCount(t *testing.T) {
	p := pricing.ModelPricing{
		InputPerMillion:      0.05,
		OutputPerMillion:     0.15,
		CacheReadPerMillion:  0.01,
		CacheWritePerMillion: 0.05,
	}
	// OpenAI-style: prompt 7962 includes 4352 cached + 2176 created.
	openAIStyle := &TokenUsage{
		PromptTokens:            ptrInt64(7962),
		CompletionTokens:        ptrInt64(182),
		CacheReadTokens:         ptrInt64(4352),
		CacheWriteTokens:        ptrInt64(2176),
		PromptIncludesCacheRead: true,
	}
	got := openAIStyle.SpendWithPricing(p)
	want := (7962-4352)*0.05/1e6 + 182*0.15/1e6 + 4352*0.01/1e6
	if got == nil || math.Abs(*got-want) > 1e-12 {
		t.Fatalf("openai-style spend = %v, want %v (no cache-write charge)", got, want)
	}
	// Anthropic-style: input 7962 excludes cache tokens; created billed at
	// the dedicated cache-write rate.
	anthropicStyle := &TokenUsage{
		PromptTokens:            ptrInt64(7962),
		CompletionTokens:        ptrInt64(182),
		CacheReadTokens:         ptrInt64(4352),
		CacheWriteTokens:        ptrInt64(2176),
		PromptIncludesCacheRead: false,
	}
	got = anthropicStyle.SpendWithPricing(p)
	want = 7962*0.05/1e6 + 182*0.15/1e6 + 4352*0.01/1e6 + 2176*0.05/1e6
	if got == nil || math.Abs(*got-want) > 1e-12 {
		t.Fatalf("anthropic-style spend = %v, want %v (cache-write charge included)", got, want)
	}
}

// TestParseOpenAIUsageNilPromptTokensDetails guards the created_cache_tokens
// mapping against payloads without prompt_tokens_details (the common
// OpenAI/DeepSeek shape) — it must not nil-panic.
func TestParseOpenAIUsageNilPromptTokensDetails(t *testing.T) {
	u, err := parseOpenAIUsage([]byte(`{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110}`))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if u == nil || u.CacheReadTokens != nil || u.CacheWriteTokens != nil {
		t.Fatalf("unexpected usage %+v", u)
	}
	if u.PromptTokens == nil || *u.PromptTokens != 100 {
		t.Fatalf("prompt tokens = %v, want 100", u.PromptTokens)
	}
}

func ptrInt64(v int64) *int64 { return &v }

// TestNewClientRuninfraParsing pins client construction through the REAL
// parse path (NewClient, not a bare struct) for both spellings of the
// provider: the registered id "runinfra/..." and the domain-spelled alias
// "runinfra.ai/..." from the user-facing model string
// runinfra.ai/nvidia/NVIDIA-Nemotron-3.5-Lightning-30B-A3B-BF16. Both must
// resolve to the canonical provider id so credentials, base URL and the
// stream usage opt-in apply uniformly.
func TestNewClientRuninfraParsing(t *testing.T) {
	t.Setenv("RUNINFRA_GATEWAY_KEY", "test-key")
	const modelID = "nvidia/NVIDIA-Nemotron-3.5-Lightning-30B-A3B-BF16"
	for _, tc := range []struct {
		name  string
		model string
	}{
		{"canonical id", "runinfra/" + modelID},
		{"domain alias", "runinfra.ai/" + modelID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl := NewClient(nil, tc.model)
			if cl == nil {
				t.Fatalf("NewClient(%q) returned nil", tc.model)
			}
			gc := cl.(*GenericClient)
			if gc.GetProvider() != "runinfra" {
				t.Fatalf("provider = %q, want canonical %q", gc.GetProvider(), "runinfra")
			}
			if gc.GetModel() != modelID {
				t.Fatalf("model = %q, want %q", gc.GetModel(), modelID)
			}
			if gc.BaseURL != "https://api.runinfra.ai/v1" {
				t.Fatalf("baseURL = %q", gc.BaseURL)
			}
			if !providerNeedsStreamUsageOptIn(gc.GetProvider()) {
				t.Fatal("parsed runinfra client must carry the stream usage opt-in")
			}
		})
	}
}

// TestContextAgentRoutingResolvesRuninfraClient pins the subagent routing
// chain end-to-end at the unit level: subagent.go calls
// injectPurposeModelIfEligible, which must inject the configured context
// model for the context/doc-sync agents, and NewClient on the injected
// string must build the runinfra client. Explicit spec models still win, and
// the small model must not override the purpose model.
func TestContextAgentRoutingResolvesRuninfraClient(t *testing.T) {
	t.Setenv("RUNINFRA_GATEWAY_KEY", "test-key")
	cfg := &config.Config{}
	cfg.Ocode.ContextModel = "runinfra/nvidia/NVIDIA-Nemotron-3.5-Lightning-30B-A3B-BF16"
	cfg.Ocode.ContextModelEnabled = true
	cfg.Ocode.SmallModel = "opencode-go/small-model"
	cfg.Ocode.SmallModelEnabled = true

	for _, name := range []string{"context", "doc-sync"} {
		spec := &AgentSpec{Name: name}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != cfg.Ocode.ContextModel {
			t.Fatalf("[%s] purpose model not injected: got %q want %q", name, spec.Model, cfg.Ocode.ContextModel)
		}
		cl := NewClient(cfg, spec.Model)
		if cl == nil {
			t.Fatalf("[%s] NewClient(%q) returned nil", name, spec.Model)
		}
		if gc := cl.(*GenericClient); gc.GetProvider() != "runinfra" {
			t.Fatalf("[%s] resolved provider = %q, want runinfra", name, gc.GetProvider())
		}
	}

	// Explicit model on the spec wins over the purpose model.
	explicit := &AgentSpec{Name: "context", Model: "openai/gpt-4o"}
	injectPurposeModelIfEligible(nil, explicit, cfg)
	if explicit.Model != "openai/gpt-4o" {
		t.Fatalf("explicit spec model overridden: %q", explicit.Model)
	}
}
