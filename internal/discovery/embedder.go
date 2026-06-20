// Package discovery ranks skills and MCP tools by semantic similarity to the
// current task so only relevant ones are attached to the LLM context.
package discovery

import (
	"context"
	"hash/fnv"
	"math"
)

// EmbedKind selects asymmetric encoding: documents (corpus) vs queries.
type EmbedKind int

const (
	Passage EmbedKind = iota // skill / MCP tool descriptions
	Query                    // the task / user message
)

// Embedder turns text into vectors. Implementations: httpEmbedder, FakeEmbedder,
// and (Plan 2) the local LFM2-5 backend.
type Embedder interface {
	Embed(ctx context.Context, texts []string, kind EmbedKind) ([][]float32, error)
	ID() string
	Dim() int
}

// FakeEmbedder produces deterministic vectors from word hashes. Test-only:
// related texts (shared words) score higher than unrelated ones, which is all
// the selection logic needs to be exercised.
type FakeEmbedder struct{ Dimension int }

func (f FakeEmbedder) ID() string { return "fake/deterministic" }
func (f FakeEmbedder) Dim() int {
	if f.Dimension == 0 {
		return 64
	}
	return f.Dimension
}

func (f FakeEmbedder) Embed(_ context.Context, texts []string, _ EmbedKind) ([][]float32, error) {
	out := make([][]float32, len(texts))
	dim := f.Dim()
	for i, t := range texts {
		vec := make([]float32, dim)
		for _, w := range splitWords(t) {
			h := fnv.New32a()
			_, _ = h.Write([]byte(w))
			vec[int(h.Sum32())%dim] += 1
		}
		normalize(vec)
		out[i] = vec
	}
	return out, nil
}

func splitWords(s string) []string {
	var words []string
	cur := make([]rune, 0, 16)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == '/' || r == '-' || r == '_' {
			flush()
			continue
		}
		cur = append(cur, r|0x20) // cheap ASCII lowercase
	}
	flush()
	return words
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	n := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= n
	}
}

// Cosine returns the cosine similarity of two equal-length vectors.
func Cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
