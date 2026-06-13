package pricing

import "strings"

type ModelPricing struct {
	InputPerMillion      float64
	OutputPerMillion     float64
	CacheReadPerMillion  float64
	CacheWritePerMillion float64
}

// registryLookup, when set, is consulted before the hardcoded fallback map.
// The agent package registers its models.dev-backed lookup here at init so
// every pricing.Lookup caller (spend calc, usage report, model picker) shares
// the same authoritative, up-to-date pricing source without an import cycle.
var registryLookup func(string) (ModelPricing, bool)

// RegisterRegistry installs a primary pricing source (the models.dev registry).
// Lookup tries this before falling back to the hardcoded map.
func RegisterRegistry(fn func(string) (ModelPricing, bool)) {
	registryLookup = fn
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

	return ModelPricing{}, false
}

func normalizeModelName(model string) string {
	for _, sep := range []string{":", "/"} {
		if parts := strings.SplitN(model, sep, 2); len(parts) == 2 {
			model = parts[1]
		}
	}
	return strings.TrimSpace(model)
}
