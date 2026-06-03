package provider

import (
	"context"
	"net/http"
	"testing"
)

type stubProvider struct {
	id string
}

func (s *stubProvider) ID() string { return s.id }
func (s *stubProvider) AuthMethods() []AuthMethod {
	return nil
}
func (s *stubProvider) Authenticate(ctx context.Context, m AuthMethod) (AuthResult, error) {
	return AuthResult{}, nil
}
func (s *stubProvider) ModelAllowed(modelID string) bool { return true }
func (s *stubProvider) AdjustModel(m Model) Model        { return m }
func (s *stubProvider) RequestHeaders(ctx RequestContext) http.Header {
	return nil
}
func (s *stubProvider) RequestParams(ctx RequestContext) map[string]any { return nil }

func resetRegistry() {
	mu.Lock()
	registry = map[string]Provider{}
	mu.Unlock()
}

func TestRegisterAndGet(t *testing.T) {
	resetRegistry()
	Register(&stubProvider{id: "test"})
	p, ok := Get("test")
	if !ok || p.ID() != "test" {
		t.Fatalf("expected to get stub provider, got ok=%v id=%q", ok, p.ID())
	}
}

func TestGetNotFound(t *testing.T) {
	resetRegistry()
	_, ok := Get("nonexistent")
	if ok {
		t.Fatal("expected ok=false for nonexistent provider")
	}
}

func TestAll(t *testing.T) {
	resetRegistry()
	Register(&stubProvider{id: "a"})
	Register(&stubProvider{id: "b"})

	all := All()
	if len(all) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(all))
	}
}
