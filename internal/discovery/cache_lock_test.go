package discovery

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingEmbedder wraps FakeEmbedder and records every text handed to Embed, so
// a test can assert not just the miss COUNT but which docs were embedded — the
// only way to catch a duplicated corpus build.
type countingEmbedder struct {
	FakeEmbedder
	calls   atomic.Int32
	texts   atomic.Int64
	mu      sync.Mutex
	seen    []string
	block   chan struct{} // when non-nil, Embed waits on it
	entered chan struct{}
	once    sync.Once
}

func newCountingEmbedder(dim int) *countingEmbedder {
	return &countingEmbedder{FakeEmbedder: FakeEmbedder{Dimension: dim}}
}

func (c *countingEmbedder) Embed(ctx context.Context, texts []string, k EmbedKind) ([][]float32, error) {
	c.calls.Add(1)
	c.texts.Add(int64(len(texts)))
	c.mu.Lock()
	c.seen = append(c.seen, texts...)
	c.mu.Unlock()
	if c.entered != nil {
		c.once.Do(func() { close(c.entered) })
	}
	if c.block != nil {
		<-c.block
	}
	return c.FakeEmbedder.Embed(ctx, texts, k)
}

func corpusDocsFixture() []Doc {
	return []Doc{
		{ID: "skill:a", Kind: "skill", Name: "a", Text: "alpha text"},
		{ID: "skill:b", Kind: "skill", Name: "b", Text: "beta text"},
		{ID: "skill:c", Kind: "skill", Name: "c", Text: "gamma text"},
	}
}

// shortenCorpusLockWait makes contended-path tests fast. Production never writes
// corpusLockTimeout.
func shortenCorpusLockWait(t *testing.T, d time.Duration) {
	t.Helper()
	prev := corpusLockWait()
	corpusLockTimeout.Store(int64(d))
	t.Cleanup(func() { corpusLockTimeout.Store(int64(prev)) })
}

// holdCorpusLock takes the corpus lock and returns only once it is ACTUALLY held.
// Release is registered with t.Cleanup: a t.Fatal skips the rest of the body, and
// a leaked holder would wedge every later test using the same cache dir.
func holdCorpusLock(t *testing.T, cachePath string) (release func()) {
	t.Helper()
	acquired := make(chan struct{})
	open := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = withCorpusLock(context.Background(), cachePath, time.Minute, func() error {
			close(acquired)
			<-open
			return nil
		})
	}()
	select {
	case <-acquired:
	case <-done:
		t.Fatal("could not take the corpus lock to simulate a peer holding it")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting to take the corpus lock")
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

// When every doc is already cached there is nothing to embed and nothing to
// write, so the build must not contend for the lock at all. Contending
// unconditionally is what makes a second instance wait on every turn.
func TestBuildCorpusCachedDoesNotLockWhenFullyCached(t *testing.T) {
	dir := t.TempDir()
	docs := corpusDocsFixture()
	fe := newCountingEmbedder(64)

	seed, err := LoadCache(dir, fe.ID(), fe.Dim())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildCorpusCached(context.Background(), fe, docs, seed); err != nil {
		t.Fatal(err)
	}

	// Hold the lock far longer than any sane acquire budget.
	release := holdCorpusLock(t, seed.path)

	warm, err := LoadCache(dir, fe.ID(), fe.Dim())
	if err != nil {
		t.Fatal(err)
	}
	before := fe.calls.Load()
	start := time.Now()
	corpus, misses, err := BuildCorpusCached(context.Background(), fe, docs, warm)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("a fully-cached build must not need the lock, got %v", err)
	}
	if misses != 0 {
		t.Fatalf("expected 0 misses, got %d", misses)
	}
	if got := fe.calls.Load() - before; got != 0 {
		t.Fatalf("a fully-cached build must not embed, got %d embed calls", got)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("a fully-cached build waited %s — it must not touch the lock", elapsed)
	}
	if corpus == nil || len(corpus.Docs) != len(docs) {
		t.Fatalf("expected a full corpus, got %+v", corpus)
	}
	release()
}

// A second instance that cannot take the lock must SKIP, not embed anyway.
// Embedding regardless is exactly the duplicate work the lock prevents: the
// holder is building the very same corpus.
func TestBuildCorpusCachedSkipsWhenAnotherInstanceHoldsTheLock(t *testing.T) {
	shortenCorpusLockWait(t, 200*time.Millisecond)

	dir := t.TempDir()
	docs := corpusDocsFixture()
	fe := newCountingEmbedder(64)

	cold, err := LoadCache(dir, fe.ID(), fe.Dim())
	if err != nil {
		t.Fatal(err)
	}
	release := holdCorpusLock(t, cold.path)
	defer release()

	_, _, err = BuildCorpusCached(context.Background(), fe, docs, cold)
	if err == nil {
		t.Fatal("building against a held lock must report that it was skipped")
	}
	if !isCorpusLocked(err) {
		t.Fatalf("want a corpus-locked error, got %v", err)
	}
	if got := fe.calls.Load(); got != 0 {
		t.Fatalf("a skipped build must not embed, got %d calls", got)
	}
	// Nothing was persisted, so the holder's results cannot be clobbered.
	if reloaded, _ := LoadCache(dir, fe.ID(), fe.Dim()); len(reloaded.Items) != 0 {
		t.Fatalf("a skipped build must not write the cache, got %d entries", len(reloaded.Items))
	}
}

