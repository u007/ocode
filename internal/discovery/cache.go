package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cache is a per-model on-disk store of passage vectors keyed by DocHash.
type Cache struct {
	path  string
	Model string               `json:"model"`
	Dim   int                  `json:"dim"`
	Items map[string][]float32 `json:"items"`
}

func sanitizeModelID(id string) string {
	return strings.NewReplacer("/", "_", ":", "_", " ", "_").Replace(id)
}

// LoadCache reads the cache file for modelID. A missing file, or a file whose
// model/dim don't match, yields an empty (fresh) cache — that is the
// invalidation path.
func LoadCache(dir, modelID string, dim int) (*Cache, error) {
	path := filepath.Join(dir, "corpus-"+sanitizeModelID(modelID)+".json")
	fresh := &Cache{path: path, Model: modelID, Dim: dim, Items: map[string][]float32{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fresh, nil
		}
		return nil, fmt.Errorf("read discovery cache %s: %w", path, err)
	}
	var loaded Cache
	if err := json.Unmarshal(data, &loaded); err != nil {
		// Corrupt cache is non-fatal: log and start fresh (the only recovery
		// path, explicitly logged per the no-silent-recovery rule).
		emitDiscoveryDebug("WARN", fmt.Sprintf("discovery cache %s unreadable, rebuilding: %v", path, err))
		return fresh, nil
	}
	if loaded.Model != modelID || loaded.Dim != dim || loaded.Items == nil {
		return fresh, nil
	}
	loaded.path = path
	return &loaded, nil
}

func (c *Cache) Get(hash string) ([]float32, bool) { v, ok := c.Items[hash]; return v, ok }
func (c *Cache) Put(hash string, vec []float32)   { c.Items[hash] = vec }

// Save writes the cache atomically (temp + rename) so concurrent sessions can't
// observe a half-written file.
func (c *Cache) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return fmt.Errorf("mkdir discovery cache dir: %w", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal discovery cache: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write discovery cache tmp: %w", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("rename discovery cache: %w", err)
	}
	return nil
}

// BuildCorpusCached builds a corpus, embedding only docs missing from the cache.
// Returns the corpus and the count of docs actually embedded (cache misses).
func BuildCorpusCached(ctx context.Context, e Embedder, docs []Doc, c *Cache) (*Corpus, int, error) {
	vecs := make([][]float32, len(docs))
	var missIdx []int
	var missTexts []string
	for i, d := range docs {
		if v, ok := c.Get(DocHash(d)); ok {
			vecs[i] = v
			continue
		}
		missIdx = append(missIdx, i)
		missTexts = append(missTexts, d.Text)
	}
	if len(missTexts) > 0 {
		embedded, err := e.Embed(ctx, missTexts, Passage)
		if err != nil {
			return nil, 0, fmt.Errorf("embed %d corpus misses: %w", len(missTexts), err)
		}
		if len(embedded) != len(missTexts) {
			return nil, 0, fmt.Errorf("embedder returned %d vectors for %d misses", len(embedded), len(missTexts))
		}
		for k, idx := range missIdx {
			vecs[idx] = embedded[k]
			c.Put(DocHash(docs[idx]), embedded[k])
		}
		if err := c.Save(); err != nil {
			// Non-fatal: corpus is usable in-memory this session; log it.
			emitDiscoveryDebug("WARN", fmt.Sprintf("persist discovery cache failed: %v", err))
		}
	}
	return &Corpus{Docs: docs, Vecs: vecs}, len(missTexts), nil
}
