package agent

import (
	"encoding/json"

	"github.com/jamesmercstudio/ocode/internal/pricing"
)

type TokenUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens *int64 `json:"completion_tokens,omitempty"`
	TotalTokens      *int64 `json:"total_tokens,omitempty"`
}

func parseOpenAIUsage(raw json.RawMessage) (*TokenUsage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var usage TokenUsage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, err
	}

	return &usage, nil
}

func parseAnthropicUsage(raw json.RawMessage) (*TokenUsage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload struct {
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	return &TokenUsage{
		PromptTokens:     payload.InputTokens,
		CompletionTokens: payload.OutputTokens,
	}, nil
}

func usageForProvider(provider string, raw json.RawMessage) (*TokenUsage, error) {
	switch provider {
	case "anthropic":
		return parseAnthropicUsage(raw)
	default:
		return parseOpenAIUsage(raw)
	}
}

func (u *TokenUsage) Spend(model string) *float64 {
	if u == nil {
		return nil
	}

	modelPricing, ok := pricing.Lookup(model)
	if !ok {
		return nil
	}

	return u.SpendWithPricing(modelPricing)
}

func (u *TokenUsage) SpendWithPricing(modelPricing pricing.ModelPricing) *float64 {
	if u == nil || u.PromptTokens == nil || u.CompletionTokens == nil {
		return nil
	}

	spend := (float64(*u.PromptTokens) * modelPricing.InputPerMillion / 1_000_000) +
		(float64(*u.CompletionTokens) * modelPricing.OutputPerMillion / 1_000_000)
	return &spend
}
