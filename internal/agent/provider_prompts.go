package agent

import (
	_ "embed"
	"strings"
)

//go:embed prompts/claude.txt
var claudePromptText string

//go:embed prompts/gpt.txt
var gptPromptText string

//go:embed prompts/reasoning.txt
var reasoningPromptText string

//go:embed prompts/gemini.txt
var geminiPromptText string

//go:embed prompts/copilot.txt
var copilotPromptText string

//go:embed prompts/kimi.txt
var kimiPromptText string

//go:embed prompts/deepseek.txt
var deepseekPromptText string

//go:embed prompts/small_model.txt
var smallModelPromptText string

// modelFamilyPrompt returns a short tuning fragment for the given
// provider/model pair. Model-ID routing wins over provider routing so that
// reasoning models and codex variants land in their own buckets even when
// served through the generic "openai" provider. Returns "" for unknown
// combinations so we stay silent rather than hallucinating guidance.
//
// The fragments are intentionally small (a handful of bullets). They
// complement, not replace, the agent/mode prompt.
func modelFamilyPrompt(provider, model string) string {
	var basePrompt string
	p := strings.ToLower(strings.TrimSpace(provider))
	m := strings.ToLower(strings.TrimSpace(model))

	// Reasoning models — check first so they win over generic gpt/claude.
	if isReasoningModel(m) {
		basePrompt = reasoningPromptText
	} else {
		// Model-ID hints.
		switch {
		case strings.Contains(m, "claude"):
			basePrompt = claudePromptText
		case strings.Contains(m, "gemini"):
			basePrompt = geminiPromptText
		case strings.Contains(m, "kimi"):
			basePrompt = kimiPromptText
		case strings.Contains(m, "deepseek"):
			basePrompt = deepseekPromptText
		case strings.Contains(m, "gpt"):
			basePrompt = gptPromptText
		default:
			// Provider fallback.
			switch p {
			case "anthropic", "claude":
				basePrompt = claudePromptText
			case "openai", "gpt", "azure":
				basePrompt = gptPromptText
			case "google", "gemini", "vertex":
				basePrompt = geminiPromptText
			case "copilot", "github":
				basePrompt = copilotPromptText
			case "moonshot", "kimi":
				basePrompt = kimiPromptText
			case "deepseek":
				basePrompt = deepseekPromptText
			}
		}
	}

	// For small/cheap models, enhance with intent analysis guidance.
	// Note: appending smallModelPromptText changes the system-prompt content
	// for the same provider/model, which means switching a session between
	// a small model and a "big" sibling invalidates the model's prefix cache
	// at the promptProviderMarker boundary. This is acceptable: small models
	// have cheap re-inserts, and the small-model guidance is the whole point
	// of this branch. If a future change starts branching on per-request
	// state (e.g. tool calls, time of day), it MUST stay outside this
	// function to preserve the cache-stability contract documented in
	// internal/agent/append_stable.go.
	if isSmallModel(model) {
		if basePrompt != "" {
			return basePrompt + "\n\n" + smallModelPromptText
		}
		return smallModelPromptText
	}

	return basePrompt
}

// providerPrompt is kept for backwards compatibility with callers that only
// have a provider string. New code should call modelFamilyPrompt or use
// the Agent.ModelFamilyPrompt() method.
func providerPrompt(provider string) string {
	return modelFamilyPrompt(provider, "")
}

// ModelFamilyPrompt returns the provider/model tuning fragment that the
// agent would prepend to its system prompt for the current client. Returns
// "" if no client is configured or if the provider/model pair is unknown.
//
// This is the canonical way to read the fragment from outside the agent
// package (e.g. the TUI /context command). Callers that hold an *Agent
// should prefer this over the lower-level modelFamilyPrompt helper, which
// is unexported and can change signature.
func (a *Agent) ModelFamilyPrompt() string {
	if a == nil || a.client == nil {
		return ""
	}
	return modelFamilyPrompt(a.client.GetProvider(), a.client.GetModel())
}

func isReasoningModel(model string) bool {
	if model == "" {
		return false
	}
	// Match common reasoning-model identifiers across providers.
	// OpenAI: o1, o1-mini, o1-preview, o3, o3-mini, o4-mini.
	// Anthropic: *-thinking variants.
	// Generic: any model id containing "thinking".
	if strings.Contains(model, "thinking") {
		return true
	}
	// OpenAI o-series: bare "o1"/"o3"/"o4" optionally followed by '-' or end.
	for _, prefix := range []string{"o1", "o3", "o4"} {
		if model == prefix || strings.HasPrefix(model, prefix+"-") {
			return true
		}
	}
	return false
}

// isSmallModel reports whether the model identifier matches a small/cheap
// model from the SmallModelPriority list. These models benefit from enhanced
// intent analysis guidance.
//
// The input is expected to be the bare model name (e.g. "deepseek-v4-flash"),
// matching what LLMClient.GetModel() returns — NOT a "provider/model" string.
// The "no" cases in TestIsSmallModel lock this in: full "provider/model"
// strings deliberately do NOT match, so callers that still hold a
// "provider/model" string should split on the last "/" first.
func isSmallModel(model string) bool {
	if model == "" {
		return false
	}
	// Use the SmallModelPriority list as the authoritative source of small models.
	// The model string from GetModel() is just the model part (e.g., "deepseek-v4-flash"),
	// so we extract the model part from each SmallModelPriority entry for comparison.
	m := strings.ToLower(strings.TrimSpace(model))
	for _, small := range SmallModelPriority {
		// Extract just the model part after the last "/" (if present)
		smallModel := small
		if idx := strings.LastIndex(small, "/"); idx >= 0 {
			smallModel = small[idx+1:]
		}
		smallNorm := strings.ToLower(strings.TrimSpace(smallModel))
		if m == smallNorm {
			return true
		}
	}
	return false
}
