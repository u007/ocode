---
type: Gotcha
title: AIHubMix Test — Global Cache State Leakage
description: AIHubMix tests leak global cache state between runs — missing t.Cleanup snapshot/restore causes test pollution and flaky failures
tags:
  - aihubmix
  - test-leak
  - cache
  - global-state
  - flaky-tests
timestamp: 2026-08-26T03:26:28Z
---
## AIHubMix test cache leakage

**File:** `internal/agent/models_registry_aihubmix_test.go`

### The problem

`resetAIHubMixCache()` clears the global `aiHubMixLiveData` struct between tests:

```go
func resetAIHubMixCache() {
    aiHubMixLiveData = struct {
        mu        sync.RWMutex
        models    []string
        lastFetch time.Time
    }{}
}
```

Each test calls this at the top, which is correct. However, `TestProviderModelsAIHubMixMergesLive` calls `providerModelsFromRegistry("aihubmix", true)`, which populates the global cache with live-fetch results. There is **no cleanup** after this test — the cache retains stale data that leaks into subsequent tests.

This mirrors the exact pattern that `TestProviderModelsOpenRouterMergesLive` solved with `seedOpenRouterLiveCache` + `t.Cleanup` snapshot/restore — the OpenRouter tests snapshot and restore the cache state in cleanup, but the AIHubMix tests don't.

### Impact

- Test pollution: later tests may observe cached data from earlier runs instead of fresh fetches
- Flaky failures when test ordering changes or when running with `-count=N`
- Silent — the cache looks "warm" so fetches are skipped, producing wrong model lists

### Fix direction

Snapshot/restore `aiHubMixLiveData.models` + `lastFetch` in `t.Cleanup` under the mutex, matching the existing OpenRouter `seedOpenRouterLiveCache` pattern:

```go
func TestProviderModelsAIHubMixMergesLive(t *testing.T) {
    resetAIHubMixCache()
    // snapshot before
    aiHubMixLiveData.mu.RLock()
    savedModels := append([]string(nil), aiHubMixLiveData.models...)
    savedFetch := aiHubMixLiveData.lastFetch
    aiHubMixLiveData.mu.RUnlock()
    t.Cleanup(func() {
        aiHubMixLiveData.mu.Lock()
        aiHubMixLiveData.models = savedModels
        aiHubMixLiveData.lastFetch = savedFetch
        aiHubMixLiveData.mu.Unlock()
    })
    // ... test body ...
}
```

### Source

Discovered during the LLM streaming timeout review (2026-08-27).