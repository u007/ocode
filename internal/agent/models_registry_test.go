package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// seedOpenRouterLiveCache installs a fake OpenRouter live model cache so tests
// can exercise ModelWindow / modelEntryFor / provider model listing without
// hitting the network. The cache is marked fresh (lastFetch = now) so callers
// that consult openRouterCacheFresh() read the seeded data instead of triggering
// a real fetch. The previous cache is restored when the test ends so this helper
// does not leak global state into other tests.
func seedOpenRouterLiveCache(t *testing.T, entries map[string]modelEntry) {
	t.Helper()
	openRouterLiveData.mu.Lock()
	prevModels := openRouterLiveData.models
	prevFetch := openRouterLiveData.lastFetch
	openRouterLiveData.models = entries
	openRouterLiveData.lastFetch = time.Now()
	openRouterLiveData.mu.Unlock()
	t.Cleanup(func() {
		openRouterLiveData.mu.Lock()
		openRouterLiveData.models = prevModels
		openRouterLiveData.lastFetch = prevFetch
		openRouterLiveData.mu.Unlock()
	})
}

func TestModelWindowOpenRouterLiveFallback(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "syntheticsnap", time.Now()))
	// A model absent from the models.dev snapshot (the id below is synthetic;
	// real ids like tencent/hy3:free may drift into the snapshot on
	// regeneration, e.g. via kilo/tencent/hy3:free) must resolve through the
	// live cache.
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"unseen-model/alpha:free": {
			ID:    "unseen-model/alpha:free",
			Name:  "Unseen Model Alpha (free)",
			Limit: modelLimit{Context: 131072, Output: 8192},
			Cost:  modelCost{Input: 0, Output: 0},
		},
		// Sanity: a colon-variant not prefixed should also match a bare id.
		"anthropic/claude-3.5-sonnet": {
			ID:    "anthropic/claude-3.5-sonnet",
			Limit: modelLimit{Context: 200000},
		},
	})

	got := ModelWindow("openrouter/unseen-model/alpha:free")
	if got != 131072 {
		t.Fatalf("ModelWindow(openrouter/unseen-model/alpha:free) = %d, want 131072", got)
	}

	// Bare model id should also resolve via the live cache fallback.
	got = ModelWindow("unseen-model/alpha:free")
	if got != 131072 {
		t.Fatalf("ModelWindow(unseen-model/alpha:free) = %d, want 131072", got)
	}

	// Unknown model must still return 0 (no false positives).
	if got := ModelWindow("openrouter/nonexistent/model:xyz"); got != 0 {
		t.Fatalf("ModelWindow(unknown) = %d, want 0", got)
	}
}

func TestModelMaxOutputTokensOpenRouterLiveFallback(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "syntheticsnap", time.Now()))
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"unseen-model/alpha:free": {
			ID:    "unseen-model/alpha:free",
			Limit: modelLimit{Context: 131072, Output: 8192},
		},
	})

	if got := ModelMaxOutputTokens("openrouter/unseen-model/alpha:free"); got != 8192 {
		t.Fatalf("ModelMaxOutputTokens(openrouter/unseen-model/alpha:free) = %d, want 8192", got)
	}

	// Unknown model must return 0 so callers know to fall back, not silently
	// send a wrong cap.
	if got := ModelMaxOutputTokens("openrouter/nonexistent/model:xyz"); got != 0 {
		t.Fatalf("ModelMaxOutputTokens(unknown) = %d, want 0", got)
	}
}

func TestModelEntryForOpenRouterColonVariant(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "syntheticsnap", time.Now()))
	// The live cache keys by the bare id (after any "openrouter/" prefix), so a
	// lookup of "openrouter/tencent/hy3:free" must hit the cached
	// "tencent/hy3:free" entry.
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free": {
			ID:    "tencent/hy3:free",
			Limit: modelLimit{Context: 262144},
		},
	})

	m, ok := modelEntryFor("openrouter/tencent/hy3:free")
	if !ok {
		t.Fatalf("modelEntryFor(openrouter/tencent/hy3:free) returned ok=false, want true")
	}
	if m.Limit.Context != 262144 {
		t.Fatalf("resolved context = %d, want 262144", m.Limit.Context)
	}
}

