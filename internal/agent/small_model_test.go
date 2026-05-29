package agent

import (
	"testing"

	"github.com/jamesmercstudio/ocode/internal/config"
)

func TestResolveSmallModel_ReturnsFirstViable(t *testing.T) {
	orig := newClientFn
	defer func() { newClientFn = orig }()
	newClientFn = func(cfg *config.Config, model string) LLMClient {
		if model == SmallModelPriority[1] {
			return &GenericClient{}
		}
		return nil
	}

	got := ResolveSmallModel(nil)
	if got != SmallModelPriority[1] {
		t.Fatalf("ResolveSmallModel = %q, want %q", got, SmallModelPriority[1])
	}
}

func TestResolveSmallModel_ReturnsEmptyWhenNoneViable(t *testing.T) {
	orig := newClientFn
	defer func() { newClientFn = orig }()
	newClientFn = func(_ *config.Config, _ string) LLMClient { return nil }

	got := ResolveSmallModel(nil)
	if got != "" {
		t.Fatalf("ResolveSmallModel = %q, want empty", got)
	}
}

func TestResolveSmallModel_SkipsWhenAlreadySet(t *testing.T) {
	called := false
	orig := newClientFn
	defer func() { newClientFn = orig }()
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		called = true
		return &GenericClient{}
	}

	cfg := &config.Config{}
	cfg.Ocode.SmallModel = "existing/model"
	got := ResolveSmallModel(cfg)
	if got != "existing/model" {
		t.Fatalf("ResolveSmallModel should return existing, got %q", got)
	}
	if called {
		t.Fatal("should not probe NewClient when SmallModel already set")
	}
}
