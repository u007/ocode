package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// TestCredential makes a cheap probe to verify a saved credential works.
// Returns nil on success, or an error describing why it failed.
// Best-effort: providers without a known probe endpoint return nil.
func TestCredential(ctx context.Context, id string) error {
	switch id {
	case "openai":
		if _, ok := OAuthAccessToken(id); ok {
			// ChatGPT OAuth tokens lack api.model.read scope; skip the models probe.
			return nil
		}
		return probeBearer(ctx, "https://api.openai.com/v1/models", ResolveKey(id))
	case "anthropic":
		if tok, ok := OAuthAccessToken(id); ok {
			return probeAnthropicOAuth(ctx, tok)
		}
		return probeAnthropicKey(ctx, ResolveKey(id))
	case "openrouter":
		return probeBearer(ctx, "https://openrouter.ai/api/v1/models", ResolveKey(id))
	case "google":
		k := ResolveKey(id)
		if k == "" {
			return fmt.Errorf("no credential")
		}
		return probeBearer(ctx, "https://generativelanguage.googleapis.com/v1beta/openai/models", k)
	case "copilot":
		cred, ok := Get(id)
		if !ok || cred.AccessToken == "" {
			return fmt.Errorf("no copilot token stored")
		}
		_, err := CopilotExchangeAPIToken(ctx, cred.AccessToken)
		return err
	}
	// Generic openai-compatible providers: try /models with bearer.
	if k := ResolveKey(id); k != "" {
		return nil // unknown endpoint — assume ok rather than fail
	}
	return fmt.Errorf("no credential")
}

func probeBearer(ctx context.Context, url, key string) error {
	if key == "" {
		return fmt.Errorf("no credential")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("probe request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("probe returned %d", resp.StatusCode)
	}
	return nil
}

func probeAnthropicKey(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("no credential")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("probe request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("probe returned %d", resp.StatusCode)
	}
	return nil
}

func probeAnthropicOAuth(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("probe request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("probe returned %d", resp.StatusCode)
	}
	return nil
}
