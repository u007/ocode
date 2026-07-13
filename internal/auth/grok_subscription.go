package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Grok subscription endpoints.
//
// A Grok (xAI) subscription is tied to an x.com account, not the xAI API key.
// To use it programmatically we exchange the x.com session cookies
// (auth_token + ct0) for a short-lived grok.com SSO access token, then talk to
// grok.com's OpenAI-compatible backend. These are the endpoints grok.com's own
// web client uses; they are best-effort and may need adjustment if xAI changes
// them.
const GrokSubscriptionBaseURL = "https://grok.com/rest/openai/v1"

// GrokAuthURL is the grok.com SSO token endpoint. It is a var (not const) so
// tests can point it at a mock server.
var GrokAuthURL = "https://grok.com/rest/app-user/auth"

// grokAuthResponse models the grok.com /rest/app-user/auth response. The access
// token may arrive under a few keys across xAI rollouts, so we tolerate them.
type grokAuthResponse struct {
	AccessToken string `json:"access_token"`
	Token       string `json:"token"`
	SSO         string `json:"sso"`
	ExpiresIn   int64  `json:"expires_in"`
}

// GrokSubscriptionLogin exchanges x.com session cookies for a grok.com SSO
// access token and returns a credential ready to store. authToken and ct0 are
// the values of the x.com cookies `auth_token` and `ct0` respectively.
func GrokSubscriptionLogin(ctx context.Context, authToken, ct0 string) (Credential, error) {
	authToken = strings.TrimSpace(authToken)
	ct0 = strings.TrimSpace(ct0)
	if authToken == "" || ct0 == "" {
		return Credential{}, fmt.Errorf("both x.com cookies (auth_token and ct0) are required")
	}

	token, expiresAt, err := grokExchangeToken(ctx, authToken, ct0)
	if err != nil {
		return Credential{}, err
	}

	return Credential{
		Kind:            KindOAuth,
		AccessToken:     token,
		ExpiresAt:       expiresAt,
		Account:         "grok-subscription",
		BaseURL:         GrokSubscriptionBaseURL,
		CookieAuthToken: authToken,
		CookieCt0:       ct0,
	}, nil
}

// GrokSubscriptionRefresh re-authenticates using the cookies stored on the
// credential, returning a refreshed credential (preserving the cookies).
func GrokSubscriptionRefresh(ctx context.Context, cred Credential) (Credential, error) {
	if cred.CookieAuthToken == "" || cred.CookieCt0 == "" {
		return cred, fmt.Errorf("no x.com cookies stored; re-run /connect grok")
	}
	token, expiresAt, err := grokExchangeToken(ctx, cred.CookieAuthToken, cred.CookieCt0)
	if err != nil {
		return cred, err
	}
	refreshed := cred
	refreshed.AccessToken = token
	refreshed.ExpiresAt = expiresAt
	refreshed.Kind = KindOAuth
	return refreshed, nil
}

func grokExchangeToken(ctx context.Context, authToken, ct0 string) (string, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GrokAuthURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("build grok auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", fmt.Sprintf("auth_token=%s; ct0=%s", authToken, ct0))
	// grok.com requires the csrf cookie echoed as the x-csrf-token header.
	req.Header.Set("x-csrf-token", ct0)
	req.Header.Set("User-Agent", "ocode/1.0 (grok-subscription)")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("grok auth request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("grok auth failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed grokAuthResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", 0, fmt.Errorf("parse grok auth response: %w", err)
	}

	token := parsed.AccessToken
	if token == "" {
		token = parsed.Token
	}
	if token == "" {
		token = parsed.SSO
	}
	if token == "" {
		return "", 0, fmt.Errorf("grok auth response contained no access token: %s", strings.TrimSpace(string(body)))
	}

	expiresAt := int64(0)
	if parsed.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second).Unix()
	}
	return token, expiresAt, nil
}

// GrokSubscriptionCookies returns the stored x.com cookies for a credential.
func GrokSubscriptionCookies(cred Credential) (authToken, ct0 string) {
	return cred.CookieAuthToken, cred.CookieCt0
}
