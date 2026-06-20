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
