package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
