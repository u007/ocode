package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

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
