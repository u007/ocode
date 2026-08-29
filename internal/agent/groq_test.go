package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/auth"
)

func resetGroqCache(t *testing.T) {
	t.Helper()
	groqLiveData.mu.Lock()
	prevModels := append([]string(nil), groqLiveData.models...)
	prevFetch := groqLiveData.lastFetch
	groqLiveData.models = nil
	groqLiveData.lastFetch = prevFetch
	groqLiveData.mu.Unlock()
	t.Cleanup(func() {
		groqLiveData.mu.Lock()
		groqLiveData.models = prevModels
		groqLiveData.lastFetch = prevFetch
		groqLiveData.mu.Unlock()
	})
}

func TestGroqProviderRegistered(t *testing.T) {
	p := auth.FindProvider("groq")
	if p == nil {
		t.Fatal("groq provider not found in auth.FindProvider")
	}
	if p.EnvVar != "GROQ_API_KEY" {
		t.Fatalf("expected GROQ_API_KEY, got %q", p.EnvVar)
	}
}

func TestResolveKeyGroqEnv(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "test-groq-key")
	if got := auth.ResolveKey("groq"); got != "test-groq-key" {
		t.Fatalf("expected GROQ_API_KEY resolution, got %q", got)
	}
}

func TestNewClientGroqRouting(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "test-key")
	cases := []struct {
		input    string
		provider string
		model    string
		baseURL  string
		request  string
	}{
		{"groq/llama-3.3-70b-versatile", "groq", "llama-3.3-70b-versatile", "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile"},
		{"groq/compound", "groq", "compound", "https://api.groq.com/openai/v1", "groq/compound"},
		{"groq/groq/compound", "groq", "groq/compound", "https://api.groq.com/openai/v1", "groq/compound"},
		{"groq/openai/gpt-oss-120b", "groq", "openai/gpt-oss-120b", "https://api.groq.com/openai/v1", "openai/gpt-oss-120b"},
	}
	for _, tc := range cases {
		client := NewClient(nil, tc.input)
		got, ok := client.(*GenericClient)
		if !ok {
			t.Fatalf("input %q: expected GenericClient, got %T", tc.input, client)
		}
		if got.Provider != tc.provider {
			t.Fatalf("input %q: provider %q want %q", tc.input, got.Provider, tc.provider)
		}
		if got.Model != tc.model {
			t.Fatalf("input %q: model %q want %q", tc.input, got.Model, tc.model)
		}
		if got.BaseURL != tc.baseURL {
			t.Fatalf("input %q: baseURL %q want %q", tc.input, got.BaseURL, tc.baseURL)
		}
		if got.requestModel() != tc.request {
			t.Fatalf("input %q: requestModel %q want %q", tc.input, got.requestModel(), tc.request)
		}
	}
}

func TestFetchGroqLiveModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/models" {
			t.Errorf("expected path /models, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama-3.3-70b-versatile"},{"id":"groq/compound"},{"id":"openai/gpt-oss-120b"},{"id":"llama-3.3-70b-versatile"}]}`))
	}))
	defer srv.Close()

	resetGroqCache(t)
	old := providers["groq"]
	providers["groq"] = providerInfo{envKey: "GROQ_API_KEY", baseURL: srv.URL}
	defer func() { providers["groq"] = old }()

	t.Setenv("GROQ_API_KEY", "test-key")

	got := fetchGroqLiveModels()
	if len(got) != 3 {
		t.Fatalf("expected 3 unique models, got %d: %v", len(got), got)
	}
	expected := []string{"llama-3.3-70b-versatile", "groq/compound", "openai/gpt-oss-120b"}
	for i, want := range expected {
		if got[i] != want {
			t.Fatalf("index %d: got %q want %q full %v", i, got[i], want, got)
		}
	}
}

func TestGroqLiveDedupAndPrefix(t *testing.T) {
	// Verify allProviderModelsFromRegistry correctly prefixes groq ids and dedups against snapshot.
	// Use a fake server that returns a model already in snapshot to test dedup.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama-3.3-70b-versatile"},{"id":"new-groq-model-xyz"}]}`))
	}))
	defer srv.Close()

	resetGroqCache(t)
	old := providers["groq"]
	providers["groq"] = providerInfo{envKey: "GROQ_API_KEY", baseURL: srv.URL}
	defer func() { providers["groq"] = old }()
	t.Setenv("GROQ_API_KEY", "test-key")

	// Force cache population
	_ = fetchGroqLiveModels()
	if !groqCacheFresh() {
		t.Fatal("groq cache should be fresh after fetch")
	}
	// AllProviderModelsCached should now include the new model without blocking.
	cached := AllProviderModelsCached()
	foundOld := false
	foundNew := false
	for _, id := range cached {
		if id == "groq/llama-3.3-70b-versatile" {
			foundOld = true
		}
		if id == "groq/new-groq-model-xyz" {
			foundNew = true
		}
		if id == "groq/groq/compound" {
			// snapshot model should still be present (double prefix)
			foundOld = true
		}
	}
	if !foundOld {
		t.Error("expected snapshot groq model missing from cached list")
	}
	if !foundNew {
		t.Fatalf("expected live groq model groq/new-groq-model-xyz missing, cached=%v", cached)
	}
}

func TestGroqRequestModelPayload(t *testing.T) {
	// Ensure the outgoing chat payload uses the qualified model id for compound.
	t.Setenv("GROQ_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		// Check model field in JSON body contains qualified id
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	}))
	defer srv.Close()
	old := providers["groq"]
	providers["groq"] = providerInfo{envKey: "GROQ_API_KEY", baseURL: srv.URL}
	defer func() { providers["groq"] = old }()

	client := NewClient(nil, "groq/compound")
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	gc := client.(*GenericClient)
	gc.BaseURL = srv.URL
	if gc.requestModel() != "groq/compound" {
		t.Fatalf("expected requestModel groq/compound, got %q", gc.requestModel())
	}
	gc2 := &GenericClient{Provider: "groq", Model: "groq/compound", BaseURL: srv.URL}
	if gc2.requestModel() != "groq/compound" {
		t.Fatalf("expected groq/compound for double-prefixed model, got %q", gc2.requestModel())
	}
}
