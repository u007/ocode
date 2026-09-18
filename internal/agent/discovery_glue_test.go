package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/redact"
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

func TestDiscoveryQuerySkipsProjectSignalForShortText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	short := discoveryQueryFromMessages([]Message{{Role: "user", Content: "use agent browser"}}, dir)
	if containsSubstr(short, "Project context") {
		t.Fatalf("short query must not carry project signal, got %q", short)
	}
	long := discoveryQueryFromMessages([]Message{{Role: "user", Content: "refactor this function to use context cancellation properly"}}, dir)
	if !containsSubstr(long, "Go golang project") {
		t.Fatalf("long query must carry project signal, got %q", long)
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
	on := LoadContext(map[string]bool{}, false, true, "", "")
	off := LoadContext(map[string]bool{}, false, false, "", "")
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
	// SkillTotal must equal the admitted corpus (what the names-index actually
	// contains) — Kaizen skills gated out for the active model must not inflate
	// the denominator, or /discover status's attached/total misleads.
	if st.SkillTotal != len(st.AllSkills) {
		t.Fatalf("SkillTotal = %d, want %d (admitted corpus size); AllSkills=%v", st.SkillTotal, len(st.AllSkills), st.AllSkills)
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

func TestInjectDiscoveryContextRedactionAwareness(t *testing.T) {
	a := newGateAgent()
	a.disco = &discoveryState{enabled: true,
		session: discovery.NewSession(discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir()))}
	a.config = &config.Config{}
	a.config.Ocode.Discovery.Enabled = true
	base := []Message{{Role: "user", Content: "hi"}}

	// Discovery on, redaction off: no redaction awareness.
	got := a.injectDiscoveryContext(base)
	if containsSubstr(got[len(got)-1].Content, "OCSEC") {
		t.Fatalf("redaction off must not include OCSEC awareness, got: %q", got[len(got)-1].Content)
	}

	// Discovery on, redaction on: includes redaction awareness as an extra system message.
	a.redactionEnabled = true
	a.redactionRegistry = redact.NewRegistry("test123")
	got = a.injectDiscoveryContext(base)
	last := got[len(got)-1]
	if last.Role != "system" {
		t.Fatalf("last message must be system when redaction is active, got role %q", last.Role)
	}
	if !containsSubstr(last.Content, "OCSEC") {
		t.Fatalf("redaction on must include OCSEC awareness, got: %q", last.Content)
	}
	if !containsSubstr(last.Content, "Redacted Secrets") {
		t.Fatalf("redaction awareness must mention Redacted Secrets, got: %q", last.Content)
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

// hy3Client reports the novita-hosted tencent/hy3 model so the Kaizen gate
// (modelMatchesTuned) admits the conduct tuning skill.
type hy3Client struct{ MockClient }

func (*hy3Client) GetModel() string { return "novita-ai/tencent/hy3" }

// repoRootForTest resolves the repo root from this test file's location so the
// project-local skills/kaizen tree is on the skill search path.
func repoRootForTest() string {
	_, f, _, _ := runtime.Caller(0) // internal/agent/discovery_glue_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(f), "..", ".."))
}

// TestKaizenSkillAdvertisedInDiscovery proves the delivery fix: with discovery
// enabled, the model-gated conduct tuning skill appears (names only) in BOTH the
// fail-open catalog and the active names-index — never dependent on the embedder.
func TestKaizenSkillAdvertisedInDiscovery(t *testing.T) {
	a := newGateAgent()
	a.client = &hy3Client{}
	a.workDir = repoRootForTest()
	a.config = &config.Config{}
	a.config.Ocode.Discovery.Enabled = true

	base := []Message{{Role: "user", Content: "hi"}}

	// Fail-open (embedder never resolved: a.disco == nil).
	failOpen := a.injectDiscoveryContext(base)
	if len(failOpen) != len(base)+1 {
		t.Fatalf("fail-open must append one catalog message, got %d", len(failOpen))
	}
	if !containsSubstr(failOpen[len(failOpen)-1].Content, "conduct-tuning-tencent-hy3") {
		t.Fatalf("fail-open catalog must advertise the tuning skill:\n%s", failOpen[len(failOpen)-1].Content)
	}

	// Active discovery: names-index (system message) must list it too.
	a.disco = &discoveryState{enabled: true,
		session: discovery.NewSession(discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir()))}
	active := a.injectDiscoveryContext(base)
	var names string
	for _, m := range active {
		if m.Role == "system" {
			names += m.Content
		}
	}
	if !containsSubstr(names, "conduct-tuning-tencent-hy3") {
		t.Fatalf("active names-index must list the tuning skill:\n%s", names)
	}
}

// --- TypeSafe discovery judge wiring (Tasks 3/4) -------------------------

// discoveryGlueTool is a tool with a distinct description so the discovery
// corpus text (name + ": " + description) ranks it predictably.
type discoveryGlueTool struct{ name, desc string }

func (d discoveryGlueTool) Name() string                       { return d.name }
func (d discoveryGlueTool) Description() string                { return d.desc }
func (d discoveryGlueTool) Definition() map[string]interface{} { return map[string]interface{}{"name": d.name} }
func (d discoveryGlueTool) Execute(json.RawMessage) (string, error) {
	return "", nil
}
func (d discoveryGlueTool) Parallel() bool { return false }

// newDiscoveryGlueAgent builds a discovery-enabled gate agent whose corpus is
// two MCP docs with strong, distinct descriptions, plus whatever skills the
// host machine has on its search path.
func newDiscoveryGlueAgent(t *testing.T) *Agent {
	t.Helper()
	a := newGateAgent()
	a.config = &config.Config{}
	a.tools["Notion/search"] = discoveryGlueTool{name: "Notion/search", desc: "search notion pages"}
	a.tools["Notion/update"] = discoveryGlueTool{name: "Notion/update", desc: "update notion pages"}
	eng := discovery.NewEngine(discovery.FakeEmbedder{Dimension: 64}, t.TempDir())
	a.disco = &discoveryState{
		enabled: true,
		engine:  eng,
		session: discovery.NewSession(eng),
	}
	return a
}

// discoveryGlueJudgeServer answers every noul question with defaultNoul, except
// ids in vetoed (answered 0.0), and counts requests so tests can assert when the
// judge was (not) consulted.
type discoveryGlueJudgeServer struct {
	mu          sync.Mutex
	requests    int
	body        map[string]any
	defaultNoul float64
	vetoed      map[string]bool
	status      int
}

func newDiscoveryGlueJudgeServer(t *testing.T, defaultNoul float64, vetoed map[string]bool, status int) (*discoveryGlueJudgeServer, *httptest.Server) {
	t.Helper()
	h := &discoveryGlueJudgeServer{defaultNoul: defaultNoul, vetoed: vetoed, status: status}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("judge decode: %v", err)
		}
		h.body = body
		if h.status != 0 {
			http.Error(w, "boom", h.status)
			return
		}
		answers := map[string]any{}
		if qs, ok := body["questions"].(map[string]any); ok {
			for id := range qs {
				noul := h.defaultNoul
				if h.vetoed[id] {
					noul = 0
				}
				answers[id] = map[string]any{"type": "noul", "noul": noul}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-latest",
			"answers": answers,
			"usage":   map[string]any{"input_tokens": 5, "output_tokens": 1},
		})
	}))
	t.Cleanup(srv.Close)
	return h, srv
}

