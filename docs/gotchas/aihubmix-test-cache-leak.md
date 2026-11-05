---
type: Gotcha
title: AIHubMix Test — Global Cache State Leakage
description: RESOLVED. AIHubMix tests once leaked global live-cache state because they had no t.Cleanup snapshot/restore; the helper now snapshots and restores it. Kept as history of the cache-leak bug.
tags:
  - aihubmix
  - test-leak
  - cache
  - global-state
  - flaky-tests
timestamp: 2026-11-05T13:12:09Z
---
## AIHubMix test cache leakage

**Status: resolved.** `resetAIHubMixCache(t)` now snapshots `aiHubMixLiveData` and restores it in `t.Cleanup`. The "no cleanup" text under *The problem* describes the code before that fix and is kept as history. Evidence (checked 2026-11-05):

- `internal/agent/models_registry_aihubmix_test.go:11-25`: the helper takes `t`, saves `models` and `lastFetch` under the mutex (lines 13-18), clears `models`, and registers a `t.Cleanup` at lines 19-24 that writes both values back.
- `TestProviderModelsAIHubMixMergesLive` (line 67) calls `resetAIHubMixCache(t)` at line 74, so it gets the restore.
- The helper's signature differs from the snippet in *Fix direction* below (which shows a no-argument call with an inline snapshot). The behaviour is the same, and the helper form is the one in the code.

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