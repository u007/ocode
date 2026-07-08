package agent

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/models"
	"github.com/u007/ocode/internal/pricing"
)

//go:embed models-snapshot.json
var modelsSnapshotData []byte

const (
	modelsDevURL    = "https://models.dev/api.json"
	modelsCacheTTL  = 5 * time.Minute
	modelsCacheFile = "models.json"
	envModelsPath   = "OPENCODE_MODELS_PATH"
)

type modelLimit struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

// modelCost holds per-million-token USD prices as published by models.dev.
type modelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type modelModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type modelEntry struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Family      string          `json:"family"`
	Attachment  bool            `json:"attachment"`
	Reasoning   bool            `json:"reasoning"`
	ToolCall    bool            `json:"tool_call"`
	Temperature bool            `json:"temperature"`
	Knowledge   string          `json:"knowledge"`
	ReleaseDate string          `json:"release_date"`
	LastUpdated string          `json:"last_updated"`
	OpenWeights bool            `json:"open_weights"`
	Modalities  modelModalities `json:"modalities"`
	Limit       modelLimit      `json:"limit"`
	Cost        modelCost       `json:"cost"`
}

func init() {
	// Make models.dev pricing the primary source for every pricing.Lookup
	// caller (spend calc, usage report, model picker). No import cycle: the
	// pricing package stores the callback, it does not import agent.
	pricing.RegisterRegistry(ModelCost)
}

type providerEntry struct {
	ID     string                `json:"id"`
	Models map[string]modelEntry `json:"models"`
}

type modelsRegistry struct {
	mu        sync.RWMutex
	data      map[string]providerEntry
	fetchedAt time.Time
}

var registry = &modelsRegistry{}

func cachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "opencode", modelsCacheFile), nil
	}
	return filepath.Join(home, ".config", "opencode", modelsCacheFile), nil
}

func loadCache() (map[string]providerEntry, time.Time, error) {
	path, err := cachePath()
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var parsed map[string]providerEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, time.Time{}, err
	}
	return parsed, info.ModTime(), nil
}

func writeCache(data map[string]providerEntry) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// fetchRemoteClient is the HTTP client used by fetchRemote. Tests can swap
// it for an httptest-backed client. Zero value uses a 10s-timeout default.
var fetchRemoteClient = &http.Client{Timeout: 10 * time.Second}

func fetchRemote() (map[string]providerEntry, error) {
	resp, err := fetchRemoteClient.Get(modelsDevURL)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("models.dev returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read models.dev: %w", err)
	}
	var parsed map[string]providerEntry
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse models.dev: %w", err)
	}
	return parsed, nil
}

func loadFromEnvPath() (map[string]providerEntry, bool) {
	path := os.Getenv(envModelsPath)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("%s read failed: %v", envModelsPath, err))
		return nil, false
	}
	var parsed map[string]providerEntry
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed) == 0 {
		emitDebug("AGENT", fmt.Sprintf("%s parse failed: %v", envModelsPath, err))
		return nil, false
	}
	return parsed, true
}

func loadFromSnapshot() (map[string]providerEntry, bool) {
	var parsed map[string]providerEntry
	if err := json.Unmarshal(modelsSnapshotData, &parsed); err != nil || len(parsed) == 0 {
		return nil, false
	}
	return parsed, true
}

