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
	// 2 docs only → cannot exceed available; returns 2.
	if got := len(SelectRankRelative(ranked[:2])); got != 2 {
		t.Fatalf("must clamp to available: got %d", got)
	}
	// All scores below SelectMin → nothing passes, even with delta.
	low := []Scored{{Score: 0.9}, {Score: 0.2}, {Score: 0.1}, {Score: 0.05}, {Score: 0.01}, {Score: 0.0}}
	if got := len(SelectRankRelative(low)); got != 1 {
		t.Fatalf("only top+above-min should pass: got %d want 1", got)
	}
	// All scores below SelectMin → empty result.
	allLow := []Scored{{Score: 0.3}, {Score: 0.2}, {Score: 0.1}}
	if got := len(SelectRankRelative(allLow)); got != 0 {
		t.Fatalf("all below SelectMin should yield 0: got %d", got)
	}
}
