package agent

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// seedOpenRouterLiveCache installs a fake OpenRouter live model cache so tests
// can exercise ModelWindow / modelEntryFor / provider model listing without
// hitting the network. The cache is marked fresh (lastFetch = now) so callers
// that consult openRouterCacheFresh() read the seeded data instead of triggering
// a real fetch. The previous cache is restored when the test ends so this helper
// does not leak global state into other tests.
func seedOpenRouterLiveCache(t *testing.T, entries map[string]modelEntry) {
	t.Helper()
	openRouterLiveData.mu.Lock()
	prevModels := openRouterLiveData.models
	prevFetch := openRouterLiveData.lastFetch
	openRouterLiveData.models = entries
	openRouterLiveData.lastFetch = time.Now()
	openRouterLiveData.mu.Unlock()
	t.Cleanup(func() {
		openRouterLiveData.mu.Lock()
		openRouterLiveData.models = prevModels
		openRouterLiveData.lastFetch = prevFetch
		openRouterLiveData.mu.Unlock()
	})
}

func TestModelWindowOpenRouterLiveFallback(t *testing.T) {
	// openrouter/tencent/hy3:free is absent from the models.dev snapshot; the
	// live cache must supply its context window.
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free": {
			ID:    "tencent/hy3:free",
			Name:  "Tencent Hunyuan hy3 (free)",
			Limit: modelLimit{Context: 131072, Output: 8192},
			Cost:  modelCost{Input: 0, Output: 0},
		},
		// Sanity: a colon-variant not prefixed should also match a bare id.
		"anthropic/claude-3.5-sonnet": {
			ID:    "anthropic/claude-3.5-sonnet",
			Limit: modelLimit{Context: 200000},
		},
	})

	got := ModelWindow("openrouter/tencent/hy3:free")
	if got != 131072 {
		t.Fatalf("ModelWindow(openrouter/tencent/hy3:free) = %d, want 131072", got)
	}

	// Bare model id should also resolve via the live cache fallback.
	got = ModelWindow("tencent/hy3:free")
	if got != 131072 {
		t.Fatalf("ModelWindow(tencent/hy3:free) = %d, want 131072", got)
	}

	// Unknown model must still return 0 (no false positives).
	if got := ModelWindow("openrouter/nonexistent/model:xyz"); got != 0 {
		t.Fatalf("ModelWindow(unknown) = %d, want 0", got)
	}
}

func TestModelEntryForOpenRouterColonVariant(t *testing.T) {
	// The live cache keys by the bare id (after any "openrouter/" prefix), so a
	// lookup of "openrouter/tencent/hy3:free" must hit the cached
	// "tencent/hy3:free" entry.
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free": {
			ID:    "tencent/hy3:free",
			Limit: modelLimit{Context: 262144},
		},
	})

	m, ok := modelEntryFor("openrouter/tencent/hy3:free")
	if !ok {
		t.Fatalf("modelEntryFor(openrouter/tencent/hy3:free) returned ok=false, want true")
	}
	if m.Limit.Context != 262144 {
		t.Fatalf("resolved context = %d, want 262144", m.Limit.Context)
	}
}

func TestOpenRouterModelsLoaded(t *testing.T) {
	// The OpenRouter cache is package-global and may be left populated by other
	// tests (e.g. ones that call AllProviderModels() and hit the real API). Save
	// the prior state and force an unloaded start so the assertion is
	// deterministic, then restore the prior state at the end so we don't leak.
	openRouterLiveData.mu.Lock()
	savedModels := openRouterLiveData.models
	savedFetch := openRouterLiveData.lastFetch
	openRouterLiveData.models = nil
	openRouterLiveData.lastFetch = time.Time{}
	openRouterLiveData.mu.Unlock()
	t.Cleanup(func() {
		openRouterLiveData.mu.Lock()
		openRouterLiveData.models = savedModels
		openRouterLiveData.lastFetch = savedFetch
		openRouterLiveData.mu.Unlock()
	})

	// Unseeded cache reports not-loaded.
	if OpenRouterModelsLoaded() {
		t.Fatalf("OpenRouterModelsLoaded() = true before seeding, want false")
	}
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"openai/gpt-4o": {ID: "openai/gpt-4o", Limit: modelLimit{Context: 128000}},
	})
	if !OpenRouterModelsLoaded() {
		t.Fatalf("OpenRouterModelsLoaded() = false after seeding, want true")
	}
}

