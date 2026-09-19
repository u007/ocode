package agent

import (
	"fmt"
	"testing"
)

// modelContextBenchIDs builds a realistic-sized picker id set (~8k models,
// matching the live models.dev registry the web picker lists).
func modelContextBenchIDs() []string {
	ids := make([]string, 0, 8000)
	for p := 0; p < 40; p++ {
		for m := 0; m < 200; m++ {
			ids = append(ids, fmt.Sprintf("provider-%d/model-%d", p, m))
		}
	}
	return ids
}

// BenchmarkModelContextKindsAt measures the batched annotation used by
// HandleListModels (one directory scan for the whole list).
func BenchmarkModelContextKindsAt(b *testing.B) {
	root := b.TempDir()
	ids := modelContextBenchIDs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ModelContextKindsAt(root, ids)
	}
}

// BenchmarkLoadModelContextWithSourceAtPerModel measures the previous
// per-model scan pattern (loadModelContextWithSource once per id) for contrast.
func BenchmarkLoadModelContextWithSourceAtPerModel(b *testing.B) {
	root := b.TempDir()
	ids := modelContextBenchIDs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, id := range ids {
			_ = LoadModelContextWithSourceAt(root, id).Kind
		}
	}
}
