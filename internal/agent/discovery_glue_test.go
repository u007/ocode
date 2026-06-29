package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	q := discoveryQueryFromMessages(msgs, "")
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

func TestSyncPinnedSkillsSeedsAndUnpins(t *testing.T) {
	a := newGateAgent()
	a.config = &config.Config{}
	eng := discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir())
	a.disco = &discoveryState{
		enabled:    true,
		engine:     eng,
		session:    discovery.NewSession(eng),
		lastPinned: map[string]struct{}{},
	}

	// Pin two skills. After SyncPinnedSkills, the session should report them
	// as attached, and lastPinned should reflect the new set.
	a.config.Ocode.Discovery.PinnedSkills = []string{"alpha", "beta"}
	a.SyncPinnedSkills()
	attached := a.disco.session.Attached()
	if !containsStr(attached, "skill:alpha") || !containsStr(attached, "skill:beta") {
		t.Fatalf("expected alpha and beta to be attached, got %v", attached)
	}

	// Now simulate a discover_more attaching an unrelated MCP. It must
	// survive a subsequent SyncPinnedSkills that only re-seeds the pinned
	// set.
	a.disco.session.Seed([]string{"mcp:notion/notes"})
	a.SyncPinnedSkills() // no change to pinned set → must be a no-op
	attached = a.disco.session.Attached()
	if !containsStr(attached, "mcp:notion/notes") {
		t.Fatalf("discover_more MCP must be preserved across a no-op SyncPinnedSkills; got %v", attached)
	}

	// Unpin alpha. SyncPinnedSkills must drop skill:alpha from the
	// attached set while keeping skill:beta and the MCP.
	a.config.Ocode.Discovery.PinnedSkills = []string{"beta"}
	a.SyncPinnedSkills()
	attached = a.disco.session.Attached()
	if containsStr(attached, "skill:alpha") {
		t.Fatalf("unpinned alpha must be removed; got %v", attached)
	}
	if !containsStr(attached, "skill:beta") {
		t.Fatalf("still-pinned beta must remain; got %v", attached)
	}
	if !containsStr(attached, "mcp:notion/notes") {
		t.Fatalf("discover_more MCP must remain after re-seed; got %v", attached)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
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

func TestOnDiscoveryCallback(t *testing.T) {
	a := newGateAgent()
	eng := discovery.NewEngine(discovery.FakeEmbedder{Dimension: 64}, t.TempDir())
	_ = eng.Warm(context.Background(), []discovery.Doc{
		{ID: "mcp:Notion/search", Kind: "mcp", Name: "Notion/search", Text: "search notion"},
		{ID: "mcp:Notion/update", Kind: "mcp", Name: "Notion/update", Text: "update notion page"},
	})
	sess := discovery.NewSession(eng)
	a.disco = &discoveryState{enabled: true, engine: eng, session: sess}

	var got string
	a.OnDiscovery = func(names string) {
		got = names
	}

	// First call: both are new, both should be discovered
	a.RunDiscovery("search")
	if got == "" {
		t.Fatalf("OnDiscovery should have been called with names, got empty")
	}
	if !strings.Contains(got, "Notion/search") && !strings.Contains(got, "Notion/update") {
		t.Fatalf("expected Notion tools in discovered names, got %q", got)
	}
	first := got

	// Second call: nothing new — OnDiscovery should not fire
	got = ""
	a.RunDiscovery("search")
	if got != "" {
		t.Fatalf("OnDiscovery should not have been called when nothing new is attached, got %q", got)
	}

	// Reset callback and test with empty string
	got = ""
	a.OnDiscovery = func(names string) {
		got = names
	}
	// Seed a new session to clear state, then discover again
	sess2 := discovery.NewSession(eng)
	a.disco.session = sess2
	a.RunDiscovery("update")
	if got == "" {
		t.Fatal("OnDiscovery should fire for new session")
	}
	_ = first // used
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

func TestRenderDiscoverySplitIsCacheStable(t *testing.T) {
	// pdf's description is intentionally >40 chars so its name-index hint is
	// truncated — the full tail then appears ONLY in the volatile block.
	const pdfFull = "pdf: manipulate pdf documents, fill forms, merge, split, and extract pages from archives"
	const pdfTail = "extract pages from archives"
	const guideSummary = "how to deploy and roll back"
	const guideBody = "REAL FILE BODY SHOULD NOT APPEAR"
	root := t.TempDir()
	guidePath := filepath.Join(root, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(guidePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(guidePath, []byte(guideBody), 0o644); err != nil {
		t.Fatal(err)
	}
	docs := []discovery.Doc{
		{ID: "mcp:Notion/search", Kind: "mcp", Name: "Notion/search", Text: "Notion/search: search notion pages"},
		{ID: "skill:pdf", Kind: "skill", Name: "pdf", Text: pdfFull},
		{ID: "skill:brainstorm", Kind: "skill", Name: "brainstorm", Text: "brainstorm: explore ideas into designs"},
		{ID: "md:docs/guide.md", Kind: "md", Name: "docs/guide.md", Text: "docs/guide.md: " + guideSummary, Source: guidePath},
		{ID: "md:docs/notes.md", Kind: "md", Name: "docs/notes.md", Text: "docs/notes.md: meeting notes and backlog", Source: filepath.Join(root, "docs", "notes.md")},
	}

	// No skills attached.
	sysNone, volNone := renderDiscoveryContext(docs, func(string) bool { return false })
	// One skill attached.
	sysOne, volOne := renderDiscoveryContext(docs, func(id string) bool { return id == "skill:pdf" })
	mdVol := (&Agent{}).renderAttachedMarkdown(docs, func(id string) bool { return id == "md:docs/guide.md" })

	// CACHE INVARIANT: attaching a skill must NOT change the system block (it is
	// hoisted into the cached system prompt; any change busts the whole prompt).
	if sysNone != sysOne {
		t.Fatalf("system block must be independent of attachment (cache-stable)\nnone:\n%s\none:\n%s", sysNone, sysOne)
	}
	// The full description of an attached skill must live in the VOLATILE block,
	// never the cached system block.
	if containsSubstr(sysOne, pdfTail) {
		t.Fatal("full attached-skill description must not be in the cached system block")
	}
	if containsSubstr(sysOne, "docs/guide.md") || containsSubstr(sysOne, guideSummary) || containsSubstr(sysOne, "docs/notes.md") {
		t.Fatalf("unattached md docs must not appear in the cached system block: %q", sysOne)
	}
	if volNone != "" {
		t.Fatalf("no attachment → empty volatile block, got %q", volNone)
	}
	if !containsSubstr(volOne, pdfTail) {
		t.Fatalf("attached skill full description must be in the volatile block: %q", volOne)
	}
	if containsSubstr(volOne, "docs/guide.md") || containsSubstr(volOne, guideSummary) || containsSubstr(volOne, "docs/notes.md") {
		t.Fatalf("md docs must not be emitted in the skills volatile block: %q", volOne)
	}
	if !containsSubstr(mdVol, "docs/guide.md") || !containsSubstr(mdVol, guideSummary) {
		t.Fatalf("attached md docs must emit filename+summary only: %q", mdVol)
	}
	if containsSubstr(mdVol, guideBody) {
		t.Fatalf("attached md docs must not emit full file content: %q", mdVol)
	}
	if containsSubstr(mdVol, "docs/notes.md") {
		t.Fatalf("unattached md docs must not be emitted: %q", mdVol)
	}
	// System block still carries the full name index + contract.
	if !containsSubstr(sysOne, "Notion/search") || !containsSubstr(sysOne, "pdf") || !containsSubstr(sysOne, "discover_more") {
		t.Fatalf("system block must carry name index + contract: %q", sysOne)
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

func TestProjectSignals(t *testing.T) {
	dir := t.TempDir()
	// Empty dir → no signals.
	if got := projectSignals(dir); got != "" {
		t.Fatalf("empty dir should yield no signals, got %q", got)
	}
	// Empty workDir → no signals.
	if got := projectSignals(""); got != "" {
		t.Fatalf("empty workDir should yield no signals, got %q", got)
	}
	// Create go.mod in root.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module foo"), 0644); err != nil {
		t.Fatal(err)
	}
	got := projectSignals(dir)
	if !containsSubstr(got, "Go golang") {
		t.Fatalf("should detect go.mod in root: %q", got)
	}
	// Add pubspec.yaml in subdirectory (monorepo).
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "pubspec.yaml"), []byte("name: flutter_app"), 0644); err != nil {
		t.Fatal(err)
	}
	got = projectSignals(dir)
	if !containsSubstr(got, "Go golang") || !containsSubstr(got, "Flutter Dart") {
		t.Fatalf("monorepo should detect both: %q", got)
	}
	// Dedup: same marker in root and sub shouldn't duplicate.
	sub2 := filepath.Join(dir, "sub2")
	if err := os.MkdirAll(sub2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub2, "go.mod"), []byte("module bar"), 0644); err != nil {
		t.Fatal(err)
	}
	got = projectSignals(dir)
	if strings.Count(got, "Go golang") != 1 {
		t.Fatalf("should dedup signals: %q", got)
	}
}
