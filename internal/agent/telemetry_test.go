package agent

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/pricing"
)

func TestParseOpenAIUsage(t *testing.T) {
	usage, err := parseOpenAIUsage(json.RawMessage(`{"prompt_tokens":12,"completion_tokens":34,"total_tokens":46}`))
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
}

func TestParseAnthropicUsage(t *testing.T) {
	usage, err := parseAnthropicUsage(json.RawMessage(`{"input_tokens":7,"output_tokens":11}`))
	if err != nil {
		t.Fatalf("parseAnthropicUsage failed: %v", err)
	}

	if got := usage.PromptTokens; got == nil || *got != 7 {
		t.Fatalf("expected input tokens mapped to prompt tokens 7, got %#v", got)
	}
	if got := usage.CompletionTokens; got == nil || *got != 11 {
		t.Fatalf("expected output tokens mapped to completion tokens 11, got %#v", got)
	}
	if usage.TotalTokens != nil {
		t.Fatalf("expected total tokens to stay nil when missing, got %#v", usage.TotalTokens)
	}
}

func TestTokenUsageSpendUsesBundledPricing(t *testing.T) {
	usage := &TokenUsage{
		PromptTokens:     int64Ptr(1000),
		CompletionTokens: int64Ptr(2000),
	}

	spend := usage.Spend("gpt-4o")
	if spend == nil {
		t.Fatal("expected spend, got nil")
	}

	if math.Abs(*spend-0.035) > 1e-9 {
		t.Fatalf("expected spend 0.035, got %v", *spend)
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
	a := NewAgent(mock, nil, nil)

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
