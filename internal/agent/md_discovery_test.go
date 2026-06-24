package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
)

// fakeSummClient is an LLMClient that returns a fixed summary for every call.
type fakeSummClient struct {
	reply string
	calls int32
}

func (c *fakeSummClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	atomic.AddInt32(&c.calls, 1)
	return &Message{Role: "assistant", Content: c.reply}, nil
}
func (c *fakeSummClient) GetProvider() string { return "fake" }
func (c *fakeSummClient) GetModel() string    { return "fake-small" }

func TestMDIsAlwaysOn(t *testing.T) {
	on := []string{"AGENTS.md", "CLAUDE.md", "OCODE.md", ".cursorrules", ".opencode/rules/style.md", "sub/AGENTS.md"}
	for _, p := range on {
		if !mdIsAlwaysOn(p) {
			t.Errorf("%q should be always-on", p)
		}
	}
	off := []string{"README.md", "docs/guide.md", "docs/AGENTS.md.bak", ".opencode/skills/x/SKILL.md"}
	for _, p := range off {
		if mdIsAlwaysOn(p) {
			t.Errorf("%q should NOT be always-on", p)
		}
	}
}

func TestWalkMarkdownFiles(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# readme")
	write("docs/guide.md", "# guide")
	write("AGENTS.md", "briefing")             // always-on → excluded
	write(".opencode/rules/r.md", "rule")      // always-on → excluded
	write("node_modules/pkg/doc.md", "vendor") // ignored dir → excluded
	write("build/out.md", "generated")         // gitignored → excluded
	write("notes.txt", "not markdown")         // not md → excluded
	write(".gitignore", "build/\n")

	got := walkMarkdownFiles(root)
	var rels []string
	for _, r := range got {
		rels = append(rels, r.rel)
	}
	sort.Strings(rels)
	want := []string{"README.md", "docs/guide.md"}
	if strings.Join(rels, ",") != strings.Join(want, ",") {
		t.Fatalf("walk = %v, want %v", rels, want)
	}
}

func TestMDSummaryCacheRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "md-summaries.json")
	in := map[string]mdEntry{
		"docs/a.md": {Hash: "h1", Summary: "covers a", MTime: 1, Size: 2},
	}
	if err := saveMDCache(path, in); err != nil {
		t.Fatal(err)
	}
	out := loadMDCache(path)
	if out["docs/a.md"].Summary != "covers a" || out["docs/a.md"].Hash != "h1" {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
	// Missing file → empty map, no error.
	if m := loadMDCache(filepath.Join(dir, "nope.json")); len(m) != 0 {
		t.Fatalf("missing cache should be empty, got %v", m)
	}
}

