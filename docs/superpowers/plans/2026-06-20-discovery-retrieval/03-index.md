# Part 03 — Corpus Index + Rank-Relative Selection

The corpus is pure data: a list of `Doc` (one per skill / per MCP tool) and their
vectors. Gathering raw skills/MCP into `[]Doc` happens later in the agent glue
(Part 06) to keep this package free of agent imports.

## Task 4: Doc + Corpus + BuildCorpus

**Files:**
- Create: `internal/discovery/index.go`
- Test: `internal/discovery/index_test.go`

**Interfaces:**
- Consumes: `Embedder`, `Passage` (Task 2).
- Produces:
  - `type Doc struct { ID, Kind, Name, Text string }` — `Kind` ∈ `"skill"`,`"mcp"`. `ID` is the stable lookup key (e.g. `"skill:brainstorming"`, `"mcp:Notion/search"`).
  - `func DocHash(d Doc) string` — `sha256(Kind+Name+Text)` hex, the cache key per item.
  - `type Corpus struct { Docs []Doc; Vecs [][]float32 }`
  - `func BuildCorpus(ctx context.Context, e Embedder, docs []Doc) (*Corpus, error)` — embeds all docs as `Passage` in one batch.

- [ ] **Step 1: Write the failing test**

Create `internal/discovery/index_test.go`:

```go
package discovery

import (
	"context"
	"testing"
)

func TestDocHashStable(t *testing.T) {
	d := Doc{ID: "skill:x", Kind: "skill", Name: "x", Text: "do things"}
	if DocHash(d) != DocHash(d) {
		t.Fatal("hash must be stable")
	}
	d2 := d
	d2.Text = "do other things"
	if DocHash(d) == DocHash(d2) {
		t.Fatal("hash must change with text")
	}
}

func TestBuildCorpus(t *testing.T) {
	docs := []Doc{
		{ID: "mcp:mail/send", Kind: "mcp", Name: "mail/send", Text: "send email message"},
		{ID: "mcp:rust/build", Kind: "mcp", Name: "rust/build", Text: "compile rust binary"},
	}
	c, err := BuildCorpus(context.Background(), FakeEmbedder{Dimension: 64}, docs)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Docs) != 2 || len(c.Vecs) != 2 || len(c.Vecs[0]) != 64 {
		t.Fatalf("bad corpus: docs=%d vecs=%d", len(c.Docs), len(c.Vecs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run 'TestDocHash|TestBuildCorpus' -v`
Expected: FAIL — undefined `Doc`/`BuildCorpus`.

- [ ] **Step 3: Write the implementation**

Create `internal/discovery/index.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discovery/ -run 'TestDocHash|TestBuildCorpus' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/index.go internal/discovery/index_test.go
git commit -m "feat(discovery): corpus docs + stable per-item hash + BuildCorpus"
```

---

## Task 5: Cosine ranking + rank-relative selection

**Files:**
- Modify: `internal/discovery/index.go`
- Test: `internal/discovery/index_test.go`

**Interfaces:**
- Consumes: `Corpus`, `Cosine`.
- Produces:
  - `type Scored struct { Doc Doc; Score float32 }`
  - `func (c *Corpus) Rank(query []float32) []Scored` — all docs, sorted score desc.
  - selection constants `const ( SelectDelta = 0.15; SelectFloor = 5; SelectCap = 30 )`
  - `func SelectRankRelative(ranked []Scored) []Scored` — keep within `SelectDelta` of top, bounded `[SelectFloor, SelectCap]`.

Selection rule, precisely: given `ranked` (desc): always keep the first `SelectFloor`
(or all if fewer); additionally keep any item with `top.Score - item.Score <= SelectDelta`;
never exceed `SelectCap`. Empty corpus → empty result.

- [ ] **Step 1: Write the failing test**

Append to `internal/discovery/index_test.go`:

```go
func TestRankAndSelect(t *testing.T) {
	// Build a corpus where two docs share words with the query.
	docs := []Doc{
		{ID: "mcp:notion/notes", Kind: "mcp", Name: "notion/notes", Text: "query notion meeting notes"},
		{ID: "mcp:mail/send", Kind: "mcp", Name: "mail/send", Text: "send email to team"},
		{ID: "mcp:rust/build", Kind: "mcp", Name: "rust/build", Text: "compile rust binary cargo"},
	}
	fe := FakeEmbedder{Dimension: 128}
	c, _ := BuildCorpus(context.Background(), fe, docs)
	qv, _ := fe.Embed(context.Background(), []string{"summarize notion meeting notes"}, Query)

	ranked := c.Rank(qv[0])
	if len(ranked) != 3 {
		t.Fatalf("rank should score all docs, got %d", len(ranked))
	}
	if ranked[0].Doc.ID != "mcp:notion/notes" {
		t.Fatalf("notion doc should rank top, got %s (%.3f)", ranked[0].Doc.ID, ranked[0].Score)
	}
	if ranked[0].Score < ranked[2].Score {
		t.Fatalf("ranking not sorted descending")
	}
}

func TestSelectRankRelativeBounds(t *testing.T) {
	// 40 docs all near top → cap at SelectCap.
	ranked := make([]Scored, 40)
	for i := range ranked {
		ranked[i] = Scored{Doc: Doc{ID: "d"}, Score: 0.9}
	}
	if got := len(SelectRankRelative(ranked)); got != SelectCap {
		t.Fatalf("cap not enforced: got %d want %d", got, SelectCap)
	}
	// 2 docs only → floor cannot exceed available; returns 2.
	if got := len(SelectRankRelative(ranked[:2])); got != 2 {
		t.Fatalf("floor must clamp to available: got %d", got)
	}
	// floor pulls in low scorers even outside delta.
	spread := []Scored{{Score: 0.9}, {Score: 0.2}, {Score: 0.1}, {Score: 0.05}, {Score: 0.01}, {Score: 0.0}}
	if got := len(SelectRankRelative(spread)); got != SelectFloor {
		t.Fatalf("floor must apply: got %d want %d", got, SelectFloor)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run 'TestRankAndSelect|TestSelectRankRelative' -v`
Expected: FAIL — `Rank`/`SelectRankRelative` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/discovery/index.go` (add `"sort"` to the import block):

```go
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

// Selection policy constants (internal — see spec; not user-tunable).
const (
	SelectDelta float32 = 0.15 // keep items within this of the top score
	SelectFloor         = 5    // always attach at least this many (or all if fewer)
	SelectCap           = 30   // never attach more than this
)

// SelectRankRelative applies the rank-relative policy to a descending-sorted slice.
func SelectRankRelative(ranked []Scored) []Scored {
	if len(ranked) == 0 {
		return nil
	}
	top := ranked[0].Score
	keep := 0
	for i, s := range ranked {
		within := top-s.Score <= SelectDelta
		if i < SelectFloor || within {
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discovery/ -run 'TestRankAndSelect|TestSelectRankRelative' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/index.go internal/discovery/index_test.go
git commit -m "feat(discovery): cosine ranking + rank-relative selection (delta/floor/cap)"
```