// TestAllProviderModelsOpenRouterPrefix verifies that allProviderModelsFromRegistry
// supplements the snapshot with live OpenRouter models using the fully-qualified
// "openrouter/<bare-id>" form (and does not double-prefix ids that already contain
// slashes, e.g. "openrouter/google/gemini-2.0-flash").
func TestAllProviderModelsOpenRouterPrefix(t *testing.T) {
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free":         {ID: "tencent/hy3:free", Limit: modelLimit{Context: 131072}},
		"google/gemini-2.0-flash":  {ID: "google/gemini-2.0-flash", Limit: modelLimit{Context: 1000000}},
		"anthropic/claude-3.5-son": {ID: "anthropic/claude-3.5-son", Limit: modelLimit{Context: 200000}},
	})

	got := allProviderModelsFromRegistry(true)
	have := make(map[string]bool, len(got))
	for _, id := range got {
		have[id] = true
	}

	if !have["openrouter/tencent/hy3:free"] {
		t.Fatalf("allProviderModelsFromRegistry missing openrouter/tencent/hy3:free; got %v", got)
	}
	// Multi-slash live id keeps a single "openrouter/" prefix, not "openrouter/openrouter/...".
	if !have["openrouter/google/gemini-2.0-flash"] {
		t.Fatalf("allProviderModelsFromRegistry missing openrouter/google/gemini-2.0-flash; got %v", got)
	}
	// The bare (unprefixed) form must NOT appear — the picker prepends the provider.
	if have["tencent/hy3:free"] {
		t.Fatalf("allProviderModelsFromRegistry leaked bare id tencent/hy3:free (expected openrouter/ prefix); got %v", got)
	}
}

// TestAllProviderModelsCachedSkipsStaleOpenRouter is the regression test for the
// main-loop blocking bug: when the OpenRouter cache is stale (past TTL) and the
// caller passes refresh=false (the TUI main loop via AllProviderModelsCached), the
// live models must NOT be consulted — so no blocking network fetch occurs. The
// snapshot-only result must omit the live "openrouter/tencent/hy3:free" id.
func TestAllProviderModelsCachedSkipsStaleOpenRouter(t *testing.T) {
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free": {ID: "tencent/hy3:free", Limit: modelLimit{Context: 131072}},
	})
	// Force the cache to look stale (zero lastFetch) so openRouterCacheFresh() is false.
	openRouterLiveData.mu.Lock()
	openRouterLiveData.lastFetch = time.Time{}
	openRouterLiveData.mu.Unlock()

	got := allProviderModelsFromRegistry(false)
	for _, id := range got {
		if id == "openrouter/tencent/hy3:free" {
			t.Fatalf("allProviderModelsFromRegistry(false) with stale cache pulled live openrouter id (would block on network); got %v", got)
		}
	}
}

// TestProviderModelsOpenRouterBare verifies that providerModelsFromRegistry("openrouter")
// returns BARE model ids (matching providerModelsFromSnapshot), so the picker — which
// prepends the provider segment — reconstructs the correct "openrouter/<id>".
func TestProviderModelsOpenRouterBare(t *testing.T) {
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free":        {ID: "tencent/hy3:free", Limit: modelLimit{Context: 131072}},
		"anthropic/claude-3.7-so": {ID: "anthropic/claude-3.7-so", Limit: modelLimit{Context: 200000}},
	})

	got := providerModelsFromRegistry("openrouter", true)

	// Build a set keyed by whether each returned id is prefixed.
	prefixed, bare := 0, 0
	for _, id := range got {
		if strings.HasPrefix(id, "openrouter/") {
			prefixed++
		} else {
			bare++
		}
	}
	if prefixed != 0 {
		t.Fatalf("providerModelsFromRegistry(openrouter) returned %d prefixed ids %v; expected all bare", prefixed, got)
	}
	if !slices.Contains(got, "tencent/hy3:free") {
		t.Fatalf("providerModelsFromRegistry(openrouter) missing bare id tencent/hy3:free; got %v", got)
	}
	if !slices.Contains(got, "anthropic/claude-3.7-so") {
		t.Fatalf("providerModelsFromRegistry(openrouter) missing bare id anthropic/claude-3.7-so; got %v", got)
	}
}
