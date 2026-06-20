# Part 05 — Engine, Backend Resolution, Sticky Session

`Engine` owns the embedder + warmed corpus (shared). `Session` is per-agent-instance
and holds the grow-only sticky set. `ResolveEmbedder` picks the backend and is the
seam where Plan 2 plugs in the local/MLX embedder.

## Task 7: Engine + ResolveEmbedder + Session

**Files:**
- Create: `internal/discovery/engine.go`
- Test: `internal/discovery/engine_test.go`

**Interfaces:**
- Consumes: `Embedder`, `Doc`, `Corpus`, `BuildCorpusCached`, `LoadCache`, `Query`, `Rank`, `SelectRankRelative`, `HTTPModelByID`, `NewHTTPEmbedder`.
- Produces:
  - `func ResolveEmbedder(backend, modelID string, keyFor func(envVar string) string) (Embedder, error)`
  - `type Engine struct { ... }`; `func NewEngine(e Embedder, cacheDir string) *Engine`; `(*Engine) Warm(ctx, docs []Doc) error`; `(*Engine) Ready() bool`; `(*Engine) Rank(ctx, query string) ([]Scored, error)`
  - `type Session struct { ... }`; `func NewSession(eng *Engine) *Session`; `(*Session) Discover(ctx, query string) ([]Doc, error)`; `(*Session) IsAttached(id string) bool`; `(*Session) Attached() []string`; `(*Session) Seed(ids []string)`

- [ ] **Step 1: Write the failing test**

Create `internal/discovery/engine_test.go`:

```go
package discovery

import (
	"context"
	"testing"
)

func docsFixture() []Doc {
	return []Doc{
		{ID: "mcp:notion/notes", Kind: "mcp", Name: "notion/notes", Text: "query notion meeting notes"},
		{ID: "mcp:mail/send", Kind: "mcp", Name: "mail/send", Text: "send email to the team"},
		{ID: "mcp:rust/build", Kind: "mcp", Name: "rust/build", Text: "compile rust cargo binary"},
		{ID: "skill:pdf", Kind: "skill", Name: "pdf", Text: "manipulate pdf documents"},
		{ID: "skill:docx", Kind: "skill", Name: "docx", Text: "edit word documents"},
		{ID: "skill:brainstorm", Kind: "skill", Name: "brainstorm", Text: "explore ideas into designs"},
	}
}

func TestStickyGrowsNeverShrinks(t *testing.T) {
	eng := NewEngine(FakeEmbedder{Dimension: 128}, t.TempDir())
	if err := eng.Warm(context.Background(), docsFixture()); err != nil {
		t.Fatal(err)
	}
	if !eng.Ready() {
		t.Fatal("engine should be ready after warm")
	}
	s := NewSession(eng)

	added1, err := s.Discover(context.Background(), "summarize notion meeting notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(added1) == 0 || !s.IsAttached("mcp:notion/notes") {
		t.Fatalf("notion should attach on first query, added=%v", ids(added1))
	}
	before := len(s.Attached())

	// A different-topic query must only ADD, never remove the notion attachment.
	_, _ = s.Discover(context.Background(), "send email to the team")
	if !s.IsAttached("mcp:notion/notes") {
		t.Fatal("sticky set must not drop a previously attached item")
	}
	if !s.IsAttached("mcp:mail/send") {
		t.Fatal("new query should attach mail")
	}
	if len(s.Attached()) < before {
		t.Fatal("attached set must be grow-only")
	}
}

func TestResolveEmbedderIsHTTPOnly(t *testing.T) {
	// ResolveEmbedder handles HTTP only; the local backend is constructed in the
	// agent glue (it needs the process supervisor), so ResolveEmbedder("local")
	// returns an error and is never called for local.
	_, err := ResolveEmbedder("local", "lfm2-5", func(string) string { return "" })
	if err == nil {
		t.Fatal("ResolveEmbedder must not handle the local backend")
	}
}

func TestResolveEmbedderHTTPRequiresKey(t *testing.T) {
	if _, err := ResolveEmbedder("http", "openai/text-embedding-3-small", func(string) string { return "" }); err == nil {
		t.Fatal("missing key must be a hard error, not a silent default")
	}
	if _, err := ResolveEmbedder("http", "openai/text-embedding-3-small", func(string) string { return "k" }); err != nil {
		t.Fatalf("valid key should resolve: %v", err)
	}
}

func ids(ds []Doc) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.ID
	}
	return out
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run 'TestSticky|TestResolveEmbedder' -v`
Expected: FAIL — `NewEngine`/`ResolveEmbedder`/`Session` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/discovery/engine.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discovery/ -v`
Expected: PASS (all discovery package tests).

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/engine.go internal/discovery/engine_test.go
git commit -m "feat(discovery): engine, backend resolution, per-instance sticky session"
```
