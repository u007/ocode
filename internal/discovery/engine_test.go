package discovery

import (
	"context"
	"reflect"
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

func TestWarmBecomesReady(t *testing.T) {
	eng := NewEngine(FakeEmbedder{Dimension: 32}, t.TempDir())
	if eng.Ready() {
		t.Fatal("not ready before warm")
	}
	if err := eng.Warm(context.Background(), docsFixture()); err != nil {
		t.Fatalf("Warm failed: %v", err)
	}
	if !eng.Ready() {
		t.Fatal("Warm should make the engine ready")
	}
	// Second call with the same doc-set is a no-op (idempotent via docSetHash).
	if err := eng.Warm(context.Background(), docsFixture()); err != nil {
		t.Fatalf("second Warm failed: %v", err)
	}
	if !eng.Ready() {
		t.Fatal("idempotent Warm must keep ready")
	}
}

// TestSelectDoesNotAttach: Select is non-mutating — it returns rank-relative
// candidates without touching the sticky attached set. Seed is what attaches.
func TestSelectDoesNotAttach(t *testing.T) {
	eng := NewEngine(FakeEmbedder{Dimension: 128}, t.TempDir())
	if err := eng.Warm(context.Background(), docsFixture()); err != nil {
		t.Fatal(err)
	}
	s := NewSession(eng)
	got, err := s.Select(context.Background(), "summarize notion meeting notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("Select should return candidates for a matching query")
	}
	for _, d := range got {
		if s.IsAttached(d.ID) {
			t.Fatalf("Select must not attach %s", d.ID)
		}
	}
	if attached := s.Attached(); len(attached) != 0 {
		t.Fatalf("Select must leave the sticky set empty, got %v", attached)
	}
}

// TestSelectSkipsAlreadyAttached: seeded ids must not be returned again by
// Select, so the caller never re-judges or re-seeds an attached doc.
func TestSelectSkipsAlreadyAttached(t *testing.T) {
	eng := NewEngine(FakeEmbedder{Dimension: 128}, t.TempDir())
	// Both docs contain the full query, so both pass rank-relative selection.
	docs := []Doc{
		{ID: "skill:a", Kind: "skill", Name: "a", Text: "search notion meeting notes"},
		{ID: "skill:b", Kind: "skill", Name: "b", Text: "search notion meeting notes"},
	}
	if err := eng.Warm(context.Background(), docs); err != nil {
		t.Fatal(err)
	}
	s := NewSession(eng)
	q := "search notion meeting notes"
	first, err := s.Select(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 2 {
		t.Fatalf("fixture should select both docs, got %v", ids(first))
	}
	s.Seed([]string{first[0].ID})

	second, err := s.Select(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first)-1 {
		t.Fatalf("Select should return all but the attached doc: first=%v second=%v", ids(first), ids(second))
	}
	for _, d := range second {
		if d.ID == first[0].ID {
			t.Fatalf("already-attached %s must be skipped", d.ID)
		}
	}
}

// TestDiscoverEqualsSelectPlusSeed: Discover is exactly Select followed by
// Seed — same ids returned, and the sticky set reflects them afterward.
func TestDiscoverEqualsSelectPlusSeed(t *testing.T) {
	eng := NewEngine(FakeEmbedder{Dimension: 128}, t.TempDir())
	if err := eng.Warm(context.Background(), docsFixture()); err != nil {
		t.Fatal(err)
	}
	q := "manipulate pdf documents"

	selected, err := NewSession(eng).Select(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := NewSession(eng).Discover(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(selected), ids(discovered)) {
		t.Fatalf("Discover != Select+Seed: select=%v discover=%v", ids(selected), ids(discovered))
	}

	s := NewSession(eng)
	added, err := s.Discover(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range added {
		if !s.IsAttached(d.ID) {
			t.Fatalf("Discover must leave %s attached", d.ID)
		}
	}
}

func ids(ds []Doc) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.ID
	}
	return out
}
