package auth

import (
	"os"
	"time"
)

// refreshIfExpiring returns a fresh credential if the stored OAuth token is
// expired or within the skew window. Falls back to the original credential
// on refresh failure (caller decides whether to surface the error).
func refreshIfExpiring(id string, cred Credential) Credential {
	if cred.Kind != KindOAuth {
		return cred
	}
	if cred.ExpiresAt == 0 || cred.RefreshToken == "" {
		return cred
	}
	const skew = 60 * time.Second
	if time.Until(time.Unix(cred.ExpiresAt, 0)) > skew {
		return cred
	}
	var refreshed Credential
	var err error
	switch id {
	case "anthropic":
		refreshed, err = AnthropicRefresh(cred.RefreshToken)
	case "openai":
		refreshed, err = OpenAIRefresh(cred.RefreshToken)
	default:
		return cred
	}
	if err != nil || refreshed.AccessToken == "" {
		return cred
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = cred.RefreshToken
	}
	refreshed.Account = cred.Account
	_ = Set(id, refreshed)
	return refreshed
}

// Provider describes a configurable LLM provider.
type Provider struct {
	ID        string // canonical id, e.g. "openai"
	Label     string // human-readable name
	EnvVar    string // env var that holds the API key (precedence over stored key)
	OAuthFlow string // "" if not supported; otherwise a flow id ("google", "openai", "anthropic", "copilot")
}

// Providers is the registry. Order = display order in the dialog.
var Providers = []Provider{
	{ID: "openai", Label: "OpenAI", EnvVar: "OPENAI_API_KEY", OAuthFlow: "openai"},
	{ID: "anthropic", Label: "Anthropic", EnvVar: "ANTHROPIC_API_KEY", OAuthFlow: "anthropic"},
	{ID: "google", Label: "Google", EnvVar: "GOOGLE_API_KEY", OAuthFlow: "google"},
	{ID: "copilot", Label: "GitHub Copilot", EnvVar: "GITHUB_COPILOT_TOKEN", OAuthFlow: "copilot"},
	{ID: "openrouter", Label: "OpenRouter", EnvVar: "OPENROUTER_API_KEY"},
	{ID: "zai", Label: "Z.AI", EnvVar: "ZAI_API_KEY"},
	{ID: "zai-coding", Label: "Z.AI Coding", EnvVar: "ZAI_CODING_API_KEY"},
	{ID: "moonshot", Label: "Moonshot", EnvVar: "MOONSHOT_API_KEY"},
	{ID: "minimax", Label: "MiniMax", EnvVar: "MINIMAX_API_KEY"},
	{ID: "alibaba", Label: "Alibaba (DashScope)", EnvVar: "DASHSCOPE_API_KEY"},
	{ID: "alibaba-coding", Label: "Alibaba Coding", EnvVar: "DASHSCOPE_CODING_API_KEY"},
	{ID: "chutes", Label: "Chutes", EnvVar: "CHUTES_API_KEY"},
}

// GetBaseURL returns the per-credential base URL override, or "" if none.
func GetBaseURL(id string) string {
	cred, ok := Get(id)
	if !ok {
		return ""
	}
	return cred.BaseURL
}

// OAuthAccessToken returns the (possibly refreshed) OAuth access token for a provider.
// Returns "", false if no OAuth credential is stored.
func OAuthAccessToken(id string) (string, bool) {
	cred, ok := Get(id)
	if !ok || cred.Kind != KindOAuth {
		return "", false
	}
	cred = refreshIfExpiring(id, cred)
	if cred.AccessToken == "" {
		return "", false
	}
	return cred.AccessToken, true
}

// FindProvider returns a provider by id (or nil).
func FindProvider(id string) *Provider {
	for i := range Providers {
		if Providers[i].ID == id {
			return &Providers[i]
		}
	}
	return nil
}

// ResolveKey returns the effective API key for a provider.
// Precedence: env var > stored credential > "".
func ResolveKey(id string) string {
	p := FindProvider(id)
	if p == nil {
		return ""
	}
	if p.EnvVar != "" {
		if v := os.Getenv(p.EnvVar); v != "" {
			return v
		}
	}
	cred, ok := Get(id)
	if !ok {
		return ""
	}
	cred = refreshIfExpiring(id, cred)
	if cred.Kind == KindAPIKey {
		return cred.Key
	}
	return ""
}

// HydrateEnv loads stored credentials into the process env so existing
// callers that read os.Getenv keep working. Env vars already set win.
func HydrateEnv() error {
	if err := LoadStore(); err != nil {
		return err
	}
	for _, p := range Providers {
		if p.EnvVar == "" {
			continue
		}
		if os.Getenv(p.EnvVar) != "" {
			continue
		}
		cred, ok := Get(p.ID)
		if !ok {
			continue
		}
		cred = refreshIfExpiring(p.ID, cred)
		switch cred.Kind {
		case KindAPIKey:
			if cred.Key != "" {
				_ = os.Setenv(p.EnvVar, cred.Key)
			}
		}
	}
	return nil
}

// Status returns a short symbol + label describing the credential state.
func Status(id string) (symbol string, detail string) {
	p := FindProvider(id)
	if p == nil {
		return "✗", "unknown"
	}
	if p.EnvVar != "" && os.Getenv(p.EnvVar) != "" {
		return "✓", "env"
	}
	cred, ok := Get(id)
	if !ok {
		return "✗", "not configured"
	}
	switch cred.Kind {
	case KindAPIKey:
		return "✓", "api key"
	case KindOAuth:
		if cred.Account != "" {
			return "✓", "oauth (" + cred.Account + ")"
		}
		return "✓", "oauth"
	}
	return "✗", "not configured"
}