func TestSanitizeMDSummary(t *testing.T) {
	cases := map[string]string{
		"  hello world  ":    "hello world",
		"\"quoted\"":         "quoted",
		"line one\nline two": "line one",
		"`code`":             "code",
	}
	for in, want := range cases {
		if got := sanitizeMDSummary(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMDSummarizePassBuildsSnapshotAndCachesByContent(t *testing.T) {
	root := t.TempDir()
	docPath := filepath.Join(root, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("# Guide\nHow to deploy."), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &fakeSummClient{reply: "Deployment guide."}
	a := &Agent{client: client, workDir: root}
	// Init state directly (ensureMDState would also launch a concurrent background
	// pass); drive the pass synchronously for determinism.
	a.mdState = &mdDiscoveryState{
		cache:     map[string]mdEntry{},
		cachePath: filepath.Join(root, ".ocode", "md-summaries.json"),
		root:      root,
		client:    client,
	}
	a.mdSummarizePass(root)

	docs := a.mdDocs()
	if len(docs) != 1 || docs[0].ID != "md:docs/guide.md" {
		t.Fatalf("expected one md doc, got %+v", docs)
	}
	if !strings.Contains(docs[0].Text, "Deployment guide.") {
		t.Fatalf("doc text must carry summary: %q", docs[0].Text)
	}
	firstCalls := atomic.LoadInt32(&client.calls)
	if firstCalls != 1 {
		t.Fatalf("expected 1 summarize call, got %d", firstCalls)
	}

	// Second pass with unchanged content → no new model calls (cache hit).
	a.mdSummarizePass(root)
	if got := atomic.LoadInt32(&client.calls); got != firstCalls {
		t.Fatalf("unchanged file must not re-summarize: calls %d → %d", firstCalls, got)
	}

	// Change content → summary regenerates (content-hash invalidation).
	if err := os.WriteFile(docPath, []byte("# Guide\nHow to roll back."), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mdSummarizePass(root)
	if got := atomic.LoadInt32(&client.calls); got != firstCalls+1 {
		t.Fatalf("changed file must re-summarize: calls %d → %d", firstCalls, got)
	}
}

func TestMDSummaryClientFallsBackToMainClient(t *testing.T) {
	client := &fakeSummClient{reply: "x"}
	root := t.TempDir()
	// No small model configured, but a main client is present → md discovery is
	// active and uses the main client (fallback by request).
	a := &Agent{client: client, config: &config.Config{}, workDir: root}
	a.config.Ocode.SmallModelEnabled = false
	if got := a.mdSummaryClient(); got == nil {
		t.Fatal("mdSummaryClient must fall back to the main client when no small model is set")
	}
	a.ensureMDState()
	if a.mdState == nil {
		t.Fatal("md discovery must be active when a main client is available")
	}

	// With no client at all → inactive.
	b := &Agent{config: &config.Config{}}
	b.ensureMDState()
	if b.mdState != nil {
		t.Fatal("md discovery must be inactive when there is no LLM client")
	}
}

func TestMDSummarizeFailureBacksOff(t *testing.T) {
	root := t.TempDir()
	docPath := filepath.Join(root, "docs", "x.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &fakeSummClient{reply: ""} // empty reply → summarization "fails"
	a := &Agent{client: client, workDir: root}
	a.mdState = &mdDiscoveryState{
		cache:     map[string]mdEntry{},
		cachePath: filepath.Join(root, ".ocode", "md-summaries.json"),
		root:      root,
		client:    client,
	}

	a.mdSummarizePass(root)
	if got := atomic.LoadInt32(&client.calls); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
	}
	if len(a.mdDocs()) != 0 {
		t.Fatal("failed summary must not produce a doc (no placeholder)")
	}
	// Second pass within backoff window → no retry (negative cache).
	a.mdSummarizePass(root)
	if got := atomic.LoadInt32(&client.calls); got != 1 {
		t.Fatalf("failed file must back off, not retry every scan: calls = %d", got)
	}
}

func TestRenderDiscoveryIncludesMDSectionAndAttachedContent(t *testing.T) {
	root := t.TempDir()
	docPath := filepath.Join(root, "docs", "arch.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const fullBody = "# Architecture\nThe full architecture detail lives here."
	if err := os.WriteFile(docPath, []byte(fullBody), 0o644); err != nil {
		t.Fatal(err)
	}

	docs := []discovery.Doc{
		{ID: "skill:pdf", Kind: "skill", Name: "pdf", Text: "pdf: work with pdfs"},
		{ID: "md:docs/arch.md", Kind: "md", Name: "docs/arch.md", Text: "docs/arch.md: architecture overview", Source: docPath},
	}

	// Names-index (system block) lists the md doc by name; it must NOT contain the
	// full file body (that would bust the cache and bloat the system prompt).
	sys, _ := renderDiscoveryContext(docs, func(string) bool { return false })
	if !strings.Contains(sys, "Project docs") || !strings.Contains(sys, "docs/arch.md") {
		t.Fatalf("system block must list md doc name: %q", sys)
	}
	if strings.Contains(sys, "full architecture detail") {
		t.Fatal("full md body must not be in the cached system block")
	}

	a := &Agent{}
	// Not attached → no full content.
	if got := a.renderAttachedMarkdown(docs, func(string) bool { return false }); got != "" {
		t.Fatalf("unattached md must produce no content, got %q", got)
	}
	// Attached → full file content in the volatile tail.
	got := a.renderAttachedMarkdown(docs, func(id string) bool { return id == "md:docs/arch.md" })
	if !strings.Contains(got, "full architecture detail") || !strings.Contains(got, "docs/arch.md") {
		t.Fatalf("attached md must inject full file content: %q", got)
	}
}
