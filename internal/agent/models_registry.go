package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

const (
	modelsDevURL    = "https://models.dev/api.json"
	modelsCacheTTL  = 24 * time.Hour
	modelsCacheFile = "models.json"
)

type modelEntry struct {
	ID string `json:"id"`
}

type providerEntry struct {
	ID     string                `json:"id"`
	Models map[string]modelEntry `json:"models"`
}

type modelsRegistry struct {
	mu       sync.RWMutex
	data     map[string]providerEntry
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

func fetchRemote() (map[string]providerEntry, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(modelsDevURL)
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

	if cached, modTime, err := loadCache(); err == nil && time.Since(modTime) < modelsCacheTTL {
		registry.data = cached
		registry.fetchedAt = modTime
		return cached
	}

	remote, err := fetchRemote()
	if err != nil {
		fmt.Fprintf(os.Stderr, "models.dev fetch failed: %v\n", err)
		if cached, modTime, err := loadCache(); err == nil {
			registry.data = cached
			registry.fetchedAt = modTime
			return cached
		}
		return nil
	}

	if err := writeCache(remote); err != nil {
		fmt.Fprintf(os.Stderr, "models.dev cache write failed: %v\n", err)
	}
	registry.data = remote
	registry.fetchedAt = time.Now()
	return remote
}

// ProviderModels returns model IDs for a provider from models.dev, falling
// back to a small hardcoded list on failure.
func ProviderModels(provider string) []string {
	if models := providerModelsFromRegistry(provider); len(models) > 0 {
		return models
	}
	return fallbackProviderModels(provider)
}

func providerModelsFromRegistry(provider string) []string {
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

func fallbackProviderModels(provider string) []string {
	switch provider {
	case "anthropic":
		return []string{"claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307"}
	case "google":
		return []string{"gemini-1.5-pro", "gemini-1.5-flash"}
	default:
		return []string{"gpt-4o", "gpt-4o-mini", "o1-preview"}
	}
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
	sort.Strings(ids)
	return ids
}
