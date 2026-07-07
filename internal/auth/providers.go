package auth

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/config"
)

var (
	refreshMu  = map[string]*sync.Mutex{}
	refreshMuL sync.Mutex
)

func getRefreshMu(provider string) *sync.Mutex {
	refreshMuL.Lock()
	defer refreshMuL.Unlock()
	if m, ok := refreshMu[provider]; ok {
		return m
	}
	m := &sync.Mutex{}
	refreshMu[provider] = m
	return m
}

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

	mu := getRefreshMu(id)
	mu.Lock()
	defer mu.Unlock()

	// Re-check after acquiring lock — another goroutine may have refreshed.
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
	refreshed.AccountID = cred.AccountID
	if refreshed.AccountID == "" && cred.AccountID != "" {
		log.Printf("warn: %s token refresh lost AccountID (was %q)", id, cred.AccountID)
	}
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
	{ID: "opencode", Label: "OpenCode Zen", EnvVar: "OPENCODE_API_KEY"},
	{ID: "opencode-go", Label: "OpenCode Go", EnvVar: "OPENCODE_API_KEY"},
	{ID: "openrouter", Label: "OpenRouter", EnvVar: "OPENROUTER_API_KEY"},
	{ID: "zai", Label: "Z.AI", EnvVar: "ZAI_API_KEY"},
	{ID: "zai-coding", Label: "Z.AI Coding", EnvVar: "ZAI_CODING_API_KEY"},
	{ID: "moonshot", Label: "Moonshot", EnvVar: "MOONSHOT_API_KEY"},
	{ID: "minimax", Label: "MiniMax", EnvVar: "MINIMAX_API_KEY"},
	{ID: "alibaba", Label: "Alibaba (DashScope)", EnvVar: "DASHSCOPE_API_KEY"},
	{ID: "alibaba-coding", Label: "Alibaba Coding", EnvVar: "DASHSCOPE_CODING_API_KEY"},
	{ID: "chutes", Label: "Chutes", EnvVar: "CHUTES_API_KEY"},
	{ID: "deepseek", Label: "DeepSeek", EnvVar: "DEEPSEEK_API_KEY"},
	{ID: "novita-ai", Label: "Novita AI", EnvVar: "NOVITA_API_KEY"},
	{ID: "requesty", Label: "Requesty", EnvVar: "REQUESTY_API_KEY"},
	{ID: "deepinfra", Label: "DeepInfra", EnvVar: "DEEPINFRA_API_KEY"},
	{ID: "nvidia", Label: "NVIDIA NIM", EnvVar: "NVIDIA_API_KEY"},
	{ID: "lmstudio", Label: "LM Studio (local)"},
	{ID: "cloudflare-workers", Label: "Cloudflare Workers AI", EnvVar: "CLOUDFLARE_API_KEY"},
	{ID: "cloudflare-gateway", Label: "Cloudflare AI Gateway", EnvVar: "CLOUDFLARE_GATEWAY_KEY"},
	{ID: "codex", Label: "OpenAI Codex", EnvVar: "OPENAI_API_KEY", OAuthFlow: "openai"},
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

// resolveKeyWithConfig returns the effective API key for a provider given a
// pre-loaded config. Precedence: OPENCODE_AUTH_TOKEN > env var > config file
// options.apiKey > stored credential > "". Accepts a pre-loaded config to avoid
// repeated disk reads when called in a loop (e.g. HydrateEnv).
func resolveKeyWithConfig(p *Provider, id string, cfg *config.Config) string {
	// 0. OPENCODE_AUTH_TOKEN env var — highest priority override. When set,
	//    it takes precedence over all other resolution (env vars, config,
	//    stored credentials) for every provider.
	if v := os.Getenv("OPENCODE_AUTH_TOKEN"); v != "" {
		return v
	}
	// 1. Environment variable (highest priority — allows env to override config).
	if p.EnvVar != "" {
		if v := os.Getenv(p.EnvVar); v != "" {
			return v
		}
	}
	// 2. Config file provider.<id>.options.apiKey.
	if key := providerConfigAPIKey(cfg, id); key != "" {
		return key
	}
	// 3. Stored credential (auth store).
	cred, ok := Get(id)
	if ok {
		cred = refreshIfExpiring(id, cred)
		if cred.Kind == KindAPIKey && cred.Key != "" {
			return cred.Key
		}
	}
	return ""
}

// ResolveKey returns the effective API key for a provider.
// Precedence: OPENCODE_AUTH_TOKEN > env var > config file options.apiKey > stored credential > "".
func ResolveKey(id string) string {
	p := FindProvider(id)
	if p == nil {
		return ""
	}
	cfg, _ := config.Load()
	return resolveKeyWithConfig(p, id, cfg)
}

// providerConfigAPIKey extracts provider.<id>.options.apiKey from the opencode config.
// If the value is of the form `{env:VAR}` it resolves the env var.
func providerConfigAPIKey(cfg *config.Config, id string) string {
	if cfg == nil {
		return ""
	}
	raw, ok := cfg.Provider[id]
	if !ok {
		return ""
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	opts, ok := m["options"].(map[string]interface{})
	if !ok {
		return ""
	}
	apiKeyVal, ok := opts["apiKey"]
	if !ok {
		return ""
	}
	rawKey, ok := apiKeyVal.(string)
	if !ok {
		return ""
	}
	key := ResolveEnvVarRef(rawKey)
	if key == "" {
		if envName, ok := strings.CutPrefix(rawKey, "{env:"); ok {
			envName = strings.TrimSuffix(envName, "}")
			log.Printf("warn: config provider.%s.options.apiKey references env var %s which is not set", id, envName)
		}
	}
	return key
}

// ResolveEnvVarRef resolves a value of the form {env:VAR} to the value of
// environment variable VAR. If the value doesn't match the pattern, it's
// returned as-is. If the env var is not set, an empty string is returned.
func ResolveEnvVarRef(value string) string {
	if strings.HasPrefix(value, "{env:") && strings.HasSuffix(value, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(value, "{env:"), "}")
		return os.Getenv(envVar)
	}
	return value
}

// HydrateEnv loads stored credentials into the process env so existing
// callers that read os.Getenv keep working. Env vars already set win.
// Precedence: existing env > config file options.apiKey > auth store.
func HydrateEnv() error {
	if err := LoadStore(); err != nil {
		return err
	}
	cfg, _ := config.Load()
	for _, p := range Providers {
		if p.EnvVar == "" {
			continue
		}
		if os.Getenv(p.EnvVar) != "" {
			continue
		}
		// Use the shared resolution order (skipping env-var step since we
		// already know it is empty above).
		if key := resolveKeyWithConfig(&p, p.ID, cfg); key != "" {
			_ = os.Setenv(p.EnvVar, key)
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
	if cred, ok := Get(id); ok {
		switch cred.Kind {
		case KindAPIKey:
			return "✓", "api key"
		case KindOAuth:
			if cred.Account != "" {
				return "✓", "oauth (" + cred.Account + ")"
			}
			return "✓", "oauth"
		}
	}
	// Fall back to checking the opencode config file for options.apiKey.
	cfg, _ := config.Load()
	if key := providerConfigAPIKey(cfg, id); key != "" {
		return "✓", "config"
	}
	return "✗", "not configured"
}
