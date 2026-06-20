# Part 01 — Discovery Config + Savers

Adds a `discovery` block to `OcodeConfig`, with JSON round-trip and three
targeted load-modify-write savers (never a whole-snapshot write).

## Task 1: Discovery config block + savers

**Files:**
- Modify: `internal/config/ocodeconfig.go` (struct ~75-103; file mirror ~228-247; default ~283-294; load ~367-506; write ~682-752; add savers near `SaveUploadDir` ~875)
- Test: `internal/config/discovery_config_test.go` (create)

**Interfaces:**
- Produces:
  - `type DiscoveryConfig struct { Enabled bool; EmbeddingModel string; EmbeddingBackend string; LocalModelStatus string; PinnedSkills []string }`
  - `OcodeConfig.Discovery DiscoveryConfig`
  - `func defaultDiscoveryConfig() DiscoveryConfig`
  - `func SaveDiscoveryEnabled(enabled bool) error`
  - `func SaveQueryEmbeddingModel(modelID, backend string) error`
  - `func SaveLocalModelStatus(status string) error`

- [ ] **Step 1: Write the failing test**

Create `internal/config/discovery_config_test.go`:

```go
package config

import "testing"

func TestDefaultDiscoveryConfig(t *testing.T) {
	d := defaultDiscoveryConfig()
	if d.Enabled {
		t.Fatalf("discovery must default to disabled")
	}
	if d.EmbeddingModel != "" {
		t.Fatalf("embedding_model must default empty (no implicit vendor), got %q", d.EmbeddingModel)
	}
	if len(d.PinnedSkills) == 0 {
		t.Fatalf("pinned skills must seed defaults")
	}
}

func TestDiscoveryConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/ocodeconfig.json"

	cfg := defaultOcodeConfig()
	cfg.Discovery.Enabled = true
	cfg.Discovery.EmbeddingModel = "openai/text-embedding-3-small"
	cfg.Discovery.EmbeddingBackend = "http"
	cfg.Discovery.LocalModelStatus = "none"
	cfg.Discovery.PinnedSkills = []string{"brainstorming"}

	if err := writeOcodeConfigFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	got := defaultOcodeConfig()
	if err := loadOcodeConfigFile(path, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Discovery.Enabled ||
		got.Discovery.EmbeddingModel != "openai/text-embedding-3-small" ||
		got.Discovery.EmbeddingBackend != "http" ||
		len(got.Discovery.PinnedSkills) != 1 {
		t.Fatalf("round-trip mismatch: %+v", got.Discovery)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestDiscovery -v`
Expected: FAIL — `defaultDiscoveryConfig` / `OcodeConfig.Discovery` undefined.

- [ ] **Step 3: Add the struct + file mirror + default**

In `internal/config/ocodeconfig.go`, add the struct near the other config types:

```go
type DiscoveryConfig struct {
	Enabled          bool
	EmbeddingModel   string
	EmbeddingBackend string // "http" | "local"
	LocalModelStatus string // none | downloading | ready
	PinnedSkills     []string
}
```

Add a field to `OcodeConfig` (after `Plugins PluginsConfig`):

```go
	Discovery DiscoveryConfig
```

Add the JSON mirror struct:

```go
type discoveryConfigFile struct {
	Enabled          *bool    `json:"enabled,omitempty"`
	EmbeddingModel   string   `json:"embedding_model,omitempty"`
	EmbeddingBackend string   `json:"embedding_backend,omitempty"`
	LocalModelStatus string   `json:"local_model_status,omitempty"`
	PinnedSkills     []string `json:"pinned_skills,omitempty"`
}
```

Add the field to `ocodeConfigFile` (after `Plugins`):

```go
	Discovery discoveryConfigFile `json:"discovery"`
```

Add the default constructor and call it from `defaultOcodeConfig()`:

```go
func defaultDiscoveryConfig() DiscoveryConfig {
	return DiscoveryConfig{
		Enabled:          false,
		EmbeddingModel:   "",
		EmbeddingBackend: "http",
		LocalModelStatus: "none",
		PinnedSkills:     []string{"brainstorming", "using-superpowers"},
	}
}
```

In `defaultOcodeConfig()` add `Discovery: defaultDiscoveryConfig(),` to the returned literal.

- [ ] **Step 4: Wire load + write**

In `loadOcodeConfigFile()`, after the `plugins` block, add:

```go
	if _, ok := raw["discovery"]; ok {
		applyDiscoveryConfig(&cfg.Discovery, file.Discovery)
		delete(raw, "discovery")
	}
```

Add the apply helper (mirrors the other `applyXConfig` helpers — only overwrite when the file provided a value, so a partial block can't blank defaults):

```go
func applyDiscoveryConfig(dst *DiscoveryConfig, f discoveryConfigFile) {
	if f.Enabled != nil {
		dst.Enabled = *f.Enabled
	}
	if f.EmbeddingModel != "" {
		dst.EmbeddingModel = f.EmbeddingModel
	}
	if f.EmbeddingBackend != "" {
		dst.EmbeddingBackend = f.EmbeddingBackend
	}
	if f.LocalModelStatus != "" {
		dst.LocalModelStatus = f.LocalModelStatus
	}
	if f.PinnedSkills != nil {
		dst.PinnedSkills = append([]string{}, f.PinnedSkills...)
	}
}
```

In `writeOcodeConfigFile()`, after the `plugins` block, add (always persist the block so `enabled=false` is explicit):

```go
	payload["discovery"] = map[string]interface{}{
		"enabled":            cfg.Discovery.Enabled,
		"embedding_model":    cfg.Discovery.EmbeddingModel,
		"embedding_backend":  cfg.Discovery.EmbeddingBackend,
		"local_model_status": cfg.Discovery.LocalModelStatus,
		"pinned_skills":      cfg.Discovery.PinnedSkills,
	}
```

Also add `"discovery"` to the `Extra` skip-list in `writeOcodeConfigFile` (the loop that copies `cfg.Extra`) so it is never double-written:

```go
		if k == "compact" || k == "advisor" || k == "permissions" || k == "plugins" || k == "extra_allowed_paths" || k == "max_steps" || k == "discovery" {
			continue
		}
```

- [ ] **Step 5: Add the three targeted savers**

Near `SaveUploadDir` (~line 884):

```go
// SaveDiscoveryEnabled persists only the discovery.enabled flag using
// load-modify-write so it cannot clobber a concurrent session's other config.
func SaveDiscoveryEnabled(enabled bool) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Discovery.Enabled = enabled
	return SaveOcodeConfig(cfg)
}

// SaveQueryEmbeddingModel persists the discovery embedding model + backend.
func SaveQueryEmbeddingModel(modelID, backend string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Discovery.EmbeddingModel = modelID
	if backend != "" {
		cfg.Discovery.EmbeddingBackend = backend
	}
	return SaveOcodeConfig(cfg)
}

// SaveLocalModelStatus persists the local model download status.
func SaveLocalModelStatus(status string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Discovery.LocalModelStatus = status
	return SaveOcodeConfig(cfg)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestDiscovery -v`
Expected: PASS (both tests).

Also run the full config package to ensure no regression in existing round-trips:
Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/config/discovery_config_test.go
git commit -m "feat(config): add discovery config block + targeted savers"
```
