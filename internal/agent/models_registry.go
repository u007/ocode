package agent

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	modelsDevURL        = "https://models.dev/api.json"
	modelsCacheTTL      = 5 * time.Minute
	modelsCacheFile     = "models.json"
	envModelsPath       = "OPENCODE_MODELS_PATH"
	orcaRouterModelsURL = "https://api.orcarouter.ai/v1/models"
)

// registryLockWait bounds how long a registry load waits on the cross-process
// models.dev refresh lock before giving up and using stale data. Kept short so
// a slow refresh in another ocode process can't stall lookups; flock
// auto-releases if the holder crashes. A var (not a const) so tests can shrink
// the wait.
var registryLockWait = 5 * time.Second

// errRegistryLockHeld reports that another ocode process currently holds the
// models.dev refresh lock, so this process skips its own fetch and falls back
// to the data it already has rather than fetching concurrently.
var errRegistryLockHeld = errors.New("models.dev refresh lock held by another process")

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
	mu               sync.RWMutex
	data             map[string]providerEntry
	fetchedAt        time.Time
	lastFetchAttempt time.Time
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
	// Current format: the registryFile wrapper with tracking metadata. Older
	// caches written before the wrapper existed are a bare provider map — keep
	// loading those so a mixed fleet of binaries can share the file.
	var wrapped registryFile
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Providers) > 0 {
		return wrapped.Providers, info.ModTime(), nil
	}
	var legacy map[string]providerEntry
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, time.Time{}, err
	}
	return legacy, info.ModTime(), nil
}

// writeCache atomically replaces the on-disk models cache with data plus
// tracking metadata: when it was fetched, what triggered the fetch (source),
// how long the fetch took, a content hash, and the model count. The file's
// mtime doubles as the cross-process "last updated" signal.
func writeCache(data map[string]providerEntry, source string, duration time.Duration) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(registryFile{
		FetchedAt:       time.Now().UTC().Format(time.RFC3339),
		FetchSource:     source,
		FetchDurationMS: duration.Milliseconds(),
		ContentHash:     contentHash(data),
		ModelCount:      countModels(data),
		Providers:       data,
	}, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file in the same directory, then rename over the target:
	// concurrent readers (other ocode processes doing a freshness check) see
	// either the old or the new file, never a partially written one.
	tmp, err := os.CreateTemp(filepath.Dir(path), modelsCacheFile+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// contentHash returns a sha256 hex digest of the canonical JSON encoding of
// the provider map, so any process can tell at a glance whether its in-memory
// copy matches the on-disk cache without a full compare.
func contentHash(data map[string]providerEntry) string {
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// countModels returns the total number of model entries across all providers.
func countModels(data map[string]providerEntry) int {
	n := 0
	for _, p := range data {
		n += len(p.Models)
	}
	return n
}

// cacheModTime returns the mtime of the on-disk models cache, which doubles as
// the cross-process "last updated" record. Returns ok=false when the cache
// does not exist or cannot be stat'd.
func cacheModTime() (time.Time, bool) {
	p, err := cachePath()
	if err != nil {
		return time.Time{}, false
	}
	info, err := os.Stat(p)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// ── models.dev refresh history ──────────────────────────────────────────────

const (
	// modelsHistoryFile is the bounded append-only audit log of models.dev
	// fetches, one JSON line per fetch attempt (success or failure), written
	// under the same cross-process refresh lock that serializes the fetches
	// themselves.
	modelsHistoryFile = "models-history.jsonl"
)

// modelsHistoryMaxLines caps the sidecar at the most recent entries (one per
// fetch, roughly one per 5-min TTL window per process). A var (not a const) so
// tests can shrink the cap.
var modelsHistoryMaxLines = 5000

// HistoryEntry is one line of the models.dev refresh audit log.
type HistoryEntry struct {
	// TS is the RFC3339 UTC time the fetch attempt happened.
	TS string `json:"ts"`
	// Source is what triggered the fetch: "auto" or "force".
	Source string `json:"source"`
	// ModelCount is the number of model entries in a successful fetch.
	ModelCount int `json:"model_count,omitempty"`
	// DurationMS is how long the models.dev request took.
	DurationMS int64 `json:"duration_ms,omitempty"`
	// Hash is the content hash of a successful fetch.
	Hash string `json:"hash,omitempty"`
	// Error is set on a failed fetch attempt.
	Error string `json:"error,omitempty"`
}

func historyPath() (string, error) {
	p, err := cachePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), modelsHistoryFile), nil
}

// appendHistory appends one entry to the bounded JSONL history and trims it to
// the newest modelsHistoryMaxLines entries. The caller must hold the
// cross-process models.dev refresh lock (as fetchRegistryWithLock does) so
// appends from different processes never interleave.
func appendHistory(e HistoryEntry) {
	path, err := historyPath()
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("models history path: %v", err))
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		emitDebug("AGENT", fmt.Sprintf("models history dir: %v", err))
		return
	}
	line, err := json.Marshal(e)
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("models history marshal: %v", err))
		return
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("models history open: %v", err))
		return
	}
	defer f.Close()

	var lines [][]byte
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, append([]byte(nil), sc.Bytes()...))
	}
	lines = append(lines, line)
	if len(lines) > modelsHistoryMaxLines {
		lines = lines[len(lines)-modelsHistoryMaxLines:]
	}

	if err := f.Truncate(0); err != nil {
		return
	}
	if _, err := f.Seek(0, 0); err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, l := range lines {
		w.Write(append(l, '\n'))
	}
	w.Flush()
}