func loadRegistry() map[string]providerEntry {
	registry.mu.RLock()
	if registry.data != nil && time.Since(registry.fetchedAt) < modelsCacheTTL {
		d := registry.data
		registry.mu.RUnlock()
		return d
	}
	registry.mu.RUnlock()

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if registry.data != nil && time.Since(registry.fetchedAt) < modelsCacheTTL {
		return registry.data
	}

	if data, ok := loadFromEnvPath(); ok {
		registry.data = data
		registry.fetchedAt = time.Now()
		return data
	}

	if data, ok := loadFromSnapshot(); ok {
		registry.data = data
		registry.fetchedAt = time.Now()
		return data
	}

	if cached, modTime, err := loadCache(); err == nil && time.Since(modTime) < modelsCacheTTL {
		registry.data = cached
		registry.fetchedAt = modTime
		return cached
	}

	remote, err := fetchRemote()
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("models.dev fetch failed: %v", err))
		if cached, modTime, err := loadCache(); err == nil {
			registry.data = cached
			registry.fetchedAt = modTime
			return cached
		}
		return nil
	}

	if err := writeCache(remote); err != nil {
		emitDebug("AGENT", fmt.Sprintf("models.dev cache write failed: %v", err))
	}
	registry.data = remote
	registry.fetchedAt = time.Now()
	return remote
}

// PreloadRegistry fetches the models.dev registry in the background so it is
// warm before the first call to ModelWindow or ProviderModels.
func PreloadRegistry() {
	go loadRegistry()
}

// PreloadNovitaModels fetches Novita's live model list in the background so the
// context window / pricing / vision info for novita-ai models (which are not in
// the models.dev registry) is available as soon as the UI needs it — in
// particular the sidebar's "context used / max context" line. It is a no-op when
// no Novita credential is configured or a fetch is already cached.
func PreloadNovitaModels() {
	go fetchNovitaLiveModels()
}

// NovitaModelsLoaded reports whether the Novita live model cache has been
// populated (fetched successfully at least once). Safe for concurrent use.
func NovitaModelsLoaded() bool {
	novitaLiveData.mu.RLock()
	defer novitaLiveData.mu.RUnlock()
	return novitaLiveData.models != nil
}

// ForceRefreshRegistry synchronously fetches the models.dev registry and
// updates the in-memory cache, bypassing the 5-minute TTL. It does NOT bypass
// the OPENCODE_MODELS_PATH env var or the embedded snapshot in loadRegistry —
// the freshly-updated fetchedAt makes the TTL short-circuit in loadRegistry
// return the new data on subsequent calls, so AllProviderModels and friends
// see the refreshed list immediately.
//
// Returns the new data on success, or the existing cached data plus the
// error on failure (so the caller can decide whether to repopulate or surface
// the error). Returns (nil, err) only when both the remote fetch fails and
// there is no prior in-memory data to fall back on.
func ForceRefreshRegistry() (map[string]providerEntry, error) {
	remote, err := fetchRemote()
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("force refresh: models.dev fetch failed: %v", err))
		registry.mu.RLock()
		existing := registry.data
		registry.mu.RUnlock()
		if existing != nil {
			return existing, err
		}
		return nil, err
	}

	registry.mu.Lock()
	registry.data = remote
	registry.fetchedAt = time.Now()
	registry.mu.Unlock()

	if err := writeCache(remote); err != nil {
		emitDebug("AGENT", fmt.Sprintf("force refresh: cache write failed: %v", err))
	}
	return remote, nil
}

func registrySnapshotIfReady() map[string]providerEntry {
	if !registry.mu.TryRLock() {
		return nil
	}
	defer registry.mu.RUnlock()
	if registry.data == nil || time.Since(registry.fetchedAt) >= modelsCacheTTL {
		return nil
	}
	return registry.data
}

// RegistryReady reports whether the models.dev registry has been loaded and is
// not stale. Safe to call from any goroutine.
func RegistryReady() bool {
	return registrySnapshotIfReady() != nil
}

// ProviderModels returns model IDs for a provider from the loaded registry.
// It may refresh live model sources (OpenRouter, Novita) over the network — only
// call it from background goroutines, never the TUI main loop.
// Returns nil if the registry is not available.
func ProviderModels(provider string) []string {
	return providerModelsFromRegistry(provider, true)
}

// AllProviderModels returns opencode-style provider/model IDs for model pickers.
// It may refresh live model sources over the network — only call it from background
// goroutines (e.g. the async model-picker loader), never the TUI main loop.
// Returns nil if the registry is not available.
func AllProviderModels() []string {
	return allProviderModelsFromRegistry(true)
}

