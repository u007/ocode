package codex

import (
	"context"
	"net/http"
	"runtime"

	"github.com/u007/ocode/internal/auth"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

// version is set at build time via ldflags.
var version = "dev"

func init() {
	providerplugin.Register(&CodexProvider{})
}

// CodexProvider implements providerplugin.Provider for the OpenAI/Codex subscription.
type CodexProvider struct{}

func (c *CodexProvider) ID() string { return "openai" }

func (c *CodexProvider) AuthMethods() []providerplugin.AuthMethod {
	return []providerplugin.AuthMethod{
		{Label: "ChatGPT Pro/Plus (browser)", Type: "oauth", Run: browserFlow},
		{Label: "ChatGPT Pro/Plus (device code)", Type: "oauth", Run: deviceFlow},
		{Label: "Manually enter API Key", Type: "api", Run: nil},
	}
}

func (c *CodexProvider) Authenticate(ctx context.Context, method providerplugin.AuthMethod) (providerplugin.AuthResult, error) {
	if method.Run == nil {
		return providerplugin.AuthResult{Type: "api"}, nil
	}
	return method.Run(ctx)
}

func (c *CodexProvider) ModelAllowed(modelID string) bool {
	return isAllowed(modelID)
}

func (c *CodexProvider) AdjustModel(m providerplugin.Model) providerplugin.Model {
	if isAllowed(m.ID) {
		m.Cost.Input = 0
		m.Cost.Output = 0
		m.CacheRead = 0
		m.CacheWrite = 0
	}
	return m
}

func (c *CodexProvider) RequestHeaders(ctx providerplugin.RequestContext) http.Header {
	h := http.Header{}
	h.Set("originator", "opencode")
	h.Set("User-Agent", "opencode/"+version+" ("+runtime.GOOS+" "+runtime.GOARCH+")")
	if ctx.SessionID != "" {
		h.Set("session-id", ctx.SessionID)
	}
	return h
}

func (c *CodexProvider) RequestParams(ctx providerplugin.RequestContext) map[string]any {
	return map[string]any{
		"max_output_tokens": nil,
	}
}

func browserFlow(ctx context.Context) (providerplugin.AuthResult, error) {
	cred, err := auth.OpenAILogin(ctx)
	if err != nil {
		return providerplugin.AuthResult{}, err
	}
	return providerplugin.AuthResult{
		Type:      "oauth",
		Access:    cred.AccessToken,
		Refresh:   cred.RefreshToken,
		Expires:   cred.ExpiresAt * 1000,
		AccountID: cred.AccountID,
	}, nil
}

func deviceFlow(ctx context.Context) (providerplugin.AuthResult, error) {
	cred, err := auth.OpenAIDeviceLogin(ctx)
	if err != nil {
		return providerplugin.AuthResult{}, err
	}
	return providerplugin.AuthResult{
		Type:      "oauth",
		Access:    cred.AccessToken,
		Refresh:   cred.RefreshToken,
		Expires:   cred.ExpiresAt * 1000,
		AccountID: cred.AccountID,
	}, nil
}
