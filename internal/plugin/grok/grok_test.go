package grok

import (
	"testing"

	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

func TestGrokProviderRegistered(t *testing.T) {
	p, ok := providerplugin.Get("grok")
	if !ok {
		t.Fatal("grok provider not registered")
	}
	if p.ID() != "grok" {
		t.Errorf("ID = %q, want grok", p.ID())
	}
}

func TestGrokProviderAuthMethods(t *testing.T) {
	p, _ := providerplugin.Get("grok")
	methods := p.AuthMethods()
	if len(methods) != 2 {
		t.Fatalf("expected 2 auth methods, got %d", len(methods))
	}
	if methods[0].Label != "Grok API Key" || methods[0].Type != "api" {
		t.Errorf("method[0] = %+v", methods[0])
	}
	if methods[1].Label != "Grok Subscription (x.com)" || methods[1].Type != "oauth" {
		t.Errorf("method[1] = %+v", methods[1])
	}
	if methods[1].Run == nil {
		t.Errorf("subscription method Run must be non-nil so it is shown in /connect")
	}
}

func TestGrokProviderModelAllowlist(t *testing.T) {
	p, _ := providerplugin.Get("grok")
	if !p.ModelAllowed("grok-4") {
		t.Error("grok-4 should be allowed")
	}
	if !p.ModelAllowed("grok-3-mini") {
		t.Error("grok-3-mini should be allowed")
	}
	if p.ModelAllowed("gpt-5") {
		t.Error("gpt-5 should not be allowed for grok provider")
	}
}

func TestGrokProviderAdjustModelZeroesCost(t *testing.T) {
	p, _ := providerplugin.Get("grok")
	m := providerplugin.Model{ID: "grok-4"}
	m.Cost.Input = 5
	m.Cost.Output = 15
	m = p.AdjustModel(m)
	if m.Cost.Input != 0 || m.Cost.Output != 0 {
		t.Errorf("expected zeroed cost, got in=%v out=%v", m.Cost.Input, m.Cost.Output)
	}
}
