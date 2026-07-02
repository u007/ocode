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
// Returns nil if the registry is not available.
func ProviderModels(provider string) []string {
	return providerModelsFromRegistry(provider)
}

// AllProviderModels returns opencode-style provider/model IDs for model pickers.
// Returns nil if the registry is not available.
func AllProviderModels() []string {
	return allProviderModelsFromRegistry()
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

	return modelEntry{}, false
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

func allProviderModelsFromRegistry() []string {
	ids := make([]string, 0)
	var requestyRegistryFallback []string
	if data := loadRegistry(); data != nil {
		for provider, entry := range data {
			if provider == "lmstudio" {
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
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return ids
}

func providerModelsFromRegistry(provider string) []string {
	if provider == "lmstudio" {
		return fetchLMStudioModels()
	}
	if provider == models.RequestyProvider {
		live := fetchRequestyLiveModels()
		if len(live) > 0 {
			sort.Strings(live)
			return live
		}
		// Fall back to registry snapshot.
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

// FetchLMStudioModels queries the local LM Studio API for available models.
// Returns an empty result silently if LM Studio is not running.
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
// models (matching names containing "ocr", "paddle", "vision", "read", "caption").
// Returns nil silently if LM Studio is not running.
func OcrModelsFromLMStudio() []string {
	all := FetchLMStudioModels().Models
	if len(all) == 0 {
		return nil
	}
	var ocrModels []string
	for _, m := range all {
		lower := strings.ToLower(m)
		if strings.Contains(lower, "ocr") || strings.Contains(lower, "paddle") ||
			strings.Contains(lower, "vision") || strings.Contains(lower, "caption") {
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
	sort.Strings(ids)
	return ids
}
