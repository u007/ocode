package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	case "orcarouter":
		return probeBearer(ctx, "https://api.orcarouter.ai/v1/models", ResolveKey(id))
	case "google":
		k := ResolveKey(id)
		if k == "" {
			return fmt.Errorf("no credential")
		}
		return probeBearer(ctx, "https://generativelanguage.googleapis.com/v1beta/openai/models", k)
	case "groq":
		return probeBearer(ctx, "https://api.groq.com/openai/v1/models", ResolveKey(id))
	case "ollama-cloud":
		// /v1/models is public on ollama.com and returns 200 for any (or no)
		// credential, so probing it cannot detect a bad key. /api/usage is
		// auth-gated: 401 without a valid key, 200 with one.
		return probeBearer(ctx, "https://ollama.com/api/usage", ResolveKey(id))
	case "runinfra":
		return probeBearer(ctx, "https://api.runinfra.ai/v1/models", ResolveKey(id))
	case "opencode":
		return probeOpenAIInference(ctx, "https://opencode.ai/zen/v1", ResolveKey(id), true)
	case "opencode-go":
		return probeOpenAIInference(ctx, "https://opencode.ai/zen/go/v1", ResolveKey(id), true)
	case "copilot":
		cred, ok := Get(id)
		if !ok || cred.AccessToken == "" {
			return fmt.Errorf("no copilot token stored")
		}
		_, err := CopilotExchangeAPIToken(ctx, cred.AccessToken)
		return err
	}
	// Provider plugins may supply a provider-specific probe (e.g. Grok's
	// subscription SSO probe). Prefer it over the generic fallback.
	if c, ok := Get(id); ok {
		if handled, err := probeViaHook(id, c); handled {
			return err
		}
	}
	// Generic openai-compatible providers: probe /models (and a single
	// inference) when a base URL is known; otherwise fall back to "assume ok"
	// so providers without a known endpoint are not falsely rejected.
	if k := ResolveKey(id); k != "" {
		if bu := GetBaseURL(id); bu != "" {
			return probeOpenAIInference(ctx, bu, k, false)
		}
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

// probeOpenAIInference verifies a credential by sending a single minimal
// chat-completion request (one user message, max_tokens=1) and failing on any
// non-2xx response. It first lists models to pick a usable model id, since
// providers reject requests that name an unknown model even when the key is
// valid. Returns an error describing the failure.
func probeOpenAIInference(ctx context.Context, baseURL, key string, sessionHeader bool) error {
	if key == "" {
		return fmt.Errorf("no credential")
	}
	base := strings.TrimRight(baseURL, "/")
	// Pick a model from /models.
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/models", nil)
	if err != nil {
		return fmt.Errorf("build models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if sessionHeader {
		req.Header.Set("X-Opencode-Session", "ocode-auth-probe")
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("models request: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("read models response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("models probe returned %d", resp.StatusCode)
	}
	// Parse model ids from either OpenAI shape.
	var m struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("parse models response: %w", err)
	}
	var ids []string
	for _, d := range m.Data {
		ids = append(ids, d.ID)
	}
	for _, d := range m.Models {
		ids = append(ids, d.ID)
	}
	modelID := firstChatModelID(ids)
	if modelID == "" {
		return fmt.Errorf("models probe listed no chat model to verify the credential against")
	}
	// Send a single-message completion.
	payload := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"max_tokens":1}`, modelID)
	creq, err := http.NewRequestWithContext(ctx, "POST", base+"/chat/completions", bytes.NewReader([]byte(payload)))
	if err != nil {
		return fmt.Errorf("build completion request: %w", err)
	}
	creq.Header.Set("Authorization", "Bearer "+key)
	creq.Header.Set("Content-Type", "application/json")
	if sessionHeader {
		creq.Header.Set("X-Opencode-Session", "ocode-auth-probe")
	}
	cresp, err := (&http.Client{Timeout: 30 * time.Second}).Do(creq)
	if err != nil {
		return fmt.Errorf("completion request: %w", err)
	}
	defer cresp.Body.Close()
	if cresp.StatusCode >= 400 {
		return fmt.Errorf("inference probe returned %d", cresp.StatusCode)
	}
	return nil
}

// nonChatModelHints are id fragments of models that cannot serve
// /chat/completions; probing one would report a valid key as rejected.
var nonChatModelHints = []string{"embed", "whisper", "tts", "transcribe", "moderation", "rerank", "dall-e", "image", "audio", "speech"}

// firstChatModelID returns the first id that does not look like a non-chat
// model, or "" when every listed id does.
func firstChatModelID(ids []string) string {
	for _, id := range ids {
		lower := strings.ToLower(id)
		chat := id != ""
		for _, hint := range nonChatModelHints {
			if strings.Contains(lower, hint) {
				chat = false
				break
			}
		}
		if chat {
			return id
		}
	}
	return ""
}
