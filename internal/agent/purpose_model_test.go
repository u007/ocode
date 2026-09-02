package agent

import (
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestInjectPurposeModelIfEligible(t *testing.T) {
	t.Run("explorer model wins for explore agent", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.ExplorerModel = "opencode-go/explorer-model"
		cfg.Ocode.ExplorerModelEnabled = true
		cfg.Ocode.SmallModel = "opencode-go/small-model"
		cfg.Ocode.SmallModelEnabled = true
		spec := &AgentSpec{Name: "explore"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "opencode-go/explorer-model" {
			t.Fatalf("expected explorer model injected, got %q", spec.Model)
		}
	})

	t.Run("explorer model applies to scout agent", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.ExplorerModel = "opencode-go/explorer-model"
		cfg.Ocode.ExplorerModelEnabled = true
		spec := &AgentSpec{Name: "scout"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "opencode-go/explorer-model" {
			t.Fatalf("expected explorer model injected for scout, got %q", spec.Model)
		}
	})

	t.Run("context model wins for context agent", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.ContextModel = "opencode-go/context-model"
		cfg.Ocode.ContextModelEnabled = true
		spec := &AgentSpec{Name: "context"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "opencode-go/context-model" {
			t.Fatalf("expected context model injected, got %q", spec.Model)
		}
	})

	t.Run("context model applies to doc-sync agent", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.ContextModel = "opencode-go/context-model"
		cfg.Ocode.ContextModelEnabled = true
		spec := &AgentSpec{Name: "doc-sync"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "opencode-go/context-model" {
			t.Fatalf("expected context model injected for doc-sync, got %q", spec.Model)
		}
	})

	t.Run("falls back to small model when explorer model disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.ExplorerModel = "opencode-go/explorer-model"
		cfg.Ocode.ExplorerModelEnabled = false
		cfg.Ocode.SmallModel = "opencode-go/small-model"
		cfg.Ocode.SmallModelEnabled = true
		spec := &AgentSpec{Name: "explore"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "opencode-go/small-model" {
			t.Fatalf("expected fallback to small model, got %q", spec.Model)
		}
	})

	t.Run("falls back to small model when explorer model unset", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.ExplorerModelEnabled = true
		cfg.Ocode.SmallModel = "opencode-go/small-model"
		cfg.Ocode.SmallModelEnabled = true
		spec := &AgentSpec{Name: "explore"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "opencode-go/small-model" {
			t.Fatalf("expected fallback to small model, got %q", spec.Model)
		}
	})

	t.Run("falls back to main model when both disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.ExplorerModelEnabled = false
		cfg.Ocode.SmallModelEnabled = false
		spec := &AgentSpec{Name: "explore"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "" {
			t.Fatalf("expected no override (inherit main model), got %q", spec.Model)
		}
	})

	t.Run("explicit model always wins over explorer/context model", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.ExplorerModel = "opencode-go/explorer-model"
		cfg.Ocode.ExplorerModelEnabled = true
		spec := &AgentSpec{Name: "explore", Model: "anthropic/claude-sonnet-4"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "anthropic/claude-sonnet-4" {
			t.Fatalf("should not override explicit model, got %q", spec.Model)
		}
	})

	t.Run("scout not eligible for small model fallback without purpose map", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.SmallModel = "opencode-go/small-model"
		cfg.Ocode.SmallModelEnabled = true
		spec := &AgentSpec{Name: "scout"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "opencode-go/small-model" {
			t.Fatalf("expected scout to fall back to small model, got %q", spec.Model)
		}
	})

	t.Run("no-op for non-purpose ineligible agent", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.SmallModel = "opencode-go/small-model"
		cfg.Ocode.SmallModelEnabled = true
		spec := &AgentSpec{Name: "build"}
		injectPurposeModelIfEligible(nil, spec, cfg)
		if spec.Model != "" {
			t.Fatalf("should not inject for build agent, got %q", spec.Model)
		}
	})
}
