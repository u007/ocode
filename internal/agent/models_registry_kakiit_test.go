package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// resetKakiitCache clears the kakiit live-model cache between tests and
// restores the previous process-global state afterwards — the snapshot/
// restore pattern from docs/gotchas/aihubmix-test-cache-leak.md, so one test's
// seeded cache never leaks into another test's fetch.
func resetKakiitCache(t *testing.T) {
	t.Helper()
	kakiitLiveData.mu.Lock()
	previousModels := kakiitLiveModelsCopyLocked()
	previousFetch := kakiitLiveData.lastFetch
	previousErr := kakiitLiveData.authErr
	kakiitLiveData.models = nil
	kakiitLiveData.lastFetch = time.Time{}
	kakiitLiveData.authErr = nil
	kakiitLiveData.mu.Unlock()
	t.Cleanup(func() {
		kakiitLiveData.mu.Lock()
		kakiitLiveData.models = previousModels
		kakiitLiveData.lastFetch = previousFetch
		kakiitLiveData.authErr = previousErr
		kakiitLiveData.mu.Unlock()
	})
}

// kakiitModelsFixture is a /v1/models body shaped per the kakiit gateway spec:
// one Anthropic-protocol model with pricing, one free OpenAI-responses model,
// one openai-chat model.
const kakiitModelsFixture = `{
  "data": [
    {"id":"claude-sonnet-4-6","object":"model","owned_by":"anthropic","context_length":200000,
     "pricing":{"prompt":"0.000003","completion":"0.000015"},
     "x_kakiit_protocol":"anthropic-messages","x_kakiit_free":false},
    {"id":"gpt-5.6-luna","object":"model","owned_by":"openai","context_length":400000,
     "pricing":{"prompt":"0.00000125","completion":"0.000010"},
     "x_kakiit_protocol":"openai-responses","x_kakiit_free":true},
    {"id":"deepseek-v4-flash","object":"model","owned_by":"deepseek","context_length":128000,
     "pricing":{"prompt":"0.00000027","completion":"0.0000011"},
     "x_kakiit_protocol":"openai-chat","x_kakiit_free":false}
  ]
}`

// TestKakiitFetchParsesVendorExtensions pins the parse: context length,
// pricing, free flag and protocol all land in the cache; the picker list is
// kakiit/<id>; and a second call inside the TTL does not refetch.
func TestKakiitFetchParsesVendorExtensionsIntoContextPricingFreeAndProtocol(t *testing.T) {
	var fetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		if r.Header.Get("Authorization") != "Bearer kk-test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key","type":"invalid_request_error","code":"invalid_api_key"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kakiitModelsFixture))
	}))
	defer srv.Close()

	resetKakiitCache(t)
	old := providers["kakiit"]
	providers["kakiit"] = providerInfo{envKey: "KAKIIT_API_KEY", baseURL: srv.URL}
	defer func() { providers["kakiit"] = old }()
	t.Setenv("KAKIIT_API_KEY", "kk-test-key")
	t.Setenv("KAKIIT_BASE_URL", srv.URL)

	ids := fetchKakiitLiveModels()
	if len(ids) != 3 {
		t.Fatalf("expected 3 models, got %d: %v", len(ids), ids)
	}
	// Sorted alphabetically like every other live fetcher.
	if ids[0] != "claude-sonnet-4-6" || ids[1] != "deepseek-v4-flash" || ids[2] != "gpt-5.6-luna" {
		t.Fatalf("unexpected ids: %v", ids)
	}

	entry, ok := kakiitModelEntry("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected cached entry for claude-sonnet-4-6")
	}
	if entry.Protocol != "anthropic-messages" {
		t.Fatalf("protocol = %q, want anthropic-messages", entry.Protocol)
	}
	if entry.ContextLength != 200000 {
		t.Fatalf("context_length = %d, want 200000", entry.ContextLength)
	}
	if entry.Free {
		t.Fatal("claude entry must not be free")
	}
	if entry.Pricing.modelPricing(entry.Free).InputPerMillion <= 0 || entry.Pricing.modelPricing(entry.Free).OutputPerMillion <= 0 {
		t.Fatalf("pricing not parsed: %+v", entry.Pricing)
	}

	freeEntry, _ := kakiitModelEntry("gpt-5.6-luna")
	if !freeEntry.Free {
		t.Fatal("gpt-5.6-luna should carry x_kakiit_free")
	}
	if freeEntry.Protocol != "openai-responses" {
		t.Fatalf("protocol = %q, want openai-responses", freeEntry.Protocol)
	}

	// TTL: a second call must not refetch.
	before := fetches
	if again := fetchKakiitLiveModels(); len(again) != 3 {
		t.Fatalf("second fetch returned %v", again)
	}
	if fetches != before {
		t.Fatalf("TTL cache miss: refetched (%d → %d)", before, fetches)
	}

	// The protocol helper Task 3 routes on reads the same cache.
	if p, ok := kakiitModelProtocol("claude-sonnet-4-6"); !ok || p != "anthropic-messages" {
		t.Fatalf("kakiitModelProtocol = %q, %v; want anthropic-messages, true", p, ok)
	}
}

