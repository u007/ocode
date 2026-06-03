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

var (
	deviceAuthURL  = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	deviceTokenURL = "https://auth.openai.com/api/accounts/deviceauth/token"
	deviceTimeout  = 5 * time.Minute
)

type deviceCodeResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	Interval     string `json:"interval"`
}

type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

// OpenAIDeviceLogin runs the full device auth flow.
// It blocks until the user completes browser authorization or ctx cancels.
func OpenAIDeviceLogin(ctx context.Context) (Credential, error) {
	ctx, cancel := context.WithTimeout(ctx, deviceTimeout)
	defer cancel()

	codeResp, err := requestDeviceCode(ctx)
	if err != nil {
		return Credential{}, fmt.Errorf("device code request: %w", err)
	}

	interval, err := time.ParseDuration(codeResp.Interval + "s")
	if err != nil {
		interval = 5 * time.Second
	}

	authCode, err := pollDeviceToken(ctx, codeResp.DeviceAuthID, codeResp.UserCode, interval)
	if err != nil {
		return Credential{}, err
	}

	return openaiExchangeCode(authCode, codeResp.UserCode)
}

func requestDeviceCode(ctx context.Context) (*deviceCodeResponse, error) {
	body := fmt.Sprintf(`{"client_id":"%s"}`, openaiClientID)
	req, err := http.NewRequestWithContext(ctx, "POST", deviceAuthURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed (%d): %s", resp.StatusCode, string(b))
	}

	var result deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}
	return &result, nil
}

func pollDeviceToken(ctx context.Context, deviceAuthID, userCode string, interval time.Duration) (string, error) {
	safetyMargin := 3 * time.Second
	client := &http.Client{Timeout: 30 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval + safetyMargin):
		}

		body := fmt.Sprintf(`{"device_auth_id":"%s","user_code":"%s"}`, deviceAuthID, userCode)
		req, err := http.NewRequestWithContext(ctx, "POST", deviceTokenURL, strings.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		if resp.StatusCode == http.StatusOK {
			var result deviceTokenResponse
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				return "", fmt.Errorf("decode device token response: %w", err)
			}
			resp.Body.Close()
			if result.AuthorizationCode == "" {
				return "", fmt.Errorf("device token response missing authorization_code")
			}
			return result.AuthorizationCode, nil
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			return "", fmt.Errorf("device token poll failed: %d", resp.StatusCode)
		}
	}
}
