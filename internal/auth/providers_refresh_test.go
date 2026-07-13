package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRefreshLocked_SerializesConcurrentRefreshes verifies that concurrent
// RefreshLocked calls for the same provider serialize on the per-provider mutex
// and that only one refresh actually runs; the rest reuse the first result via
// the post-lock re-check. This is the regression guard for the grok refresh
// hook, which previously dropped the mutex and re-check.
func TestRefreshLocked_SerializesConcurrentRefreshes(t *testing.T) {
	resetStoreForTest(t)

	const id = "grok-concurrent-refresh"
	seed := Credential{
		Kind:            KindOAuth,
		AccessToken:     "old-token",
		ExpiresAt:       time.Now().Add(-time.Minute).Unix(), // already expired
		CookieAuthToken: "at",
		CookieCt0:       "ct",
	}
	if err := Set(id, seed); err != nil {
		t.Fatalf("seed Set: %v", err)
	}
	t.Cleanup(func() { _ = Remove(id) })

	var live int32 // goroutines currently inside refresh()
	var maxLive int32
	var calls int32 // total refresh() invocations
	refresh := func() (Credential, error) {
		n := atomic.AddInt32(&live, 1)
		for {
			m := atomic.LoadInt32(&maxLive)
			if n <= m || atomic.CompareAndSwapInt32(&maxLive, m, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond) // hold the lock window open for contention
		atomic.AddInt32(&live, -1)
		atomic.AddInt32(&calls, 1)
		return Credential{
			Kind:        KindOAuth,
			AccessToken: "new-token",
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
		}, nil
	}

	const N = 25
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, handled := RefreshLocked(id, seed, refresh)
			if !handled {
				t.Errorf("RefreshLocked returned handled=false")
			}
			if out.AccessToken != "new-token" {
				t.Errorf("RefreshLocked returned AccessToken=%q, want new-token", out.AccessToken)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxLive); got != 1 {
		t.Errorf("RefreshLocked allowed %d concurrent refreshes; want 1 (serialized)", got)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("RefreshLocked ran refresh %d times; want 1 (others reused the result)", got)
	}
}