func (h *discoveryGlueJudgeServer) requestCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.requests
}

func (h *discoveryGlueJudgeServer) asked(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	qs, ok := h.body["questions"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = qs[id]
	return ok
}

func (h *discoveryGlueJudgeServer) setVetoed(id string, veto bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.vetoed == nil {
		h.vetoed = map[string]bool{}
	}
	h.vetoed[id] = veto
}

func useJudgeFactory(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("k", "jev-latest", srv.URL)
	}
}

func useNonTypesafeFactory(t *testing.T) {
	t.Helper()
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient { return &MockClient{} }
}

const discoveryGlueQuery = "notion search pages update"

func TestRunDiscoveryNoJudgeWhenTypesafeNotConnected(t *testing.T) {
	a := newDiscoveryGlueAgent(t)
	h, _ := newDiscoveryGlueJudgeServer(t, 0.99, nil, 0)
	useNonTypesafeFactory(t)

	a.RunDiscovery(discoveryGlueQuery)
	if h.requestCount() != 0 {
		t.Fatalf("judge must not be called without a connected typesafe client, got %d requests", h.requestCount())
	}
	for _, id := range []string{"mcp:Notion/search", "mcp:Notion/update"} {
		if !a.disco.session.IsAttached(id) {
			t.Fatalf("%s must attach when the judge is not connected", id)
		}
	}
}

