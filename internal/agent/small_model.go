package agent

import "github.com/jamesmercstudio/ocode/internal/config"

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
