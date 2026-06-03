package codex

import (
	"context"
	"testing"

	providerplugin "github.com/jamesmercstudio/ocode/internal/plugin/provider"
)

func TestCodexProvider_ID(t *testing.T) {
	p := &CodexProvider{}
	if p.ID() != "openai" {
		t.Errorf("expected ID=openai, got %s", p.ID())
	}
}

func TestModelAllowed(t *testing.T) {
	p := &CodexProvider{}
	if !p.ModelAllowed("gpt-5.4") {
		t.Error("expected gpt-5.4 allowed")
	}
	if p.ModelAllowed("gpt-4o") {
		t.Error("expected gpt-4o rejected")
	}
}

func TestAdjustModel_Allowed(t *testing.T) {
	p := &CodexProvider{}
	m := providerplugin.Model{ID: "gpt-5.4"}
	m.Cost.Input = 10
	m.Cost.Output = 20
	result := p.AdjustModel(m)
	if result.Cost.Input != 0 || result.Cost.Output != 0 {
		t.Errorf("expected cost=0, got input=%v output=%v", result.Cost.Input, result.Cost.Output)
	}
}

func TestAdjustModel_Rejected(t *testing.T) {
	p := &CodexProvider{}
	m := providerplugin.Model{ID: "gpt-4o"}
	m.Cost.Input = 10
	m.Cost.Output = 20
	result := p.AdjustModel(m)
	if result.Cost.Input != 10 || result.Cost.Output != 20 {
		t.Errorf("expected cost unchanged, got input=%v output=%v", result.Cost.Input, result.Cost.Output)
	}
}

func TestRequestHeaders(t *testing.T) {
	p := &CodexProvider{}
	h := p.RequestHeaders(providerplugin.RequestContext{SessionID: "sess-123"})
	if h.Get("originator") != "opencode" {
		t.Errorf("expected originator=opencode, got %s", h.Get("originator"))
	}
	if h.Get("session-id") != "sess-123" {
		t.Errorf("expected session-id=sess-123, got %s", h.Get("session-id"))
	}
	if h.Get("User-Agent") == "" {
		t.Error("expected non-empty User-Agent")
	}
}

func TestRequestParams(t *testing.T) {
	p := &CodexProvider{}
	params := p.RequestParams(providerplugin.RequestContext{})
	if _, ok := params["max_output_tokens"]; !ok {
		t.Error("expected max_output_tokens in params")
	}
}

func TestAuthMethods(t *testing.T) {
	p := &CodexProvider{}
	methods := p.AuthMethods()
	if len(methods) != 3 {
		t.Fatalf("expected 3 auth methods, got %d", len(methods))
	}
	if methods[0].Label != "ChatGPT Pro/Plus (browser)" {
		t.Errorf("unexpected first method label: %s", methods[0].Label)
	}
	if methods[1].Label != "ChatGPT Pro/Plus (device code)" {
		t.Errorf("unexpected second method label: %s", methods[1].Label)
	}
}

func TestAuthenticate_APIKey(t *testing.T) {
	p := &CodexProvider{}
	result, err := p.Authenticate(context.Background(), providerplugin.AuthMethod{Type: "api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != "api" {
		t.Errorf("expected type=api, got %s", result.Type)
	}
}