// RegistryHistory returns the most recent limit refresh entries in
// chronological order (limit <= 0 returns all). Best-effort: malformed or
// partially written lines are skipped rather than failing, so it is safe to
// call while another process is appending.
func RegistryHistory(limit int) []HistoryEntry {
	path, err := historyPath()
	if err != nil {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []HistoryEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e HistoryEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue // skip torn/malformed lines
		}
		entries = append(entries, e)
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries
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

// registryFile is the on-disk/embedded form of the models registry — used both
// by the build-time snapshot (models-snapshot.json) and the runtime cache
// (models.json). It wraps the provider map with tracking metadata so any
// process can tell when a copy was produced, where it came from, and whether
// its content matches what a peer loaded.
type registryFile struct {
	// GeneratedAt is the snapshot's build time (written by
	// tools/gen-models-snapshot). The runtime treats the snapshot as fresh for
	// modelsCacheTTL after this stamp, then refreshes from models.dev.
	GeneratedAt string `json:"generated_at,omitempty"`
	// FetchedAt is when the runtime cache was last written after a models.dev
	// fetch — the explicit "last updated" answer for the cache file.
	FetchedAt string `json:"fetched_at,omitempty"`
	// FetchSource records what triggered the fetch: "auto" (loadRegistry TTL
	// refresh) or "force" (model-picker ctrl+r).
	FetchSource string `json:"fetch_source,omitempty"`
	// FetchDurationMS is how long the models.dev fetch took.
	FetchDurationMS int64 `json:"fetch_duration_ms,omitempty"`
	// ContentHash is a sha256 hex digest of the canonical provider-map JSON.
	ContentHash string `json:"content_hash,omitempty"`
	// ModelCount is the total number of model entries across all providers.
	ModelCount int `json:"model_count,omitempty"`

	Providers map[string]providerEntry `json:"providers"`
}

// snapshotCache memoizes parsing of the embedded snapshot, which is immutable
// for the lifetime of the process.
type snapshotCache struct {
	once      sync.Once
	providers map[string]providerEntry
	generated time.Time // zero => legacy snapshot with unknown age
	ok        bool
}

var snapshotState = &snapshotCache{}

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

// loadFromSnapshot returns the embedded snapshot's providers, the UTC time it
// was generated (zero for legacy snapshots without a generated_at stamp), and
// whether it parsed. Callers treat a zero generated time as "unknown age" —
// i.e. stale — while still using the data as a fetch-failure fallback.
func loadFromSnapshot() (map[string]providerEntry, time.Time, bool) {
	snapshotState.once.Do(func() {
		parseSnapshot(modelsSnapshotData, snapshotState)
	})
	return snapshotState.providers, snapshotState.generated, snapshotState.ok
}

func parseSnapshot(data []byte, st *snapshotCache) {
	var wrapped registryFile
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Providers) > 0 {
		st.providers = wrapped.Providers
		if t, err := time.Parse(time.RFC3339, wrapped.GeneratedAt); err == nil {
			st.generated = t
		}
		st.ok = true
		return
	}
	// Legacy format: a bare provider map with no generated_at stamp. Its age
	// is unknown, so callers treat it as stale and refresh from models.dev.
	var legacy map[string]providerEntry
	if err := json.Unmarshal(data, &legacy); err == nil && len(legacy) > 0 {
		st.providers = legacy
		st.ok = true
	}
}