// TestKakiitFreeFlagSurfacesAsZeroPriceInPicker pins the free flag's picker
// meaning: a free model reports zero prices through the same pricing shape
// the picker/usage ledger consumes.
func TestKakiitFreeFlagSurfacesAsZeroPriceInPicker(t *testing.T) {
	resetKakiitCache(t)
	seedKakiitCacheForTest(t, []kakiitModelInfo{
		{ID: "gpt-5.6-luna", Protocol: "openai-responses", Free: true, ContextLength: 400000},
	})

	if !kakiitModelFree("gpt-5.6-luna") {
		t.Fatal("free flag not surfaced")
	}
	pricing, ok := KakiitModelCost("kakiit/gpt-5.6-luna")
	if !ok {
		t.Fatal("expected pricing lookup for free kakiit model")
	}
	if pricing.InputPerMillion != 0 || pricing.OutputPerMillion != 0 {
		t.Fatalf("free model must surface zero prices, got %+v", pricing)
	}
	if kakiitModelFree("claude-sonnet-4-6") {
		t.Fatal("non-free model reported free")
	}
}

// TestKakiitFetchFailureYieldsEmptyListAndDebugLine pins failure behavior: an
// unreachable gateway or malformed body yields an empty (nil) list plus a
// debug line — never a panic, and never a models.dev fallback for kakiit ids
// (kakiit is absent from the snapshot, so any non-empty result here would be
// evidence of a wrong fallback).
func TestKakiitFetchFailureYieldsEmptyListAndDebugLineNotStaleFallback(t *testing.T) {
	resetKakiitCache(t)
	old := providers["kakiit"]
	providers["kakiit"] = providerInfo{envKey: "KAKIIT_API_KEY", baseURL: "http://127.0.0.1:1"}
	defer func() { providers["kakiit"] = old }()
	t.Setenv("KAKIIT_API_KEY", "kk-test-key")
	t.Setenv("KAKIIT_BASE_URL", "http://127.0.0.1:1")

	var debugLines []string
	restoreDebug := stubEmitDebug(func(kind, msg string) {
		if strings.Contains(msg, "kakiit") {
			debugLines = append(debugLines, msg)
		}
	})
	defer restoreDebug()

	if got := fetchKakiitLiveModels(); got != nil {
		t.Fatalf("expected nil list on fetch failure, got %v", got)
	}
	if len(debugLines) == 0 {
		t.Fatal("expected a kakiit debug line on fetch failure")
	}

	// The provider list for the picker degrades to empty, not to models.dev.
	if got := providerModelsFromRegistry("kakiit", true); len(got) != 0 {
		t.Fatalf("kakiit picker list must be empty on fetch failure, got %v", got)
	}
}

