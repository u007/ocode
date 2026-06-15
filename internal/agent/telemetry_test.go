package agent

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/pricing"
)

func TestParseOpenAIUsage(t *testing.T) {
	usage, err := parseOpenAIUsage(json.RawMessage(`{"prompt_tokens":12,"completion_tokens":34,"total_tokens":46,"prompt_tokens_details":{"cached_tokens":8}}`))
	if err != nil {
		t.Fatalf("parseOpenAIUsage failed: %v", err)
	}

	if got := usage.PromptTokens; got == nil || *got != 12 {
		t.Fatalf("expected prompt tokens 12, got %#v", got)
	}
	if got := usage.CompletionTokens; got == nil || *got != 34 {
		t.Fatalf("expected completion tokens 34, got %#v", got)
	}
	if got := usage.TotalTokens; got == nil || *got != 46 {
		t.Fatalf("expected total tokens 46, got %#v", got)
	}
	if got := usage.CacheReadTokens; got == nil || *got != 8 {
		t.Fatalf("expected cached tokens 8, got %#v", got)
	}
	if !usage.PromptIncludesCacheRead {
		t.Fatalf("expected OpenAI usage to mark prompt tokens as cache-inclusive")
	}
}

func TestParseOpenAIUsageDeepSeekCacheHitTokens(t *testing.T) {
	usage, err := parseOpenAIUsage(json.RawMessage(`{"prompt_tokens":120,"completion_tokens":34,"total_tokens":154,"prompt_cache_hit_tokens":80}`))
	if err != nil {
		t.Fatalf("parseOpenAIUsage failed: %v", err)
	}

	if got := usage.CacheReadTokens; got == nil || *got != 80 {
		t.Fatalf("expected deepseek-style cached tokens 80, got %#v", got)
	}
	if !usage.PromptIncludesCacheRead {
		t.Fatalf("expected OpenAI usage to mark prompt tokens as cache-inclusive")
	}
}

func TestParseOpenAIResponsesUsage(t *testing.T) {
	usage, err := parseOpenAIResponsesUsage(json.RawMessage(`{"input_tokens":12,"output_tokens":34,"total_tokens":46,"prompt_tokens_details":{"cached_tokens":8}}`))
	if err != nil {
		t.Fatalf("parseOpenAIResponsesUsage failed: %v", err)
	}

	if got := usage.PromptTokens; got == nil || *got != 12 {
		t.Fatalf("expected input tokens 12, got %#v", got)
	}
	if got := usage.CacheReadTokens; got == nil || *got != 8 {
		t.Fatalf("expected cached tokens 8, got %#v", got)
	}
	if !usage.PromptIncludesCacheRead {
		t.Fatalf("expected OpenAI responses usage to mark prompt tokens as cache-inclusive")
	}
}

func TestParseAnthropicUsage(t *testing.T) {
	usage, err := parseAnthropicUsage(json.RawMessage(`{"input_tokens":7,"output_tokens":11,"cache_read_input_tokens":3}`))
	if err != nil {
		t.Fatalf("parseAnthropicUsage failed: %v", err)
	}

	if got := usage.PromptTokens; got == nil || *got != 7 {
		t.Fatalf("expected input tokens mapped to prompt tokens 7, got %#v", got)
	}
	if got := usage.CompletionTokens; got == nil || *got != 11 {
		t.Fatalf("expected output tokens mapped to completion tokens 11, got %#v", got)
	}
	if got := usage.CacheReadTokens; got == nil || *got != 3 {
		t.Fatalf("expected cached tokens 3, got %#v", got)
	}
	if usage.PromptIncludesCacheRead {
		t.Fatalf("expected anthropic usage input tokens to exclude cache reads")
	}
	if usage.TotalTokens != nil {
		t.Fatalf("expected total tokens to stay nil when missing, got %#v", usage.TotalTokens)
	}
}

func TestUsageForProviderAutoDetectsAnthropicShape(t *testing.T) {
	usage, err := usageForProvider("minimax", json.RawMessage(`{"input_tokens":7,"output_tokens":11,"cache_read_input_tokens":3}`))
	if err != nil {
		t.Fatalf("usageForProvider failed: %v", err)
	}

	if got := usage.PromptTokens; got == nil || *got != 7 {
		t.Fatalf("expected input tokens mapped to prompt tokens 7, got %#v", got)
	}
	if got := usage.CacheReadTokens; got == nil || *got != 3 {
		t.Fatalf("expected cached tokens 3, got %#v", got)
	}
	if usage.PromptIncludesCacheRead {
		t.Fatalf("expected anthropic-shaped usage to exclude cache reads from prompt tokens")
	}
}

// Pricing now comes from the models.dev registry (embedded snapshot) first,
// falling back to the hardcoded map only for models the registry doesn't know.
// gpt-4o is in the registry, so its current price (input 2.5 / output 10 per
// million) is used rather than the stale bundled 5/15.
func TestTokenUsageSpendUsesRegistryPricing(t *testing.T) {
	usage := &TokenUsage{
		PromptTokens:     int64Ptr(1000),
		CompletionTokens: int64Ptr(2000),
	}

	spend := usage.Spend("gpt-4o")
	if spend == nil {
		t.Fatal("expected spend, got nil")
	}

	// 1000*2.5/1e6 + 2000*10/1e6 = 0.0225
	if math.Abs(*spend-0.0225) > 1e-9 {
		t.Fatalf("expected spend 0.0225, got %v", *spend)
	}
}

