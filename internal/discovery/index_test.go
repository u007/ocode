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
