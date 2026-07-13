package grok

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/u007/ocode/internal/auth"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

// version is set at build time via ldflags.
var version = "dev"

func init() {
	providerplugin.Register(&GrokProvider{})
	// Register the subscription refresh/probe logic with the shared auth layer
	// so auth.RefreshIfExpiring / auth.TestCredential delegate here instead of
	// special-casing the "grok" provider by ID.
	auth.RegisterRefreshHook("grok", grokRefreshHook)
	auth.RegisterProbeHook("grok", grokProbeHook)
}

// grokRefreshHook refreshes a Grok subscription credential via the stored x.com
// session cookies. It implements auth.RefreshHook so the shared auth layer
// (refreshIfExpiring) no longer special-cases the "grok" provider by ID.
func grokRefreshHook(ctx context.Context, cred auth.Credential) (auth.Credential, bool) {
	if cred.CookieAuthToken == "" || cred.CookieCt0 == "" {
		return cred, true
	}
	const skew = 60 * time.Second
	if cred.ExpiresAt != 0 && time.Until(time.Unix(cred.ExpiresAt, 0)) > skew {
		return cred, true
	}
	// Serialize concurrent grok refreshes on the shared per-provider mutex so
	// that two simultaneous NewClient("grok/...") calls during token expiry do
	// not both hit GrokSubscriptionRefresh; the second reuses the first's result.
	return auth.RefreshLocked("grok", cred, func() (auth.Credential, error) {
		return auth.GrokSubscriptionRefresh(ctx, cred)
	})
}

// grokProbeHook verifies a Grok subscription credential by probing the
// grok.com OpenAI-compatible /models endpoint with the SSO bearer token and,
// when present, the x.com session cookies. It implements auth.ProbeHook.
func grokProbeHook(ctx context.Context, cred auth.Credential) error {
	if cred.AccessToken == "" {
		return fmt.Errorf("no subscription token")
	}
	url := auth.GrokSubscriptionBaseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build grok probe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	if cred.CookieAuthToken != "" && cred.CookieCt0 != "" {
		req.Header.Set("Cookie", fmt.Sprintf("auth_token=%s; ct0=%s", cred.CookieAuthToken, cred.CookieCt0))
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("grok probe request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("grok probe returned %d", resp.StatusCode)
	}
	return nil
}

// GrokProvider implements providerplugin.Provider for Grok (xAI). It offers a
// plain xAI API key method and a subscription method that authenticates with
// the user's x.com session cookies (see internal/auth/grok_subscription.go).
type GrokProvider struct{}

func (g *GrokProvider) ID() string { return "grok" }

func (g *GrokProvider) AuthMethods() []providerplugin.AuthMethod {
	return []providerplugin.AuthMethod{
		{Label: "Grok API Key", Type: "api", Run: nil},
		{Label: "Grok Subscription (x.com)", Type: "oauth", Run: subscriptionFlow},
	}
}

// Authenticate is only reached for methods whose Run is non-nil. The x.com
// subscription method is driven by the TUI (which collects the cookies), so its
// Run is a guard that should never be invoked directly.
func (g *GrokProvider) Authenticate(ctx context.Context, method providerplugin.AuthMethod) (providerplugin.AuthResult, error) {
	if method.Run == nil {
		return providerplugin.AuthResult{Type: "api"}, nil
	}
	return method.Run(ctx)
}

// isAllowed reports whether the model is a Grok model served by the subscription
// backend (grok-2, grok-3, grok-4, grok-beta, etc.).
func isAllowed(modelID string) bool {
	return strings.HasPrefix(strings.ToLower(modelID), "grok")
}

func (g *GrokProvider) ModelAllowed(modelID string) bool {
	return isAllowed(modelID)
}

// AdjustModel zeroises cost for subscription-routed models — a Grok
// subscription is billed as part of the x.com plan, not per-token here.
func (g *GrokProvider) AdjustModel(m providerplugin.Model) providerplugin.Model {
	if isAllowed(m.ID) {
		m.Cost.Input = 0
		m.Cost.Output = 0
		m.CacheRead = 0
		m.CacheWrite = 0
	}
	return m
}

func (g *GrokProvider) RequestHeaders(ctx providerplugin.RequestContext) http.Header {
	h := http.Header{}
	h.Set("originator", "opencode")
	h.Set("User-Agent", "opencode/"+version+" ("+runtime.GOOS+" "+runtime.GOARCH+")")
	if ctx.SessionID != "" {
		h.Set("session-id", ctx.SessionID)
	}
	return h
}

func (g *GrokProvider) RequestParams(ctx providerplugin.RequestContext) map[string]any {
	return nil
}

// subscriptionFlow is a guard: the TUI collects the x.com cookies and calls
// auth.GrokSubscriptionLogin directly, so this should not run. It returns a
// clear error if it ever is invoked standalone.
func subscriptionFlow(ctx context.Context) (providerplugin.AuthResult, error) {
	return providerplugin.AuthResult{}, fmt.Errorf("grok subscription requires x.com cookies; use the /connect dialog, not a direct auth method")
}
