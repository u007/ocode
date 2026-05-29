package agent

import (
	"testing"

	"github.com/jamesmercstudio/ocode/internal/config"
)

func TestSmallModelEligible(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"explore", true},
		{"general", true},
			{"compaction", true},
		{"build", false},
		{"plan", false},
		{"", false},
	}
	for _, c := range cases {
		got := smallModelEligible(c.name)
		if got != c.want {
			t.Errorf("smallModelEligible(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestInjectSmallModelIfEligible(t *testing.T) {
	t.Run("injects for eligible agent with no explicit model", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.SmallModel = "opencode-go/deepseek-v4-flash"
		spec := &AgentSpec{Name: "explore"}
		injectSmallModelIfEligible(nil, spec, cfg)
		if spec.Model != "opencode-go/deepseek-v4-flash" {
			t.Fatalf("expected small model injected, got %q", spec.Model)
		}
	})

	t.Run("no-op when spec already has model", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.SmallModel = "opencode-go/deepseek-v4-flash"
		spec := &AgentSpec{Name: "explore", Model: "anthropic/claude-sonnet-4"}
		injectSmallModelIfEligible(nil, spec, cfg)
		if spec.Model != "anthropic/claude-sonnet-4" {
			t.Fatalf("should not override explicit model, got %q", spec.Model)
		}
	})

	t.Run("no-op for ineligible agent", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.SmallModel = "opencode-go/deepseek-v4-flash"
		spec := &AgentSpec{Name: "build"}
		injectSmallModelIfEligible(nil, spec, cfg)
		if spec.Model != "" {
			t.Fatalf("should not inject for build agent, got %q", spec.Model)
		}
	})

	t.Run("no-op when SmallModel is empty", func(t *testing.T) {
		cfg := &config.Config{}
		spec := &AgentSpec{Name: "explore"}
		injectSmallModelIfEligible(nil, spec, cfg)
		if spec.Model != "" {
			t.Fatalf("should not inject when SmallModel empty, got %q", spec.Model)
		}
	})

	t.Run("no-op when cfg is nil", func(t *testing.T) {
		spec := &AgentSpec{Name: "explore"}
		injectSmallModelIfEligible(nil, spec, nil)
		if spec.Model != "" {
			t.Fatalf("should not inject when cfg nil, got %q", spec.Model)
		}
	})
}
