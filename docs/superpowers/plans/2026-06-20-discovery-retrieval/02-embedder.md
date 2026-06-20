# Part 02 — Embedder Interface, Fake, and HTTP Backend

The `Embedder` abstraction the whole package is built on. `FakeEmbedder` makes
every downstream task testable without a network or model download.

## Task 2: Embedder interface + FakeEmbedder

**Files:**
- Create: `internal/discovery/embedder.go`
- Test: `internal/discovery/embedder_test.go`

**Interfaces:**
- Produces:
  - `type EmbedKind int` with `const ( Passage EmbedKind = iota; Query )`
  - `type Embedder interface { Embed(ctx context.Context, texts []string, kind EmbedKind) ([][]float32, error); ID() string; Dim() int }`
  - `type FakeEmbedder struct { Dimension int }` implementing `Embedder` deterministically (hash → vector)
  - `func Cosine(a, b []float32) float32`

- [ ] **Step 1: Write the failing test**

Create `internal/discovery/embedder_test.go`:

```go
package discovery

import (
	"context"
	"testing"
)

func TestFakeEmbedderDeterministic(t *testing.T) {
	fe := FakeEmbedder{Dimension: 64}
	a, err := fe.Embed(context.Background(), []string{"hello world"}, Passage)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := fe.Embed(context.Background(), []string{"hello world"}, Passage)
	if len(a) != 1 || len(a[0]) != 64 {
		t.Fatalf("want 1x64, got %dx%d", len(a), len(a[0]))
	}
	if Cosine(a[0], b[0]) < 0.999 {
		t.Fatalf("same text must embed identically")
	}
}

func TestFakeEmbedderDiscriminates(t *testing.T) {
	fe := FakeEmbedder{Dimension: 128}
	v, _ := fe.Embed(context.Background(),
		[]string{"send email to the team", "send email", "compile rust binary"}, Passage)
	near := Cosine(v[0], v[1])  // related
	far := Cosine(v[0], v[2])   // unrelated
	if near <= far {
		t.Fatalf("related texts must score higher: near=%.3f far=%.3f", near, far)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run TestFakeEmbedder -v`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/discovery/embedder.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discovery/ -run TestFakeEmbedder -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/embedder.go internal/discovery/embedder_test.go
git commit -m "feat(discovery): Embedder interface + deterministic FakeEmbedder + cosine"
```

---

## Task 3: HTTP embedder + curated model registry

**Files:**
- Create: `internal/discovery/http.go`
- Test: `internal/discovery/http_test.go`

**Interfaces:**
- Consumes: `Embedder`, `EmbedKind` (Task 2).
- Produces:
  - `type HTTPModel struct { ID, Provider, Endpoint, EnvVar string; Dimension int }`
  - `var HTTPModels []HTTPModel` (curated; sorted by ID)
  - `func HTTPModelByID(id string) (HTTPModel, bool)`
  - `func NewHTTPEmbedder(m HTTPModel, apiKey string) *httpEmbedder` implementing `Embedder`

Note: OpenAI-compatible `/v1/embeddings` ignores an input-type hint, so `kind` is
accepted but only affects local/LFM2 backends (Plan 2). The request batches all
texts in one call.

- [ ] **Step 1: Write the failing test** (uses an `httptest` server, no real network)

Create `internal/discovery/http_test.go`:

```go
package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPEmbedderParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testkey" {
			t.Errorf("missing auth header")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"embedding": []float32{0.1, 0.2, 0.3}},
				{"embedding": []float32{0.4, 0.5, 0.6}},
			},
		})
	}))
	defer srv.Close()

	m := HTTPModel{ID: "test/model", Endpoint: srv.URL, Dimension: 3}
	e := NewHTTPEmbedder(m, "testkey")
	vecs, err := e.Embed(context.Background(), []string{"a", "b"}, Passage)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 3 || vecs[1][2] != 0.6 {
		t.Fatalf("bad parse: %+v", vecs)
	}
}

func TestHTTPModelByID(t *testing.T) {
	if _, ok := HTTPModelByID("openai/text-embedding-3-small"); !ok {
		t.Fatalf("expected openai small model in registry")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run 'TestHTTP' -v`
Expected: FAIL — `NewHTTPEmbedder`/`HTTPModels` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/discovery/http.go`:

```go
package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// HTTPModel describes a curated embeddings model. Endpoint is the full URL of an
// OpenAI-compatible /v1/embeddings route. EnvVar names the API key variable.
type HTTPModel struct {
	ID        string
	Provider  string
	Endpoint  string
	EnvVar    string
	Dimension int
}

// HTTPModels is the curated list. NOT sourced from the models.dev chat registry
// (which has no embedding models). Keep sorted by ID.
var HTTPModels = func() []HTTPModel {
	m := []HTTPModel{
		{ID: "openai/text-embedding-3-small", Provider: "openai", Endpoint: "https://api.openai.com/v1/embeddings", EnvVar: "OPENAI_API_KEY", Dimension: 1536},
		{ID: "openai/text-embedding-3-large", Provider: "openai", Endpoint: "https://api.openai.com/v1/embeddings", EnvVar: "OPENAI_API_KEY", Dimension: 3072},
		{ID: "voyage/voyage-3", Provider: "voyage", Endpoint: "https://api.voyageai.com/v1/embeddings", EnvVar: "VOYAGE_API_KEY", Dimension: 1024},
		{ID: "voyage/voyage-3-lite", Provider: "voyage", Endpoint: "https://api.voyageai.com/v1/embeddings", EnvVar: "VOYAGE_API_KEY", Dimension: 512},
	}
	sort.Slice(m, func(i, j int) bool { return m[i].ID < m[j].ID })
	return m
}()

// HTTPModelByID looks up a curated model.
func HTTPModelByID(id string) (HTTPModel, bool) {
	for _, m := range HTTPModels {
		if m.ID == id {
			return m, true
		}
	}
	return HTTPModel{}, false
}

type httpEmbedder struct {
	model  HTTPModel
	apiKey string
	client *http.Client
}

// NewHTTPEmbedder builds an OpenAI-compatible embeddings client.
func NewHTTPEmbedder(m HTTPModel, apiKey string) *httpEmbedder {
	return &httpEmbedder{model: m, apiKey: apiKey, client: &http.Client{Timeout: 20 * time.Second}}
}

func (e *httpEmbedder) ID() string  { return e.model.ID }
func (e *httpEmbedder) Dim() int    { return e.model.Dimension }

func (e *httpEmbedder) Embed(ctx context.Context, texts []string, _ EmbedKind) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	// model field carries the bare model name after the provider/ prefix.
	bareModel := e.model.ID
	if i := indexByte(bareModel, '/'); i >= 0 {
		bareModel = bareModel[i+1:]
	}
	body, _ := json.Marshal(map[string]interface{}{"model": bareModel, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.model.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embeddings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request to %s: %w", e.model.Endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings %s returned status %d", e.model.ID, resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discovery/ -run 'TestHTTP' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/http.go internal/discovery/http_test.go
git commit -m "feat(discovery): OpenAI-compatible HTTP embedder + curated model registry"
```
