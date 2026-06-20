package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/tool"
)

type fakeTool struct{ name string }

func (f fakeTool) Name() string                            { return f.name }
func (f fakeTool) Description() string                     { return f.name }
func (f fakeTool) Definition() map[string]interface{}      { return map[string]interface{}{"name": f.name} }
func (f fakeTool) Execute(json.RawMessage) (string, error) { return "", nil }
func (f fakeTool) Parallel() bool                          { return false }

func newGateAgent() *Agent {
	a := &Agent{
		tools:    map[string]tool.Tool{},
		mcpTools: map[string]struct{}{},
	}
	a.tools["read"] = fakeTool{"read"} // built-in: never gated
	a.tools["Notion/search"] = fakeTool{"Notion/search"}
	a.tools["Notion/update"] = fakeTool{"Notion/update"}
	a.mcpTools["Notion/search"] = struct{}{}
	a.mcpTools["Notion/update"] = struct{}{}
	return a
}

func TestGateOffAttachesEverythingSorted(t *testing.T) {
	a := newGateAgent() // a.disco == nil → discovery off
	defs := a.GetToolDefinitions()
	var names []string
	for _, d := range defs {
		names = append(names, d["name"].(string))
	}
	want := []string{"Notion/search", "Notion/update", "read"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("off path must include all, sorted: got %v want %v", names, want)
	}
}

func TestGateOnFiltersMCPOnly(t *testing.T) {
	a := newGateAgent()
	eng := discovery.NewEngine(discovery.FakeEmbedder{Dimension: 64}, t.TempDir())
	_ = eng.Warm(context.Background(), []discovery.Doc{
		{ID: "mcp:Notion/search", Kind: "mcp", Name: "Notion/search", Text: "search notion"},
		{ID: "mcp:Notion/update", Kind: "mcp", Name: "Notion/update", Text: "update notion page"},
	})
	sess := discovery.NewSession(eng)
	sess.Seed([]string{"mcp:Notion/search"}) // only search attached
	a.disco = &discoveryState{enabled: true, engine: eng, session: sess}

	var names []string
	for _, d := range a.GetToolDefinitions() {
		names = append(names, d["name"].(string))
	}
	want := []string{"Notion/search", "read"} // update gated out; read (built-in) stays
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("on path should gate unattached MCP only: got %v want %v", names, want)
	}
}

func TestDiscoveryQueryUsesRecentUserTurns(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "set up notion sync"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "yes"},
	}
	q := discoveryQueryFromMessages(msgs)
	if !containsSubstr(q, "notion") || !containsSubstr(q, "yes") {
		t.Fatalf("query must blend recent user turns, got %q", q)
	}
}

func containsSubstr(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestSkillsJoinCorpusAndIndex(t *testing.T) {
	a := newGateAgent()
	a.config = &config.Config{}
	a.config.Ocode.Discovery.Enabled = true
	// Active discovery with an empty corpus engine is fine for the index test;
	// injectDiscoveryContext lists docs from discoveryDocs(), not the corpus.
	a.disco = &discoveryState{enabled: true,
		session: discovery.NewSession(discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir()))}

	got := a.injectDiscoveryContext([]Message{{Role: "user", Content: "hi"}})
	last := got[len(got)-1].Content
	if !containsSubstr(last, "Notion/search") {
		t.Fatalf("MCP tools must appear in the index: %q", last)
	}
	// discoveryDocs now also returns skills (from skill.LoadSkills); the section
	// header must be present even if this test env has no skills installed.
	if !containsSubstr(last, "Available skills") {
		t.Fatalf("skill index section header must be present: %q", last)
	}
}

func TestLoadContextSuppressesCatalogWhenDiscoveryOn(t *testing.T) {
	on := LoadContext(map[string]bool{}, false, true)
	off := LoadContext(map[string]bool{}, false, false)
	// The catalog header only appears when there ARE skills; assert the flag at
	// least never ADDS the catalog when on. (If skills exist, off contains it; on must not.)
	if containsSubstr(on, "--- Skill Catalog ---") {
		t.Fatalf("discoveryOn must suppress the skill catalog")
	}
	_ = off
}