func TestOpenRouterModelsLoaded(t *testing.T) {
	// The OpenRouter cache is package-global and may be left populated by other
	// tests (e.g. ones that call AllProviderModels() and hit the real API). Save
	// the prior state and force an unloaded start so the assertion is
	// deterministic, then restore the prior state at the end so we don't leak.
	openRouterLiveData.mu.Lock()
	savedModels := openRouterLiveData.models
	savedFetch := openRouterLiveData.lastFetch
	openRouterLiveData.models = nil
	openRouterLiveData.lastFetch = time.Time{}
	openRouterLiveData.mu.Unlock()
	t.Cleanup(func() {
		openRouterLiveData.mu.Lock()
		openRouterLiveData.models = savedModels
		openRouterLiveData.lastFetch = savedFetch
		openRouterLiveData.mu.Unlock()
	})

	// Unseeded cache reports not-loaded.
	if OpenRouterModelsLoaded() {
		t.Fatalf("OpenRouterModelsLoaded() = true before seeding, want false")
	}
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"openai/gpt-4o": {ID: "openai/gpt-4o", Limit: modelLimit{Context: 128000}},
	})
	if !OpenRouterModelsLoaded() {
		t.Fatalf("OpenRouterModelsLoaded() = false after seeding, want true")
	}
}

// TestAllProviderModelsOpenRouterPrefix verifies that allProviderModelsFromRegistry
// supplements the snapshot with live OpenRouter models using the fully-qualified
// "openrouter/<bare-id>" form (and does not double-prefix ids that already contain
// slashes, e.g. "openrouter/google/gemini-2.0-flash").
func TestAllProviderModelsOpenRouterPrefix(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "syntheticsnap", time.Now()))
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free":         {ID: "tencent/hy3:free", Limit: modelLimit{Context: 131072}},
		"google/gemini-2.0-flash":  {ID: "google/gemini-2.0-flash", Limit: modelLimit{Context: 1000000}},
		"anthropic/claude-3.5-son": {ID: "anthropic/claude-3.5-son", Limit: modelLimit{Context: 200000}},
	})

	got := allProviderModelsFromRegistry(true)
	have := make(map[string]bool, len(got))
	for _, id := range got {
		have[id] = true
	}

	if !have["openrouter/tencent/hy3:free"] {
		t.Fatalf("allProviderModelsFromRegistry missing openrouter/tencent/hy3:free; got %v", got)
	}
	// Multi-slash live id keeps a single "openrouter/" prefix, not "openrouter/openrouter/...".
	if !have["openrouter/google/gemini-2.0-flash"] {
		t.Fatalf("allProviderModelsFromRegistry missing openrouter/google/gemini-2.0-flash; got %v", got)
	}
	// The bare (unprefixed) form must NOT appear — the picker prepends the provider.
	if have["tencent/hy3:free"] {
		t.Fatalf("allProviderModelsFromRegistry leaked bare id tencent/hy3:free (expected openrouter/ prefix); got %v", got)
	}
}

// TestAllProviderModelsCachedSkipsStaleOpenRouter is the regression test for the
// main-loop blocking bug: when the OpenRouter cache is stale (past TTL) and the
// caller passes refresh=false (the TUI main loop via AllProviderModelsCached), the
// live models must NOT be consulted — so no blocking network fetch occurs. The
// snapshot-only result must omit the live "openrouter/tencent/hy3:free" id.
func TestAllProviderModelsCachedSkipsStaleOpenRouter(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "syntheticsnap", time.Now()))
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free": {ID: "tencent/hy3:free", Limit: modelLimit{Context: 131072}},
	})
	// Force the cache to look stale (zero lastFetch) so openRouterCacheFresh() is false.
	openRouterLiveData.mu.Lock()
	openRouterLiveData.lastFetch = time.Time{}
	openRouterLiveData.mu.Unlock()

	got := allProviderModelsFromRegistry(false)
	for _, id := range got {
		if id == "openrouter/tencent/hy3:free" {
			t.Fatalf("allProviderModelsFromRegistry(false) with stale cache pulled live openrouter id (would block on network); got %v", got)
		}
	}
}