// AllProviderModelsCached returns the same list as AllProviderModels but never
// blocks on a network call: it only consults live caches that are already fresh
// (e.g. populated by the Init-time PreloadOpenRouterModels / async picker loader)
// and falls back to the snapshot otherwise. Safe to call on the TUI main loop
// (model-picker refresh, slash-command autocomplete).
func AllProviderModelsCached() []string {
	return allProviderModelsFromRegistry(false)
}

// ModelWindow returns the context window size for a given model ID in
// "provider/model" format (e.g. "openai/gpt-4o") or bare model name.
// It checks the models.dev registry first, then falls back to 0.
func ModelWindow(modelID string) int64 {
	if m, ok := modelEntryFor(modelID); ok {
		return m.Limit.Context
	}
	return 0
}

// ModelSupportsVision reports whether a model can accept image input. The
// models.dev registry (Modalities.Input containing "image") is authoritative
// when it knows the model. For models absent from the registry (cold cache,
// offline, or brand-new IDs) it falls back to the IsVisionModel heuristic,
// which fails open for the current model families so we never wrongly stub out
// images for a capable default like Claude Opus.
func ModelSupportsVision(modelID string) bool {
	if m, ok := modelEntryFor(modelID); ok {
		for _, in := range m.Modalities.Input {
			if in == "image" {
				return true
			}
		}
		// Registry knows this model and it lists no image input → text-only.
		return false
	}
	return IsVisionModel(modelID)
}

// modelEntryFor resolves a model ID in "provider/model" format (e.g.
// "deepseek/deepseek-v4-flash") or bare model name to its registry entry.
// When multiple providers list the same model, entries with non-zero costs
// are preferred (some providers list the model with zero/cost-to-follow).
func modelEntryFor(modelID string) (modelEntry, bool) {
	data := loadRegistry()
	if data == nil {
		return modelEntry{}, false
	}

	// Try "provider/model" format first
	if provider, model, ok := splitModelID(modelID); ok {
		if entry, ok := data[provider]; ok {
			if m, ok := entry.Models[model]; ok {
				return m, true
			}
		}
		// Check Novita live cache for novita-ai/ prefixed models.
		if provider == "novita-ai" {
			if m, ok := novitaLiveModelEntry(model); ok {
				return m, true
			}
		}
		// Check OpenRouter live cache for openrouter/ prefixed models.
		if provider == "openrouter" {
			if m, ok := openRouterLiveModelEntry(model); ok {
				return m, true
			}
		}
		// Routing-prefixed ids (e.g. "opencode-go/deepseek-v4-flash") whose
		// provider segment isn't a real models.dev provider — match the model
		// segment across all providers.
		if m, ok := bestPricedEntry(data, model); ok {
			return m, ok
		}
	}

	// Try bare model name — search all providers, prefer non-zero cost
	if m, ok := bestPricedEntry(data, modelID); ok {
		return m, ok
	}

	// Fall back to Novita live cache for bare model names.
	if m, ok := novitaLiveModelEntry(modelID); ok {
		return m, ok
	}

	// Fall back to OpenRouter live cache for bare model names.
	if m, ok := openRouterLiveModelEntry(modelID); ok {
		return m, ok
	}

	return modelEntry{}, false
}

// novitaLiveModelEntry returns a model entry from the Novita live cache.
// Returns false if the cache is empty or the model is not found.
func novitaLiveModelEntry(name string) (modelEntry, bool) {
	novitaLiveData.mu.RLock()
	defer novitaLiveData.mu.RUnlock()
	if novitaLiveData.models == nil {
		return modelEntry{}, false
	}
	m, ok := novitaLiveData.models[name]
	return m, ok
}

