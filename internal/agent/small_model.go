package agent

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/config"
)

// SmallModelPriority is the ordered list of cheap/fast models tried when
// auto-selecting a small model for lightweight tasks (title generation, etc.).
// First candidate whose provider has a usable API key wins.
// "opencode/mimo-v2-flash-free" is keyless and serves as a reliable fallback.
var SmallModelPriority = []string{
	"opencode-go/qwen3.5-plus",
	"opencode/mimo-v2-flash-free",
	"deepseek/deepseek-chat",
	"xiaomi-token-plan-sgp/MiMo-V2.5",
}

// newClientFn is the production factory; tests override it.
var newClientFn = func(cfg *config.Config, model string) LLMClient {
	return NewClient(cfg, model)
}

// ResolveSmallModel returns the first candidate in SmallModelPriority for which
// a client can be constructed (i.e. its provider key is available). Returns the
// already-configured value unchanged if cfg.Ocode.SmallModel is non-empty.
func ResolveSmallModel(cfg *config.Config) string {
	if cfg != nil && cfg.Ocode.SmallModel != "" {
		return cfg.Ocode.SmallModel
	}
	for _, candidate := range SmallModelPriority {
		if c := newClientFn(cfg, candidate); c != nil {
			return candidate
		}
	}
	return ""
}

// smallModelEligibleNames is the set of agent names that may use the small
// model. Primary coding agents (build, plan) are excluded to avoid downgrading
// the main coding loop.
var smallModelEligibleNames = map[string]bool{
	"explore":    true,
	"general":    true,
	"context":    true,
	"compaction": true,
	// orchestrator-planner intentionally excluded: requires reliable JSON output
	"orchestrator-explorer": true,
	"doc-sync":              true,
	"scout":                 true,
}

// smallModelEligible reports whether the named agent is a candidate for the
// small model. Empty name returns false.
func smallModelEligible(name string) bool {
	return name != "" && smallModelEligibleNames[name]
}

// injectSmallModelIfEligible sets spec.Model to the configured small model
// when the spec has no explicit model and the agent name is eligible.
// No-op if cfg is nil, cfg.Ocode.SmallModel is empty, cfg.Ocode.SmallModelEnabled
// is false, or spec already has a Model set (explicit registry override takes precedence).
func injectSmallModelIfEligible(a *Agent, spec *AgentSpec, cfg *config.Config) {
	if cfg == nil || cfg.Ocode.SmallModel == "" || !cfg.Ocode.SmallModelEnabled {
		return
	}
	if spec == nil || !smallModelEligible(spec.Name) {
		return
	}
	if strings.TrimSpace(spec.Model) != "" {
		return // explicit override in agent definition wins
	}
	spec.Model = cfg.Ocode.SmallModel
	a.emitDebug("AGENT", fmt.Sprintf("spec %q: injecting small model %s", spec.Name, spec.Model))
}

// agentPurpose maps an agent name to the purpose-specific model slot it draws
// from before falling back to the small model: an explorer-family agent
// (explore, scout) may use the configured explorer model, and a
// context-family agent (context, doc-sync) may use the configured context
// model.
var agentPurpose = map[string]string{
	"explore":  "explorer",
	"scout":    "explorer",
	"context":  "context",
	"doc-sync": "context",
}

// injectPurposeModelIfEligible sets spec.Model following the fallback chain:
// explicit model (unchanged) > purpose-specific model (explorer/context, if
// enabled and configured) > small model (via injectSmallModelIfEligible) >
// main model (left unset). No-op if spec already has an explicit model —
// an explicit registry/markdown override always wins.
func injectPurposeModelIfEligible(a *Agent, spec *AgentSpec, cfg *config.Config) {
	if cfg == nil || spec == nil || strings.TrimSpace(spec.Model) != "" {
		injectSmallModelIfEligible(a, spec, cfg)
		return
	}
	switch agentPurpose[spec.Name] {
	case "explorer":
		if cfg.Ocode.ExplorerModelEnabled && cfg.Ocode.ExplorerModel != "" {
			spec.Model = cfg.Ocode.ExplorerModel
			a.emitDebug("AGENT", fmt.Sprintf("spec %q: injecting explorer model %s", spec.Name, spec.Model))
			return
		}
	case "context":
		if cfg.Ocode.ContextModelEnabled && cfg.Ocode.ContextModel != "" {
			spec.Model = cfg.Ocode.ContextModel
			a.emitDebug("AGENT", fmt.Sprintf("spec %q: injecting context model %s", spec.Name, spec.Model))
			return
		}
	}
	injectSmallModelIfEligible(a, spec, cfg)
}