// TestProviderModelsOpenRouterBare verifies that providerModelsFromRegistry("openrouter")
// returns BARE model ids (matching providerModelsFromSnapshot), so the picker — which
// prepends the provider segment — reconstructs the correct "openrouter/<id>".
func TestProviderModelsOpenRouterBare(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "syntheticsnap", time.Now()))
	seedOpenRouterLiveCache(t, map[string]modelEntry{
		"tencent/hy3:free":        {ID: "tencent/hy3:free", Limit: modelLimit{Context: 131072}},
		"anthropic/claude-3.7-so": {ID: "anthropic/claude-3.7-so", Limit: modelLimit{Context: 200000}},
	})

	got := providerModelsFromRegistry("openrouter", true)

	// Build a set keyed by whether each returned id is prefixed.
	prefixed, bare := 0, 0
	for _, id := range got {
		if strings.HasPrefix(id, "openrouter/") {
			prefixed++
		} else {
			bare++
		}
	}
	if prefixed != 0 {
		t.Fatalf("providerModelsFromRegistry(openrouter) returned %d prefixed ids %v; expected all bare", prefixed, got)
	}
	if !slices.Contains(got, "tencent/hy3:free") {
		t.Fatalf("providerModelsFromRegistry(openrouter) missing bare id tencent/hy3:free; got %v", got)
	}
	if !slices.Contains(got, "anthropic/claude-3.7-so") {
		t.Fatalf("providerModelsFromRegistry(openrouter) missing bare id anthropic/claude-3.7-so; got %v", got)
	}
}

func TestProviderModelsOrcaRouterIncludesAuto(t *testing.T) {
	got := providerModelsFromRegistry("orcarouter", false)
	if !containsString(got, "auto") {
		t.Fatalf("providerModelsFromRegistry(orcarouter) = %v, want documented auto model", got)
	}
}

// ── Registry freshness / refresh tests ─────────────────────────────────────

// withSandboxedModelsCache redirects the models.json cache, its lock file, and
// the OPENCODE_MODELS_PATH override into a fresh temp dir so registry tests are
// hermetic and never read or write the developer's real ~/.config/opencode.
func withSandboxedModelsCache(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)
	t.Setenv("APPDATA", home) // windows
	t.Setenv(envModelsPath, "")
}

// withSnapshotBytes swaps the embedded snapshot (and its parse-once cache) for
// the duration of the test, restoring both afterwards. Lets tests exercise the
// snapshot-staleness / refresh paths without depending on the age of the real
// embedded snapshot artifact on the machine running them. It also resets the
// in-memory registry so a swapped snapshot can't be shadowed by data a
// previous test adopted (and so this test's adopted data can't leak into later
// tests).
func withSnapshotBytes(t *testing.T, data []byte) {
	t.Helper()
	prevData := modelsSnapshotData
	prevState := snapshotState
	modelsSnapshotData = data
	snapshotState = &snapshotCache{}
	t.Cleanup(func() {
		modelsSnapshotData = prevData
		snapshotState = prevState
	})

	registry.mu.Lock()
	prevRegData := registry.data
	prevFetchedAt := registry.fetchedAt
	prevAttempt := registry.lastFetchAttempt
	registry.data = nil
	registry.fetchedAt = time.Time{}
	registry.lastFetchAttempt = time.Time{}
	registry.mu.Unlock()
	t.Cleanup(func() {
		registry.mu.Lock()
		registry.data = prevRegData
		registry.fetchedAt = prevFetchedAt
		registry.lastFetchAttempt = prevAttempt
		registry.mu.Unlock()
	})
}

// mustSnapshotWrapper builds a wrapped registryFile payload containing a single
// provider with one model, stamped with the given generated time.
func mustSnapshotWrapper(t *testing.T, provider string, generated time.Time) []byte {
	t.Helper()
	wrapped := registryFile{
		GeneratedAt: generated.UTC().Format(time.RFC3339),
		Providers: map[string]providerEntry{
			provider: {
				ID: provider,
				Models: map[string]modelEntry{
					"model-a": {ID: "model-a", Limit: modelLimit{Context: 4096}},
				},
			},
		},
	}
	b, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("marshal snapshot wrapper: %v", err)
	}
	return b
}