// bestPricedEntry searches all providers for a model by bare name and returns
// the first entry that has non-zero costs. If every match has zero costs it
// returns the first match anyway (so callers see "found but zero cost").
func bestPricedEntry(data map[string]providerEntry, modelID string) (modelEntry, bool) {
	var fallback modelEntry
	var haveFallback bool
	for _, entry := range data {
		if m, ok := entry.Models[modelID]; ok {
			if m.Cost.Input != 0 || m.Cost.Output != 0 || m.Cost.CacheRead != 0 {
				return m, true
			}
			if !haveFallback {
				fallback = m
				haveFallback = true
			}
		}
	}
	if haveFallback {
		return fallback, true
	}
	return modelEntry{}, false
}

// ModelCost returns models.dev pricing (USD per million tokens) for a model ID
// in "provider/model" or bare form. Returns false when the model is unknown or
// the registry carries no price for it.
func ModelCost(modelID string) (pricing.ModelPricing, bool) {
	m, ok := modelEntryFor(modelID)
	if !ok {
		emitDebug("AGENT", fmt.Sprintf("ModelCost: %q not found in registry (model may be custom or unsupported; cost defaults to $0)", modelID))
		return pricing.ModelPricing{}, false
	}
	if m.Cost.Input == 0 && m.Cost.Output == 0 && m.Cost.CacheRead == 0 {
		emitDebug("AGENT", fmt.Sprintf("ModelCost: %q found but has zero cost in registry", modelID))
		return pricing.ModelPricing{}, false
	}
	return pricing.ModelPricing{
		InputPerMillion:      m.Cost.Input,
		OutputPerMillion:     m.Cost.Output,
		CacheReadPerMillion:  m.Cost.CacheRead,
		CacheWritePerMillion: m.Cost.CacheWrite,
	}, true
}

func splitModelID(id string) (provider, model string, ok bool) {
	for i := 0; i < len(id); i++ {
		if id[i] == '/' {
			return id[:i], id[i+1:], true
		}
	}
	return "", "", false
}