// modelsLockPath returns the path of the cross-process lock file guarding
// models.dev refreshes. It sits next to the on-disk cache so its mtime-based
// freshness and the flock are shared with every ocode process on the machine.
func modelsLockPath() (string, error) {
	p, err := cachePath()
	if err != nil {
		return "", err
	}
	return p + ".lock", nil
}

// refreshRegistryWithLock is the automatic path (loadRegistry): it re-checks
// the shared on-disk cache after acquiring the cross-process lock, in case
// another ocode process just refreshed it, and only fetches when that cache is
// still stale.
func refreshRegistryWithLock() (map[string]providerEntry, error) {
	return fetchRegistryWithLock(true, "auto")
}

// forceFetchRegistryWithLock is the explicit path (ForceRefreshRegistry /
// model-picker refresh): it always fetches from models.dev, even when the
// cache is fresh, but still serializes on the cross-process lock so two
// processes can't fetch simultaneously.
func forceFetchRegistryWithLock() (map[string]providerEntry, error) {
	return fetchRegistryWithLock(false, "force")
}

// fetchRegistryWithLock fetches the models.dev registry under a cross-process
// flock (next to the on-disk cache). Only one ocode process refreshes at a
// time: contenders wait up to registryLockWait for the holder to finish (flock
// auto-releases if the holder crashes), then either use the holder's freshly
// written cache (when recheckFreshCache) or proceed. On timeout the caller
// receives errRegistryLockHeld and falls back to the data it already has —
// it never fetches concurrently.
//
// Every actual fetch attempt (success or failure) is appended to the bounded
// models-history.jsonl audit log, so "when was the registry last updated (and
// by what)" has an explicit record rather than only a file mtime.
func fetchRegistryWithLock(recheckFreshCache bool, source string) (map[string]providerEntry, error) {
	lockPath, err := modelsLockPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create models cache dir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open models refresh lock: %w", err)
	}
	defer f.Close()

	deadline := time.Now().Add(registryLockWait)
	for {
		if err := tryLockFile(f); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, errRegistryLockHeld
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer unlockFile(f)

	// Another process may have refreshed between our staleness check and lock
	// acquisition — use its fresh cache instead of fetching again.
	if recheckFreshCache {
		if cached, modTime, err := loadCache(); err == nil && time.Since(modTime) < modelsCacheTTL {
			return cached, nil
		}
	}

	start := time.Now()
	remote, err := fetchRemote()
	dur := time.Since(start)
	if err != nil {
		appendHistory(HistoryEntry{
			TS:         time.Now().UTC().Format(time.RFC3339),
			Source:     source,
			DurationMS: dur.Milliseconds(),
			Error:      err.Error(),
		})
		return nil, err
	}
	if err := writeCache(remote, source, dur); err != nil {
		emitDebug("AGENT", fmt.Sprintf("models.dev cache write failed: %v", err))
	}
	appendHistory(HistoryEntry{
		TS:         time.Now().UTC().Format(time.RFC3339),
		Source:     source,
		ModelCount: countModels(remote),
		DurationMS: dur.Milliseconds(),
		Hash:       contentHash(remote),
	})
	return remote, nil
}

