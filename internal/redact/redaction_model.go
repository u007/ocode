package redact

import (
	"strings"

	"github.com/u007/ocode/internal/config"
)

// NormalizeRedactionModelName returns the canonical persisted/display name for
// the tier-2 model. Bare LM Studio ids are normalized to lmstudio/<name> when
// the configured base_url points at the default LM Studio endpoint.
func NormalizeRedactionModelName(modelName, baseURL string) string {
	if modelName == "" {
		return ""
	}
	if strings.Contains(modelName, "/") {
		return modelName
	}
	switch strings.TrimRight(baseURL, "/") {
	case "http://localhost:1234/v1", "http://localhost:1234":
		return "lmstudio/" + modelName
	}
	return modelName
}

// DefaultRedactionBaseURL returns the default base URL for a model if its
// provider is a known local server (e.g. lmstudio → http://localhost:1234/v1).
// Returns "" when the provider has no static local default. The "local"
// provider's base URL is a per-model port derived from the registered
// local-model set (see discovery.AssignChatPort), which callers with access to
// that graph compute themselves; this helper therefore returns "" for "local".
func DefaultRedactionBaseURL(modelName string) string {
	if modelName == "" {
		return ""
	}
	provider, _ := config.SplitProviderModel(modelName)
	switch provider {
	case "lmstudio":
		return "http://localhost:1234/v1"
	}
	return ""
}

// ScannerRequestModelName returns the model id to send to the tier-2 scanner.
// LM Studio expects the bare model name, even when the persisted/display name
// is lmstudio/<name>.
func ScannerRequestModelName(modelName string) string {
	if strings.HasPrefix(modelName, "lmstudio/") {
		return strings.TrimPrefix(modelName, "lmstudio/")
	}
	return modelName
}

// NewScanner builds a tier-2 LLM scanner that calls an OpenAI-compatible local
// model server. Returns nil when no base URL or model is configured, or when
// the endpoint is not local (and allowRemote is false). The model argument
// must already be the scanner-request model id (after any provider rewrite);
// callers are responsible for provider-specific rewrites (e.g. LM Studio prefix
// stripping via ScannerRequestModelName, or /localmodel manifest rewriting).
func NewScanner(baseURL, model string, allowRemote bool, apiKey string) *LLMScanner {
	if baseURL == "" || model == "" {
		return nil
	}
	if !allowRemote && !IsLocalEndpoint(baseURL) {
		return nil
	}
	return &LLMScanner{BaseURL: baseURL, Model: model, AllowRemote: allowRemote, APIKey: apiKey}
}