// allProviderModelsFromRegistry returns opencode-style "provider/model" IDs for the
// model picker. When refresh is true it may hit the network to refresh live model
// sources (used by background goroutines, e.g. the async model-picker loader, where
// a brief blocking fetch is acceptable). When refresh is false it only consults live
// caches that are already fresh, so it is safe to call on the TUI main loop — it will
// never block on a network call.
func allProviderModelsFromRegistry(refresh bool) []string {
	ids := make([]string, 0)
	var requestyRegistryFallback []string
	if data := loadRegistry(); data != nil {
		for provider, entry := range data {
			if provider == "lmstudio" || provider == "novita-ai" {
				continue // handled via live API fetch below
			}
			if provider == models.RequestyProvider {
				// Save registry list as fallback; prefer live API fetch.
				for model := range entry.Models {
					requestyRegistryFallback = append(requestyRegistryFallback, provider+"/"+model)
				}
				continue
			}
			for model := range entry.Models {
				ids = append(ids, provider+"/"+model)
			}
		}
	}
	// LM Studio live models
	for _, m := range fetchLMStudioModels() {
		ids = append(ids, "lmstudio/"+m)
	}
	// Requesty live models — fall back to registry snapshot if API unreachable.
	requestyLive := fetchRequestyLiveModels()
	if len(requestyLive) > 0 {
		for _, m := range requestyLive {
			ids = append(ids, "requesty/"+m)
		}
	} else {
		ids = append(ids, requestyRegistryFallback...)
	}
	// Novita AI live models — fall back to registry snapshot if API unreachable.
	novitaLive := fetchNovitaLiveModels()
	if len(novitaLive) > 0 {
		for m := range novitaLive {
			ids = append(ids, "novita-ai/"+m)
		}
	} else if data := loadRegistry(); data != nil {
		if entry, ok := data["novita-ai"]; ok {
			for m := range entry.Models {
				ids = append(ids, "novita-ai/"+m)
			}
		}
	}
	// OpenRouter live models — supplement the snapshot so free/colon-variant
	// models (e.g. "openrouter/tencent/hy3:free") appear in the picker even when
	// absent from the models.dev snapshot. Dedup against the snapshot list.
	// When refresh is false (main-loop callers) only read the cache if it is still
	// fresh, so we never block the UI on a network fetch. The async picker loader
	// passes refresh=true and refreshes the cache in a background goroutine.
	if refresh || openRouterCacheFresh() {
		if openRouterLive := fetchOpenRouterLiveModels(); len(openRouterLive) > 0 {
			have := make(map[string]bool, len(ids))
			for _, id := range ids {
				have[id] = true
			}
			for key := range openRouterLive {
				id := "openrouter/" + key
				if !have[id] {
					ids = append(ids, id)
					have[id] = true
				}
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return ids
}

// providerModelsFromRegistry returns model IDs for a single provider. The refresh
// flag behaves as in allProviderModelsFromRegistry: when false it only reads an
// already-fresh live cache (safe for the TUI main loop); when true it may refresh
// live sources over the network.
func providerModelsFromRegistry(provider string, refresh bool) []string {
	if provider == "lmstudio" {
		return fetchLMStudioModels()
	}
	if provider == models.RequestyProvider {
		live := fetchRequestyLiveModels()
		if len(live) > 0 {
			sort.Strings(live)
			return live
		}
		return providerModelsFromSnapshot(provider)
	}
	if provider == "novita-ai" {
		live := fetchNovitaLiveModels()
		if len(live) > 0 {
			ids := make([]string, 0, len(live))
			for id := range live {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			return ids
		}
		return providerModelsFromSnapshot(provider)
	}
	if provider == "openrouter" {
		// On the main loop (refresh=false) only consult the cache when it is still
		// fresh so we never block on a network re-fetch; otherwise refresh it.
		if refresh || openRouterCacheFresh() {
			live := fetchOpenRouterLiveModels()
			if len(live) > 0 {
				ids := make([]string, 0, len(live))
				for id := range live {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				return ids
			}
		}
		return providerModelsFromSnapshot(provider)
	}
	return providerModelsFromSnapshot(provider)
}

// providerModelsFromSnapshot loads models for the given provider from the
// models.dev snapshot. Returns nil if the registry is unavailable or the
// provider is unknown.
func providerModelsFromSnapshot(provider string) []string {
	data := loadRegistry()
	if data == nil {
		return nil
	}
	entry, ok := data[provider]
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(entry.Models))
	for id := range entry.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// LMStudioResult holds the outcome of a live LM Studio model fetch.
type LMStudioResult struct {
	Models      []string
	NeedsAPIKey bool // true when LM Studio returned 401
}

func FetchLMStudioModels() LMStudioResult {
	base := os.Getenv("LMSTUDIO_BASE_URL")
	if base == "" {
		base = "http://localhost:1234/v1"
	}
	base = normalizeLMStudioBaseURL(base)

	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodGet, base+"/models", nil)
	if err != nil {
		return LMStudioResult{}
	}
	if key := auth.ResolveKey("lmstudio"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return LMStudioResult{}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return LMStudioResult{NeedsAPIKey: true}
	}
	if resp.StatusCode != http.StatusOK {
		return LMStudioResult{}
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return LMStudioResult{}
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return LMStudioResult{}
	}
	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return LMStudioResult{Models: ids}
}

func fetchLMStudioModels() []string {
	return FetchLMStudioModels().Models
}

// OcrModelsFromLMStudio returns LM Studio model IDs that look like OCR/vision
// models. Uses the same expanded keyword matching as the openai-compat backend.
// Returns nil silently if LM Studio is not running.
// Deprecated: Use ocr.Get("openai-compat").ListModels() instead.
func OcrModelsFromLMStudio() []string {
	all := FetchLMStudioModels().Models
	if len(all) == 0 {
		return nil
	}
	var ocrModels []string
	for _, m := range all {
		lower := strings.ToLower(m)
		if ocrKeywordMatch(lower) {
			ocrModels = append(ocrModels, m)
		}
	}
	// If no OCR-specific models found but LM Studio is running, return all models
	// so the user can still select one.
	if len(ocrModels) == 0 {
		return all
	}
	return ocrModels
}

// ocrKeywordMatch returns true if the lower-cased string matches known
// OCR or vision model name patterns.
func ocrKeywordMatch(lower string) bool {
	keywords := []string{
		"ocr", "paddle", "deepseek", "vision", "caption",
		"moondream", "florence", "cogvlm", "pixtral", "paligemma",
		"minicpm", "internvl", "llava", "clip", "phi",
		"gemma", "qwen",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// Check for "vl" suffix/prefix patterns (e.g. "qwen2-vl", "internvl")
	if strings.Contains(lower, "vl") {
		return true
	}
	// Check for "vlm" (vision language model)
	if strings.Contains(lower, "vlm") {
		return true
	}
	return false
}

// fetchRequestyLiveModels fetches models from the Requesty API and returns
// the raw model IDs as returned by the API (e.g. "nvidia/nemotron-...",
// "anthropic/claude-sonnet-4-6"). Returns nil silently if the API is not
// reachable or REQUESTY_API_KEY is not set.
func fetchRequestyLiveModels() []string {
	apiKey := os.Getenv("REQUESTY_API_KEY")
	entries, err := models.FetchRequestyModels(apiKey)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !seen[e.ID] {
			ids = append(ids, e.ID)
			seen[e.ID] = true
		}
	}
	return ids
}

const novitaCacheTTL = 30 * time.Second

// novitaLiveData caches live-fetched Novita AI models (with pricing/context)
// so that ModelCost can resolve cost info for models fetched from Novita's API.
var novitaLiveData struct {
	mu        sync.RWMutex
	models    map[string]modelEntry // model name → entry with cost/context
	lastFetch time.Time             // last successful fetch
}

// fetchNovitaLiveModels fetches the model list from Novita's OpenAI-compatible
// API and returns the model IDs. Returns nil silently if:
//   - NOVITA_API_KEY env var is not set AND no stored credential exists
//   - The API is unreachable or returns an error
//
// The returned map contains modelName → modelEntry with pricing converted from
// Novita's internal units (1/10000th of $ per M tokens) to USD per M tokens.
func fetchNovitaLiveModels() map[string]modelEntry {
	// Return cached models if the cache is still fresh.
	novitaLiveData.mu.RLock()
	if novitaLiveData.models != nil && time.Since(novitaLiveData.lastFetch) < novitaCacheTTL {
		models := novitaLiveData.models
		novitaLiveData.mu.RUnlock()
		return models
	}
	novitaLiveData.mu.RUnlock()

	apiKey := os.Getenv("NOVITA_API_KEY")
	if apiKey == "" {
		apiKey = auth.ResolveKey("novita-ai")
	}
	if apiKey == "" {
		return nil
	}

	// Use the v3 model listing endpoint which returns rich data with pricing
	// and context size. Note: this is different from the chat-completions base
	// URL (https://api.novita.ai/openai/v1), because Novita serves model
	// metadata under a different path.
	modelsURL := "https://api.novita.ai/v3/openai/v1/models"
	// Check config for a custom model-list URL override.
	if cred, ok := auth.Get("novita-ai"); ok && cred.BaseURL != "" {
		modelsURL = strings.TrimRight(cred.BaseURL, "/") + "/models"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil
	}
	var apiResp struct {
		Data []struct {
			ID                  string   `json:"id"`
			ContextSize         int64    `json:"context_size"`
			MaxOutputTokens     int64    `json:"max_output_tokens"`
			InputTokenPricePerM float64  `json:"input_token_price_per_m"`
			OutputTokenPricePer float64  `json:"output_token_price_per_m"`
			Features            []string `json:"features"`
			InputModalities     []string `json:"input_modalities"`
			OutputModalities    []string `json:"output_modalities"`
			Pricing             struct {
				Prompt struct {
					PricePerM float64 `json:"price_per_m"`
				} `json:"prompt"`
				Completion struct {
					PricePerM float64 `json:"price_per_m"`
				} `json:"completion"`
				InputCacheRead *struct {
					PricePerM float64 `json:"price_per_m"`
				} `json:"input_cache_read"`
			} `json:"pricing"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil
	}

	models := make(map[string]modelEntry, len(apiResp.Data))
	for _, m := range apiResp.Data {
		if m.ID == "" {
			continue
		}

		// Convert pricing: Novita API returns units of 1/10000th of $ per M tokens.
		// E.g., 2690 → $0.269/M, 4000 → $0.400/M.
		priceInput := m.InputTokenPricePerM / 10000.0
		priceOutput := m.OutputTokenPricePer / 10000.0
		var priceCacheRead float64
		if m.Pricing.InputCacheRead != nil {
			priceCacheRead = m.Pricing.InputCacheRead.PricePerM / 10000.0
		}

		// Determine features
		hasReasoning := false
		hasToolCall := false
		for _, f := range m.Features {
			switch f {
			case "reasoning":
				hasReasoning = true
			case "function-calling":
				hasToolCall = true
			}
		}

		modalities := modelModalities{
			Input:  m.InputModalities,
			Output: m.OutputModalities,
		}
		if len(modalities.Input) == 0 {
			modalities.Input = []string{"text"}
		}
		if len(modalities.Output) == 0 {
			modalities.Output = []string{"text"}
		}

		models[m.ID] = modelEntry{
			ID:          m.ID,
			Name:        m.ID,
			ToolCall:    hasToolCall,
			Reasoning:   hasReasoning,
			Temperature: true,
			Limit: modelLimit{
				Context: m.ContextSize,
				Output:  m.MaxOutputTokens,
			},
			Cost: modelCost{
				Input:     priceInput,
				Output:    priceOutput,
				CacheRead: priceCacheRead,
			},
			Modalities: modalities,
		}
	}

	// Update the cache
	novitaLiveData.mu.Lock()
	novitaLiveData.models = models
	novitaLiveData.lastFetch = time.Now()
	novitaLiveData.mu.Unlock()

	return models
}

const openRouterCacheTTL = 30 * time.Second

// openRouterLiveData caches live-fetched OpenRouter models (with pricing/context)
// so that ModelWindow / ModelCost can resolve metadata for models that are absent
// from the models.dev registry — in particular the free and colon-variant models
// OpenRouter publishes (e.g. "tencent/hy3:free", which models.dev lists as
// "tencent/hy3-free" or omits entirely). The OpenRouter models endpoint is public
// and requires no API key.
var openRouterLiveData struct {
	mu        sync.RWMutex
	models    map[string]modelEntry // bare model id (after openrouter/) → entry
	lastFetch time.Time             // last successful fetch
}

// fetchOpenRouterLiveModels fetches the model list from OpenRouter's public API and
// returns a map of bare model id → modelEntry (cost converted to USD per million
// tokens, context window carried over). Returns nil silently if the API is
// unreachable or returns an error so callers degrade to the 0/missing default
// rather than blocking on a failed network call.
func fetchOpenRouterLiveModels() map[string]modelEntry {
	// Return cached models if the cache is still fresh.
	openRouterLiveData.mu.RLock()
	if openRouterLiveData.models != nil && time.Since(openRouterLiveData.lastFetch) < openRouterCacheTTL {
		models := openRouterLiveData.models
		openRouterLiveData.mu.RUnlock()
		return models
	}
	openRouterLiveData.mu.RUnlock()

	entries, err := models.FetchAll()
	if err != nil || len(entries) == 0 {
		return nil
	}

	out := make(map[string]modelEntry, len(entries))
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		// OpenRouter IDs are already "provider/model" style (e.g.
		// "tencent/hy3:free"); the ocode model id prefixes them with "openrouter/".
		// Key the cache by the bare id (after any openrouter/ prefix) so lookups
		// from modelEntryFor match both forms.
		key := strings.TrimPrefix(e.ID, "openrouter/")
		entry := modelEntry{
			ID:          e.ID,
			Name:        e.Name,
			ToolCall:    e.Coding,
			Temperature: true,
			Limit: modelLimit{
				Context: int64(e.ContextLen),
			},
			Cost: modelCost{
				Input:  e.Pricing.Prompt * 1_000_000,
				Output: e.Pricing.Comp * 1_000_000,
			},
		}
		if e.Vision {
			entry.Modalities.Input = []string{"image"}
		}
		out[key] = entry
		if key != e.ID {
			out[e.ID] = entry
		}
	}

	openRouterLiveData.mu.Lock()
	openRouterLiveData.models = out
	openRouterLiveData.lastFetch = time.Now()
	openRouterLiveData.mu.Unlock()

	return out
}

// openRouterLiveModelEntry returns a model entry from the OpenRouter live cache.
// Accepts either a bare model id ("tencent/hy3:free") or a fully-qualified
// "openrouter/..." id. Returns false if the cache is empty or the model is
// not found.
func openRouterLiveModelEntry(name string) (modelEntry, bool) {
	openRouterLiveData.mu.RLock()
	defer openRouterLiveData.mu.RUnlock()
	if openRouterLiveData.models == nil {
		return modelEntry{}, false
	}
	if m, ok := openRouterLiveData.models[name]; ok {
		return m, true
	}
	if trimmed := strings.TrimPrefix(name, "openrouter/"); trimmed != name {
		if m, ok := openRouterLiveData.models[trimmed]; ok {
			return m, true
		}
	}
	return modelEntry{}, false
}

// PreloadOpenRouterModels fetches OpenRouter's live model list in the background
// so the context window / pricing / vision info for openrouter models (which are
// frequently absent from or differently-named in the models.dev registry) is
// available as soon as the UI needs it — in particular the sidebar's "context used
// / max context" line. It is a no-op once the cache is populated, and degrades
// gracefully when the network is unavailable.
func PreloadOpenRouterModels() {
	go fetchOpenRouterLiveModels()
}

// OpenRouterModelsLoaded reports whether the OpenRouter live model cache has been
// populated (fetched successfully at least once). Safe for concurrent use.
func OpenRouterModelsLoaded() bool {
	openRouterLiveData.mu.RLock()
	defer openRouterLiveData.mu.RUnlock()
	return openRouterLiveData.models != nil
}

// openRouterCacheFresh reports whether the OpenRouter live cache is populated AND
// still within its TTL. Unlike OpenRouterModelsLoaded (which is true forever once
// the cache has been populated at least once), this also reflects staleness so
// callers on the TUI main loop can read cached metadata WITHOUT triggering a
// blocking network re-fetch. Safe for concurrent use.
func openRouterCacheFresh() bool {
	openRouterLiveData.mu.RLock()
	defer openRouterLiveData.mu.RUnlock()
	return openRouterLiveData.models != nil && time.Since(openRouterLiveData.lastFetch) < openRouterCacheTTL
}

// AllProviders returns provider IDs known to the registry (or empty if unavailable).

func AllProviders() []string {
	data := loadRegistry()
	if data == nil {
		return nil
	}
	ids := make([]string, 0, len(data))
	for id := range data {
		ids = append(ids, id)
	}
	// requesty is always available via live API fetch.
	ids = append(ids, models.RequestyProvider)
	// novita-ai is available when credentials are configured (live API fetch).
	if os.Getenv("NOVITA_API_KEY") != "" || auth.ResolveKey("novita-ai") != "" {
		ids = append(ids, "novita-ai")
	}
	sort.Strings(ids)
	return ids
}
