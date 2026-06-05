package agent

import (
	"encoding/json"

	"github.com/u007/ocode/internal/pricing"
)

type TokenUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens *int64 `json:"completion_tokens,omitempty"`
	TotalTokens      *int64 `json:"total_tokens,omitempty"`
	CacheReadTokens  *int64 `json:"cache_read_tokens,omitempty"`
}

func parseOpenAIUsage(raw json.RawMessage) (*TokenUsage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload struct {
		PromptTokens         *int64 `json:"prompt_tokens"`
		CompletionTokens     *int64 `json:"completion_tokens"`
		TotalTokens          *int64 `json:"total_tokens"`
		PromptCacheHitTokens *int64 `json:"prompt_cache_hit_tokens"`
		PromptTokensDetails  *struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	var cacheRead *int64
	if payload.PromptTokensDetails != nil && payload.PromptTokensDetails.CachedTokens != nil {
		cacheRead = payload.PromptTokensDetails.CachedTokens
	} else if payload.PromptCacheHitTokens != nil {
		cacheRead = payload.PromptCacheHitTokens
	}

	return &TokenUsage{
		PromptTokens:     payload.PromptTokens,
		CompletionTokens: payload.CompletionTokens,
		TotalTokens:      payload.TotalTokens,
		CacheReadTokens:  cacheRead,
	}, nil
}

func parseAnthropicUsage(raw json.RawMessage) (*TokenUsage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload struct {
		InputTokens          *int64 `json:"input_tokens"`
		OutputTokens         *int64 `json:"output_tokens"`
		CacheReadInputTokens *int64 `json:"cache_read_input_tokens"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	return &TokenUsage{
		PromptTokens:     payload.InputTokens,
		CompletionTokens: payload.OutputTokens,
		CacheReadTokens:  payload.CacheReadInputTokens,
	}, nil
}

// parseOpenAIResponsesUsage parses usage from the OpenAI Responses API,
// which uses input_tokens/output_tokens instead of prompt_tokens/completion_tokens.
func parseOpenAIResponsesUsage(raw json.RawMessage) (*TokenUsage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var payload struct {
		InputTokens          *int64 `json:"input_tokens"`
		OutputTokens         *int64 `json:"output_tokens"`
		TotalTokens          *int64 `json:"total_tokens"`
		PromptCacheHitTokens *int64 `json:"prompt_cache_hit_tokens"`
		PromptTokensDetails  *struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.InputTokens == nil && payload.OutputTokens == nil && payload.TotalTokens == nil {
		return nil, nil
	}
	var cacheRead *int64
	if payload.PromptTokensDetails != nil && payload.PromptTokensDetails.CachedTokens != nil {
		cacheRead = payload.PromptTokensDetails.CachedTokens
	} else if payload.PromptCacheHitTokens != nil {
		cacheRead = payload.PromptCacheHitTokens
	}
	return &TokenUsage{
		PromptTokens:     payload.InputTokens,
		CompletionTokens: payload.OutputTokens,
		TotalTokens:      payload.TotalTokens,
		CacheReadTokens:  cacheRead,
	}, nil
}

func usageForProvider(provider string, raw json.RawMessage) (*TokenUsage, error) {
	switch provider {
	case "anthropic":
		return parseAnthropicUsage(raw)
	default:
		var probe struct {
			InputTokens          *int64 `json:"input_tokens"`
			OutputTokens         *int64 `json:"output_tokens"`
			CacheReadInputTokens *int64 `json:"cache_read_input_tokens"`
		}
		if err := json.Unmarshal(raw, &probe); err == nil {
			if probe.InputTokens != nil || probe.OutputTokens != nil || probe.CacheReadInputTokens != nil {
				return parseAnthropicUsage(raw)
			}
		}
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

	// TODO(billing): apply reduced cache-read pricing. Anthropic bills cache
	// reads at 10% of input price and cache writes at 125%; OpenAI bills
	// cached input at 50%. CacheReadTokens is captured on TokenUsage but not
	// yet factored into Spend — current numbers overstate cost on
	// cache-heavy turns. Needs a CachedInputPerMillion field on
	// pricing.ModelPricing plus provider-aware subtraction from PromptTokens
	// before applying the input rate.
	spend := (float64(*u.PromptTokens) * modelPricing.InputPerMillion / 1_000_000) +
		(float64(*u.CompletionTokens) * modelPricing.OutputPerMillion / 1_000_000)
	return &spend
}