// fetchCountingClient returns an http.Client that counts every request it
// dispatches, so tests can assert that a stale-registry path did (or did not)
// hit the network. The served payload parses as an empty provider set.
func fetchCountingClient(t *testing.T) (*http.Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	c := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	return c, &calls
}

func TestLoadRegistryFreshSnapshotSkipsFetch(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "freshprov", time.Now()))
	withRegistryState(t, nil, time.Time{})
	client, calls := fetchCountingClient(t)
	withFetchRemoteClient(t, client)

	got := loadRegistry()
	if got == nil {
		t.Fatal("expected loadRegistry to return the fresh snapshot")
	}
	if _, ok := got["freshprov"]; !ok {
		t.Fatalf("expected freshprov in registry data, got keys: %v", keysOf(got))
	}
	if calls.Load() != 0 {
		t.Fatalf("loadRegistry fetched from models.dev (%d calls) despite a fresh snapshot", calls.Load())
	}
}

func TestLoadRegistryStaleSnapshotRefreshesFromModelsDev(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "staleprov", time.Now().Add(-time.Hour)))
	withRegistryState(t, nil, time.Time{})

	want := map[string]providerEntry{
		"live": {ID: "live", Models: map[string]modelEntry{
			"fresh-model": {ID: "fresh-model"},
		}},
	}
	body, _ := json.Marshal(want)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withFetchRemoteServer(t, srv)

	got := loadRegistry()
	if got == nil {
		t.Fatal("expected loadRegistry to return refreshed data")
	}
	if _, ok := got["live"]; !ok {
		t.Fatalf("expected live provider after refresh, got keys: %v", keysOf(got))
	}

	// The refresh must be persisted to the on-disk cache so other ocode
	// processes see it — that file's mtime is the cross-process
	// "last updated" record.
	cacheFile, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("expected on-disk cache written at %s: %v", cacheFile, err)
	}
}

func TestLoadRegistryPrefersFreshDiskCacheOverStaleSnapshot(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "staleprov", time.Now().Add(-time.Hour)))
	withRegistryState(t, nil, time.Time{})
	client, calls := fetchCountingClient(t)
	withFetchRemoteClient(t, client)

	// Write a fresh on-disk cache, as another ocode process would after its
	// own fetch; its mtime must win over the stale snapshot and suppress the
	// network fetch entirely.
	cacheFile, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatal(err)
	}
	cached := map[string]providerEntry{
		"cachedprov": {ID: "cachedprov", Models: map[string]modelEntry{
			"cached-model": {ID: "cached-model"},
		}},
	}
	b, _ := json.Marshal(cached)
	if err := os.WriteFile(cacheFile, b, 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadRegistry()
	if got == nil {
		t.Fatal("expected loadRegistry to return the fresh disk cache")
	}
	if _, ok := got["cachedprov"]; !ok {
		t.Fatalf("expected cachedprov in registry data, got keys: %v", keysOf(got))
	}
	if calls.Load() != 0 {
		t.Fatalf("loadRegistry fetched from models.dev (%d calls) despite a fresh disk cache", calls.Load())
	}
}

func TestLoadRegistryLegacySnapshotRefreshes(t *testing.T) {
	withSandboxedModelsCache(t)
	// Legacy snapshot: a bare provider map with no generated_at stamp. Its age
	// is unknown, so it must be treated as stale and refreshed.
	legacy := map[string]providerEntry{
		"legacyprov": {ID: "legacyprov", Models: map[string]modelEntry{
			"legacy-model": {ID: "legacy-model"},
		}},
	}
	b, _ := json.Marshal(legacy)
	withSnapshotBytes(t, b)
	withRegistryState(t, nil, time.Time{})

	want := map[string]providerEntry{
		"live": {ID: "live", Models: map[string]modelEntry{
			"fresh-model": {ID: "fresh-model"},
		}},
	}
	body, _ := json.Marshal(want)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withFetchRemoteServer(t, srv)

	got := loadRegistry()
	if got == nil {
		t.Fatal("expected loadRegistry to refresh from models.dev for a legacy snapshot")
	}
	if _, ok := got["live"]; !ok {
		t.Fatalf("expected live provider after refresh, got keys: %v", keysOf(got))
	}
}

