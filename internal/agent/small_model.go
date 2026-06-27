package agent

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/config"
)

// SmallModelPriority is the ordered list of cheap/fast models tried when
// auto-selecting a small model for lightweight tasks (title generation, etc.).
// First candidate whose provider has a usable API key wins.
// "opencode/mimo-v2.5-free" is keyless and serves as a reliable fallback.
var SmallModelPriority = []string{
	"opencode-go/deepseek-v4-flash",
	"opencode/mimo-v2.5-free",
	"opencode-go/qwen-3.5-plus",
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
	"explore":               true,
	"general":               true,
	"compaction":            true,
	// orchestrator-planner intentionally excluded: requires reliable JSON output
	"orchestrator-explorer": true,
	"doc-sync":              true,
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
	emitDebug("AGENT", fmt.Sprintf("spec %q: injecting small model %s", spec.Name, spec.Model))
}
