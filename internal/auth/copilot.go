package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	copilotClientID         = "Iv1.b507a08c87ecfe98"
	copilotDeviceCodeURL    = "https://github.com/login/device/code"
	copilotAccessTokenURL   = "https://github.com/login/oauth/access_token"
	copilotInternalTokenURL = "https://api.github.com/copilot_internal/v2/token"
	copilotPollTimeout      = 15 * time.Minute
)

// CopilotDevice is the device-code response shown to the user.
type CopilotDevice struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        int
}

// CopilotStartDevice requests a device code from GitHub.
func CopilotStartDevice() (CopilotDevice, error) {
	payload, _ := json.Marshal(map[string]string{
		"client_id": copilotClientID,
		"scope":     "read:user",
	})
	req, err := http.NewRequest("POST", copilotDeviceCodeURL, bytes.NewReader(payload))
	if err != nil {
		return CopilotDevice{}, fmt.Errorf("build copilot device request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return CopilotDevice{}, fmt.Errorf("copilot device request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return CopilotDevice{}, fmt.Errorf("copilot device code failed: %d %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return CopilotDevice{}, fmt.Errorf("decode copilot device response: %w", err)
	}
	if parsed.Interval <= 0 {
		parsed.Interval = 5
	}
	return CopilotDevice{
		DeviceCode:      parsed.DeviceCode,
		UserCode:        parsed.UserCode,
		VerificationURI: parsed.VerificationURI,
		Interval:        parsed.Interval,
	}, nil
}

// CopilotPoll polls the access-token endpoint until the user authorises (or ctx cancels).
// Returns a credential containing the long-lived GitHub OAuth token as AccessToken.
// (Copilot exchanges this for a short-lived API token at request time; we store the GH token.)
func CopilotPoll(ctx context.Context, dev CopilotDevice) (Credential, error) {
	interval := time.Duration(dev.Interval) * time.Second
	deadline := time.Now().Add(copilotPollTimeout)

	for {
		if time.Now().After(deadline) {
			return Credential{}, fmt.Errorf("copilot device flow timed out after %v", copilotPollTimeout)
		}

		payload, _ := json.Marshal(map[string]string{
			"client_id":   copilotClientID,
			"device_code": dev.DeviceCode,
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		})
		req, err := http.NewRequestWithContext(ctx, "POST", copilotAccessTokenURL, bytes.NewReader(payload))
		if err != nil {
			return Credential{}, fmt.Errorf("build copilot poll request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")

		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return Credential{}, fmt.Errorf("copilot poll request: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return Credential{}, fmt.Errorf("copilot poll failed: %d %s", resp.StatusCode, string(body))
		}
		var parsed struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return Credential{}, fmt.Errorf("decode copilot poll response: %w", err)
		}
		if parsed.AccessToken != "" {
			return Credential{
				Kind:        KindOAuth,
				AccessToken: parsed.AccessToken,
				// GitHub OAuth tokens are long-lived; no refresh token.
			}, nil
		}
		switch parsed.Error {
		case "authorization_pending", "":
			// keep polling
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return Credential{}, fmt.Errorf("copilot device code expired; restart login")
		default:
			return Credential{}, fmt.Errorf("copilot authorization failed: %s", parsed.Error)
		}

		select {
		case <-ctx.Done():
			return Credential{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// copilotAPIToken is a cached short-lived API token derived from the GH OAuth token.
type copilotAPIToken struct {
	Token     string
	ExpiresAt int64
}

var (
	copilotCacheMu sync.Mutex
	copilotCache   = map[string]copilotAPIToken{} // keyed by gh token
)

// CopilotExchangeAPIToken exchanges a stored GitHub OAuth token for the short-lived
// Copilot API token used to call the Copilot chat completions endpoint. Result is
// cached in-memory until 60s before expiry.
func CopilotExchangeAPIToken(ctx context.Context, ghToken string) (string, error) {
	if ghToken == "" {
		return "", fmt.Errorf("copilot: empty github token")
	}
	copilotCacheMu.Lock()
	if t, ok := copilotCache[ghToken]; ok && time.Now().Unix() < t.ExpiresAt-60 {
		copilotCacheMu.Unlock()
		return t.Token, nil
	}
	copilotCacheMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "GET", copilotInternalTokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("build copilot api-token request: %w", err)
	}
	req.Header.Set("Authorization", "token "+ghToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")
	req.Header.Set("Editor-Version", "vscode/1.95.0")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.35.0")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("copilot api-token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("copilot api-token failed: %d %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode copilot api-token: %w", err)
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("copilot api-token response missing token")
	}
	if parsed.ExpiresAt == 0 {
		parsed.ExpiresAt = time.Now().Unix() + 1500
	}
	copilotCacheMu.Lock()
	copilotCache[ghToken] = copilotAPIToken{Token: parsed.Token, ExpiresAt: parsed.ExpiresAt}
	copilotCacheMu.Unlock()
	return parsed.Token, nil
}

// CopilotFetchAccount returns the GitHub login for a token (used to populate Credential.Account).
func CopilotFetchAccount(ghToken string) string {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "token "+ghToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var parsed struct {
		Login string `json:"login"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &parsed)
	return parsed.Login
}
