package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// resetAIHubMixCache clears the live cache between tests so each test forces a
// fresh fetch, then restores the previous process-global state afterward.
func resetAIHubMixCache(t *testing.T) {
	t.Helper()
	aiHubMixLiveData.mu.Lock()
	previousModels := append([]string(nil), aiHubMixLiveData.models...)
	previousFetch := aiHubMixLiveData.lastFetch
	aiHubMixLiveData.models = nil
	aiHubMixLiveData.lastFetch = previousFetch
	aiHubMixLiveData.mu.Unlock()
	t.Cleanup(func() {
		aiHubMixLiveData.mu.Lock()
		aiHubMixLiveData.models = previousModels
		aiHubMixLiveData.lastFetch = previousFetch
		aiHubMixLiveData.mu.Unlock()
	})
}

func TestFetchAIHubMixLiveModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"ox-alpha"},{"id":"claude-opus-4-6"},{"id":"ox-alpha"}]}`))
	}))
	defer srv.Close()

	resetAIHubMixCache(t)
	old := providers["aihubmix"]
	providers["aihubmix"] = providerInfo{envKey: "AIHUBMIX_API_KEY", baseURL: srv.URL}
	defer func() { providers["aihubmix"] = old }()

	t.Setenv("AIHUBMIX_API_KEY", "test-key")

	got := fetchAIHubMixLiveModels()
	if len(got) != 2 {
		t.Fatalf("expected 2 unique models, got %d: %v", len(got), got)
	}
	if got[0] != "ox-alpha" || got[1] != "claude-opus-4-6" {
		t.Fatalf("unexpected merged order: %v", got)
	}
}

func TestFetchAIHubMixLiveModelsNoKeyReturnsNil(t *testing.T) {
	resetAIHubMixCache(t)
	old := providers["aihubmix"]
	providers["aihubmix"] = providerInfo{envKey: "AIHUBMIX_API_KEY", baseURL: "http://127.0.0.1:0"}
	defer func() { providers["aihubmix"] = old }()

	t.Setenv("AIHUBMIX_API_KEY", "")

	if got := fetchAIHubMixLiveModels(); got != nil {
		t.Fatalf("expected nil when key is absent, got %v", got)
	}
}

func TestProviderModelsAIHubMixMergesLive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"ox-alpha"}]}`))
	}))
	defer srv.Close()

	resetAIHubMixCache(t)
	old := providers["aihubmix"]
	providers["aihubmix"] = providerInfo{envKey: "AIHUBMIX_API_KEY", baseURL: srv.URL}
	defer func() { providers["aihubmix"] = old }()
	t.Setenv("AIHUBMIX_API_KEY", "test-key")

	// refresh=true forces a live fetch and merges it with the snapshot list.
	got := providerModelsFromRegistry("aihubmix", true)
	if !containsString(got, "ox-alpha") {
		t.Fatalf("ox-alpha missing from merged aihubmix list: %v", got)
	}
}

func TestMergeModelIDs(t *testing.T) {
	a := []string{"x", "y", "z"}
	b := []string{"y", "w", "x"}
	merged := mergeModelIDs(a, b)
	want := map[string]bool{"x": true, "y": true, "z": true, "w": true}
	if len(merged) != len(want) {
		t.Fatalf("expected %d unique ids, got %d: %v", len(want), len(merged), merged)
	}
	for _, id := range merged {
		if !want[id] {
			t.Fatalf("unexpected id %q in %v", id, merged)
		}
	}
}
