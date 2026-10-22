package pricing

import (
	"log"
	"strings"
	"sync"
	"time"
)

type ModelPricing struct {
	InputPerMillion      float64
	OutputPerMillion     float64
	CacheReadPerMillion  float64
	CacheWritePerMillion float64
}

// Unknown-model warnings are rate-limited, not once-ever, and bounded:
//   - a model re-warns after unknownWarnTTL, so a model that gained pricing and
//     later regressed to unknown (a registry refresh failure) is reported again;
//   - at most unknownWarnMax names are remembered, because the keys are
//     caller-supplied (user-typed or per-request model strings) and a
//     long-lived server must not grow without bound.
const (
	unknownWarnTTL = time.Hour
	unknownWarnMax = 256
)

var (
	unknownWarnMu sync.Mutex
	unknownWarned = map[string]time.Time{}
)

// shouldWarnUnknownModel reports whether model's unknown-pricing warning is due,
// and records it. Lookup runs on every LLM call, so the common case is a map hit
// under a short mutex.
func shouldWarnUnknownModel(model string, now time.Time) bool {
	unknownWarnMu.Lock()
	defer unknownWarnMu.Unlock()
	if at, ok := unknownWarned[model]; ok && now.Sub(at) < unknownWarnTTL {
		return false
	}
	if len(unknownWarned) >= unknownWarnMax {
		for name, at := range unknownWarned {
			if now.Sub(at) >= unknownWarnTTL {
				delete(unknownWarned, name)
			}
		}
		if len(unknownWarned) >= unknownWarnMax {
			// Every entry is fresh: forget them all rather than grow. The worst
			// case is a repeat warning, never unbounded memory.
			clear(unknownWarned)
		}
	}
	unknownWarned[model] = now
	return true
}

// registryLookup, when set, is consulted before the hardcoded fallback map.
// The agent package registers its models.dev-backed lookup here at init so
// every pricing.Lookup caller (spend calc, usage report, model picker) shares
// the same authoritative, up-to-date pricing source without an import cycle.
//
// Protected by registerOnce: RegisterRegistry is called from an init() in the
// agent package, but is also safe to call explicitly from main() or tests.
var registryLookup func(string) (ModelPricing, bool)
var registerOnce sync.Once

// RegisterRegistry installs a primary pricing source (the models.dev registry).
// Lookup tries this before falling back to the hardcoded map.
// Safe to call multiple times — only the first call takes effect.
func RegisterRegistry(fn func(string) (ModelPricing, bool)) {
	registerOnce.Do(func() {
		registryLookup = fn
	})
}

var modelsDevPricing = map[string]ModelPricing{
	"gpt-4o": {
		InputPerMillion:  5,
		OutputPerMillion: 15,
	},
	"gpt-4o-mini": {
		InputPerMillion:  0.15,
		OutputPerMillion: 0.6,
	},
	"o1-preview": {
		InputPerMillion:  15,
		OutputPerMillion: 60,
	},
	"claude-3-5-sonnet-20241022": {
		InputPerMillion:  3,
		OutputPerMillion: 15,
	},
	"claude-3-opus-20240229": {
		InputPerMillion:  15,
		OutputPerMillion: 75,
	},
	"claude-3-haiku-20240307": {
		InputPerMillion:  0.25,
		OutputPerMillion: 1.25,
	},
	"deepseek-v4-flash": {
		InputPerMillion:      0.14,
		OutputPerMillion:     0.28,
		CacheReadPerMillion:  0.0028,
		CacheWritePerMillion: 0,
	},
	// OpenRouter's MiniMax-M3 family (e.g. minimax/minimax-m3-20260531).
	"minimax-m3": {
		InputPerMillion:  0.30,
		OutputPerMillion: 1.20,
	},
	// OpenAI's GPT-5.6 codex family (used via the codex provider plugin).
	// Kept in the fallback map because the embedded models-snapshot.json is a
	// gitignored build-time artifact that predates these models until
	// regenerated with `make models-snapshot`; without these entries spend
	// resolves to $0 and logs "[PRICING] unknown model".
	"gpt-5.6-luna": {
		InputPerMillion:      0.20,
		OutputPerMillion:     1.20,
		CacheReadPerMillion:  0.02,
		CacheWritePerMillion: 0.25,
	},
	"gpt-5.6-terra": {
		InputPerMillion:      2,
		OutputPerMillion:     12,
		CacheReadPerMillion:  0.2,
		CacheWritePerMillion: 2.5,
	},
	"gpt-5.6-sol": {
		InputPerMillion:      5,
		OutputPerMillion:     30,
		CacheReadPerMillion:  0.5,
		CacheWritePerMillion: 6.25,
	},
}

func Lookup(model string) (ModelPricing, bool) {
	if registryLookup != nil {
		if pricing, ok := registryLookup(model); ok {
			return pricing, true
		}
	}

	if pricing, ok := modelsDevPricing[model]; ok {
		return pricing, true
	}

	normalized := normalizeModelName(model)
	if pricing, ok := modelsDevPricing[normalized]; ok {
		return pricing, true
	}

	for key, pricing := range modelsDevPricing {
		if strings.HasPrefix(normalized, key) && len(normalized) > len(key) {
			next := normalized[len(key)]
			if next == '-' || next == ':' || next == '/' || next == '.' {
				return pricing, true
			}
		}
	}

	// Model not found in any source — log a warning so users aren't surprised
	// by silent $0 pricing. The caller always gets false and can decide how to
	// handle it (e.g. fall back to default_cost, show "unknown" in UI).
	// Lookup runs on every LLM call, so the warning is rate-limited per model.
	if shouldWarnUnknownModel(model, time.Now()) {
		log.Printf("[PRICING] unknown model %q — no pricing data found; falling back to $0", model)
	}
	return ModelPricing{}, false
}

func normalizeModelName(model string) string {
	for _, sep := range []string{":", "/"} {
		if parts := strings.SplitN(model, sep, 2); len(parts) == 2 {
			model = parts[1]
		}
	}
	return strings.ToLower(strings.TrimSpace(model))
}
