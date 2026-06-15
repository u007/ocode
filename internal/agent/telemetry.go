package agent

import (
	"encoding/json"
	"fmt"

	"github.com/u007/ocode/internal/pricing"
)

type TokenUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens *int64 `json:"completion_tokens,omitempty"`
	TotalTokens      *int64 `json:"total_tokens,omitempty"`
	CacheReadTokens  *int64 `json:"cache_read_tokens,omitempty"`
	// PromptIncludesCacheRead marks providers whose prompt/input token count
	// already includes cache-read tokens (e.g. OpenAI/DeepSeek-style usage
	// payloads). Anthropic-style payloads keep this false because their input
	// tokens exclude cache reads.
	PromptIncludesCacheRead bool `json:"-"`
	// CacheWriteTokens counts tokens written to the prompt cache (Anthropic's
	// cache_creation_input_tokens), billed at the model's cache_write rate.
	CacheWriteTokens *int64 `json:"cache_write_tokens,omitempty"`
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
		PromptTokens:            payload.PromptTokens,
		CompletionTokens:        payload.CompletionTokens,
		TotalTokens:             payload.TotalTokens,
		CacheReadTokens:         cacheRead,
		PromptIncludesCacheRead: true,
	}, nil
}

func parseAnthropicUsage(raw json.RawMessage) (*TokenUsage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload struct {
		InputTokens              *int64 `json:"input_tokens"`
		OutputTokens             *int64 `json:"output_tokens"`
		CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	return &TokenUsage{
		PromptTokens:            payload.InputTokens,
		CompletionTokens:        payload.OutputTokens,
		CacheReadTokens:         payload.CacheReadInputTokens,
		PromptIncludesCacheRead: false,
		CacheWriteTokens:        payload.CacheCreationInputTokens,
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
		PromptTokens:            payload.InputTokens,
		CompletionTokens:        payload.OutputTokens,
		TotalTokens:             payload.TotalTokens,
		CacheReadTokens:         cacheRead,
		PromptIncludesCacheRead: true,
	}, nil
}

func usageForProvider(provider string, raw json.RawMessage) (*TokenUsage, error) {
	switch provider {
	case "anthropic":
		return parseAnthropicUsage(raw)
	default:
		var probe struct {
			InputTokens              *int64 `json:"input_tokens"`
			OutputTokens             *int64 `json:"output_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
		}
		if err := json.Unmarshal(raw, &probe); err == nil {
			if probe.InputTokens != nil || probe.OutputTokens != nil || probe.CacheReadInputTokens != nil || probe.CacheCreationInputTokens != nil {
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

func (u *TokenUsage) DebugLog(model string) {
	if u == nil {
		return
	}
	prompt := int64(0)
	if u.PromptTokens != nil {
		prompt = *u.PromptTokens
	}
	completion := int64(0)
	if u.CompletionTokens != nil {
		completion = *u.CompletionTokens
	}
	cacheRead := int64(0)
	if u.CacheReadTokens != nil {
		cacheRead = *u.CacheReadTokens
	}
	cacheWrite := int64(0)
	if u.CacheWriteTokens != nil {
		cacheWrite = *u.CacheWriteTokens
	}
	emitDebug("TOKENS", fmt.Sprintf("model=%s input=%d cache_read=%d cache_write=%d output=%d",
		model, prompt, cacheRead, cacheWrite, completion))
}

func (u *TokenUsage) SpendWithPricing(modelPricing pricing.ModelPricing) *float64 {
	if u == nil || u.PromptTokens == nil || u.CompletionTokens == nil {
		return nil
	}

	// PromptTokens semantics vary by provider:
	// - OpenAI/DeepSeek-style payloads include cache-read tokens inside prompt
	//   tokens and expose the cache hit count separately.
	// - Anthropic-style payloads keep cache reads out of input_tokens.
	// Bill cached tokens exactly once by subtracting them from prompt tokens when
	// they are already included, otherwise add them at the dedicated cache-read
	// rate.
	promptTokens := float64(*u.PromptTokens)
	cacheReadTokens := float64(0)
	if u.CacheReadTokens != nil {
		cacheReadTokens = float64(*u.CacheReadTokens)
	}
	if u.PromptIncludesCacheRead && cacheReadTokens > 0 && modelPricing.CacheReadPerMillion > 0 {
		if promptTokens > cacheReadTokens {
			promptTokens -= cacheReadTokens
		} else {
			promptTokens = 0
		}
	}

	spend := (promptTokens * modelPricing.InputPerMillion / 1_000_000) +
		(float64(*u.CompletionTokens) * modelPricing.OutputPerMillion / 1_000_000)
	if cacheReadTokens > 0 && modelPricing.CacheReadPerMillion > 0 {
		spend += cacheReadTokens * modelPricing.CacheReadPerMillion / 1_000_000
	}
	if u.CacheWriteTokens != nil && modelPricing.CacheWritePerMillion > 0 {
		spend += float64(*u.CacheWriteTokens) * modelPricing.CacheWritePerMillion / 1_000_000
	}
	return &spend
}