func TestRegistryRefreshLockHeldSkipsFetch(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "staleprov", time.Now().Add(-time.Hour)))
	withRegistryState(t, nil, time.Time{})
	client, calls := fetchCountingClient(t)
	withFetchRemoteClient(t, client)

	// Shrink the lock wait so the test doesn't stall on the default 5s.
	prevWait := registryLockWait
	registryLockWait = 200 * time.Millisecond
	t.Cleanup(func() { registryLockWait = prevWait })

	// Simulate another ocode process mid-refresh: hold the cross-process lock.
	lockPath, err := modelsLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := tryLockFile(f); err != nil {
		t.Fatalf("failed to acquire test lock: %v", err)
	}
	defer unlockFile(f)

	got := loadRegistry()
	if got == nil {
		t.Fatal("expected loadRegistry to fall back to stale data while the refresh lock is held")
	}
	if _, ok := got["staleprov"]; !ok {
		t.Fatalf("expected stale snapshot fallback data, got keys: %v", keysOf(got))
	}
	// The whole point: never fetch concurrently with another process.
	if calls.Load() != 0 {
		t.Fatalf("loadRegistry fetched from models.dev (%d calls) while the refresh lock was held", calls.Load())
	}
}

// TestLoadRegistryConcurrentCallersSingleFetch verifies the in-process side of
// the "avoid concurrent refetching" requirement: when many callers trip over a
// stale registry at once, the registry mutex serializes them and exactly one
// fetch hits models.dev; the rest observe the fresh in-memory result.
func TestLoadRegistryConcurrentCallersSingleFetch(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "staleprov", time.Now().Add(-time.Hour)))
	withRegistryState(t, nil, time.Time{})
	client, calls := fetchCountingClient(t)
	withFetchRemoteClient(t, client)

	const workers = 8
	var wg sync.WaitGroup
	results := make(chan map[string]providerEntry, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- loadRegistry()
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got == nil {
			t.Fatal("expected concurrent loadRegistry call to return data")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 fetch across %d concurrent callers, got %d", workers, got)
	}
}

// TestRegistryRefreshLockCrossProcess is the true multi-process contention
// check: a helper subprocess holds the models.dev refresh flock while this
// process loads a stale registry. The load must NOT fetch — it falls back to
// stale data — and once the helper releases the lock, a later load fetches
// normally.
func TestRegistryRefreshLockCrossProcess(t *testing.T) {
	if os.Getenv("GO_WANT_REGISTRY_LOCK_HELPER") == "1" {
		runRegistryLockHelper()
		return
	}

	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "staleprov", time.Now().Add(-time.Hour)))
	withRegistryState(t, nil, time.Time{})
	client, calls := fetchCountingClient(t)
	withFetchRemoteClient(t, client)
	prevWait := registryLockWait
	registryLockWait = 2 * time.Second
	t.Cleanup(func() { registryLockWait = prevWait })

	lockPath, err := modelsLockPath()
	if err != nil {
		t.Fatal(err)
	}
	// The helper process only opens the lock file; create its directory here
	// (fetchRegistryWithLock would normally do it, but it never runs while the
	// helper holds the lock).
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	readyFile := lockPath + ".ready"
	releaseFile := lockPath + ".release"

	cmd := exec.Command(os.Args[0], "-test.run=TestRegistryRefreshLockCrossProcess$")
	cmd.Env = append(os.Environ(),
		"GO_WANT_REGISTRY_LOCK_HELPER=1",
		"REGISTRY_LOCK_HELPER_PATH="+lockPath,
		"REGISTRY_LOCK_READY="+readyFile,
		"REGISTRY_LOCK_RELEASE="+releaseFile,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lock helper: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock helper never acquired the cross-process lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// While another process holds the lock, a stale load must not fetch.
	got := loadRegistry()
	if got == nil {
		t.Fatal("expected stale fallback while another process holds the lock")
	}
	if _, ok := got["staleprov"]; !ok {
		t.Fatalf("expected stale snapshot fallback, got keys: %v", keysOf(got))
	}
	if calls.Load() != 0 {
		t.Fatalf("loadRegistry fetched from models.dev (%d calls) while another process held the lock", calls.Load())
	}

	// Release the helper; a fresh load may now fetch.
	if err := os.WriteFile(releaseFile, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("lock helper exited with error: %v", err)
	}

	// Clear in-memory state (including the fetch-attempt throttle) so the next
	// load re-evaluates and hits the network.
	withRegistryState(t, nil, time.Time{})
	got = loadRegistry()
	if got == nil {
		t.Fatal("expected loadRegistry to fetch after the lock was released")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected exactly 1 fetch after lock release, got %d", calls.Load())
	}
}

