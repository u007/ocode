package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Doc is one rankable item: a skill or an MCP tool.
type Doc struct {
	ID   string // stable key, e.g. "skill:brainstorming" or "mcp:Notion/search"
	Kind string // "skill" | "mcp"
	Name string // display name, e.g. "brainstorming" or "Notion/search"
	Text string // text embedded as a passage (name + description [+ when_to_use])
}

// DocHash is the per-item cache key. Changing the embedded Text invalidates it.
func DocHash(d Doc) string {
	h := sha256.Sum256([]byte(d.Kind + "\x00" + d.Name + "\x00" + d.Text))
	return hex.EncodeToString(h[:])
}

// Corpus holds docs and their aligned passage vectors.
type Corpus struct {
	Docs []Doc
	Vecs [][]float32
}

// BuildCorpus embeds every doc as a Passage in a single batch call.
func BuildCorpus(ctx context.Context, e Embedder, docs []Doc) (*Corpus, error) {
	if len(docs) == 0 {
		return &Corpus{}, nil
	}
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.Text
	}
	vecs, err := e.Embed(ctx, texts, Passage)
	if err != nil {
		return nil, fmt.Errorf("embed corpus (%d docs): %w", len(docs), err)
	}
	if len(vecs) != len(docs) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d docs", len(vecs), len(docs))
	}
	return &Corpus{Docs: docs, Vecs: vecs}, nil
}

// Scored pairs a doc with its similarity to the query.
type Scored struct {
	Doc   Doc
	Score float32
}

// Rank scores every doc against the query and returns them sorted descending.
func (c *Corpus) Rank(query []float32) []Scored {
	out := make([]Scored, len(c.Docs))
	for i, d := range c.Docs {
		out[i] = Scored{Doc: d, Score: Cosine(query, c.Vecs[i])}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Selection policy constants (internal — not user-tunable).
const (
	SelectDelta float32 = 0.15 // keep items within this of the top score
	SelectMin   float32 = 0.40 // absolute minimum similarity to attach
	SelectFloor         = 0    // always attach at least this many (0 = only delta+min matches)
	SelectCap           = 30   // never attach more than this
)

// SelectRankRelative applies the rank-relative policy to a descending-sorted slice.
// Items must pass BOTH the relative delta (within SelectDelta of top score) AND
// the absolute minimum (>= SelectMin). SelectFloor ensures at least N items are
// returned even when all scores are low (0 = no floor).
func SelectRankRelative(ranked []Scored) []Scored {
	if len(ranked) == 0 {
		return nil
	}
	top := ranked[0].Score
	keep := 0
	for i, s := range ranked {
		within := top-s.Score <= SelectDelta
		passesMin := s.Score >= SelectMin
		if (i < SelectFloor || within) && passesMin {
			keep = i + 1
		}
	}
	if keep > SelectCap {
		keep = SelectCap
	}
	if keep > len(ranked) {
		keep = len(ranked)
	}
	return ranked[:keep]
}