func TestDiscoveryStatusAndReset(t *testing.T) {
	a := newGateAgent()
	a.config = &config.Config{}
	a.config.Ocode.Discovery.EmbeddingModel = "openai/text-embedding-3-small"
	a.config.Ocode.Discovery.EmbeddingBackend = "http"
	a.disco = &discoveryState{enabled: true, session: discovery.NewSession(
		discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir()))}

	st := a.DiscoveryStatus()
	if !st.Active || st.Model != "openai/text-embedding-3-small" || st.MCPTotal != 2 {
		t.Fatalf("bad status: %+v", st)
	}
	a.ResetDiscovery()
	if a.disco != nil {
		t.Fatal("ResetDiscovery must clear state so it re-inits next turn")
	}
}

func TestMarkMCPFromParent(t *testing.T) {
	parent := newGateAgent() // has Notion/search, Notion/update as MCP
	child := &Agent{
		tools:    map[string]tool.Tool{"Notion/search": fakeTool{"Notion/search"}, "read": fakeTool{"read"}},
		mcpTools: map[string]struct{}{},
	}
	child.markMCPFrom(parent)
	if _, ok := child.mcpTools["Notion/search"]; !ok {
		t.Fatal("child should inherit the MCP marker for tools it has")
	}
	if _, ok := child.mcpTools["Notion/update"]; ok {
		t.Fatal("child must not mark MCP tools it doesn't have")
	}
	if _, ok := child.mcpTools["read"]; ok {
		t.Fatal("read is not an MCP tool")
	}
}

func TestDiscoverMoreAttaches(t *testing.T) {
	a := newGateAgent()
	eng := discovery.NewEngine(discovery.FakeEmbedder{Dimension: 128}, t.TempDir())
	_ = eng.Warm(context.Background(), []discovery.Doc{
		{ID: "mcp:Notion/search", Kind: "mcp", Name: "Notion/search", Text: "search notion pages"},
		{ID: "mcp:Notion/update", Kind: "mcp", Name: "Notion/update", Text: "update notion page content"},
	})
	a.disco = &discoveryState{enabled: true, engine: eng, session: discovery.NewSession(eng)}

	tl := discoverMoreTool{agent: a}
	if tl.Name() != "discover_more" {
		t.Fatalf("name = %s", tl.Name())
	}
	out, err := tl.Execute([]byte(`{"need":"search notion pages"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !a.disco.session.IsAttached("mcp:Notion/search") {
		t.Fatalf("discover_more should attach the matching tool; out=%q", out)
	}
}

func TestInjectDiscoveryContextOnlyWhenActive(t *testing.T) {
	a := newGateAgent()
	base := []Message{{Role: "user", Content: "hi"}}

	// Off: byte-identical (no-op).
	if got := a.injectDiscoveryContext(base); len(got) != len(base) {
		t.Fatalf("off must be a no-op, got %d msgs", len(got))
	}

	// On: appends one system message naming every MCP tool + the contract.
	a.disco = &discoveryState{enabled: true,
		session: discovery.NewSession(discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir()))}
	a.config = &config.Config{}
	a.config.Ocode.Discovery.Enabled = true
	got := a.injectDiscoveryContext(base)
	if len(got) != len(base)+1 {
		t.Fatalf("on must append one tail message, got %d", len(got))
	}
	last := got[len(got)-1]
	if last.Role != "system" {
		t.Fatalf("tail must be a system message")
	}
	if !containsSubstr(last.Content, "Notion/search") || !containsSubstr(last.Content, "Notion/update") {
		t.Fatalf("name index must list all MCP tools: %q", last.Content)
	}
	if !containsSubstr(last.Content, "discover_more") {
		t.Fatalf("prompt contract must mention discover_more")
	}
}
