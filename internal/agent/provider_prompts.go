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

// modelFamilyPrompt returns a short tuning fragment for the given
// provider/model pair. Model-ID routing wins over provider routing so that
// reasoning models and codex variants land in their own buckets even when
// served through the generic "openai" provider. Returns "" for unknown
// combinations so we stay silent rather than hallucinating guidance.
//
// The fragments are intentionally small (a handful of bullets). They
// complement, not replace, the agent/mode prompt.
func modelFamilyPrompt(provider, model string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	m := strings.ToLower(strings.TrimSpace(model))

	// Reasoning models — check first so they win over generic gpt/claude.
	if isReasoningModel(m) {
		return reasoningPromptText
	}

	// Model-ID hints.
	switch {
	case strings.Contains(m, "claude"):
		return claudePromptText
	case strings.Contains(m, "gemini"):
		return geminiPromptText
	case strings.Contains(m, "kimi"):
		return kimiPromptText
	case strings.Contains(m, "gpt"):
		return gptPromptText
	}

	// Provider fallback.
	switch p {
	case "anthropic", "claude":
		return claudePromptText
	case "openai", "gpt", "azure":
		return gptPromptText
	case "google", "gemini", "vertex":
		return geminiPromptText
	case "copilot", "github":
		return copilotPromptText
	case "moonshot", "kimi":
		return kimiPromptText
	}
	return ""
}

// providerPrompt is kept for backwards compatibility with callers that only
// have a provider string. New code should call modelFamilyPrompt.
func providerPrompt(provider string) string {
	return modelFamilyPrompt(provider, "")
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
