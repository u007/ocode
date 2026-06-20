package discovery

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// ResolveEmbedder selects a backend. keyFor returns the API key for an env var
// name (the agent layer wires this to its auth store). This is the seam where
// Plan 2 adds the local/MLX backend.
func ResolveEmbedder(backend, modelID string, keyFor func(envVar string) string) (Embedder, error) {
	switch backend {
	case "", "http":
		m, ok := HTTPModelByID(modelID)
		if !ok {
			return nil, fmt.Errorf("unknown embedding model %q; run /discover model to choose one", modelID)
		}
		key := keyFor(m.EnvVar)
		if key == "" {
			return nil, fmt.Errorf("no API key for %s (set %s); pick a model you have a key for via /discover model, or use the local backend", m.ID, m.EnvVar)
		}
		return NewHTTPEmbedder(m, key), nil
	case "local":
		// The local backend is constructed in the agent glue (Part 13): it needs
		// the process supervisor to spawn the shared model server, which this pure
		// function doesn't have. So ResolveEmbedder never handles local.
		return nil, fmt.Errorf("local backend is constructed by the agent, not ResolveEmbedder")
	default:
		return nil, fmt.Errorf("unknown embedding backend %q", backend)
	}
}

// Engine holds the embedder and the warmed corpus (shared across sessions).
type Engine struct {
	mu       sync.RWMutex
	embedder Embedder
	cacheDir string
	corpus   *Corpus
	docHash  string // identity of the doc-set the corpus was built from
	// warmOnce guards the background warm goroutine so WarmAsync is idempotent
	// (safe to call every turn).
	warmOnce sync.Once
	warming  bool
}

func NewEngine(e Embedder, cacheDir string) *Engine {
	return &Engine{embedder: e, cacheDir: cacheDir}
}

// Warm builds (or rebuilds, if the doc-set changed) the corpus from cache.
func (eng *Engine) Warm(ctx context.Context, docs []Doc) error {
	h := docSetHash(docs)
	eng.mu.RLock()
	already := eng.corpus != nil && eng.docHash == h
	eng.mu.RUnlock()
	if already {
		return nil
	}
	cache, err := LoadCache(eng.cacheDir, eng.embedder.ID(), eng.embedder.Dim())
	if err != nil {
		return fmt.Errorf("load discovery cache: %w", err)
	}
	corpus, misses, err := BuildCorpusCached(ctx, eng.embedder, docs, cache)
	if err != nil {
		return err
	}
	emitDiscoveryDebug("DISCOVERY", fmt.Sprintf("corpus warm: %d docs (%d embedded) model=%s dim=%d",
		len(docs), misses, eng.embedder.ID(), eng.embedder.Dim()))
	eng.mu.Lock()
	eng.corpus = corpus
	eng.docHash = h
	eng.mu.Unlock()
	return nil
}

func (eng *Engine) Ready() bool {
	eng.mu.RLock()
	defer eng.mu.RUnlock()
	return eng.corpus != nil
}

// WarmAsync warms the corpus in a background goroutine, once. Safe to call every
// turn; subsequent calls are no-ops while/after warming. Until the corpus is
// ready, callers must treat the engine as "not gating" (Ready() returns false).
func (eng *Engine) WarmAsync(docs []Doc) {
	eng.warmOnce.Do(func() {
		eng.mu.Lock()
		eng.warming = true
		eng.mu.Unlock()
		go func() {
			if err := eng.Warm(context.Background(), docs); err != nil {
				emitDiscoveryDebug("WARN", "background warm failed: "+err.Error())
			}
			eng.mu.Lock()
			eng.warming = false
			eng.mu.Unlock()
		}()
	})
}

// Rank embeds the query and scores every corpus doc.
func (eng *Engine) Rank(ctx context.Context, query string) ([]Scored, error) {
	eng.mu.RLock()
	corpus := eng.corpus
	eng.mu.RUnlock()
	if corpus == nil || len(corpus.Docs) == 0 {
		return nil, nil
	}
	qv, err := eng.embedder.Embed(ctx, []string{query}, Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(qv) != 1 {
		return nil, fmt.Errorf("embedder returned %d query vectors", len(qv))
	}
	return corpus.Rank(qv[0]), nil
}

// Session is per-agent-instance: a grow-only sticky attached set.
type Session struct {
	eng      *Engine
	mu       sync.RWMutex
	attached map[string]bool
}

func NewSession(eng *Engine) *Session {
	return &Session{eng: eng, attached: map[string]bool{}}
}

// Discover ranks query against the corpus, selects rank-relative, and adds any
// new selections to the sticky set. Returns the docs newly added this call.
func (s *Session) Discover(ctx context.Context, query string) ([]Doc, error) {
	ranked, err := s.eng.Rank(ctx, query)
	if err != nil {
		return nil, err
	}
	selected := SelectRankRelative(ranked)
	var added []Doc
	s.mu.Lock()
	for _, sc := range selected {
		if !s.attached[sc.Doc.ID] {
			s.attached[sc.Doc.ID] = true
			added = append(added, sc.Doc)
		}
	}
	s.mu.Unlock()
	return added, nil
}

// Seed marks ids as attached without ranking (used to restore a resumed session
// from tools already referenced in its history).
func (s *Session) Seed(ids []string) {
	s.mu.Lock()
	for _, id := range ids {
		s.attached[id] = true
	}
	s.mu.Unlock()
}

func (s *Session) IsAttached(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attached[id]
}

func (s *Session) Attached() []string {
	s.mu.RLock()
	out := make([]string, 0, len(s.attached))
	for id := range s.attached {
		out = append(out, id)
	}
	s.mu.RUnlock()
	sort.Strings(out)
	return out
}

func docSetHash(docs []Doc) string {
	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = DocHash(d)
	}
	sort.Strings(ids)
	h := ""
	for _, id := range ids {
		h += id
	}
	return h
}
