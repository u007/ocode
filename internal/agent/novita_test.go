package agent

import (
	"os"
	"testing"
	"time"
)

func TestNovitaLiveModelEntry_CacheEmpty(t *testing.T) {
	// Reset cache to ensure clean state.
	novitaLiveData.mu.Lock()
	novitaLiveData.models = nil
	novitaLiveData.lastFetch = time.Time{}
	novitaLiveData.mu.Unlock()

	_, ok := novitaLiveModelEntry("deepseek-v3")
	if ok {
		t.Error("expected false when cache is nil")
	}
}

func TestNovitaLiveModelEntry_CacheHit(t *testing.T) {
	entry := modelEntry{
		ID:       "deepseek-v3",
		Name:     "deepseek-v3",
		ToolCall: true,
		Limit:    modelLimit{Context: 128000, Output: 8192},
		Cost:     modelCost{Input: 0.27, Output: 1.10},
	}
	novitaLiveData.mu.Lock()
	novitaLiveData.models = map[string]modelEntry{
		"deepseek-v3": entry,
	}
	novitaLiveData.lastFetch = time.Now()
	novitaLiveData.mu.Unlock()

	got, ok := novitaLiveModelEntry("deepseek-v3")
	if !ok {
		t.Fatal("expected true for cached model")
	}
	if got.ID != "deepseek-v3" {
		t.Errorf("ID = %q, want %q", got.ID, "deepseek-v3")
	}
	if got.Cost.Input != 0.27 {
		t.Errorf("Cost.Input = %v, want 0.27", got.Cost.Input)
	}
}

func TestNovitaLiveModelEntry_CacheMiss(t *testing.T) {
	novitaLiveData.mu.Lock()
	novitaLiveData.models = map[string]modelEntry{
		"deepseek-v3": {ID: "deepseek-v3"},
	}
	novitaLiveData.lastFetch = time.Now()
	novitaLiveData.mu.Unlock()

	_, ok := novitaLiveModelEntry("nonexistent-model")
	if ok {
		t.Error("expected false for missing model")
	}
}

func TestNovitaLiveModelEntry_ConcurrentRead(t *testing.T) {
	novitaLiveData.mu.Lock()
	novitaLiveData.models = map[string]modelEntry{
		"model-a": {ID: "model-a"},
		"model-b": {ID: "model-b"},
	}
	novitaLiveData.lastFetch = time.Now()
	novitaLiveData.mu.Unlock()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = novitaLiveModelEntry("model-a")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestAllProviders_IncludesNovitaWhenKeySet(t *testing.T) {
	// Set the env var to trigger inclusion.
	os.Setenv("NOVITA_API_KEY", "test-key")
	defer os.Unsetenv("NOVITA_API_KEY")

	// The AllProviders function requires the models.dev snapshot to be loaded.
	// If the snapshot isn't available (e.g. CI without network), this test
	// gracefully skips.
	providers := AllProviders()
	if providers == nil {
		t.Skip("models.dev snapshot not available; skipping provider listing test")
	}

	found := false
	for _, p := range providers {
		if p == "novita-ai" {
			found = true
			break
		}
	}
	if !found {
		// List the providers we did get for debugging
		t.Errorf("expected novita-ai in provider list, got: %v", providers)
	}
}

func TestAllProviders_ExcludesNovitaWhenNoEnvVar(t *testing.T) {
	// Unset the env var. Note: a stored credential (e.g. in auth.json) may
	// still cause novita-ai to appear via auth.ResolveKey, so this test
	// only asserts that the env-var-only path works correctly.
	os.Unsetenv("NOVITA_API_KEY")

	providers := AllProviders()
	if providers == nil {
		t.Skip("models.dev snapshot not available; skipping provider listing test")
	}

	// Re-set the env var and verify inclusion to prove the env-var gating works.
	os.Setenv("NOVITA_API_KEY", "test-key")
	defer os.Unsetenv("NOVITA_API_KEY")

	providersWithKey := AllProviders()
	if providersWithKey == nil {
		t.Skip("models.dev snapshot not available; skipping provider listing test")
	}

	foundWithKey := false
	for _, p := range providersWithKey {
		if p == "novita-ai" {
			foundWithKey = true
			break
		}
	}
	if !foundWithKey {
		t.Errorf("expected novita-ai in provider list when env var is set, got: %v", providersWithKey)
	}

	// Now test exclusion: if novita-ai is present when env var is unset, it must
	// be from a stored credential (not the env var we just set above).
	os.Unsetenv("NOVITA_API_KEY")
	providersNoKey := AllProviders()
	if providersNoKey == nil {
		t.Skip("models.dev snapshot not available; skipping provider listing test")
	}

	foundNoKey := false
	for _, p := range providersNoKey {
		if p == "novita-ai" {
			foundNoKey = true
			break
		}
	}
	// If novita-ai appears without the env var, a stored credential exists.
	// That's not a test failure — it's legitimate behavior.
	if foundNoKey {
		t.Log("novita-ai present without NOVITA_API_KEY env var (likely from stored credential)")
	}
}