// OpenAI/DeepSeek-style cache hits are included in prompt_tokens and must be
// billed only once: uncached prompt tokens at input rate, cache hits at cache
// read rate.
func TestTokenUsageSpendBillsCacheReadTokens(t *testing.T) {
	usage := &TokenUsage{
		PromptTokens:            int64Ptr(1_000_000),
		CompletionTokens:        int64Ptr(1_000_000),
		CacheReadTokens:         int64Ptr(1_000_000),
		PromptIncludesCacheRead: true,
	}

	spend := usage.SpendWithPricing(pricing.ModelPricing{
		InputPerMillion:     0.14,
		OutputPerMillion:    0.28,
		CacheReadPerMillion: 0.0028,
	})
	if spend == nil {
		t.Fatal("expected spend, got nil")
	}

	// (1.0 - 1.0) * 0.14 + 1.0 * 0.0028 + 1.0 * 0.28 = 0.2828
	if math.Abs(*spend-0.2828) > 1e-9 {
		t.Fatalf("expected spend 0.2828, got %v", *spend)
	}
}

// Cache-write (cache_creation) tokens are billed at the registry's cache_write
// rate, on top of input/output/cache_read. claude-sonnet-4-6: input 3, output
// 15, cache_read 0.3, cache_write 3.75 per million.
func TestTokenUsageSpendBillsCacheWriteTokens(t *testing.T) {
	usage := &TokenUsage{
		PromptTokens:            int64Ptr(1_000_000),
		CompletionTokens:        int64Ptr(1_000_000),
		CacheReadTokens:         int64Ptr(1_000_000),
		CacheWriteTokens:        int64Ptr(1_000_000),
		PromptIncludesCacheRead: false,
	}

	spend := usage.Spend("anthropic/claude-sonnet-4-6")
	if spend == nil {
		t.Fatal("expected spend, got nil")
	}

	// 3 + 15 + 0.3 + 3.75 = 22.05
	if math.Abs(*spend-22.05) > 1e-9 {
		t.Fatalf("expected spend 22.05, got %v", *spend)
	}
}

// Anthropic usage payloads carry cache_creation_input_tokens for cache writes.
func TestParseAnthropicUsageCacheWrite(t *testing.T) {
	u, err := parseAnthropicUsage(json.RawMessage(`{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":5,"cache_creation_input_tokens":7}`))
	if err != nil {
		t.Fatalf("parseAnthropicUsage failed: %v", err)
	}
	if u.CacheWriteTokens == nil || *u.CacheWriteTokens != 7 {
		t.Fatalf("expected cache write tokens 7, got %#v", u.CacheWriteTokens)
	}
}

func TestTokenUsageSpendUnknownModelReturnsNil(t *testing.T) {
	usage := &TokenUsage{
		PromptTokens:     int64Ptr(1000),
		CompletionTokens: int64Ptr(2000),
	}

	if spend := usage.Spend("unknown-model"); spend != nil {
		t.Fatalf("expected nil spend for unknown model, got %v", *spend)
	}
}

func TestAgentStepPreservesTelemetry(t *testing.T) {
	spend := 0.035
	mock := &MockClient{
		Response: &Message{
			Role:    "assistant",
			Content: "Hello!",
			Usage: &TokenUsage{
				PromptTokens:     int64Ptr(1000),
				CompletionTokens: int64Ptr(2000),
			},
			Spend: &spend,
		},
	}
	a := NewAgent(mock, nil, nil, nil)

	msgs, err := a.Step([]Message{{Role: "user", Content: "Hi"}})
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Usage == nil || msgs[0].Usage.PromptTokens == nil || *msgs[0].Usage.PromptTokens != 1000 {
		t.Fatalf("expected usage to survive Step, got %#v", msgs[0].Usage)
	}
	if msgs[0].Spend == nil || *msgs[0].Spend != 0.035 {
		t.Fatalf("expected spend to survive Step, got %#v", msgs[0].Spend)
	}
}

func TestTelemetryDoesNotMarshalIntoRequests(t *testing.T) {
	msg := Message{
		Role:    "assistant",
		Content: "Hello!",
		Model:   "gpt-4o",
		Usage: &TokenUsage{
			PromptTokens:     int64Ptr(1),
			CompletionTokens: int64Ptr(2),
		},
		Spend: float64Ptr(0.01),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	got := string(data)
	if strings.Contains(got, "usage") || strings.Contains(got, "spend") || strings.Contains(got, "model") {
		t.Fatalf("expected telemetry fields to stay out of request JSON, got %s", got)
	}
}

func TestPricingLookupIsExplicitForUnknownModels(t *testing.T) {
	if _, ok := pricing.Lookup("does-not-exist"); ok {
		t.Fatal("expected unknown model pricing lookup to return false")
	}
}

func int64Ptr(v int64) *int64       { return &v }
func float64Ptr(v float64) *float64 { return &v }
