package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	write(".opencode/notes.md", "opencode")    // discovery default ignore → excluded
	write(".claude/notes.md", "claude")        // discovery default ignore → excluded
	write(".qwen/notes.md", "qwen")            // discovery default ignore → excluded
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

func TestWalkMarkdownFilesSkipsActiveBundle(t *testing.T) {
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
	// Active OKF bundle: docs/index.md carries the okf_version marker.
	write("docs/index.md", "---\nokf_version: \"0.1\"\n---\n# Index\n")
	write("docs/knowledge-bundle.md", "# bundle")
	write("docs/plugins.md", "# plugins")
	// Non-bundle docs must remain discoverable.
	write("README.md", "# readme")
	write("CHANGELOG.md", "# changes")

	got := walkMarkdownFiles(root)
	var rels []string
	for _, r := range got {
		rels = append(rels, r.rel)
	}
	sort.Strings(rels)
	want := []string{"CHANGELOG.md", "README.md"}
	if strings.Join(rels, ",") != strings.Join(want, ",") {
		t.Fatalf("walk with active bundle = %v, want %v (docs/ must be excluded)", rels, want)
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
		cachePath: mdSummaryCachePath(root),
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

func TestMDDiscoveryRequiresBoundProjectWorkDir(t *testing.T) {
	client := &fakeSummClient{reply: "should not be called"}
	a := &Agent{client: client, config: &config.Config{}}

	a.ensureMDState()

	if a.mdState == nil {
		t.Fatal("unbound markdown discovery should be marked inactive")
	}
	if got := atomic.LoadInt32(&client.calls); got != 0 {
		t.Fatalf("unbound discovery must not summarize files from process cwd: %d calls", got)
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
		cachePath: mdSummaryCachePath(root),
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

func TestRenderDiscoveryShowsAttachedMDSummariesOnly(t *testing.T) {
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

	// Names-index (system block) must not contain md docs at all.
	sys, _ := renderDiscoveryContext(docs, func(string) bool { return false })
	if strings.Contains(sys, "Project docs") || strings.Contains(sys, "docs/arch.md") || strings.Contains(sys, "architecture overview") {
		t.Fatalf("system block must not list md docs: %q", sys)
	}

	a := &Agent{}
	// Not attached → no content.
	if got := a.renderAttachedMarkdown(docs, func(string) bool { return false }); got != "" {
		t.Fatalf("unattached md must produce no content, got %q", got)
	}
	// Attached → filename + summary only in the volatile tail.
	got := a.renderAttachedMarkdown(docs, func(id string) bool { return id == "md:docs/arch.md" })
	if !strings.Contains(got, "docs/arch.md") || !strings.Contains(got, "architecture overview") {
		t.Fatalf("attached md must inject filename+summary: %q", got)
	}
	if strings.Contains(got, "full architecture detail") {
		t.Fatal("attached md must not inject full file content")
	}
}

// newSharedMDAgent builds an Agent with its own mdDiscoveryState (its own mutex)
// pointed at a shared cachePath — the in-process equivalent of a second ocode
// instance on the same project. The cache FILE is the only shared resource, so
// two of these model two processes faithfully.
func newSharedMDAgent(root, cachePath string, client LLMClient) *Agent {
	a := &Agent{client: client, workDir: root}
	a.mdState = &mdDiscoveryState{
		cache:     loadMDCache(cachePath),
		cachePath: cachePath,
		root:      root,
		client:    client,
	}
	return a
}

func writeMDFixture(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(root, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# "+n+"\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// shortenMDLockWait makes the contended-path tests fast. Production never writes
// mdLockAcquireTimeout.
func shortenMDLockWait(t *testing.T, d time.Duration) {
	t.Helper()
	prev := mdLockWait()
	mdLockAcquireTimeout.Store(int64(d))
	t.Cleanup(func() { mdLockAcquireTimeout.Store(int64(prev)) })
}

// holdMDCacheLock takes the project lock and returns only once it is ACTUALLY
// held, so a contending pass really does race a live peer rather than a sleep
// that happens to line up.
//
// Releasing is registered with t.Cleanup, not left to the caller: a t.Fatal skips
// the rest of the body, and a leaked holder would block every later test that
// touches this project cache. The returned func is idempotent so a test can also
// release early and still let cleanup run.
func holdMDCacheLock(t *testing.T, cachePath string) (release func()) {
	t.Helper()
	acquired := make(chan struct{})
	open := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = withMDCacheLock(cachePath, func() error {
			close(acquired)
			<-open
			return nil
		})
	}()
	// Bound the wait. If the lock cannot be taken, withMDCacheLock returns
	// without ever calling fn, so `acquired` is never closed — an unbounded
	// receive here would hang the whole suite instead of failing one test.
	select {
	case <-acquired:
	case <-done:
		t.Fatal("could not take the project lock to simulate a peer holding it")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting to take the project lock")
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			close(open)
			<-done
		})
	}
	t.Cleanup(release)
	return release
}

// The contended wait is added to the user's response time (refreshMDSummaries
// runs on the turn path), so the production default must stay short. Every
// contention test overrides it, which means nothing else pins this number —
// guard it directly so it cannot be raised to cover a long pass.
func TestMDLockAcquireTimeoutStaysShort(t *testing.T) {
	if mdLockWait() > 10*time.Second {
		t.Fatalf("lock wait of %s would stall the turn it runs on; keep it seconds-scale", mdLockWait())
	}
}

// When nothing is out of date the pass must NOT touch the lock at all — that is
// the steady state, and contending for a lock you do not need is what makes a
// second instance wait pointlessly on every turn.
//
// The instance under test must already be SYNCED (it has loaded the shared cache
// at least once), because a fresh instance legitimately needs the lock just to
// load it. With a generous acquire timeout, a pass that wrongly contended would
// visibly block.
func TestMDSummarizePassDoesNotLockWhenAlreadyUpToDate(t *testing.T) {
	shortenMDLockWait(t, 3*time.Second)

	root := t.TempDir()
	writeMDFixture(t, root, "README.md")
	cachePath := mdSummaryCachePath(root)

	client := &fakeSummClient{reply: "indexed."}
	a := newSharedMDAgent(root, cachePath, client)
	a.mdSummarizePass(root) // indexes, and marks the state synced
	if len(a.mdDocs()) != 1 {
		t.Fatal("precondition: the file should be indexed")
	}

	// A peer now holds the lock for far longer than the acquire timeout.
	release := holdMDCacheLock(t, cachePath)

	start := time.Now()
	a.mdSummarizePass(root)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("an up-to-date pass took %s — it must not contend for the lock at all", elapsed)
	}
	if got := atomic.LoadInt32(&client.calls); got != 1 {
		t.Fatalf("an up-to-date pass must make no further model calls, got %d total", got)
	}
	release()
}

// A second instance that cannot get the project lock must SKIP: zero model calls,
// no error, and — critically — no fall-through to an unlocked pass, which is
// exactly the duplicate work the lock exists to prevent.
func TestMDSummarizePassSkipsWhenAnotherInstanceHoldsTheLock(t *testing.T) {
	shortenMDLockWait(t, 200*time.Millisecond)

	root := t.TempDir()
	writeMDFixture(t, root, "README.md", "CHANGELOG.md")
	cachePath := mdSummaryCachePath(root)

	release := holdMDCacheLock(t, cachePath)
	defer release()

	client := &fakeSummClient{reply: "must not run."}
	a := newSharedMDAgent(root, cachePath, client)

	start := time.Now()
	a.mdSummarizePass(root) // must give up rather than block on the holder
	waited := time.Since(start)

	if got := atomic.LoadInt32(&client.calls); got != 0 {
		t.Fatalf("a skipped pass must make no model calls, got %d", got)
	}
	if waited > 2*time.Second {
		t.Fatalf("skipping must be bounded by the acquire timeout, waited %s", waited)
	}
	// Nothing was written, and no in-memory state was invented.
	if m := loadMDCache(cachePath); len(m) != 0 {
		t.Fatalf("a skipped pass must not write the cache, got %v", m)
	}
	if docs := a.mdDocs(); len(docs) != 0 {
		t.Fatalf("a skipped pass must not publish docs, got %+v", docs)
	}
}

// Once the holder is gone, the next pass picks the work up — skipping is a
// delay, not a loss.
func TestMDSummarizePassResumesAfterALockContentionClears(t *testing.T) {
	shortenMDLockWait(t, 200*time.Millisecond)

	root := t.TempDir()
	writeMDFixture(t, root, "README.md")
	cachePath := mdSummaryCachePath(root)

	client := &fakeSummClient{reply: "indexed later."}
	a := newSharedMDAgent(root, cachePath, client)

	release := holdMDCacheLock(t, cachePath)
	a.mdSummarizePass(root)
	if got := atomic.LoadInt32(&client.calls); got != 0 {
		t.Fatalf("contended pass must skip, got %d calls", got)
	}
	release()

	a.mdSummarizePass(root)
	if got := atomic.LoadInt32(&client.calls); got != 1 {
		t.Fatalf("after contention clears the file must be indexed, got %d calls", got)
	}
	docs := a.mdDocs()
	if len(docs) != 1 || !strings.Contains(docs[0].Text, "indexed later.") {
		t.Fatalf("expected the skipped doc to be indexed on the retry, got %+v", docs)
	}
}

// A pass that runs after another instance finished must adopt its summaries
// rather than re-summarizing: the freshness re-check reads the shared cache, not
// this instance's (possibly stale) in-memory view.
func TestMDSummarizePassAdoptsAnotherInstancesCompletedSummaries(t *testing.T) {
	root := t.TempDir()
	writeMDFixture(t, root, "README.md")
	cachePath := mdSummaryCachePath(root)

	a := newSharedMDAgent(root, cachePath, &fakeSummClient{reply: "from agent A."})
	a.mdSummarizePass(root)

	// Agent B, holding a cache snapshot taken BEFORE A wrote anything — the stale
	// in-memory view a long-lived process carries.
	bClient := &fakeSummClient{reply: "from agent B."}
	b := &Agent{client: bClient, workDir: root, mdState: &mdDiscoveryState{
		cache:     map[string]mdEntry{},
		cachePath: cachePath,
		root:      root,
		client:    bClient,
	}}
	b.mdSummarizePass(root)

	if got := atomic.LoadInt32(&bClient.calls); got != 0 {
		t.Fatalf("B must adopt A's on-disk summary, made %d calls", got)
	}
	docs := b.mdDocs()
	if len(docs) != 1 || !strings.Contains(docs[0].Text, "from agent A.") {
		t.Fatalf("must adopt agent A's summary, got %+v", docs)
	}
}

// A synced instance must notice files a PEER indexed after its last pass, even
// though every file it walked is itself unchanged. Its own mtime+size gates all
// pass, so only comparing the ready-doc count against the loaded snapshot can
// reveal that new work landed — without this, the instance keeps ranking a stale
// corpus indefinitely.
func TestMDSummarizePassPicksUpFilesAPeerIndexedAfterwards(t *testing.T) {
	root := t.TempDir()
	writeMDFixture(t, root, "README.md")
	cachePath := mdSummaryCachePath(root)

	// B indexes and syncs on a repo that has one doc.
	bClient := &fakeSummClient{reply: "readme summary."}
	b := newSharedMDAgent(root, cachePath, bClient)
	b.mdSummarizePass(root)
	if len(b.mdDocs()) != 1 {
		t.Fatal("precondition: B should have one doc")
	}

	// A new file appears, and a peer indexes it.
	writeMDFixture(t, root, "CHANGELOG.md")
	peerClient := &fakeSummClient{reply: "changelog summary."}
	peer := newSharedMDAgent(root, cachePath, peerClient)
	peer.mdSummarizePass(root)
	if got := atomic.LoadInt32(&peerClient.calls); got != 1 {
		t.Fatalf("precondition: the peer should have indexed the new file, got %d calls", got)
	}

	// B's own files are untouched, so only the count check can reveal the new doc.
	b.mdSummarizePass(root)

	if got := atomic.LoadInt32(&bClient.calls); got != 1 {
		t.Fatalf("B must adopt the peer's summary, not re-summarize: %d total calls", got)
	}
	docs := b.mdDocs()
	if len(docs) != 2 {
		t.Fatalf("B's corpus must include the peer's new doc, got %d docs", len(docs))
	}
	joined := docs[0].Text + docs[1].Text
	if !strings.Contains(joined, "readme summary.") || !strings.Contains(joined, "changelog summary.") {
		t.Fatalf("B must hold both summaries, got %+v", docs)
	}
}

// Re-check and write must be one critical section. If they were split, two
// instances could both read "missing" and both summarize. Several passes over
// one shared cache must therefore cost one summarize call per file in total.
func TestMDSummarizePassNeverRunsUnlockedWhenTheLockIsHeld(t *testing.T) {
	shortenMDLockWait(t, 200*time.Millisecond)

	root := t.TempDir()
	writeMDFixture(t, root, "README.md", "CHANGELOG.md")
	cachePath := mdSummaryCachePath(root)

	var passes, calls int32
	for i := 0; i < 4; i++ {
		client := &fakeSummClient{reply: "summary."}
		a := newSharedMDAgent(root, cachePath, client)
		atomic.AddInt32(&passes, 1)
		a.mdSummarizePass(root)
		atomic.AddInt32(&calls, client.calls)
	}

	// Two files, however many passes ran: the first indexes them and the rest
	// find them up to date.
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 summarize calls across all passes, got %d (passes=%d)", got, passes)
	}
	for _, n := range []string{"README.md", "CHANGELOG.md"} {
		if e := loadMDCache(cachePath)[n]; e.Summary != "summary." {
			t.Fatalf("%s must end up indexed, got %+v", n, e)
		}
	}
}
