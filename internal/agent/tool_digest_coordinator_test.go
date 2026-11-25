package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// digestDigestOf returns a ToolDigest whose summary names the run, so a test
// can tell which run's result a caller received.
func digestDigestOf(name string) ToolDigest {
	return ToolDigest{Summary: name, Model: "testprov/coord"}
}

func TestToolDigestCoordinator_DifferentAsksIndependent(t *testing.T) {
	c := newToolDigestCoordinator(time.Now)
	started := make(chan string, 2)
	release := make(chan struct{})
	fn := func(name string) func(context.Context) (ToolDigest, error) {
		return func(ctx context.Context) (ToolDigest, error) {
			started <- name
			<-release
			if err := ctx.Err(); err != nil {
				return ToolDigest{}, fmt.Errorf("%s was cancelled: %w", name, err)
			}
			return digestDigestOf(name), nil
		}
	}

	type outcome struct {
		d   ToolDigest
		err error
	}
	results := make(chan outcome, 2)
	for _, key := range []string{"ask-a", "ask-b"} {
		go func(k string) {
			d, err := c.run(withToolDigestOwner(context.Background(), "owner-1"), k, k, fn(k))
			results <- outcome{d, err}
		}(key)
	}
	<-started
	<-started
	close(release)
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("a different ask was cancelled: %v", r.err)
		}
	}
}

