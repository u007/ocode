package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireLocalModelSlotLimitsConcurrency(t *testing.T) {
	dir := t.TempDir()
	const maxParallel = 2
	const workers = 5

	var inFlight int32
	var maxObserved int32
	done := make(chan struct{}, workers)

	for i := 0; i < workers; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release, err := acquireLocalModelSlot(ctx, "local/test-model", maxParallel, dir)
			if err != nil {
				t.Errorf("acquireLocalModelSlot: %v", err)
				done <- struct{}{}
				return
			}
			n := atomic.AddInt32(&inFlight, 1)
			for {
				cur := atomic.LoadInt32(&maxObserved)
				if n <= cur || atomic.CompareAndSwapInt32(&maxObserved, cur, n) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			release()
			done <- struct{}{}
		}()
	}

	for i := 0; i < workers; i++ {
		<-done
	}

	if got := atomic.LoadInt32(&maxObserved); got > maxParallel {
		t.Fatalf("observed %d concurrent holders, want <= %d", got, maxParallel)
	}
}

func TestAcquireLocalModelSlotReleaseIsIdempotentAndFreesSlot(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	release1, err := acquireLocalModelSlot(ctx, "local/test-model", 1, dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// A second acquire with the single slot held must block until released.
	acquired := make(chan struct{})
	go func() {
		release2, err := acquireLocalModelSlot(context.Background(), "local/test-model", 1, dir)
		if err != nil {
			t.Errorf("second acquire: %v", err)
			return
		}
		close(acquired)
		release2()
	}()

	select {
	case <-acquired:
		t.Fatal("second acquire succeeded while slot was still held")
	case <-time.After(200 * time.Millisecond):
	}

	release1()
	release1() // idempotent: must not panic or double-free

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire did not proceed after release")
	}
}

func TestAcquireLocalModelSlotRespectsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireLocalModelSlot(context.Background(), "local/test-model", 1, dir)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := acquireLocalModelSlot(ctx, "local/test-model", 1, dir); err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
}