func loadRegistry() map[string]providerEntry {
	registry.mu.RLock()
	if registry.data != nil && time.Since(registry.fetchedAt) < modelsCacheTTL {
		// Memory is fresh, but another ocode process may have refreshed
		// models.dev since we loaded and written a newer on-disk cache. Adopt
		// it immediately rather than serving a 5-minute-old in-memory copy, so
		// concurrent processes converge on the same "last updated" data.
		if mt, ok := cacheModTime(); ok && mt.After(registry.fetchedAt) {
			registry.mu.RUnlock()
			registry.mu.Lock()
			// Double-check under the write lock: a peer may have adopted it
			// (or refreshed the cache) while we were waiting.
			if registry.data != nil && !registry.fetchedAt.IsZero() {
				if mt, ok := cacheModTime(); ok && mt.After(registry.fetchedAt) {
					if cached, _, err := loadCache(); err == nil {
						registry.data = cached
						registry.fetchedAt = mt
					}
				}
			}
			d := registry.data
			registry.mu.Unlock()
			return d
		}
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

	// An explicit OPENCODE_MODELS_PATH override is always authoritative and
	// never subject to the TTL.
	if data, ok := loadFromEnvPath(); ok {
		registry.data = data
		registry.fetchedAt = time.Now()
		return data
	}

	// The on-disk cache is the cross-process "last updated" record: its mtime
	// is set by whichever ocode process fetched models.dev last. A fresh cache
	// wins over the build-time snapshot — it is strictly newer registry state.
	if cached, modTime, err := loadCache(); err == nil && time.Since(modTime) < modelsCacheTTL {
		registry.data = cached
		registry.fetchedAt = modTime
		return cached
	}

	// The embedded snapshot is only fresh within modelsCacheTTL of its
	// generated_at stamp. Legacy unstamped snapshots have unknown age and are
	// treated as stale (they still serve as a fetch-failure fallback below).
	if snap, snapTime, ok := loadFromSnapshot(); ok {
		if !snapTime.IsZero() && time.Since(snapTime) < modelsCacheTTL {
			registry.data = snap
			registry.fetchedAt = snapTime
			return snap
		}
	}

	// Everything is stale: refresh from models.dev, throttled to one attempt
	// per TTL window per process so a down network or a slow lock holder can't
	// stall every lookup or trigger concurrent refetching.
	if time.Since(registry.lastFetchAttempt) >= modelsCacheTTL {
		registry.lastFetchAttempt = time.Now()
		if remote, err := refreshRegistryWithLock(); err == nil {
			registry.data = remote
			registry.fetchedAt = time.Now()
			return remote
		} else {
			emitDebug("AGENT", fmt.Sprintf("models.dev refresh skipped or failed (using stale data): %v", err))
		}
	}

	// Fetch failed or was throttled/skipped: use the newest stale copy.
	if cached, modTime, err := loadCache(); err == nil {
		registry.data = cached
		registry.fetchedAt = modTime
		return cached
	}
	if snap, snapTime, ok := loadFromSnapshot(); ok {
		registry.data = snap
		registry.fetchedAt = snapTime
		return snap
	}
	return nil
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
// updates the in-memory cache, bypassing the 5-minute TTL. The fetch runs under
// the cross-process refresh lock, so concurrent processes (or the automatic
// loadRegistry refresh) can't fetch simultaneously. It does NOT bypass the
// OPENCODE_MODELS_PATH env var or the embedded snapshot in loadRegistry —
// the freshly-updated fetchedAt makes the TTL short-circuit in loadRegistry
// return the new data on subsequent calls, so AllProviderModels and friends
// see the refreshed list immediately.
//
// Returns the new data on success, or the existing cached data plus the
// error on failure (so the caller can decide whether to repopulate or surface
// the error). Returns (nil, err) only when both the remote fetch fails and
// there is no prior in-memory data to fall back on.
func ForceRefreshRegistry() (map[string]providerEntry, error) {
	remote, err := forceFetchRegistryWithLock()
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

// ProviderModelsCached returns the same list as ProviderModels but never blocks
// on a network call: it only consults live caches that are already fresh.
func ProviderModelsCached(provider string) []string {
	return providerModelsFromRegistry(provider, false)
}

// ModelDisplayName returns the human-readable model name from the models.dev
// registry — e.g. "Ox Alpha Free" for the zen codename "opencode/x-preview-f-free".
// Returns "" when the registry doesn't know the model or the entry has no name,
// so callers fall back to the raw provider/model id. Safe to call from any
// goroutine; never touches the network (reads the loaded registry/snapshot).
func ModelDisplayName(modelID string) string {
	if m, ok := modelEntryFor(modelID); ok {
		return m.Name
	}
	return ""
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

// ModelMaxOutputTokens returns the model's maximum completion/output token
// count for a given model ID in "provider/model" format or bare model name.
// It checks the models.dev registry first, then falls back to 0 (unknown).
func ModelMaxOutputTokens(modelID string) int64 {
	if m, ok := modelEntryFor(modelID); ok {
		return m.Limit.Output
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

// ModelGeneratesImages reports whether a model can generate images. The
// models.dev registry (Modalities.Output containing "image") is authoritative
// when it knows the model. Models absent from the registry (cold cache,
// offline, or brand-new IDs) return false — the picker falls back to the
// user typing an explicit provider/model spec for those.
func ModelGeneratesImages(modelID string) bool {
	if m, ok := modelEntryFor(modelID); ok {
		for _, out := range m.Modalities.Output {
			if out == "image" {
				return true
			}
		}
		// Registry knows this model and it lists no image output → not an
		// image generator.
		return false
	}
	return false
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
// a deterministic entry with non-zero costs. Iteration over the provider map is
// randomized by Go, so we sort keys to avoid flaky pricing (e.g. gpt-4o exists
// in many providers with slightly different costs like cortecs vs openai).
func bestPricedEntry(data map[string]providerEntry, modelID string) (modelEntry, bool) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var fallback modelEntry
	var haveFallback bool
	for _, k := range keys {
		entry := data[k]
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
	// LM Studio, Requesty and Novita live models are only fetched on a live
	// refresh (async picker loader, background preload). The cached path used by
	// synchronous HTTP handlers and the TUI main loop must never block on the
	// network, so it falls back to the embedded snapshot entries for those
	// providers instead.
	if refresh {
		for _, m := range fetchLMStudioModels() {
			ids = append(ids, "lmstudio/"+m)
		}
	} else if data := loadRegistry(); data != nil {
		if entry, ok := data["lmstudio"]; ok {
			for m := range entry.Models {
				ids = append(ids, "lmstudio/"+m)
			}
		}
	}
	// Requesty live models — fall back to registry snapshot if API unreachable.
	if refresh {
		requestyLive := fetchRequestyLiveModels()
		if len(requestyLive) > 0 {
			for _, m := range requestyLive {
				ids = append(ids, "requesty/"+m)
			}
		} else {
			ids = append(ids, requestyRegistryFallback...)
		}
	} else {
		ids = append(ids, requestyRegistryFallback...)
	}
	// Novita AI live models — fall back to registry snapshot if API unreachable.
	if refresh {
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
	// OrcaRouter live models — supplement the snapshot so hosted models appear
	// in the picker. Guarded by refresh to avoid blocking the main loop.
	if refresh {
		if orcaLive := fetchOrcaRouterLiveModels(); len(orcaLive) > 0 {
			have := make(map[string]bool, len(ids))
			for _, id := range ids {
				have[id] = true
			}
			for _, key := range orcaLive {
				id := "orcarouter/" + key
				if !have[id] {
					ids = append(ids, id)
					have[id] = true
				}
			}
		}
	}
	// OrcaRouter's built-in router is available independently of the upstream
	// model catalog. Keep its documented default visible in the picker even when
	// models.dev does not list OrcaRouter yet.
	if !containsString(ids, "orcarouter/auto") {
		ids = append(ids, "orcarouter/auto")
	}
	// AIHubMix live models — supplement the snapshot so models the models.dev
	// catalog omits (e.g. "ox-alpha") appear in the picker. Guarded by refresh (or
	// a still-fresh cache) to avoid blocking the main loop on a network fetch.
	if refresh || aiHubMixCacheFresh() {
		if aiHubMixLive := fetchAIHubMixLiveModels(); len(aiHubMixLive) > 0 {
			have := make(map[string]bool, len(ids))
			for _, id := range ids {
				have[id] = true
			}
			for _, key := range aiHubMixLive {
				id := "aihubmix/" + key
				if !have[id] {
					ids = append(ids, id)
					have[id] = true
				}
			}
		}
	}
	// Groq live models — supplement the snapshot so newly released Groq models
	// appear in the picker even when absent from the models.dev snapshot.
	// Guarded by refresh (or a still-fresh cache) to avoid blocking the main loop.
	if refresh || groqCacheFresh() {
		if groqLive := fetchGroqLiveModels(); len(groqLive) > 0 {
			have := make(map[string]bool, len(ids))
			for _, id := range ids {
				have[id] = true
			}
			for _, key := range groqLive {
				id := key
				if !strings.HasPrefix(key, "groq/") {
					id = "groq/" + key
				}
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
	if provider == "orcarouter" {
		// OrcaRouter exposes an authenticated OpenAI-compatible /models endpoint.
		// Prefer it so newly added hosted models (including free deployments) are
		// available in the picker; retain auto and the static fallback on failure.
		// Guarded by refresh to avoid blocking the main loop.
		if refresh {
			if live := fetchOrcaRouterLiveModels(); len(live) > 0 {
				ids := make([]string, 0, len(live)+1)
				ids = append(ids, live...)
				if !containsString(ids, "auto") {
					ids = append(ids, "auto")
				}
				sort.Strings(ids)
				return ids
			}
		}
	}
	if provider == "aihubmix" {
		// AIHubMix serves models (e.g. "ox-alpha") that the models.dev catalog
		// has not indexed. Merge the live /models list with the snapshot so the
		// picker shows everything; pricing/context for known models still resolve
		// from the snapshot/registry via modelEntryFor.
		if refresh || aiHubMixCacheFresh() {
			if live := fetchAIHubMixLiveModels(); len(live) > 0 {
				snap := providerModelsFromSnapshot(provider)
				merged := mergeModelIDs(snap, live)
				sort.Strings(merged)
				return merged
			}
		}
		return providerModelsFromSnapshot(provider)
	}
	if provider == "groq" {
		// Groq serves fast inference models (e.g. "llama-3.3-70b-versatile").
		// Merge live /models list with snapshot so newly released models appear.
		if refresh || groqCacheFresh() {
			if live := fetchGroqLiveModels(); len(live) > 0 {
				snap := providerModelsFromSnapshot(provider)
				prefixed := make([]string, 0, len(live))
				for _, k := range live {
					if strings.HasPrefix(k, "groq/") {
						prefixed = append(prefixed, k)
					} else {
						prefixed = append(prefixed, "groq/"+k)
					}
				}
				merged := mergeModelIDs(snap, prefixed)
				sort.Strings(merged)
				return merged
			}
		}
		return providerModelsFromSnapshot(provider)
	}
	ids := providerModelsFromSnapshot(provider)
	if provider == "orcarouter" && !containsString(ids, "auto") {
		ids = append(ids, "auto")
		sort.Strings(ids)
	}
	return ids
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// mergeModelIDs returns the union of two model-ID slices with duplicates removed
// (order of a preserved first, then new entries from b). Used to combine the
// models.dev snapshot list with a provider's live /models list.
func mergeModelIDs(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, id := range a {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range b {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
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

const aiHubMixCacheTTL = 5 * time.Minute

// aiHubMixLiveData caches the model IDs fetched live from AIHubMix's
// OpenAI-compatible /models endpoint. AIHubMix serves models (e.g. "ox-alpha")
// that the models.dev catalog has not indexed, so querying the provider directly
// surfaces them in the picker. The endpoint only returns IDs (no pricing/
// context), so we use the live list to *discover* IDs and merge it with the
// models.dev snapshot, which still supplies pricing/context for known models.
var aiHubMixLiveData struct {
	mu        sync.RWMutex
	models    []string // bare model ids
	lastFetch time.Time
}

// fetchAIHubMixLiveModels fetches the model list from AIHubMix's OpenAI-compatible
// /models endpoint and returns the bare model IDs. Returns nil silently if:
//   - AIHUBMIX_API_KEY is not set AND no stored credential exists
//   - The API is unreachable, returns a non-200, or the body is malformed
//
// Callers degrade to the models.dev snapshot on a nil result.
func fetchAIHubMixLiveModels() []string {
	aiHubMixLiveData.mu.RLock()
	if aiHubMixLiveData.models != nil && time.Since(aiHubMixLiveData.lastFetch) < aiHubMixCacheTTL {
		models := aiHubMixLiveData.models
		aiHubMixLiveData.mu.RUnlock()
		return models
	}
	aiHubMixLiveData.mu.RUnlock()

	key := auth.ResolveKey("aihubmix")
	if key == "" {
		return nil
	}

	base := providers["aihubmix"].baseURL
	if cred, ok := auth.Get("aihubmix"); ok && cred.BaseURL != "" {
		base = strings.TrimRight(cred.BaseURL, "/")
	}
	modelsURL := strings.TrimRight(base, "/") + "/models"

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+key)

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
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil
	}

	ids := make([]string, 0, len(apiResp.Data))
	seen := make(map[string]bool, len(apiResp.Data))
	for _, m := range apiResp.Data {
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		return nil
	}

	aiHubMixLiveData.mu.Lock()
	aiHubMixLiveData.models = ids
	aiHubMixLiveData.lastFetch = time.Now()
	aiHubMixLiveData.mu.Unlock()

	return ids
}

// aiHubMixCacheFresh reports whether the AIHubMix live cache is populated AND
// still within its TTL (so the main loop can consult it without a network call).
func aiHubMixCacheFresh() bool {
	aiHubMixLiveData.mu.RLock()
	defer aiHubMixLiveData.mu.RUnlock()
	return aiHubMixLiveData.models != nil && time.Since(aiHubMixLiveData.lastFetch) < aiHubMixCacheTTL
}

// PreloadAIHubMixModels fetches AIHubMix's live model list in the background so
// models the models.dev catalog omits (e.g. "ox-alpha") are available in the
// picker as soon as the UI needs them. No-op until the key is configured;
// degrades gracefully when the network is unavailable.
func PreloadAIHubMixModels() {
	go fetchAIHubMixLiveModels()
}

const groqCacheTTL = 5 * time.Minute

// groqLiveData caches the model IDs fetched live from Groq's OpenAI-compatible
// /models endpoint. Groq serves models (e.g. "llama-3.3-70b-versatile",
// "openai/gpt-oss-120b") that may not yet be in the models.dev snapshot, so
// querying the provider directly supplements the picker.
var groqLiveData struct {
	mu        sync.RWMutex
	models    []string // bare model ids
	lastFetch time.Time
}

// fetchGroqLiveModels fetches the model list from Groq's OpenAI-compatible
// /models endpoint and returns the bare model IDs. Returns nil silently if:
//   - GROQ_API_KEY is not set AND no stored credential exists
//   - The API is unreachable, returns a non-200, or the body is malformed
//
// Callers degrade to the models.dev snapshot on a nil result.
func fetchGroqLiveModels() []string {
	groqLiveData.mu.RLock()
	if groqLiveData.models != nil && time.Since(groqLiveData.lastFetch) < groqCacheTTL {
		models := groqLiveData.models
		groqLiveData.mu.RUnlock()
		return models
	}
	groqLiveData.mu.RUnlock()

	key := auth.ResolveKey("groq")
	if key == "" {
		return nil
	}

	base := providers["groq"].baseURL
	if cred, ok := auth.Get("groq"); ok && cred.BaseURL != "" {
		base = strings.TrimRight(cred.BaseURL, "/")
	}
	if b := auth.GetBaseURL("groq"); b != "" {
		base = strings.TrimRight(b, "/")
	}
	modelsURL := strings.TrimRight(base, "/") + "/models"

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+key)

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
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil
	}

	ids := make([]string, 0, len(apiResp.Data))
	seen := make(map[string]bool, len(apiResp.Data))
	for _, m := range apiResp.Data {
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		return nil
	}

	groqLiveData.mu.Lock()
	groqLiveData.models = ids
	groqLiveData.lastFetch = time.Now()
	groqLiveData.mu.Unlock()

	return ids
}

// groqCacheFresh reports whether the Groq live cache is populated AND
// still within its TTL (so the main loop can consult it without a network call).
func groqCacheFresh() bool {
	groqLiveData.mu.RLock()
	defer groqLiveData.mu.RUnlock()
	return groqLiveData.models != nil && time.Since(groqLiveData.lastFetch) < groqCacheTTL
}

// PreloadGroqModels fetches Groq's live model list in the background so models
// absent from the snapshot appear in the picker. No-op until the key is configured.
func PreloadGroqModels() {
	go fetchGroqLiveModels()
}

// GroqModelsLoaded reports whether the Groq live cache has been populated.
func GroqModelsLoaded() bool {
	groqLiveData.mu.RLock()
	defer groqLiveData.mu.RUnlock()
	return groqLiveData.models != nil
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

// fetchOrcaRouterLiveModels fetches OrcaRouter's authenticated OpenAI-compatible
// model catalog. It returns bare IDs suitable for the provider picker.
func fetchOrcaRouterLiveModels() []string {
	key := auth.ResolveKey("orcarouter")
	if key == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, orcaRouterModelsURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil
	}
	ids := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		if model.ID != "" {
			ids = append(ids, model.ID)
		}
	}
	return ids
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