func TestToolDigestCoordinator_SameKeySuperseded(t *testing.T) {
	c := newToolDigestCoordinator(time.Now)
	ctx := withToolDigestOwner(context.Background(), "owner-1")
	firstStarted := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := c.run(ctx, "same", "same", func(runCtx context.Context) (ToolDigest, error) {
			close(firstStarted)
			<-runCtx.Done()
			return ToolDigest{}, runCtx.Err()
		})
		firstDone <- err
	}()
	<-firstStarted

	second, err := c.run(ctx, "same", "same", func(context.Context) (ToolDigest, error) {
		return digestDigestOf("second"), nil
	})
	if err != nil || second.Summary != "second" {
		t.Fatalf("second run = (%+v, %v), want the replacement's result", second, err)
	}
	select {
	case err := <-firstDone:
		if !errors.Is(err, ErrToolDigestSuperseded) {
			t.Fatalf("superseded run err = %v, want ErrToolDigestSuperseded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("superseded run did not return")
	}
}

func TestToolDigestCoordinator_HashKeyIdentity(t *testing.T) {
	mask := func(s string) string { return s }
	mkPayload := func(secret string) *toolDigestPayload {
		t.Helper()
		p, err := buildToolDigestPayload(ToolCallDigestInput{Tool: "bash", Command: "echo " + secret}, func(s string) string {
			return mask(strings.ReplaceAll(s, secret, "[redacted:openai]"))
		})
		if err != nil {
			t.Fatalf("buildToolDigestPayload: %v", err)
		}
		return p
	}
	a := mkPayload("sk-AAAA")
	b := mkPayload("sk-BBBB")
	if a.sha256 != b.sha256 {
		t.Fatal("masked-identical inputs must hash identically")
	}
	if toolDigestKey("owner", "", a.sha256) != toolDigestKey("owner", "", b.sha256) {
		t.Fatal("masked-identical inputs must share a fallback key")
	}
	changed, err := buildToolDigestPayload(ToolCallDigestInput{Tool: "bash", Command: "echo other"}, mask)
	if err != nil {
		t.Fatalf("buildToolDigestPayload: %v", err)
	}
	if toolDigestKey("owner", "", a.sha256) == toolDigestKey("owner", "", changed.sha256) {
		t.Fatal("a changed field must produce a different key")
	}
	if toolDigestKey("owner-a", "", a.sha256) == toolDigestKey("owner-b", "", a.sha256) {
		t.Fatal("the owner must be part of the key")
	}
	if toolDigestKey("owner", "req-1", a.sha256) == toolDigestKey("owner", "", a.sha256) {
		t.Fatal("a request id must key differently from the payload hash")
	}
}

// TestToolDigestCoordinator_ReusedRequestIDDoesNotShareCache: a provider that
// reuses a tool-call id across turns must not get another call's explanation
// from the cache. The cache keys on the payload, so each payload runs once.
func TestToolDigestCoordinator_ReusedRequestIDDoesNotShareCache(t *testing.T) {
	c := newToolDigestCoordinator(time.Now)
	ctx := withToolDigestOwner(context.Background(), "owner")
	var calls atomic.Int32
	run := func(cacheKey, text string) ToolDigest {
		t.Helper()
		d, err := c.run(ctx, "call_0", cacheKey, func(context.Context) (ToolDigest, error) {
			calls.Add(1)
			return digestDigestOf(text), nil
		})
		if err != nil {
			t.Fatalf("run(%s): %v", cacheKey, err)
		}
		return d
	}
	if d := run("payload-a", "Deletes the build directory"); d.Summary != "Deletes the build directory" {
		t.Fatalf("first run summary = %q", d.Summary)
	}
	if d := run("payload-b", "Reads the home config"); d.Summary != "Reads the home config" {
		t.Fatalf("a different payload with a reused id got %q, want its own explanation", d.Summary)
	}
	if d := run("payload-a", "must not run"); d.Summary != "Deletes the build directory" {
		t.Fatalf("repeat of payload-a got %q, want the cached explanation", d.Summary)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("fn ran %d times, want 2 (one per distinct payload)", n)
	}
}

func TestToolDigestCoordinator_CacheHitExpiryLRU(t *testing.T) {
	var clockMu sync.Mutex
	clock := time.Unix(1_700_000_000, 0)
	now := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		defer clockMu.Unlock()
		clock = clock.Add(d)
	}
	c := newToolDigestCoordinator(now)
	ctx := withToolDigestOwner(context.Background(), "owner-1")

	var calls atomic.Int32
	runKey := func(key string) (ToolDigest, error) {
		return c.run(ctx, key, key, func(context.Context) (ToolDigest, error) {
			calls.Add(1)
			return digestDigestOf(key), nil
		})
	}

	// Hit: no fn call on the second run.
	if _, err := runKey("k"); err != nil {
		t.Fatal(err)
	}
	if _, err := runKey("k"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("fn calls = %d after a cache hit, want 1", calls.Load())
	}

	// Expiry: after the TTL the same key runs again.
	advance(toolDigestCacheTTL + time.Second)
	if _, err := runKey("k"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("fn calls = %d after expiry, want 2", calls.Load())
	}

	// LRU: with the cache full, a touched entry survives and the least recently
	// used one is evicted.
	calls.Store(0)
	c2 := newToolDigestCoordinator(now)
	runKey2 := func(key string) {
		t.Helper()
		if _, err := c2.run(ctx, key, key, func(context.Context) (ToolDigest, error) {
			calls.Add(1)
			return digestDigestOf(key), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < toolDigestCacheEntries; i++ {
		runKey2(fmt.Sprintf("e%03d", i))
	}
	runKey2("e000") // touch the oldest entry so it becomes most recent
	calls.Store(0)
	runKey2("overflow") // evicts the least recently used: e001
	if calls.Load() != 1 {
		t.Fatalf("overflow insert ran fn %d times, want 1", calls.Load())
	}
	calls.Store(0)
	runKey2("e000")
	if calls.Load() != 0 {
		t.Fatal("the touched entry was evicted; LRU order is wrong")
	}
	runKey2("e001")
	if calls.Load() != 1 {
		t.Fatal("the least recently used entry should have been evicted")
	}
}

func TestToolDigestCoordinator_Invalidation(t *testing.T) {
	c := newToolDigestCoordinator(time.Now)
	ownerA := withToolDigestOwner(context.Background(), "owner-A")
	ownerB := withToolDigestOwner(context.Background(), "owner-B")

	var aCalls, bCalls atomic.Int32
	runA := func(key string) (ToolDigest, error) {
		return c.run(ownerA, key, key, func(context.Context) (ToolDigest, error) {
			aCalls.Add(1)
			return digestDigestOf("A"), nil
		})
	}
	runB := func(key string) (ToolDigest, error) {
		return c.run(ownerB, key, key, func(context.Context) (ToolDigest, error) {
			bCalls.Add(1)
			return digestDigestOf("B"), nil
		})
	}
	if _, err := runA("cached-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := runB("cached-b"); err != nil {
		t.Fatal(err)
	}

	// An active run for owner A, blocked until its context is cancelled.
	activeStarted := make(chan struct{})
	activeDone := make(chan error, 1)
	go func() {
		_, err := c.run(ownerA, "active-a", "active-a", func(runCtx context.Context) (ToolDigest, error) {
			close(activeStarted)
			<-runCtx.Done()
			return ToolDigest{}, runCtx.Err()
		})
		activeDone <- err
	}()
	<-activeStarted

	// A B-owned active run that must survive the invalidation of A.
	bStarted := make(chan struct{})
	bRelease := make(chan struct{})
	bDone := make(chan ToolDigest, 1)
	go func() {
		d, _ := c.run(ownerB, "active-b", "active-b", func(context.Context) (ToolDigest, error) {
			close(bStarted)
			<-bRelease
			return digestDigestOf("B-active"), nil
		})
		bDone <- d
	}()
	<-bStarted

	c.invalidate("owner-A")

	select {
	case err := <-activeDone:
		if !errors.Is(err, ErrToolDigestSuperseded) {
			t.Fatalf("invalidated active run err = %v, want ErrToolDigestSuperseded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("invalidation did not cancel owner A's active run")
	}

	if _, err := runA("cached-a"); err != nil {
		t.Fatal(err)
	}
	if aCalls.Load() != 2 {
		t.Fatalf("owner A's cached entry survived invalidation (fn calls = %d)", aCalls.Load())
	}
	if _, err := runB("cached-b"); err != nil {
		t.Fatal(err)
	}
	if bCalls.Load() != 1 {
		t.Fatalf("owner B's cached entry was removed by A's invalidation (fn calls = %d)", bCalls.Load())
	}
	close(bRelease)
	if d := <-bDone; d.Summary != "B-active" {
		t.Fatalf("owner B's active run was disturbed: %+v", d)
	}
}

func TestToolDigestCoordinator_SemaphoreThreeAndDeadline(t *testing.T) {
	prevDeadline := toolDigestRunTimeout
	prevMax := toolDigestMaxConcurrent
	toolDigestRunTimeout = 150 * time.Millisecond
	toolDigestMaxConcurrent = 3
	t.Cleanup(func() {
		toolDigestRunTimeout = prevDeadline
		toolDigestMaxConcurrent = prevMax
	})
	c := newToolDigestCoordinator(time.Now)

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var fourthRan atomic.Bool
	holding := make(chan struct{}, 3)
	errs := make(chan error, 4)
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("hold-%d", i)
		go func() {
			_, err := c.run(context.Background(), key, key, func(context.Context) (ToolDigest, error) {
				holding <- struct{}{}
				<-release
				return digestDigestOf("held"), nil
			})
			errs <- err
		}()
	}
	for i := 0; i < 3; i++ {
		<-holding
	}

	start := time.Now()
	_, err := c.run(context.Background(), "fourth", "fourth", func(context.Context) (ToolDigest, error) {
		fourthRan.Store(true)
		return digestDigestOf("fourth"), nil
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("the fourth run was admitted past a semaphore of three")
	}
	if fourthRan.Load() {
		t.Fatal("the fourth run's fn started while three slots were held")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("fourth run err = %v, want the run deadline to end its queue wait", err)
	}
	if elapsed < toolDigestRunTimeout/2 || elapsed > 5*time.Second {
		t.Fatalf("queue wait lasted %s; want it bounded by the %s run deadline", elapsed, toolDigestRunTimeout)
	}
}
