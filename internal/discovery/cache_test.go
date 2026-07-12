package discovery

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCacheReusesAndReembedsOnChange(t *testing.T) {
	dir := t.TempDir()
	fe := FakeEmbedder{Dimension: 64}
	docs := []Doc{
		{ID: "skill:a", Kind: "skill", Name: "a", Text: "alpha text"},
		{ID: "skill:b", Kind: "skill", Name: "b", Text: "beta text"},
	}

	c1, _ := LoadCache(dir, fe.ID(), fe.Dim())
	_, miss1, err := BuildCorpusCached(context.Background(), fe, docs, c1)
	if err != nil {
		t.Fatal(err)
	}
	if miss1 != 2 {
		t.Fatalf("cold build should embed all 2, got %d", miss1)
	}

	// Reload cache from disk; one doc unchanged, one changed.
	c2, _ := LoadCache(dir, fe.ID(), fe.Dim())
	docs[1].Text = "beta text CHANGED"
	_, miss2, _ := BuildCorpusCached(context.Background(), fe, docs, c2)
	if miss2 != 1 {
		t.Fatalf("warm build should re-embed only the changed doc, got %d", miss2)
	}
}

func TestCacheInvalidatesOnModelMismatch(t *testing.T) {
	dir := t.TempDir()
	fe := FakeEmbedder{Dimension: 64}
	c1, _ := LoadCache(dir, "model-A", fe.Dim())
	c1.Put("h1", []float32{1, 2, 3})
	if err := c1.Save(); err != nil {
		t.Fatal(err)
	}
	// Different model id → fresh cache, no carryover.
	c2, _ := LoadCache(dir, "model-B", fe.Dim())
	if _, ok := c2.Get("h1"); ok {
		t.Fatal("model switch must invalidate cache")
	}
}

// TestCacheInvalidatesOnFormatVersion guards the LFM2.5 MLX→llama.cpp migration:
// the embedding pipeline changed under the SAME model id + dim, so a cache
// written with an older cacheFormatVersion must be discarded rather than serving
// stale (incompatible) vectors.
func TestCacheInvalidatesOnFormatVersion(t *testing.T) {
	dir := t.TempDir()
	// Write a cache that matches on model + dim but carries a stale version.
	stale := &Cache{Version: cacheFormatVersion - 1, Model: "local/lfm2.5-embedding", Dim: 1024, Items: map[string][]float32{"h1": {1, 2, 3}}}
	stale.path = filepath.Join(dir, "corpus-"+sanitizeModelID("local/lfm2.5-embedding")+".json")
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCache(dir, "local/lfm2.5-embedding", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Get("h1"); ok {
		t.Fatal("stale cacheFormatVersion must invalidate cache (would serve mean-pooled vectors to a CLS-pooled backend)")
	}
	if got.Version != cacheFormatVersion {
		t.Fatalf("fresh cache version = %d, want %d", got.Version, cacheFormatVersion)
	}
}