// runRegistryLockHelper is the child-process half of
// TestRegistryRefreshLockCrossProcess: it acquires the cross-process refresh
// flock, signals readiness, holds the lock until told to release, then exits.
func runRegistryLockHelper() {
	lockPath := os.Getenv("REGISTRY_LOCK_HELPER_PATH")
	readyFile := os.Getenv("REGISTRY_LOCK_READY")
	releaseFile := os.Getenv("REGISTRY_LOCK_RELEASE")
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		os.Exit(2)
	}
	if err := tryLockFile(f); err != nil {
		os.Exit(3)
	}
	_ = os.WriteFile(readyFile, []byte("locked"), 0o644)
	for {
		if _, err := os.Stat(releaseFile); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = unlockFile(f)
	os.Exit(0)
}

// TestLoadRegistryAdoptsNewerCrossProcessCache covers the "multiple ocode
// processes with outdated TTL" requirement: while this process still has fresh
// in-memory data, another process refreshes models.dev and writes a newer
// on-disk cache. The next load must adopt that newer cache immediately (before
// the 5-minute in-memory TTL expires) and must not fetch.
func TestLoadRegistryAdoptsNewerCrossProcessCache(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "syntheticsnap", time.Now()))
	withRegistryState(t, nil, time.Time{})
	client, calls := fetchCountingClient(t)
	withFetchRemoteClient(t, client)

	// First load: the fresh snapshot lands in memory (fetchedAt = its
	// generated time).
	got := loadRegistry()
	if _, ok := got["syntheticsnap"]; !ok {
		t.Fatalf("expected snapshot data in initial load, got keys: %v", keysOf(got))
	}

	// Simulate another process refreshing: write a newer on-disk cache with a
	// distinct provider. Ensure its mtime strictly exceeds our fetchedAt (some
	// filesystems have coarse mtime granularity).
	time.Sleep(20 * time.Millisecond)
	cacheFile, err := cachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatal(err)
	}
	newer := map[string]providerEntry{
		"newerprov": {ID: "newerprov", Models: map[string]modelEntry{
			"newer-model": {ID: "newer-model"},
		}},
	}
	b, _ := json.Marshal(newer)
	if err := os.WriteFile(cacheFile, b, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(cacheFile, now, now); err != nil {
		t.Fatal(err)
	}

	// Next load — well inside the in-memory TTL — must adopt the newer cache
	// instead of serving the snapshot, and must not hit the network.
	got = loadRegistry()
	if _, ok := got["newerprov"]; !ok {
		t.Fatalf("expected newer cross-process cache to be adopted, got keys: %v", keysOf(got))
	}
	if calls.Load() != 0 {
		t.Fatalf("loadRegistry fetched from models.dev (%d calls) when a newer cache existed", calls.Load())
	}
}

// ── Tracking metadata + history sidecar tests ───────────────────────────────

func TestWriteCacheWritesTrackingMetadata(t *testing.T) {
	withSandboxedModelsCache(t)
	data := map[string]providerEntry{
		"prov1": {ID: "prov1", Models: map[string]modelEntry{
			"m1": {ID: "m1"},
			"m2": {ID: "m2"},
		}},
	}
	if err := writeCache(data, "auto", 1234*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Providers + mtime remain readable (existing contract).
	got, modTime, err := loadCache()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["prov1"]; !ok {
		t.Fatalf("expected prov1 from cache, got keys: %v", keysOf(got))
	}
	if time.Since(modTime) > 5*time.Second {
		t.Fatalf("cache mtime should be recent, got %v", modTime)
	}

	// Tracking metadata is present in the wrapped file.
	path, _ := cachePath()
	raw, _ := os.ReadFile(path)
	var wrapped registryFile
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		t.Fatal(err)
	}
	if wrapped.FetchSource != "auto" {
		t.Fatalf("fetch_source = %q, want auto", wrapped.FetchSource)
	}
	if wrapped.FetchedAt == "" {
		t.Fatal("fetched_at missing")
	}
	if wrapped.FetchDurationMS != 1234 {
		t.Fatalf("fetch_duration_ms = %d, want 1234", wrapped.FetchDurationMS)
	}
	if wrapped.ModelCount != 2 {
		t.Fatalf("model_count = %d, want 2", wrapped.ModelCount)
	}
	wantHash := contentHash(data)
	if wrapped.ContentHash == "" || wrapped.ContentHash != wantHash {
		t.Fatalf("content_hash = %q, want %q", wrapped.ContentHash, wantHash)
	}
}

