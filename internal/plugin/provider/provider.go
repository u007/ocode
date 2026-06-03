package provider

import (
	"context"
	"net/http"
	"sync"
)

// AuthMethod describes a single way a user can authenticate with a provider.
type AuthMethod struct {
	Label string
	Type  string // "oauth" or "api"
	Run   func(ctx context.Context) (AuthResult, error)
}

// AuthResult is the output of an auth method execution.
type AuthResult struct {
	Type      string // "oauth" or "api"
	Access    string
	Refresh   string
	Expires   int64 // unix millis
	AccountID string
	Key       string // for "api" type
}

// Model is the subset of model metadata that plugins may adjust.
type Model struct {
	ID         string
	Cost       struct{ Input, Output float64 }
	CacheRead  float64
	CacheWrite float64
	Limit      struct{ Context, Input, Output int }
}

// RequestContext carries request-scoped metadata plugins may need.
type RequestContext struct {
	Provider  string
	Model     string
	SessionID string
	Agent     string
}

// Provider is the contract a plugin fulfills for a single LLM provider.
type Provider interface {
	ID() string
	AuthMethods() []AuthMethod
	Authenticate(ctx context.Context, method AuthMethod) (AuthResult, error)
	ModelAllowed(modelID string) bool
	AdjustModel(m Model) Model
	RequestHeaders(ctx RequestContext) http.Header
	RequestParams(ctx RequestContext) map[string]any
}

var (
	mu       sync.RWMutex
	registry = map[string]Provider{}
)

// Register adds a provider plugin to the global registry.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	registry[p.ID()] = p
}

// Get returns the plugin for the given provider ID, if any.
func Get(id string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[id]
	return p, ok
}

// All returns all registered plugins.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	return out
}
