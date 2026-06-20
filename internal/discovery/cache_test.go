package discovery

import (
	"context"
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
