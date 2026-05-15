package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Anthropic OAuth constants — ported from opencode's plugin. These are
// Anthropic-issued client IDs for the Claude Pro/Max + console flows.
const (
	anthropicClientID      = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	anthropicAuthorizeMax  = "https://claude.ai/oauth/authorize"
	anthropicAuthorizeCons = "https://platform.claude.com/oauth/authorize"
	anthropicTokenURL      = "https://platform.claude.com/v1/oauth/token"
	anthropicRedirectURI   = "https://platform.claude.com/oauth/code/callback"
)

var anthropicScopes = []string{
	"org:create_api_key",
	"user:profile",
	"user:inference",
	"user:sessions:claude_code",
	"user:mcp_servers",
	"user:file_upload",
}

// AnthropicAuthorize starts the OAuth flow and returns the URL the user must visit.
// `mode` is either "max" (Claude Pro/Max subscription) or "console" (API key issuance).
type AnthropicFlow struct {
	URL      string
	State    string
	Verifier string
	Mode     string
}

func AnthropicAuthorize(mode string) (AnthropicFlow, error) {
	authorize := anthropicAuthorizeMax
	if mode == "console" {
		authorize = anthropicAuthorizeCons
	} else {
		mode = "max"
	}
	pkce, err := NewPKCE()
	if err != nil {
		return AnthropicFlow{}, err
	}
	state, err := RandomState()
	if err != nil {
		return AnthropicFlow{}, err
	}

	u, err := url.Parse(authorize)
	if err != nil {
		return AnthropicFlow{}, fmt.Errorf("parse anthropic authorize url: %w", err)
	}
	q := u.Query()
	q.Set("code", "true")
	q.Set("client_id", anthropicClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", anthropicRedirectURI)
	q.Set("scope", strings.Join(anthropicScopes, " "))
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	u.RawQuery = q.Encode()

	return AnthropicFlow{
		URL:      u.String(),
		State:    state,
		Verifier: pkce.Verifier,
		Mode:     mode,
	}, nil
}

// ParseAnthropicCallback accepts a URL, "code#state", or form-encoded string and extracts code+state.
func ParseAnthropicCallback(input string) (code, state string, ok bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", false
	}
	if u, err := url.Parse(s); err == nil && (u.Scheme != "" || u.Host != "") {
		c := u.Query().Get("code")
		st := u.Query().Get("state")
		if c != "" && st != "" {
			return c, st, true
		}
	}
	if i := strings.Index(s, "#"); i > 0 && i < len(s)-1 {
		return s[:i], s[i+1:], true
	}
	if vals, err := url.ParseQuery(s); err == nil {
		c := vals.Get("code")
		st := vals.Get("state")
		if c != "" && st != "" {
			return c, st, true
		}
	}
	return "", "", false
}

// AnthropicExchange swaps an authorization code for tokens.
func AnthropicExchange(code, state, verifier string) (Credential, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"state":         state,
		"client_id":     anthropicClientID,
		"redirect_uri":  anthropicRedirectURI,
		"code_verifier": verifier,
	}
	return anthropicTokenRequest(payload)
}

// AnthropicRefresh exchanges a refresh token for a fresh access token.
func AnthropicRefresh(refresh string) (Credential, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refresh,
		"client_id":     anthropicClientID,
	}
	return anthropicTokenRequest(payload)
}

func anthropicTokenRequest(payload map[string]string) (Credential, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Credential{}, fmt.Errorf("marshal anthropic token payload: %w", err)
	}
	req, err := http.NewRequest("POST", anthropicTokenURL, bytes.NewReader(body))
	if err != nil {
		return Credential{}, fmt.Errorf("build anthropic token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "ocode/0.1")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Credential{}, fmt.Errorf("anthropic token request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Credential{}, fmt.Errorf("anthropic token exchange failed: %d %s", resp.StatusCode, string(respBody))
	}
	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Credential{}, fmt.Errorf("decode anthropic token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return Credential{}, fmt.Errorf("anthropic token response missing access_token")
	}
	return Credential{
		Kind:         KindOAuth,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresAt:    time.Now().Unix() + parsed.ExpiresIn,
	}, nil
}