// The whole point: two instances warming the same corpus embed each doc once
// between them. The lock holder blocks mid-embed so the second instance is
// provably inside its attempt while the first is still working.
func TestBuildCorpusCachedConcurrentBuildsEmbedEachDocOnce(t *testing.T) {
	shortenCorpusLockWait(t, 200*time.Millisecond)

	dir := t.TempDir()
	docs := corpusDocsFixture()

	// Instance A: holds the lock, then blocks inside the embed.
	aEmb := newCountingEmbedder(64)
	aEmb.block = make(chan struct{})
	aEmb.entered = make(chan struct{})
	aCache, err := LoadCache(dir, aEmb.ID(), aEmb.Dim())
	if err != nil {
		t.Fatal(err)
	}

	aDone := make(chan error, 1)
	go func() {
		_, _, err := BuildCorpusCached(context.Background(), aEmb, docs, aCache)
		aDone <- err
	}()
	<-aEmb.entered // A is mid-embed, so it definitely holds the lock

	// Instance B, same cache dir, while A is still working.
	bEmb := newCountingEmbedder(64)
	bCache, err := LoadCache(dir, bEmb.ID(), bEmb.Dim())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildCorpusCached(context.Background(), bEmb, docs, bCache); err == nil || !isCorpusLocked(err) {
		t.Fatalf("B must skip while A holds the lock, got err=%v", err)
	}
	if got := bEmb.calls.Load(); got != 0 {
		t.Fatalf("B must not embed while A holds the lock, got %d calls", got)
	}

	close(aEmb.block)
	if err := <-aDone; err != nil {
		t.Fatalf("A's build failed: %v", err)
	}
	if got := aEmb.texts.Load(); got != int64(len(docs)) {
		t.Fatalf("A should embed exactly %d texts, embedded %d", len(docs), got)
	}

	// A third instance, now that the cache is populated, embeds nothing — and
	// needs no lock, since there are no misses.
	cEmb := newCountingEmbedder(64)
	cCache, err := LoadCache(dir, cEmb.ID(), cEmb.Dim())
	if err != nil {
		t.Fatal(err)
	}
	_, misses, err := BuildCorpusCached(context.Background(), cEmb, docs, cCache)
	if err != nil {
		t.Fatal(err)
	}
	if misses != 0 || cEmb.calls.Load() != 0 {
		t.Fatalf("a warm cache must embed nothing, got %d misses / %d calls", misses, cEmb.calls.Load())
	}
}

// A caller with a short deadline (the per-turn Warm budget) must not be made to
// wait out the full acquire budget: the lock wait is clamped to the deadline, so
// a contended turn is bounded by the caller's own budget.
func TestCorpusLockWaitNeverExceedsTheCallersDeadline(t *testing.T) {
	prev := corpusLockWait()
	corpusLockTimeout.Store(int64(30 * time.Second))
	t.Cleanup(func() { corpusLockTimeout.Store(int64(prev)) })

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	if got := corpusLockWaitFor(ctx); got > 250*time.Millisecond {
		t.Fatalf("lock wait %s must not exceed the caller's 250ms deadline", got)
	}
	// A background warm (generous deadline) still gets the full budget.
	if got := corpusLockWaitFor(context.Background()); got < 5*time.Second {
		t.Fatalf("an unbounded-deadline caller must get the full budget, got %s", got)
	}
	// An already-exhausted budget must still yield a small POSITIVE wait.
	// filelock treats a non-positive timeout as "use the 10s package default", so
	// returning zero here would turn an exhausted budget into a ten-second stall —
	// the exact opposite of the clamp.
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if got := corpusLockWaitFor(expired); got <= 0 {
		t.Fatalf("lock wait for an expired caller must stay positive, got %s", got)
	} else if got > time.Second {
		t.Fatalf("lock wait for an expired caller must be tiny, got %s", got)
	}
}

// The lock file lives beside the cache, which does not exist on a first run.
// filelock opens with O_CREATE, which creates the lock FILE but not its parent
// directory — so without this every first-run lock fails and the corpus is
// silently rebuilt (re-embedded) by every instance.
func TestCorpusLockWorksOnAFreshCacheDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not", "created", "yet")
	fe := newCountingEmbedder(64)
	c, err := LoadCache(dir, fe.ID(), fe.Dim())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := BuildCorpusCached(context.Background(), fe, corpusDocsFixture(), c); err != nil {
		t.Fatalf("first run must build despite a missing cache dir: %v", err)
	}
}
