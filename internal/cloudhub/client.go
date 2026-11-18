// Package cloudhub is a thin client for kakiit's /api/ocode/cloud API. It
// brokers and refreshes cloud-storage tokens so no refresh token or client
// secret ever lives in a project's local file.
//
// The base URL is resolved from sync_url only (never backend_url), and the
// sync bearer is attached as the sole Authorization header. The bearer is
// never logged or serialised into an error.
package cloudhub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
	ocodesync "github.com/u007/ocode/internal/sync"
)

const apiPrefix = "/api/ocode/cloud"

const errorBodyLimit = 4096

// successBodyLimit bounds a successful response (a large /connections list
// must not be truncated); only error excerpts are capped at errorBodyLimit.
const successBodyLimit = 1 << 20

// ErrNoSyncToken is returned by NewFromSync when this machine has not logged
// in to the sync hub yet.
var ErrNoSyncToken = errors.New("cloudhub: no sync token; run sync login first")

// resolveBaseURL and readSyncToken are indirected so tests can pin the
// sync_url-only resolution and the bearer without touching HOME/XDG or a real
// hub. Production uses sync.ResolveBaseURL(config.SyncURL) + sync.LoadToken.
var (
	resolveBaseURL = func() (string, error) {
		cfg, err := config.Load()
		if err != nil {
			// Never silently send the sync bearer to a default hub when the
			// machine's configured sync_url could not be read.
			return "", fmt.Errorf("cloudhub: load config for sync_url: %w", err)
		}
		return ocodesync.ResolveBaseURL(cfg.Ocode.SyncURL), nil
	}
	readSyncToken = ocodesync.LoadToken
)

// Client talks to the hub's /api/ocode/cloud group.
type Client struct {
	BaseURL string
	token   string
	HTTP    *http.Client
}

// NewClient builds a client for an explicit base URL and bearer. Tests use it
// to point at an httptest server; production callers should prefer NewFromSync
// so the base URL can only come from sync_url.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// NewFromSync resolves the base URL from sync_url only and attaches the local
// sync bearer from <GlobalDataDir>/sync/token.
func NewFromSync() (*Client, error) {
	token, ok, err := readSyncToken()
	if err != nil {
		return nil, fmt.Errorf("cloudhub: load sync token: %w", err)
	}
	if !ok || token == "" {
		return nil, ErrNoSyncToken
	}
	base, err := resolveBaseURL()
	if err != nil {
		return nil, err
	}
	return NewClient(base, token), nil
}

// TokenBundle is the response from POST /oauth/redeem.
type TokenBundle struct {
	ConnectionID string    `json:"connection_id"`
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ConnectionMeta is one entry from GET /connections. It deliberately carries
// no token material — only the metadata needed to bind a central connection.
type ConnectionMeta struct {
	Provider     string    `json:"provider"`
	ConnectionID string    `json:"connection_id"`
	RootPath     string    `json:"root_path"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// HubError is the typed error for a non-2xx hub response. Body is a bounded,
// token-redacted excerpt so failures are actionable without leaking the bearer.
type HubError struct {
	Op         string
	StatusCode int
	Body       string
}

func (e *HubError) Error() string {
	return fmt.Sprintf("cloudhub: %s: hub returned %d: %s", e.Op, e.StatusCode, e.Body)
}

// do performs a JSON request and returns the response body. It never returns
// the request headers in an error, and redacts the bearer from any echoed body.
func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("cloudhub: encode %s: %w", path, err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+apiPrefix+path, reader)
	if err != nil {
		return nil, fmt.Errorf("cloudhub: request %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudhub: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, successBodyLimit))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		safe := string(raw)
		if len(safe) > errorBodyLimit {
			safe = safe[:errorBodyLimit]
		}
		safe = strings.TrimSpace(safe)
		if c.token != "" {
			safe = strings.ReplaceAll(safe, c.token, "[REDACTED]")
		}
		return nil, &HubError{Op: method + " " + path, StatusCode: resp.StatusCode, Body: safe}
	}
	return raw, nil
}

// RegisterCallback registers ocode's loopback callback for this install.
func (c *Client) RegisterCallback(ctx context.Context, installID, callbackURL string) error {
	_, err := c.do(ctx, http.MethodPut, "/callback", map[string]string{
		"install_id":   installID,
		"callback_url": callbackURL,
	})
	return err
}

// StartOAuth starts a provider OAuth hop and returns the provider auth URL.
func (c *Client) StartOAuth(ctx context.Context, provider, connectorID, clientState, challenge string) (string, error) {
	raw, err := c.do(ctx, http.MethodPost, "/oauth/start", map[string]string{
		"provider":       provider,
		"connector_id":   connectorID,
		"client_state":   clientState,
		"code_challenge": challenge,
	})
	if err != nil {
		return "", err
	}
	var out struct {
		AuthURL string `json:"auth_url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("cloudhub: decode auth_url: %w", err)
	}
	if out.AuthURL == "" {
		return "", fmt.Errorf("cloudhub: empty auth_url from oauth/start")
	}
	return out.AuthURL, nil
}

// Redeem exchanges a one-time code for a short-lived access token bundle.
func (c *Client) Redeem(ctx context.Context, code, verifier, clientState string) (TokenBundle, error) {
	raw, err := c.do(ctx, http.MethodPost, "/oauth/redeem", map[string]string{
		"code":          code,
		"code_verifier": verifier,
		"client_state":  clientState,
	})
	if err != nil {
		return TokenBundle{}, err
	}
	var out TokenBundle
	if err := json.Unmarshal(raw, &out); err != nil {
		return TokenBundle{}, fmt.Errorf("cloudhub: decode token bundle: %w", err)
	}
	if out.ConnectionID == "" || out.AccessToken == "" {
		return TokenBundle{}, fmt.Errorf("cloudhub: incomplete token bundle from oauth/redeem")
	}
	return out, nil
}

// ListConnections returns this sync user's central connections (no tokens).
func (c *Client) ListConnections(ctx context.Context) ([]ConnectionMeta, error) {
	raw, err := c.do(ctx, http.MethodGet, "/connections", nil)
	if err != nil {
		return nil, err
	}
	var out []ConnectionMeta
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("cloudhub: decode connections: %w", err)
	}
	return out, nil
}

// Disconnect revokes a central connection globally for this sync user.
func (c *Client) Disconnect(ctx context.Context, connectionID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/connections/"+url.PathEscape(connectionID), nil)
	return err
}

// RefreshToken asks the hub for a valid access token, refreshing at the
// provider only when needed.
func (c *Client) RefreshToken(ctx context.Context, connectionID string) (string, time.Time, error) {
	raw, err := c.do(ctx, http.MethodPost, "/token/refresh", map[string]string{
		"connection_id": connectionID,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	var out struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", time.Time{}, fmt.Errorf("cloudhub: decode refreshed token: %w", err)
	}
	if out.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("cloudhub: empty access_token from token/refresh")
	}
	return out.AccessToken, out.ExpiresAt, nil
}