// TestKakiitRevokedKeyYieldsAuthErrorNotSilentEmptyList pins that an HTTP 401
// from the gateway is recorded as a typed auth error (surfaced to callers) and
// NOT silently swallowed as "no models".
func TestKakiitRevokedKeyYieldsAuthErrorNotSilentEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer srv.Close()

	resetKakiitCache(t)
	old := providers["kakiit"]
	providers["kakiit"] = providerInfo{envKey: "KAKIIT_API_KEY", baseURL: srv.URL}
	defer func() { providers["kakiit"] = old }()
	t.Setenv("KAKIIT_API_KEY", "kk-revoked")
	t.Setenv("KAKIIT_BASE_URL", srv.URL)

	if got := fetchKakiitLiveModels(); got != nil {
		t.Fatalf("expected nil list on 401, got %v", got)
	}
	if err := KakiitModelsAuthError(); err == nil {
		t.Fatal("401 must surface as a typed auth error, not a silent empty list")
	}
}

// TestKakiitProviderModelsMergesLiveIntoPicker pins the picker path:
// ProviderModels("kakiit") serves the live list prefixed kakiit/.
func TestKakiitProviderModelsServeLiveListInPicker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kakiitModelsFixture))
	}))
	defer srv.Close()

	resetKakiitCache(t)
	old := providers["kakiit"]
	providers["kakiit"] = providerInfo{envKey: "KAKIIT_API_KEY", baseURL: srv.URL}
	defer func() { providers["kakiit"] = old }()
	t.Setenv("KAKIIT_API_KEY", "kk-test-key")
	t.Setenv("KAKIIT_BASE_URL", srv.URL)

	got := providerModelsFromRegistry("kakiit", true)
	if len(got) != 3 {
		t.Fatalf("expected 3 kakiit/ prefixed models in the picker, got %d: %v", len(got), got)
	}
	for _, id := range got {
		if !strings.HasPrefix(id, "kakiit/") {
			t.Fatalf("picker id %q must be kakiit/-prefixed", id)
		}
	}
}

// TestKakiitModelJSONRoundTrip guards the fixture parse against shape drift:
// the exact vendor-extension names the gateway emits must unmarshal.
func TestKakiitModelJSONRoundTrip(t *testing.T) {
	var resp struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(kakiitModelsFixture), &resp); err != nil {
		t.Fatalf("fixture JSON invalid: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("fixture must carry 3 models, got %d", len(resp.Data))
	}
	var first kakiitModelInfo
	if err := json.Unmarshal(resp.Data[0], &first); err != nil {
		t.Fatalf("vendor-extension shape drifted: %v", err)
	}
	if first.Protocol == "" || first.ContextLength == 0 {
		t.Fatalf("protocol/context lost in parse: %+v", first)
	}
}

// seedKakiitCacheForTest installs parsed entries directly into the live cache
// (no network), for tests that exercise downstream consumers only.
func seedKakiitCacheForTest(t *testing.T, entries []kakiitModelInfo) {
	t.Helper()
	kakiitLiveData.mu.Lock()
	kakiitLiveData.models = entries
	kakiitLiveData.lastFetch = time.Now()
	kakiitLiveData.mu.Unlock()
}

// unmarshalKakiitModelsForTest parses a /v1/models fixture body into entries.
func unmarshalKakiitModelsForTest(t *testing.T, body []byte) []kakiitModelInfo {
	t.Helper()
	var apiResp struct {
		Data []kakiitModelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	return apiResp.Data
}

// stubEmitDebug captures debug lines for the duration of a test (the package's
// DebugAppend override pattern) and returns a restore func.
func stubEmitDebug(fn func(kind, msg string)) func() {
	prev := DebugAppend
	DebugAppend = fn
	return func() { DebugAppend = prev }
}