func TestLoadCacheLegacyBareMapFormat(t *testing.T) {
	withSandboxedModelsCache(t)
	// A cache written by a pre-wrapper binary: bare provider map.
	legacy := map[string]providerEntry{
		"legacyprov": {ID: "legacyprov", Models: map[string]modelEntry{"m": {ID: "m"}}},
	}
	b, _ := json.Marshal(legacy)
	path, _ := cachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	got, _, err := loadCache()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["legacyprov"]; !ok {
		t.Fatalf("expected legacy bare-map cache to load, got keys: %v", keysOf(got))
	}
}

func TestAppendHistoryBounded(t *testing.T) {
	withSandboxedModelsCache(t)
	prev := modelsHistoryMaxLines
	modelsHistoryMaxLines = 3
	t.Cleanup(func() { modelsHistoryMaxLines = prev })

	for i := 0; i < 5; i++ {
		appendHistory(HistoryEntry{TS: fmt.Sprintf("ts-%d", i), Source: "auto", ModelCount: i})
	}
	entries := RegistryHistory(0)
	if len(entries) != 3 {
		t.Fatalf("history trimmed to %d entries, want 3", len(entries))
	}
	// Newest three, chronological order preserved.
	if entries[0].TS != "ts-2" || entries[2].TS != "ts-4" {
		t.Fatalf("unexpected trimmed history: %+v", entries)
	}
	if entries[0].ModelCount != 2 || entries[2].ModelCount != 4 {
		t.Fatalf("unexpected model counts: %+v", entries)
	}
}

func TestRegistryHistorySkipsMalformedLines(t *testing.T) {
	withSandboxedModelsCache(t)
	path, _ := historyPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Two valid entries plus a torn line (simulates a partial append).
	content := "{\"ts\":\"a\",\"source\":\"auto\"}\nnot-json\n{\"ts\":\"b\",\"source\":\"force\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := RegistryHistory(0)
	if len(entries) != 2 {
		t.Fatalf("expected 2 parseable entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].TS != "a" || entries[1].TS != "b" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestAutoFetchWritesHistoryAndMetadata(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, mustSnapshotWrapper(t, "staleprov", time.Now().Add(-time.Hour)))
	withRegistryState(t, nil, time.Time{})

	want := map[string]providerEntry{
		"live": {ID: "live", Models: map[string]modelEntry{"m": {ID: "m"}}},
	}
	body, _ := json.Marshal(want)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withFetchRemoteServer(t, srv)

	if got := loadRegistry(); got == nil {
		t.Fatal("expected registry load")
	}

	// The auto refresh is recorded in the history sidecar.
	entries := RegistryHistory(0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].Source != "auto" || entries[0].Error != "" || entries[0].ModelCount != 1 {
		t.Fatalf("unexpected history entry: %+v", entries[0])
	}
	if entries[0].Hash == "" {
		t.Fatal("expected content hash in history entry")
	}
}

func TestForceRefreshFailureRecordsHistory(t *testing.T) {
	withSandboxedModelsCache(t)
	withRegistryState(t, nil, time.Time{})
	withFetchRemoteError(t, errors.New("simulated network down"))

	if _, err := ForceRefreshRegistry(); err == nil {
		t.Fatal("expected force refresh to fail")
	}
	entries := RegistryHistory(0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].Source != "force" || entries[0].Error == "" {
		t.Fatalf("expected force error entry, got %+v", entries[0])
	}
}
