package sync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type deviceStartResponse struct {
	DeviceCode string `json:"deviceCode"`
	UserCode   string `json:"userCode"`
	VerifyURL  string `json:"verifyUrl"`
	ExpiresIn  int    `json:"expiresIn"`
}

type deviceTokenResponse struct {
	Status string `json:"status"`
	Token  string `json:"token,omitempty"`
}

// Login runs the device-code login flow: starts a device authorization,
// prints the user code, opens the browser, polls until the user approves,
// and saves the resulting bearer token.
func (c *Client) Login(ctx context.Context, openBrowser func(url string) error, print func(format string, args ...interface{})) error {
	startResp, err := c.deviceStart(ctx)
	if err != nil {
		return fmt.Errorf("start device flow: %w", err)
	}

	print("To authorize ocode sync, visit:\n")
	print("  %s\n", startResp.VerifyURL)
	print("And enter the code:  %s\n\n", startResp.UserCode)

	if err := openBrowser(startResp.VerifyURL); err != nil {
		print("Could not open browser automatically. Please visit the URL above.\n")
	}

	ticker := time.NewTicker(c.interval())
	defer ticker.Stop()

	deadline := time.Now().Add(time.Duration(startResp.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("device code expired")
			}
			resp, err := c.deviceToken(ctx, startResp.DeviceCode)
			if err != nil {
				print("Polling error (will retry): %v\n", err)
				continue
			}
			switch resp.Status {
			case "pending":
				continue
			case "approved":
				if err := SaveToken(resp.Token); err != nil {
					return fmt.Errorf("save sync token: %w", err)
				}
				print("Sync token saved. Config sync is now active.\n")
				return nil
			case "expired":
				return fmt.Errorf("device code expired")
			default:
				return fmt.Errorf("unexpected status: %s", resp.Status)
			}
		}
	}
}

// DeviceStartResult is the exported shape of a started device-code flow,
// used by non-CLI callers (e.g. the web/desktop server) that need to
// start and poll the flow themselves instead of using the blocking Login.
type DeviceStartResult struct {
	DeviceCode string
	UserCode   string
	VerifyURL  string
	ExpiresIn  int
}

// StartDevice begins a device-code flow without polling or blocking —
// for callers (like the web server's /api/sync/login/start handler) that
// need to return the code/URL immediately and poll separately.
func (c *Client) StartDevice(ctx context.Context) (*DeviceStartResult, error) {
	resp, err := c.deviceStart(ctx)
	if err != nil {
		return nil, err
	}
	return &DeviceStartResult{
		DeviceCode: resp.DeviceCode,
		UserCode:   resp.UserCode,
		VerifyURL:  resp.VerifyURL,
		ExpiresIn:  resp.ExpiresIn,
	}, nil
}

// DevicePollResult is the exported shape of a single poll response.
type DevicePollResult struct {
	Status string // "pending" | "approved" | "expired"
	Token  string // set only when Status == "approved"
}

// PollDevice checks a device code's approval status once (no blocking
// loop) — the caller is responsible for re-polling on their own schedule.
func (c *Client) PollDevice(ctx context.Context, deviceCode string) (*DevicePollResult, error) {
	resp, err := c.deviceToken(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	return &DevicePollResult{Status: resp.Status, Token: resp.Token}, nil
}

func (c *Client) deviceStart(ctx context.Context) (*deviceStartResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/ocode/device/start", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return nil, fmt.Errorf("device start returned status %d: %s", res.StatusCode, string(respBody))
	}

	var out deviceStartResponse
	if err := c.decode(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) deviceToken(ctx context.Context, deviceCode string) (*deviceTokenResponse, error) {
	body, err := c.jsonBody(map[string]string{"deviceCode": deviceCode})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/ocode/device/token", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return nil, fmt.Errorf("device token returned status %d: %s", res.StatusCode, string(respBody))
	}

	var out deviceTokenResponse
	if err := c.decode(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Revoke tells the server to invalidate a sync bearer token.
func (c *Client) Revoke(ctx context.Context, token string) error {
	body, err := c.jsonBody(map[string]string{"token": token})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/ocode/device/revoke", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("revoke request failed: status %d", res.StatusCode)
	}
	return nil
}