func TestRunDiscoveryJudgeVetoesCandidate(t *testing.T) {
	a := newDiscoveryGlueAgent(t)
	h, srv := newDiscoveryGlueJudgeServer(t, 0.99, map[string]bool{"mcp:Notion/update": true}, 0)
	useJudgeFactory(t, srv)

	var got string
	a.OnDiscovery = func(names string) { got = names }

	a.RunDiscovery(discoveryGlueQuery)

	if !h.asked("mcp:Notion/update") {
		t.Fatalf("vetoed doc must have been a candidate; questions=%v", h.body["questions"])
	}
	if a.disco.session.IsAttached("mcp:Notion/update") {
		t.Fatal("vetoed doc must not be attached")
	}
	if !a.disco.session.IsAttached("mcp:Notion/search") {
		t.Fatal("kept doc must be attached")
	}
	if strings.Contains(got, "Notion/update") {
		t.Fatalf("OnDiscovery must not name a vetoed doc, got %q", got)
	}
	if !strings.Contains(got, "Notion/search") {
		t.Fatalf("OnDiscovery must name the kept doc, got %q", got)
	}
}

func TestRunDiscoveryJudgeFailureAttachesAll(t *testing.T) {
	a := newDiscoveryGlueAgent(t)
	h, srv := newDiscoveryGlueJudgeServer(t, 0.99, nil, http.StatusInternalServerError)
	useJudgeFactory(t, srv)

	a.RunDiscovery(discoveryGlueQuery)

	if h.requestCount() != 1 {
		t.Fatalf("judge should have been attempted once, got %d", h.requestCount())
	}
	for _, id := range []string{"mcp:Notion/search", "mcp:Notion/update"} {
		if !a.disco.session.IsAttached(id) {
			t.Fatalf("%s must attach on judge failure (fail-open)", id)
		}
	}
}

func TestRunDiscoveryVetoedDocReJudgedNextTurn(t *testing.T) {
	a := newDiscoveryGlueAgent(t)
	h, srv := newDiscoveryGlueJudgeServer(t, 0.99, map[string]bool{"mcp:Notion/update": true}, 0)
	useJudgeFactory(t, srv)

	a.RunDiscovery(discoveryGlueQuery)
	if a.disco.session.IsAttached("mcp:Notion/update") {
		t.Fatal("update should be vetoed on the first turn")
	}
	if h.requestCount() != 1 {
		t.Fatalf("first turn should consult the judge once, got %d", h.requestCount())
	}

	// The doc was vetoed, so it is still an unattached candidate: the next turn
	// must re-judge it, and a change of verdict must attach it.
	h.setVetoed("mcp:Notion/update", false)
	a.RunDiscovery(discoveryGlueQuery)
	if h.requestCount() != 2 {
		t.Fatalf("vetoed doc must be re-judged next turn, got %d requests", h.requestCount())
	}
	if !a.disco.session.IsAttached("mcp:Notion/update") {
		t.Fatal("update must attach once the judge keeps it")
	}
}

func TestRunDiscoveryNoJudgeCallWhenNothingNew(t *testing.T) {
	a := newDiscoveryGlueAgent(t)
	h, srv := newDiscoveryGlueJudgeServer(t, 0.99, nil, 0)
	useJudgeFactory(t, srv)

	a.RunDiscovery(discoveryGlueQuery)
	if h.requestCount() != 1 {
		t.Fatalf("first turn should consult the judge once, got %d", h.requestCount())
	}
	a.RunDiscovery(discoveryGlueQuery)
	if h.requestCount() != 1 {
		t.Fatalf("no new candidates means no judge call, got %d requests", h.requestCount())
	}
}

func TestRunDiscoveryForMessagesSendsTranscriptTail(t *testing.T) {
	a := newDiscoveryGlueAgent(t)
	h, srv := newDiscoveryGlueJudgeServer(t, 0.99, nil, 0)
	useJudgeFactory(t, srv)

	a.RunDiscoveryForMessages([]Message{
		{Role: "user", Content: "please search notion pages and update them"},
	})
	if h.requestCount() != 1 {
		t.Fatalf("judge should be consulted once, got %d", h.requestCount())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.body["state"] == nil {
		t.Fatal("judge request must carry state")
	}
	state, _ := h.body["state"].(map[string]any)
	tail, _ := state["transcript_tail"].([]any)
	if len(tail) != 1 {
		t.Fatalf("RunDiscoveryForMessages must pass the messages as judge tail, got %v", state["transcript_tail"])
	}
}
