package agent

import (
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

func TestNewClientGrokAPIKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "xai-test-key")
	cfg := &config.Config{}
	client := NewClient(cfg, "grok/grok-4")
	if client == nil {
		t.Fatal("expected non-nil client for grok/grok-4")
	}
	gc, ok := client.(*GenericClient)
	if !ok {
		t.Fatalf("expected *GenericClient, got %T", client)
	}
	if gc.Provider != "grok" {
		t.Errorf("Provider = %q, want grok", gc.Provider)
	}
	if gc.Model != "grok-4" {
		t.Errorf("Model = %q, want grok-4", gc.Model)
	}
	if gc.BaseURL != "https://api.x.ai/v1" {
		t.Errorf("BaseURL = %q, want https://api.x.ai/v1", gc.BaseURL)
	}
	if gc.APIKey != "xai-test-key" {
		t.Errorf("APIKey = %q, want xai-test-key", gc.APIKey)
	}
	if gc.UseOAuth {
		t.Error("UseOAuth should be false for an API-key credential")
	}
}

func TestNewClientGrokSubscription(t *testing.T) {
	// Register a grok subscription credential (SSO token + x.com cookies) and
	// confirm NewClient routes to the grok.com backend with UseOAuth set.
	cred := auth.Credential{
		Kind:            auth.KindOAuth,
		AccessToken:     "sso-token",
		BaseURL:         auth.GrokSubscriptionBaseURL,
		Account:         "grok-subscription",
		CookieAuthToken: "auth-token-val",
		CookieCt0:       "ct0-val",
	}
	if err := auth.Set("grok", cred); err != nil {
		t.Fatalf("auth.Set: %v", err)
	}
	defer auth.Remove("grok") // keep the shared auth.json clean

	cfg := &config.Config{}
	client := NewClient(cfg, "grok/grok-4")
	if client == nil {
		t.Fatal("expected non-nil client for grok subscription")
	}
	gc, ok := client.(*GenericClient)
	if !ok {
		t.Fatalf("expected *GenericClient, got %T", client)
	}
	if gc.BaseURL != auth.GrokSubscriptionBaseURL {
		t.Errorf("BaseURL = %q, want %q", gc.BaseURL, auth.GrokSubscriptionBaseURL)
	}
	if !gc.UseOAuth {
		t.Error("UseOAuth should be true for a grok subscription credential")
	}
	if gc.APIKey != "sso-token" {
		t.Errorf("APIKey = %q, want sso-token", gc.APIKey)
	}
	if gc.CookieAuthToken != "auth-token-val" || gc.CookieCt0 != "ct0-val" {
		t.Errorf("x.com cookies not carried to client: %q / %q", gc.CookieAuthToken, gc.CookieCt0)
	}
}

func TestGrokProviderRegistered(t *testing.T) {
	p := auth.FindProvider("grok")
	if p == nil {
		t.Fatal("grok provider not registered in auth.Providers")
	}
	if p.EnvVar != "XAI_API_KEY" {
		t.Errorf("EnvVar = %q, want XAI_API_KEY", p.EnvVar)
	}
	if _, ok := providers["grok"]; !ok {
		t.Error("grok not present in client providers map")
	}
}

func TestGrokSupportsReasoningEffort(t *testing.T) {
	if !providerSupportsReasoningEffort("grok") {
		t.Error("grok should support reasoning_effort")
	}
}
